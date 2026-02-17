package main

import (
	"encoding/json"
	"fmt"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
	pmconfig "github.com/cptspacemanspiff/gnome-power-display/internal/config"
)

const (
	dbusName  = "org.gnome.PowerMonitor"
	dbusPath  = "/org/gnome/PowerMonitor"
	dbusIface = "org.gnome.PowerMonitor"
)

type currentStats struct {
	Battery   *collector.BatterySample   `json:"battery"`
	Backlight *collector.BacklightSample `json:"backlight"`
}

type historyData struct {
	Battery   []collector.BatterySample   `json:"battery"`
	Backlight []collector.BacklightSample `json:"backlight"`
}

type dbusClient struct {
	conn *godbus.Conn
	obj  godbus.BusObject
}

func newDBusClient() (*dbusClient, error) {
	conn, err := godbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}
	obj := conn.Object(dbusName, dbusPath)
	return &dbusClient{conn: conn, obj: obj}, nil
}

func (c *dbusClient) GetCurrentStats() (*currentStats, error) {
	var jsonStr string
	err := c.obj.Call(dbusIface+".GetCurrentStats", 0).Store(&jsonStr)
	if err != nil {
		return nil, err
	}
	var stats currentStats
	if err := json.Unmarshal([]byte(jsonStr), &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func (c *dbusClient) GetHistory(from, to time.Time) (*historyData, error) {
	var jsonStr string
	err := c.obj.Call(dbusIface+".GetHistory", 0, from.Unix(), to.Unix()).Store(&jsonStr)
	if err != nil {
		return nil, err
	}
	var data historyData
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *dbusClient) GetBatteryHealth() (*collector.BatteryHealth, error) {
	var jsonStr string
	err := c.obj.Call(dbusIface+".GetBatteryHealth", 0).Store(&jsonStr)
	if err != nil {
		return nil, err
	}
	var health collector.BatteryHealth
	if err := json.Unmarshal([]byte(jsonStr), &health); err != nil {
		return nil, err
	}
	return &health, nil
}

func (c *dbusClient) GetPowerStateEvents(from, to time.Time) ([]collector.PowerStateEvent, error) {
	var jsonStr string
	err := c.obj.Call(dbusIface+".GetPowerStateEvents", 0, from.Unix(), to.Unix()).Store(&jsonStr)
	if err != nil {
		return nil, err
	}
	var events []collector.PowerStateEvent
	if err := json.Unmarshal([]byte(jsonStr), &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (c *dbusClient) GetConfig() (*pmconfig.Config, error) {
	var jsonStr string
	err := c.obj.Call(dbusIface+".GetConfig", 0).Store(&jsonStr)
	if err != nil {
		return nil, err
	}
	var cfg pmconfig.Config
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *dbusClient) UpdateConfig(cfg *pmconfig.Config) (*pmconfig.Config, error) {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	var jsonStr string
	err = c.obj.Call(dbusIface+".UpdateConfig", 0, string(configJSON)).Store(&jsonStr)
	if err != nil {
		return nil, err
	}

	var updated pmconfig.Config
	if err := json.Unmarshal([]byte(jsonStr), &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}
