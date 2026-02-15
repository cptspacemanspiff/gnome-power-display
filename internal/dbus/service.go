package dbus

import (
	"encoding/json"
	"fmt"

	godbus "github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/cptspacemanspiff/gnome-power-display/internal/storage"
)

const (
	BusName   = "org.gnome.PowerMonitor"
	ObjPath   = "/org/gnome/PowerMonitor"
	IfaceName = "org.gnome.PowerMonitor"
)

const introspectXML = `
<node>
  <interface name="` + IfaceName + `">
    <method name="GetCurrentStats">
      <arg direction="out" type="s" name="json"/>
    </method>
    <method name="GetHistory">
      <arg direction="in" type="x" name="from_epoch"/>
      <arg direction="in" type="x" name="to_epoch"/>
      <arg direction="out" type="s" name="json"/>
    </method>
    <method name="GetSleepEvents">
      <arg direction="in" type="x" name="from_epoch"/>
      <arg direction="in" type="x" name="to_epoch"/>
      <arg direction="out" type="s" name="json"/>
    </method>
    <method name="GetProcessHistory">
      <arg direction="in" type="x" name="from_epoch"/>
      <arg direction="in" type="x" name="to_epoch"/>
      <arg direction="out" type="s" name="json"/>
    </method>
  </interface>
` + introspect.IntrospectDataString + `
</node>`

// Service exposes the power monitor over D-Bus.
type Service struct {
	store *storage.DB
}

// NewService creates a new D-Bus service.
func NewService(store *storage.DB) *Service {
	return &Service{store: store}
}

// Export registers the service on the system bus.
func (s *Service) Export() (*godbus.Conn, error) {
	conn, err := godbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}

	conn.Export(s, ObjPath, IfaceName)
	conn.Export(introspect.Introspectable(introspectXML), ObjPath, "org.freedesktop.DBus.Introspectable")

	reply, err := conn.RequestName(BusName, godbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("request name: %w", err)
	}
	if reply != godbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("name %s already taken", BusName)
	}

	return conn, nil
}

// GetCurrentStats returns the latest battery and backlight data as JSON.
func (s *Service) GetCurrentStats() (string, *godbus.Error) {
	bat, err := s.store.LatestBatterySample()
	if err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("query battery sample: %w", err))
	}
	bl, err := s.store.LatestBacklightSample()
	if err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("query backlight sample: %w", err))
	}
	result := map[string]any{"battery": bat, "backlight": bl}
	data, err := json.Marshal(result)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return string(data), nil
}

// GetHistory returns battery and backlight samples in a time range as JSON.
func (s *Service) GetHistory(fromEpoch, toEpoch int64) (string, *godbus.Error) {
	if fromEpoch < 0 || toEpoch < fromEpoch || (toEpoch-fromEpoch) > 86400*365 {
		return "", godbus.MakeFailedError(fmt.Errorf("invalid time range: from=%d to=%d", fromEpoch, toEpoch))
	}
	bat, err := s.store.BatterySamplesInRange(fromEpoch, toEpoch)
	if err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("query battery samples: %w", err))
	}
	bl, err := s.store.BacklightSamplesInRange(fromEpoch, toEpoch)
	if err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("query backlight samples: %w", err))
	}
	result := map[string]any{"battery": bat, "backlight": bl}
	data, err := json.Marshal(result)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return string(data), nil
}

// GetSleepEvents returns sleep events in a time range as JSON.
func (s *Service) GetSleepEvents(fromEpoch, toEpoch int64) (string, *godbus.Error) {
	if fromEpoch < 0 || toEpoch < fromEpoch || (toEpoch-fromEpoch) > 86400*365 {
		return "", godbus.MakeFailedError(fmt.Errorf("invalid time range: from=%d to=%d", fromEpoch, toEpoch))
	}
	events, err := s.store.SleepEventsInRange(fromEpoch, toEpoch)
	if err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("query sleep events: %w", err))
	}
	data, err := json.Marshal(events)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return string(data), nil
}

// GetProcessHistory returns process CPU usage and CPU frequency samples in a time range as JSON.
func (s *Service) GetProcessHistory(fromEpoch, toEpoch int64) (string, *godbus.Error) {
	if fromEpoch < 0 || toEpoch < fromEpoch || (toEpoch-fromEpoch) > 86400*365 {
		return "", godbus.MakeFailedError(fmt.Errorf("invalid time range: from=%d to=%d", fromEpoch, toEpoch))
	}
	procs, err := s.store.ProcessSamplesInRange(fromEpoch, toEpoch)
	if err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("query process samples: %w", err))
	}
	freqs, err := s.store.CPUFreqSamplesInRange(fromEpoch, toEpoch)
	if err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("query CPU frequency samples: %w", err))
	}
	result := map[string]any{"processes": procs, "cpu_freq": freqs}
	data, err := json.Marshal(result)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return string(data), nil
}
