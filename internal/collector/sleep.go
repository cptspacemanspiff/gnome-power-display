package collector

import (
	"log/slog"

	"github.com/godbus/dbus/v5"
)

// SleepMonitor listens for systemd-logind PrepareForSleep/PrepareForShutdown signals.
// The file-based state log is the authoritative source of power state events, but this
// monitor provides a wake notification channel so the daemon can re-read the state log
// immediately on wake (catching short sleeps that don't trigger a wall-clock jump).
type SleepMonitor struct {
	conn *dbus.Conn
	done chan struct{}
	wake chan struct{}
	log  *slog.Logger
}

// NewSleepMonitor creates a new sleep monitor connected to the system bus.
func NewSleepMonitor(logger *slog.Logger) (*SleepMonitor, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}

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
		conn: conn,
		done: make(chan struct{}),
		wake: make(chan struct{}, 1),
		log:  logger,
	}
	go m.listen()
	return m, nil
}

// Wake returns a channel that receives a value each time the system wakes from sleep.
func (m *SleepMonitor) Wake() <-chan struct{} {
	return m.wake
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
				if active {
					m.log.Info("system preparing for shutdown/hibernate")
				}
			case "org.freedesktop.login1.Manager.PrepareForSleep":
				if active {
					m.log.Info("system going to sleep")
				} else {
					m.log.Info("system woke up")
					select {
					case m.wake <- struct{}{}:
					default:
					}
				}
			}
		case <-m.done:
			return
		}
	}
}
