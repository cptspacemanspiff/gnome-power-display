#!/bin/bash
systemctl stop power-monitor-daemon.service 2>/dev/null || true
systemctl stop power-monitor-shutdown.service 2>/dev/null || true
systemctl disable power-monitor-daemon.service 2>/dev/null || true
systemctl disable power-monitor-shutdown.service 2>/dev/null || true
systemctl daemon-reload

# Only remove data on full uninstall, not upgrade
# RPM passes $1=0 for erase, $1=1 for upgrade
if [ "${1:-0}" -eq 0 ]; then
  rm -rf /var/lib/power-monitor
fi
