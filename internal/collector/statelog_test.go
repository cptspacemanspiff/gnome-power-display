package collector

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestReconstructEvents(t *testing.T) {
	nowUnix := int64(200)

	tests := []struct {
		name    string
		entries []stateLogEntry
		want    []PowerStateEvent
	}{
		{
			name: "suspend pre post",
			entries: []stateLogEntry{
				{Ts: 100, Action: "pre", What: "suspend", SleepAction: "suspend"},
				{Ts: 120, Action: "post", What: "suspend", SleepAction: "suspend"},
			},
			want: []PowerStateEvent{{StartTime: 100, EndTime: 120, Type: "suspend", SuspendSecs: 20}},
		},
		{
			name: "hibernate pre post with fallback type",
			entries: []stateLogEntry{
				{Ts: 100, Action: "pre", What: "hibernate"},
				{Ts: 130, Action: "post", What: "hibernate"},
			},
			want: []PowerStateEvent{{StartTime: 100, EndTime: 130, Type: "hibernate", HibernateSecs: 30}},
		},
		{
			name: "shutdown pre only",
			entries: []stateLogEntry{
				{Ts: 100, Action: "pre", What: "shutdown"},
			},
			want: []PowerStateEvent{{StartTime: 100, EndTime: nowUnix, Type: "shutdown"}},
		},
		{
			name: "orphaned post skipped",
			entries: []stateLogEntry{
				{Ts: 95, Action: "post", What: "suspend", SleepAction: "suspend"},
				{Ts: 100, Action: "pre", What: "suspend", SleepAction: "suspend"},
				{Ts: 140, Action: "post", What: "suspend", SleepAction: "suspend"},
			},
			want: []PowerStateEvent{{StartTime: 100, EndTime: 140, Type: "suspend", SuspendSecs: 40}},
		},
		{
			name: "orphaned pre uses now",
			entries: []stateLogEntry{
				{Ts: 150, Action: "pre", What: "suspend", SleepAction: "suspend"},
			},
			want: []PowerStateEvent{{StartTime: 150, EndTime: nowUnix, Type: "suspend", SuspendSecs: 50}},
		},
		{
			name: "suspend then hibernate full sequence",
			entries: []stateLogEntry{
				{Ts: 100, Action: "pre", What: "suspend-then-hibernate", SleepAction: "suspend"},
				{Ts: 120, Action: "post", What: "suspend-then-hibernate", SleepAction: "suspend"},
				{Ts: 125, Action: "pre", What: "suspend-then-hibernate", SleepAction: "hibernate"},
				{Ts: 170, Action: "post", What: "suspend-then-hibernate", SleepAction: "hibernate"},
			},
			want: []PowerStateEvent{{StartTime: 100, EndTime: 170, Type: "suspend-then-hibernate", SuspendSecs: 20, HibernateSecs: 45}},
		},
		{
			name: "suspend then hibernate partial no post hibernate",
			entries: []stateLogEntry{
				{Ts: 100, Action: "pre", What: "suspend-then-hibernate", SleepAction: "suspend"},
				{Ts: 120, Action: "post", What: "suspend-then-hibernate", SleepAction: "suspend"},
				{Ts: 125, Action: "pre", What: "suspend-then-hibernate", SleepAction: "hibernate"},
			},
			want: []PowerStateEvent{{StartTime: 100, EndTime: nowUnix, Type: "suspend-then-hibernate", SuspendSecs: 20, HibernateSecs: nowUnix - 125}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconstructEvents(tt.entries, nowUnix)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("reconstructEvents() mismatch\n got: %#v\nwant: %#v", got, tt.want)
			}
		})
	}
}

func TestReconstructSuspendThenHibernate(t *testing.T) {
	nowUnix := int64(300)

	tests := []struct {
		name         string
		entries      []stateLogEntry
		wantEvent    PowerStateEvent
		wantConsumed int
	}{
		{
			name: "full sequence",
			entries: []stateLogEntry{
				{Ts: 100, Action: "pre", What: "suspend-then-hibernate", SleepAction: "suspend"},
				{Ts: 130, Action: "post", SleepAction: "suspend"},
				{Ts: 140, Action: "pre", SleepAction: "hibernate"},
				{Ts: 200, Action: "post", SleepAction: "hibernate"},
			},
			wantEvent:    PowerStateEvent{StartTime: 100, EndTime: 200, Type: "suspend-then-hibernate", SuspendSecs: 30, HibernateSecs: 60},
			wantConsumed: 4,
		},
		{
			name: "partial no post hibernate",
			entries: []stateLogEntry{
				{Ts: 100, Action: "pre", What: "suspend-then-hibernate", SleepAction: "suspend"},
				{Ts: 130, Action: "post", SleepAction: "suspend"},
				{Ts: 140, Action: "pre", SleepAction: "hibernate"},
			},
			wantEvent:    PowerStateEvent{StartTime: 100, EndTime: nowUnix, Type: "suspend-then-hibernate", SuspendSecs: 30, HibernateSecs: nowUnix - 140},
			wantConsumed: 3,
		},
		{
			name: "suspend only user wakes before hibernate",
			entries: []stateLogEntry{
				{Ts: 100, Action: "pre", What: "suspend-then-hibernate", SleepAction: "suspend"},
				{Ts: 130, Action: "post", SleepAction: "suspend"},
				{Ts: 135, Action: "post", SleepAction: "other"},
			},
			wantEvent:    PowerStateEvent{StartTime: 100, EndTime: 130, Type: "suspend", SuspendSecs: 30},
			wantConsumed: 2,
		},
		{
			name: "orphaned pre",
			entries: []stateLogEntry{
				{Ts: 100, Action: "pre", What: "suspend-then-hibernate", SleepAction: "suspend"},
			},
			wantEvent:    PowerStateEvent{StartTime: 100, EndTime: nowUnix, Type: "suspend", SuspendSecs: nowUnix - 100},
			wantConsumed: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEvent, gotConsumed := reconstructSuspendThenHibernate(tt.entries, nowUnix)
			if !reflect.DeepEqual(gotEvent, tt.wantEvent) {
				t.Fatalf("event mismatch\n got: %#v\nwant: %#v", gotEvent, tt.wantEvent)
			}
			if gotConsumed != tt.wantConsumed {
				t.Fatalf("consumed = %d, want %d", gotConsumed, tt.wantConsumed)
			}
		})
	}
}

func TestReadAndConsumeStateLog(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("missing file returns nil", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state-log.jsonl")
		got := ReadAndConsumeStateLog(logger, time.Unix(200, 0), path)
		if len(got) != 0 {
			t.Fatalf("len(events) = %d, want 0", len(got))
		}
	})

	t.Run("malformed lines are skipped and file consumed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "state-log.jsonl")
		content := "{not json}\n" +
			`{"ts":100,"action":"pre","what":"suspend","sleep_action":"suspend"}` + "\n" +
			`{"ts":140,"action":"post","what":"suspend","sleep_action":"suspend"}` + "\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write state log: %v", err)
		}

		got := ReadAndConsumeStateLog(logger, time.Unix(200, 0), path)
		want := []PowerStateEvent{{StartTime: 100, EndTime: 140, Type: "suspend", SuspendSecs: 40}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ReadAndConsumeStateLog() mismatch\n got: %#v\nwant: %#v", got, want)
		}

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("state log file should be consumed, stat err = %v", err)
		}
		processing := path + ".processing"
		if _, err := os.Stat(processing); !os.IsNotExist(err) {
			t.Fatalf("processing file should be removed, stat err = %v", err)
		}
	})
}
