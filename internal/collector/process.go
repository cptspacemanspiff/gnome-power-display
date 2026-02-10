package collector

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ProcessCollector tracks per-process CPU tick deltas across sampling intervals.
type ProcessCollector struct {
	prevTicks   map[int]int64  // pid -> previous utime+stime
	cmdlineCache map[int]string // pid -> cmdline (read once per pid lifetime)
	cpuTopology map[int]bool   // cpu_id -> is_p_core (computed once at init)
	topN        int
}

// NewProcessCollector creates a ProcessCollector, detecting CPU topology once.
func NewProcessCollector(topN int) *ProcessCollector {
	if topN <= 0 {
		topN = 10
	}
	pc := &ProcessCollector{
		prevTicks:    make(map[int]int64),
		cmdlineCache: make(map[int]string),
		cpuTopology:  make(map[int]bool),
		topN:         topN,
	}
	pc.detectTopology()
	return pc
}

// IsPCore returns whether the given CPU ID is a P-core.
func (pc *ProcessCollector) IsPCore(cpuID int) bool {
	return pc.cpuTopology[cpuID]
}

// CPUIDs returns all known CPU IDs and whether each is a P-core.
func (pc *ProcessCollector) CPUIDs() map[int]bool {
	return pc.cpuTopology
}

// detectTopology determines P-core vs E-core for each CPU.
// On hybrid Intel, E-cores have a lower base frequency than P-cores.
// On non-hybrid systems, all cores are marked as P-cores.
func (pc *ProcessCollector) detectTopology() {
	cpuDirs, err := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*")
	if err != nil {
		return
	}

	type cpuInfo struct {
		id   int
		base int64
	}
	var cpus []cpuInfo

	for _, dir := range cpuDirs {
		name := filepath.Base(dir)
		id, err := strconv.Atoi(name[3:])
		if err != nil {
			continue
		}
		// Try base_frequency first (Intel), fall back to cpuinfo_max_freq
		base, _ := readIntFile(filepath.Join(dir, "cpufreq", "base_frequency"))
		if base == 0 {
			base, _ = readIntFile(filepath.Join(dir, "cpufreq", "cpuinfo_max_freq"))
		}
		cpus = append(cpus, cpuInfo{id: id, base: base})
	}

	if len(cpus) == 0 {
		return
	}

	// Find max base frequency — cores at max are P-cores
	var maxBase int64
	for _, c := range cpus {
		if c.base > maxBase {
			maxBase = c.base
		}
	}

	for _, c := range cpus {
		pc.cpuTopology[c.id] = (c.base == maxBase)
	}
}

// ProcessCollectStats holds summary statistics from a process collection cycle.
type ProcessCollectStats struct {
	TotalProcs    int              // number of processes with nonzero delta
	TotalTicks    int64            // sum of all process tick deltas
	CapturedTicks int64            // sum of tick deltas for top N kept
	PerCoreTicks  map[int]int64    // cpu_id -> total ticks on that core (all procs)
}

type procEntry struct {
	pid    int
	comm   string
	ticks  int64 // utime + stime
	cpu    int
}

