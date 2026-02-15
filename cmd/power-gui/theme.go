package main

import (
	"image/color"
	"os/exec"
	"strings"
)

// GNOME accent color name -> libadwaita hex color
var gnomeAccentColors = map[string]color.NRGBA{
	"blue":   {R: 0x35, G: 0x84, B: 0xe4, A: 255},
	"teal":   {R: 0x21, G: 0x90, B: 0xa4, A: 255},
	"green":  {R: 0x3a, G: 0x94, B: 0x4a, A: 255},
	"yellow": {R: 0xc8, G: 0x88, B: 0x00, A: 255},
	"orange": {R: 0xed, G: 0x5b, B: 0x00, A: 255},
	"red":    {R: 0xe6, G: 0x2d, B: 0x42, A: 255},
	"pink":   {R: 0xd5, G: 0x61, B: 0x99, A: 255},
	"purple": {R: 0x91, G: 0x41, B: 0xac, A: 255},
	"slate":  {R: 0x6f, G: 0x83, B: 0x96, A: 255},
}

var accentColor = readAccentColor()

func readAccentColor() color.NRGBA {
	defaultColor := gnomeAccentColors["blue"]

	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "accent-color").Output()
	if err != nil {
		return defaultColor
	}

	name := strings.Trim(strings.TrimSpace(string(out)), "'\"")
	if c, ok := gnomeAccentColors[name]; ok {
		return c
	}
	return defaultColor
}

// accentBgColor returns the accent color at low opacity, suitable for background highlighting.
func accentBgColor() color.NRGBA {
	c := accentColor
	c.A = 38
	return c
}
