#!/bin/bash
# Refresh desktop and icon caches so the launcher entry appears immediately.
if command -v update-desktop-database &>/dev/null; then
  update-desktop-database /usr/share/applications &>/dev/null || true
fi
if command -v gtk-update-icon-cache &>/dev/null; then
  gtk-update-icon-cache -f -t /usr/share/icons/hicolor &>/dev/null || true
fi
