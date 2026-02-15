package collector

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"time"
)

// stateLogEntry is a single line from the state log file written by the systemd hooks.
type stateLogEntry struct {
	Ts          int64  `json:"ts"`
	Action      string `json:"action"`      // "pre" or "post"
	What        string `json:"what"`         // "suspend", "hibernate", "suspend-then-hibernate", "shutdown", etc.
	SleepAction string `json:"sleep_action"` // from SYSTEMD_SLEEP_ACTION env var
}

// ReadAndConsumeStateLog atomically reads the state log file and removes it,
// returning reconstructed PowerStateEvents. now is used as the end time for
// events that have no "post" entry (daemon started after hibernate/shutdown).
func ReadAndConsumeStateLog(logger *slog.Logger, now time.Time, stateLogPath string) []PowerStateEvent {
	processingPath := stateLogPath + ".processing"

	// Atomic rename so the hook creates a fresh file for new entries.
	if err := os.Rename(stateLogPath, processingPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		logger.Error("rename failed", "err", err)
		return nil
	}

	f, err := os.Open(processingPath)
	if err != nil {
		logger.Error("open processing file", "err", err)
		return nil
	}
	defer f.Close()
	defer os.Remove(processingPath)

	var entries []stateLogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e stateLogEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			logger.Warn("skip malformed line", "err", err)
			continue
		}
		entries = append(entries, e)
	}

	if len(entries) == 0 {
		return nil
	}

	return reconstructEvents(entries, now.Unix())
}

// reconstructEvents processes ordered state log entries into PowerStateEvents.
func reconstructEvents(entries []stateLogEntry, nowUnix int64) []PowerStateEvent {
	var events []PowerStateEvent
	i := 0
	for i < len(entries) {
		e := entries[i]

		if e.Action != "pre" {
			// Orphaned post without a pre — skip.
			i++
			continue
		}

		if e.What == "shutdown" {
			// Shutdown: never has a post. End time = now (next boot).
			events = append(events, PowerStateEvent{
				StartTime: e.Ts,
				EndTime:   nowUnix,
				Type:      "shutdown",
			})
			i++
			continue
		}

		if e.What == "suspend-then-hibernate" {
			event, consumed := reconstructSuspendThenHibernate(entries[i:], nowUnix)
			events = append(events, event)
			i += consumed
			continue
		}

		// Simple suspend or hibernate.
		sleepAction := e.SleepAction
		if sleepAction == "" {
			sleepAction = e.What
		}

		// Look for matching post.
		if i+1 < len(entries) && entries[i+1].Action == "post" {
			post := entries[i+1]
			evt := PowerStateEvent{
				StartTime: e.Ts,
				EndTime:   post.Ts,
				Type:      sleepAction,
			}
			duration := post.Ts - e.Ts
			if sleepAction == "hibernate" {
				evt.HibernateSecs = duration
			} else {
				evt.SuspendSecs = duration
			}
			events = append(events, evt)
			i += 2
		} else {
			// Orphaned pre with no post — use now as end time.
			evt := PowerStateEvent{
				StartTime: e.Ts,
				EndTime:   nowUnix,
				Type:      sleepAction,
			}
			duration := nowUnix - e.Ts
			if sleepAction == "hibernate" {
				evt.HibernateSecs = duration
			} else {
				evt.SuspendSecs = duration
			}
			events = append(events, evt)
			i++
		}
	}
	return events
}

// reconstructSuspendThenHibernate handles the suspend-then-hibernate sequence
// which can have 2 or 4 hook calls. Returns the event and number of entries consumed.
func reconstructSuspendThenHibernate(entries []stateLogEntry, nowUnix int64) (PowerStateEvent, int) {
	// entries[0] is the initial "pre" with what=suspend-then-hibernate.
	preTs := entries[0].Ts
	consumed := 1

	// Try to find the full sequence:
	// [0] pre  sleep_action=suspend
	// [1] post sleep_action=suspend
	// [2] pre  sleep_action=hibernate
	// [3] post sleep_action=hibernate

	// Check for post suspend (woke from suspend for hibernate transition).
	if consumed < len(entries) && entries[consumed].Action == "post" && entries[consumed].SleepAction == "suspend" {
		postSuspendTs := entries[consumed].Ts
		consumed++

		suspendSecs := postSuspendTs - preTs

		// Check for pre hibernate.
		if consumed < len(entries) && entries[consumed].Action == "pre" && entries[consumed].SleepAction == "hibernate" {
			preHibTs := entries[consumed].Ts
			consumed++

			// Check for post hibernate.
			if consumed < len(entries) && entries[consumed].Action == "post" && entries[consumed].SleepAction == "hibernate" {
				postHibTs := entries[consumed].Ts
				consumed++
				return PowerStateEvent{
					StartTime:     preTs,
					EndTime:       postHibTs,
					Type:          "suspend-then-hibernate",
					SuspendSecs:   suspendSecs,
					HibernateSecs: postHibTs - preHibTs,
				}, consumed
			}

			// No post hibernate — daemon reading on next boot.
			return PowerStateEvent{
				StartTime:     preTs,
				EndTime:       nowUnix,
				Type:          "suspend-then-hibernate",
				SuspendSecs:   suspendSecs,
				HibernateSecs: nowUnix - preHibTs,
			}, consumed
		}

		// Only suspend phase completed (user woke before hibernate timer).
		return PowerStateEvent{
			StartTime:   preTs,
			EndTime:     postSuspendTs,
			Type:        "suspend",
			SuspendSecs: suspendSecs,
		}, consumed
	}

	// Orphaned pre with no post at all.
	return PowerStateEvent{
		StartTime:   preTs,
		EndTime:     nowUnix,
		Type:        "suspend",
		SuspendSecs: nowUnix - preTs,
	}, consumed
}
