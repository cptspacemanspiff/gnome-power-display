#!/usr/bin/env python3
"""
Visualize charge sensor calibration from raw edge data.

Usage:
    python3 debug/plot_sensor_calibration.py [path/to/debug.json]

Defaults to /tmp/power-calibrate-sensor-debug.json.
"""

import json
import sys
import numpy as np
import matplotlib.pyplot as plt
import matplotlib.gridspec as gridspec

# ── Parameters (tune here without touching Go code) ──────────────────────────
PERIOD_OVERESTIMATE = 3.0   # multiply T_guess before cycle counting
MAX_SKIP            = 8     # max cycles an interval can span

# ── Load raw data ─────────────────────────────────────────────────────────────
path = sys.argv[1] if len(sys.argv) > 1 else "/tmp/power-calibrate-sensor-debug.json"
with open(path) as f:
    d = json.load(f)

times_ms        = np.array(d["edge_times_ms"])      # ms from first edge
charges         = np.array(d["edge_charge_uah"])
deltas          = np.array(d["edge_delta_uah"])
start_charge    = d["start_charge_uah"]
end_charge      = d["end_charge_uah"]
duration_ms     = d["duration_ms"]
duration_s      = duration_ms / 1000

# ── Derive initial T from Q / R ───────────────────────────────────────────────
Q = int(np.min(np.abs(deltas[deltas != 0])))        # quantization step (uAh)
total_delta = abs(start_charge - end_charge)         # total charge change (uAh)
R = total_delta / duration_ms                        # drain rate (uAh/ms)
T_initial = Q / R                                    # expected period (ms)

# ── Cycle-count unwrapping ────────────────────────────────────────────────────
T_for_counting = T_initial * PERIOD_OVERESTIMATE
intervals = np.diff(times_ms)
ms = np.round(intervals / T_for_counting).clip(1, MAX_SKIP)
cycle_counts = np.concatenate([[0], np.cumsum(ms)])

# ── OLS fit: t_k = t0 + n_k * T_refined ──────────────────────────────────────
coeffs = np.polyfit(cycle_counts, times_ms, 1)
T_refined = coeffs[0]
t0_refined = coeffs[1]
fit_line = t0_refined + cycle_counts * T_refined

# ── Phase stats helper ────────────────────────────────────────────────────────
def phase_stats(times, T):
    phases = np.mod(times - times[0], T)
    angles = 2 * np.pi * phases / T
    mean_angle = np.arctan2(np.mean(np.sin(angles)), np.mean(np.cos(angles)))
    if mean_angle < 0:
        mean_angle += 2 * np.pi
    mean_phase = mean_angle * T / (2 * np.pi)
    residuals = phases - mean_phase
    residuals = np.where(residuals >  T / 2, residuals - T, residuals)
    residuals = np.where(residuals < -T / 2, residuals + T, residuals)
    return phases, mean_phase, residuals, float(np.std(residuals))

phases_i,  mean_i,  resid_i,  std_i  = phase_stats(times_ms, T_initial)
phases_r,  mean_r,  resid_r,  std_r  = phase_stats(times_ms, T_refined)

# ── De-slope charge ───────────────────────────────────────────────────────────
slope, intercept = np.polyfit(times_ms, charges, 1)
charges_desloped = charges - (slope * times_ms + intercept)

# ── Print summary ─────────────────────────────────────────────────────────────
print(f"Loaded {len(times_ms)} edges over {duration_s:.0f}s")
print(f"  Q              = {Q} uAh")
print(f"  R              = {R*1000:.3f} uAh/s")
print(f"  T initial      = {T_initial:.2f} ms  (Q/R)")
print(f"  T refined      = {T_refined:.3f} ms  (cycle-count OLS fit)")
print(f"  stddev initial = {std_i:.2f} ms")
print(f"  stddev refined = {std_r:.2f} ms")

