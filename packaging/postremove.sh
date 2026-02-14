#!/bin/bash
# Recompile gschemas after removal
if command -v glib-compile-schemas &>/dev/null; then
  glib-compile-schemas /usr/share/glib-2.0/schemas/ 2>/dev/null || true
fi

# Remove empty extension directory
rmdir /usr/share/gnome-shell/extensions/power-monitor@gnome-power-display 2>/dev/null || true
