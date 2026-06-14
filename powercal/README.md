# powercal

Average battery power from the kernel's monotonic `charge_now` coulomb counter, with honest
error bars — a least-squares estimate plus its statistical std error, and **hard guaranteed
outer bounds** that assume nothing about the power being constant.

## Method (in one breath)

`charge_now` steps by an exact charge quantum (~1 mAh LSB). Each step ("edge") is seen a few
hundred ms late because of EC-poll / driver-cache latency, so per-edge `dQ/dt` is garbage — but
the counter is *monotonic*. Under constant power the true crossings are evenly spaced, so we fit

```
t[i] = t0 + tau * n[i]        # time on the y-axis, exact integer tick index n[i] on the x-axis
P    = quantum_uAh * V * 3.6e-3 / tau
```

by ordinary least squares. This folds in every edge (minimum variance), and a *constant* EC→gauge
latency only shifts the intercept, not the slope, so `P` is immune to it. The slope's standard
error is the statistical bar; a Bartlett-HAC variant is also reported in case of EC-clock
autocorrelation. Separately, the bracket `t*[i] ∈ (t[i-1], t[i]]` gives a hard `[lower, upper]`
envelope on the average power. See `BRACKET-METHOD.md` in the repo root for the derivation.

## Install

```bash
uv sync                       # create the venv and install powercal (editable) + dev deps
```

## Library use

```python
from powercal import calculate_power, load_edges_csv, measure_power

# 1. From a captured CSV
times, charge, volts = load_edges_csv("edges.csv")
est = calculate_power(times, charge, volts)
print(est)              # 41.556 W  +/-0.042 W (1 sigma)  guaranteed [41.396, 42.088] W
est.power_w, est.std_w, est.lower_w, est.upper_w

# 2. Capture live until the error bar is small enough (needs root)
est = measure_power(target_w=0.1)        # stops when max(std_w, std_robust_w) <= 0.1 W
```

`calculate_power(times, charge_uah, voltage=None, *, voltage_fallback=16.5, hac_lag=8)` returns a
frozen `PowerEstimate`:

| field | meaning |
|---|---|
| `power_w` | least-squares average power (the estimate) |
| `std_w` | 1σ statistical std error (OLS slope SE) |
| `lower_w` / `upper_w` | **guaranteed** hard bounds on the true average power |
| `std_robust_w` | autocorrelation-robust (Bartlett-HAC) std error |
| `autocorr_lag1` | residual lag-1 autocorrelation; \|·\|≲0.2 ⇒ trust `std_w` |
| `tau_s`, `quantum_uah`, `voltage_v`, `n_edges`, `span_s`, `resid_rms_s` | fit detail |
| `constant_power_feasible` | can one constant-power line thread every bracket? `False` ⇒ drift |

`std_w` shrinks like ~1/T^1.5 with capture time, which is why `measure_power` measures-until rather
than fixing a duration. Note `std_w` is *timing-scatter only* — for a hard guarantee drive
`est.bracket_w` down instead.

## CLI

```bash
uv run powercal analyze edges.csv          # estimate from a CSV
sudo $(which powercal) measure --target 0.1   # live capture until error <= 0.1 W
```

## Test

```bash
uv run pytest
```
