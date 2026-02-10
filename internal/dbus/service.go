package dbus

import (
	"encoding/json"
	"fmt"

	godbus "github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/cptspacemanspiff/gnome-power-display/internal/storage"
)

const (
	busName   = "org.gnome.PowerMonitor"
	objPath   = "/org/gnome/PowerMonitor"
	ifaceName = "org.gnome.PowerMonitor"
)

const introspectXML = `
<node>
  <interface name="` + ifaceName + `">
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

// Export registers the service on the session bus.
func (s *Service) Export() (*godbus.Conn, error) {
	conn, err := godbus.SessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect session bus: %w", err)
	}

	conn.Export(s, objPath, ifaceName)
	conn.Export(introspect.Introspectable(introspectXML), objPath, "org.freedesktop.DBus.Introspectable")

	reply, err := conn.RequestName(busName, godbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("request name: %w", err)
	}
	if reply != godbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("name %s already taken", busName)
	}

	return conn, nil
}

// GetCurrentStats returns the latest battery and backlight data as JSON.
func (s *Service) GetCurrentStats() (string, *godbus.Error) {
	bat, _ := s.store.LatestBatterySample()
	bl, _ := s.store.LatestBacklightSample()
	result := map[string]any{"battery": bat, "backlight": bl}
	data, err := json.Marshal(result)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return string(data), nil
}

// GetHistory returns battery and backlight samples in a time range as JSON.
func (s *Service) GetHistory(fromEpoch, toEpoch int64) (string, *godbus.Error) {
	bat, _ := s.store.BatterySamplesInRange(fromEpoch, toEpoch)
	bl, _ := s.store.BacklightSamplesInRange(fromEpoch, toEpoch)
	result := map[string]any{"battery": bat, "backlight": bl}
	data, err := json.Marshal(result)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return string(data), nil
}

// GetSleepEvents returns sleep events in a time range as JSON.
func (s *Service) GetSleepEvents(fromEpoch, toEpoch int64) (string, *godbus.Error) {
	events, _ := s.store.SleepEventsInRange(fromEpoch, toEpoch)
	data, err := json.Marshal(events)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return string(data), nil
}
