#!/bin/bash
# Compile gschemas if glib-compile-schemas is available (RPM with extension)
if command -v glib-compile-schemas &>/dev/null; then
  glib-compile-schemas /usr/share/glib-2.0/schemas/ 2>/dev/null || true
fi
