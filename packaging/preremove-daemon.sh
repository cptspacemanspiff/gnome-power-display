#!/bin/bash

# RPM passes $1=0 for erase, $1=1 for upgrade/reinstall.
if [ "${1:-0}" -eq 0 ]; then
  # Full uninstall: stop + disable services and remove data.
  systemctl stop power-monitor-daemon.service 2>/dev/null || true
  systemctl stop power-monitor-shutdown.service 2>/dev/null || true
  systemctl disable power-monitor-daemon.service 2>/dev/null || true
  systemctl disable power-monitor-shutdown.service 2>/dev/null || true
  systemctl daemon-reload
  rm -rf /var/lib/power-monitor
else
  # Upgrade/reinstall: stop services only; keep enablement state.
  systemctl stop power-monitor-daemon.service 2>/dev/null || true
  systemctl stop power-monitor-shutdown.service 2>/dev/null || true
  systemctl daemon-reload
fi
