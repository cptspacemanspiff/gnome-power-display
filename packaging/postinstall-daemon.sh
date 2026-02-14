#!/bin/bash
systemctl daemon-reload
systemctl enable power-monitor-daemon.service 2>/dev/null || true
systemctl enable power-monitor-shutdown.service 2>/dev/null || true
systemctl start power-monitor-daemon.service 2>/dev/null || true
