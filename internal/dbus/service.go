package dbus

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	godbus "github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
	"github.com/cptspacemanspiff/gnome-power-display/internal/config"
	"github.com/cptspacemanspiff/gnome-power-display/internal/storage"
)

const (
	BusName   = "org.gnome.PowerMonitor"
	ObjPath   = "/org/gnome/PowerMonitor"
	IfaceName = "org.gnome.PowerMonitor"

	maxConfigPayloadBytes = 64 * 1024
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
    <method name="GetPowerStateEvents">
      <arg direction="in" type="x" name="from_epoch"/>
      <arg direction="in" type="x" name="to_epoch"/>
      <arg direction="out" type="s" name="json"/>
    </method>
    <method name="GetBatteryHealth">
      <arg direction="out" type="s" name="json"/>
    </method>
    <method name="GetProcessHistory">
      <arg direction="in" type="x" name="from_epoch"/>
      <arg direction="in" type="x" name="to_epoch"/>
      <arg direction="out" type="s" name="json"/>
    </method>
    <method name="GetConfig">
      <arg direction="out" type="s" name="json"/>
    </method>
    <method name="UpdateConfig">
      <arg direction="in" type="s" name="config_json"/>
      <arg direction="out" type="s" name="json"/>
    </method>
  </interface>
` + introspect.IntrospectDataString + `
</node>`

// Service exposes the power monitor over D-Bus.
type Service struct {
	store      *storage.DB
	cfgMu      sync.RWMutex
	cfg        *config.Config
	configPath string
}

// NewService creates a new D-Bus service.
func NewService(store *storage.DB, cfg *config.Config, configPath string) (*Service, error) {
	trimmedConfigPath := strings.TrimSpace(configPath)
	if trimmedConfigPath == "" {
		return nil, fmt.Errorf("config path must not be empty")
	}
	sanitizedCfg, err := config.NormalizeAndValidate(cfg)
	if err != nil {
		return nil, fmt.Errorf("sanitize config: %w", err)
	}
	return &Service{store: store, cfg: sanitizedCfg, configPath: trimmedConfigPath}, nil
}

// Export registers the service on the system bus.
func (s *Service) Export() (*godbus.Conn, error) {
	conn, err := godbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}

	if err := conn.Export(s, ObjPath, IfaceName); err != nil {
		conn.Close()
		return nil, fmt.Errorf("export service interface: %w", err)
	}
	if err := conn.Export(introspect.Introspectable(introspectXML), ObjPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("export introspection interface: %w", err)
	}

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

// GetPowerStateEvents returns power state events in a time range as JSON.
func (s *Service) GetPowerStateEvents(fromEpoch, toEpoch int64) (string, *godbus.Error) {
	if fromEpoch < 0 || toEpoch < fromEpoch || (toEpoch-fromEpoch) > 86400*365 {
		return "", godbus.MakeFailedError(fmt.Errorf("invalid time range: from=%d to=%d", fromEpoch, toEpoch))
	}
	events, err := s.store.PowerStateEventsInRange(fromEpoch, toEpoch)
	if err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("query power state events: %w", err))
	}
	data, err := json.Marshal(events)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return string(data), nil
}

// GetBatteryHealth returns battery identity and health info as JSON.
func (s *Service) GetBatteryHealth() (string, *godbus.Error) {
	health, err := collector.CollectBatteryHealth()
	if err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("collect battery health: %w", err))
	}
	data, err := json.Marshal(health)
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

// GetConfig returns the daemon configuration as JSON.
func (s *Service) GetConfig() (string, *godbus.Error) {
	s.cfgMu.RLock()
	cfgCopy := *s.cfg
	s.cfgMu.RUnlock()

	data, err := json.Marshal(cfgCopy)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return string(data), nil
}

// UpdateConfig sanitizes and persists a new daemon configuration.
func (s *Service) UpdateConfig(configJSON string) (string, *godbus.Error) {
	if len(configJSON) > maxConfigPayloadBytes {
		return "", godbus.MakeFailedError(fmt.Errorf("config update payload too large: %d bytes", len(configJSON)))
	}

	var candidate config.Config
	if err := json.Unmarshal([]byte(configJSON), &candidate); err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("parse config JSON: %w", err))
	}

	sanitized, err := config.NormalizeAndValidate(&candidate)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	if err := config.Save(s.configPath, sanitized); err != nil {
		return "", godbus.MakeFailedError(fmt.Errorf("persist config: %w", err))
	}

	s.cfgMu.Lock()
	s.cfg = sanitized
	s.cfgMu.Unlock()

	data, err := json.Marshal(sanitized)
	if err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return string(data), nil
}
