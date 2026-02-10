package collector

import (
	"log"
	"time"

	"github.com/godbus/dbus/v5"
)

// SleepMonitor listens for systemd-logind PrepareForSleep/PrepareForShutdown signals.
type SleepMonitor struct {
	conn       *dbus.Conn
	events     chan SleepEvent
	sleepTime  time.Time
	sleepType  string // "suspend", "hibernate", or "unknown"
	hibernating bool  // true if PrepareForShutdown fired (hibernate)
	done       chan struct{}
}

// NewSleepMonitor creates a new sleep monitor connected to the system bus.
func NewSleepMonitor() (*SleepMonitor, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}

	// Listen for both sleep and shutdown signals
	for _, member := range []string{"PrepareForSleep", "PrepareForShutdown"} {
		err = conn.AddMatchSignal(
			dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
			dbus.WithMatchMember(member),
		)
		if err != nil {
			return nil, err
		}
	}

	m := &SleepMonitor{
		conn:   conn,
		events: make(chan SleepEvent, 16),
		done:   make(chan struct{}),
	}
	go m.listen()
	return m, nil
}

// Events returns a channel of sleep events.
func (m *SleepMonitor) Events() <-chan SleepEvent {
	return m.events
}

// Close stops the monitor.
func (m *SleepMonitor) Close() {
	close(m.done)
}

func (m *SleepMonitor) listen() {
	ch := make(chan *dbus.Signal, 16)
	m.conn.Signal(ch)
	defer m.conn.RemoveSignal(ch)

	for {
		select {
		case sig := <-ch:
			if len(sig.Body) < 1 {
				continue
			}
			active, ok := sig.Body[0].(bool)
			if !ok {
				continue
			}

			switch sig.Name {
			case "org.freedesktop.login1.Manager.PrepareForShutdown":
				// PrepareForShutdown(true) fires before hibernate (and poweroff,
				// but we won't see the false signal after poweroff).
				if active {
					m.hibernating = true
				}

			case "org.freedesktop.login1.Manager.PrepareForSleep":
				if active {
					if m.hibernating {
						m.sleepType = "hibernate"
					} else {
						m.sleepType = "suspend"
					}
					m.sleepTime = time.Now().Round(0) // Strip monotonic so Sub uses wall clock across suspend
					log.Printf("system going to %s", m.sleepType)
				} else {
					wakeTime := time.Now()
					if !m.sleepTime.IsZero() {
						m.events <- SleepEvent{
							SleepTime: m.sleepTime.Unix(),
							WakeTime:  wakeTime.Unix(),
							Type:      m.sleepType,
						}
						log.Printf("woke up after %v (%s)", wakeTime.Sub(m.sleepTime), m.sleepType)
					}
					m.hibernating = false
					m.sleepType = "unknown"
				}
			}
		case <-m.done:
			return
		}
	}
}