# ── Plot ──────────────────────────────────────────────────────────────────────
fig = plt.figure(figsize=(18, 10))
fig.suptitle(
    f"Charge Sensor  |  Q={Q} uAh  R={R*1000:.2f} uAh/s  |  "
    f"T_init={T_initial:.1f} ms  →  T_refined={T_refined:.2f} ms  |  "
    f"stddev: {std_i:.1f} ms → {std_r:.1f} ms",
    fontsize=10,
)
gs = gridspec.GridSpec(2, 3, figure=fig, hspace=0.4, wspace=0.35)

# 1. Charge over time + linear fit
ax = fig.add_subplot(gs[0, 0])
ax.plot(times_ms / 1000, charges / 1e6, color="steelblue", linewidth=0.8)
ax.plot(times_ms / 1000, (slope * times_ms + intercept) / 1e6,
        color="red", linestyle="--", linewidth=0.8, label=f"fit {slope*1000:.2f} uAh/s")
ax.set_xlabel("Time (s)")
ax.set_ylabel("Charge (mAh)")
ax.set_title("Charge over time")
ax.legend(fontsize=8)
ax.grid(True, alpha=0.3)

# 2. Inter-edge intervals
ax = fig.add_subplot(gs[0, 1])
ax.plot(intervals, color="darkorange", linewidth=0.8)
ax.axhline(T_initial, color="gray",  linestyle="--", linewidth=1, label=f"T_init={T_initial:.0f} ms")
ax.axhline(T_refined, color="red",   linestyle="--", linewidth=1, label=f"T_ref={T_refined:.1f} ms")
ax.set_xlabel("Edge index")
ax.set_ylabel("Interval (ms)")
ax.set_title("Inter-edge intervals")
ax.legend(fontsize=8)
ax.grid(True, alpha=0.3)

# 3. De-sloped charge (quantization steps)
ax = fig.add_subplot(gs[0, 2])
ax.plot(times_ms / 1000, charges_desloped, color="steelblue", linewidth=0.8)
ax.axhline(0, color="red", linestyle="--", linewidth=0.8)
ax.set_xlabel("Time (s)")
ax.set_ylabel("Charge residual (uAh)")
ax.set_title("De-sloped charge (quantization steps)")
ax.grid(True, alpha=0.3)

# 4. Phase residuals — initial T
ax = fig.add_subplot(gs[1, 0])
ax.scatter(times_ms / 1000, resid_i, s=4, color="mediumpurple", alpha=0.6)
ax.axhline(0,      color="black", linewidth=0.8)
ax.axhline( std_i, color="red", linestyle="--", linewidth=1, label=f"+1σ={std_i:.1f} ms")
ax.axhline(-std_i, color="red", linestyle="--", linewidth=1, label=f"-1σ")
ax.set_xlabel("Time (s)")
ax.set_ylabel("Residual (ms)")
ax.set_title(f"Phase residuals — T_init={T_initial:.1f} ms")
ax.legend(fontsize=8)
ax.grid(True, alpha=0.3)

# 5. Phase residuals — refined T
ax = fig.add_subplot(gs[1, 1])
ax.scatter(times_ms / 1000, resid_r, s=4, color="mediumseagreen", alpha=0.6)
ax.axhline(0,      color="black", linewidth=0.8)
ax.axhline( std_r, color="red", linestyle="--", linewidth=1, label=f"+1σ={std_r:.1f} ms")
ax.axhline(-std_r, color="red", linestyle="--", linewidth=1, label=f"-1σ")
ax.set_xlabel("Time (s)")
ax.set_ylabel("Residual (ms)")
ax.set_title(f"Phase residuals — T_refined={T_refined:.2f} ms")
ax.legend(fontsize=8)
ax.grid(True, alpha=0.3)

# 6. Cycle-count OLS fit
ax = fig.add_subplot(gs[1, 2])
ax.scatter(cycle_counts, times_ms, s=4, color="steelblue", alpha=0.6, label="observed")
ax.plot(cycle_counts, fit_line, color="red", linewidth=1, label=f"fit T={T_refined:.2f} ms")
ax.set_xlabel("Cycle count n_k")
ax.set_ylabel("Time (ms)")
ax.set_title("Cycle-count unwrap fit")
ax.legend(fontsize=8)
ax.grid(True, alpha=0.3)

plt.show()