// Collect reads /proc/*/stat, computes tick deltas from the previous call,
// and returns the top N processes by CPU usage, current CPU frequencies, and
// summary statistics for logging.
func (pc *ProcessCollector) Collect() ([]ProcessSample, []CPUFreqSample, *ProcessCollectStats, error) {
	now := time.Now().Unix()

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read /proc: %w", err)
	}

	currentTicks := make(map[int]int64, len(entries))
	var procs []procEntry
	perCoreTicks := make(map[int]int64)
	var totalTicks int64

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		pe, err := readProcStat(pid)
		if err != nil {
			continue
		}
		currentTicks[pid] = pe.ticks

		prev, ok := pc.prevTicks[pid]
		if !ok {
			continue // first observation, no delta
		}
		delta := pe.ticks - prev
		if delta <= 0 {
			continue
		}
		totalTicks += delta
		perCoreTicks[pe.cpu] += delta
		procs = append(procs, procEntry{pid: pid, comm: pe.comm, ticks: delta, cpu: pe.cpu})
	}

	// Sort by delta descending, keep top N
	sort.Slice(procs, func(i, j int) bool {
		return procs[i].ticks > procs[j].ticks
	})
	if len(procs) > pc.topN {
		procs = procs[:pc.topN]
	}

	// Build process samples and sum captured ticks
	var capturedTicks int64
	samples := make([]ProcessSample, len(procs))
	for i, p := range procs {
		capturedTicks += p.ticks
		cmdline, ok := pc.cmdlineCache[p.pid]
		if !ok {
			cmdline = readCmdline(p.pid)
			pc.cmdlineCache[p.pid] = cmdline
		}
		samples[i] = ProcessSample{
			Timestamp:     now,
			PID:           p.pid,
			Comm:          p.comm,
			Cmdline:       cmdline,
			CPUTicksDelta: p.ticks,
			LastCPU:       p.cpu,
		}
	}

	stats := &ProcessCollectStats{
		TotalProcs:    len(procs),
		TotalTicks:    totalTicks,
		CapturedTicks: capturedTicks,
		PerCoreTicks:  perCoreTicks,
	}

	// Update state: replace prevTicks, prune dead pids from cmdline cache
	pc.prevTicks = currentTicks
	for pid := range pc.cmdlineCache {
		if _, alive := currentTicks[pid]; !alive {
			delete(pc.cmdlineCache, pid)
		}
	}

	// Collect CPU frequencies
	freqSamples := pc.collectFreqs(now)

	return samples, freqSamples, stats, nil
}

func (pc *ProcessCollector) collectFreqs(now int64) []CPUFreqSample {
	cpuDirs, err := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq")
	if err != nil {
		return nil
	}
	samples := make([]CPUFreqSample, 0, len(cpuDirs))
	for _, path := range cpuDirs {
		// path: /sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq
		parts := strings.Split(path, "/")
		// parts[5] = "cpu0"
		if len(parts) < 6 {
			continue
		}
		cpuName := parts[5]
		id, err := strconv.Atoi(cpuName[3:])
		if err != nil {
			continue
		}
		freq, _ := readIntFile(path)
		if freq == 0 {
			continue
		}
		samples = append(samples, CPUFreqSample{
			Timestamp: now,
			CPUID:     id,
			FreqKHz:   freq,
			IsPCore:   pc.cpuTopology[id],
		})
	}
	return samples
}

// readProcStat parses /proc/[pid]/stat for comm, utime, stime, and processor.
func readProcStat(pid int) (procEntry, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return procEntry{}, err
	}

	// comm is in parens and may contain spaces/parens, so find last ')'
	start := bytes.IndexByte(data, '(')
	end := bytes.LastIndexByte(data, ')')
	if start < 0 || end < 0 || end >= len(data)-1 {
		return procEntry{}, fmt.Errorf("malformed stat for pid %d", pid)
	}
	comm := string(data[start+1 : end])

	// Fields after ')' are space-separated, starting at index 2 (state)
	fields := strings.Fields(string(data[end+2:]))
	// utime = field index 13 (0-based from pid), but after comm it's index 11
	// stime = field index 14 -> index 12
	// processor = field index 38 -> index 36
	if len(fields) < 37 {
		return procEntry{}, fmt.Errorf("too few fields for pid %d", pid)
	}

	utime, _ := strconv.ParseInt(fields[11], 10, 64)
	stime, _ := strconv.ParseInt(fields[12], 10, 64)
	cpu, _ := strconv.Atoi(fields[36])

	return procEntry{
		pid:   pid,
		comm:  comm,
		ticks: utime + stime,
		cpu:   cpu,
	}, nil
}

// readCmdline reads /proc/[pid]/cmdline, replacing null bytes with spaces.
func readCmdline(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		return ""
	}
	// Replace null separators with spaces, trim trailing
	return strings.TrimRight(strings.ReplaceAll(string(data), "\x00", " "), " ")
}

