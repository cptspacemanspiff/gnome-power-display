package main

import (
	"fmt"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type batteryHealthPage struct {
	container *gtk.Box
}

func newBatteryHealthPage() *batteryHealthPage {
	p := &batteryHealthPage{}

	p.container = gtk.NewBox(gtk.OrientationVertical, 12)
	p.container.SetMarginStart(24)
	p.container.SetMarginEnd(24)
	p.container.SetMarginTop(24)
	p.container.SetMarginBottom(24)

	health, err := client.GetBatteryHealth()
	if err != nil {
		status := adw.NewStatusPage()
		status.SetTitle("Battery Health Unavailable")
		status.SetDescription(err.Error())
		status.SetIconName("battery-full-symbolic")
		p.container.Append(status)
		return p
	}

	// Identity group
	identityGroup := adw.NewPreferencesGroup()
	identityGroup.SetTitle("Identity")
	identityGroup.Add(makeRow("Manufacturer", health.Manufacturer))
	identityGroup.Add(makeRow("Model", health.Model))
	identityGroup.Add(makeRow("Serial", health.Serial))
	identityGroup.Add(makeRow("Technology", health.Technology))
	p.container.Append(identityGroup)

	// Health group
	healthGroup := adw.NewPreferencesGroup()
	healthGroup.SetTitle("Health")

	designWh := chargeToWh(health.ChargeFullDesignUAH, health.VoltageMinDesignUV)
	currentWh := chargeToWh(health.ChargeFullUAH, health.VoltageMinDesignUV)
	healthGroup.Add(makeRow("Design Capacity", fmt.Sprintf("%.1f Wh", designWh)))
	healthGroup.Add(makeRow("Current Capacity", fmt.Sprintf("%.1f Wh", currentWh)))

	if health.ChargeFullDesignUAH > 0 {
		pct := float64(health.ChargeFullUAH) / float64(health.ChargeFullDesignUAH) * 100
		healthGroup.Add(makeRow("Health", fmt.Sprintf("%.1f%%", pct)))
	}

	healthGroup.Add(makeRow("Cycle Count", fmt.Sprintf("%d", health.CycleCount)))
	p.container.Append(healthGroup)

	return p
}

func makeRow(title, value string) *adw.ActionRow {
	row := adw.NewActionRow()
	row.SetTitle(title)
	label := gtk.NewLabel(value)
	label.AddCSSClass("dim-label")
	row.AddSuffix(label)
	return row
}

func chargeToWh(chargeUAH, voltageUV int64) float64 {
	return float64(chargeUAH) * float64(voltageUV) / 1e12
}
