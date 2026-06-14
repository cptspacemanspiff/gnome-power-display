# Bracketed power measurement from the battery coulomb counter

A way to get **accurate average power with provable error bounds** from `charge_now`,
without fighting the firmware averaging window or needing to know the EC's clock at all.

## The problem

We want instantaneous-ish power to calibrate out component loads (display, wifi, …). The
obvious signals both fail:

- `current_now` / `power_now` is a **~12 s exponential average** (gauge filter). Accurate once
  settled, but too slow and biased low over short windows.
- `charge_now` per-tick power (`ΔQ·V/Δt` on single ticks) is **garbage** — see below.

### Why per-tick timing fails

`charge_now` is a coulomb counter with a **1 mAh quantum** (LSB on this hardware). Each tick is
reported by the EC at its next poll, with a few hundred ms of latency/jitter. Computing power
from a single tick (`ΔQ·V / Δt_tick`) divides an exact charge quantum by a jittery time → the
inferred power swings wildly (e.g. 24 W ↔ 104 W at a true constant 41 W). Worse at higher power,
because ticks come faster and the fixed timing jitter is a larger fraction of each interval.

Note: the apparent "1 s grid" in early data was an **artifact of the Linux ACPI driver cache**
(`/sys/module/battery/parameters/cache_time`, default 1000 ms), *not* the firmware. Lowering it
removes that grid; the residual is real EC-poll jitter.

## The key insight: a monotonic counter gives deterministic time brackets

`charge_now` is monotonic. So the **true** crossing time `t*` of the tick *reported* at sample
time `t[i]` is provably bounded:

- at sample `t[i-1]` the count was still the old value → `t* > t[i-1]`
- at sample `t[i]` the count is the new value → `t* ≤ t[i]`

Therefore **`t* ∈ (t[i-1], t[i]]`** — a hard, deterministic bracket, not a confidence interval.
(Upper bound is the tick's *own* report time; later samples are irrelevant. A fixed gauge→EC
latency, if any, shifts both bounds equally and cancels in differences.)

## Power with guaranteed bounds

For a window spanning tick index `a` → `b`:

```
ΔQ = (b - a) mAh                                  EXACT (integer count, zero error)
t*_a ∈ (t[a-1], t[a]] ,  t*_b ∈ (t[b-1], t[b]]
Δt*  = t*_b - t*_a  ∈ ( t[b-1] - t[a] , t[b] - t[a-1] )

P_max = ΔQ·V / (t[b-1] - t[a])     # b earliest, a latest  → smallest Δt
P_min = ΔQ·V / (t[b]   - t[a-1])   # b latest,   a earliest → largest Δt
P_point = ΔQ·V / (t[b] - t[a])     # best estimate (observed times)
```

`[P_min, P_max]` is a **guaranteed** envelope on the true average power over the window.

Power units: `P_W = ΔQ_uAh · V · 3.6e-3 / Δt_s` (1 mAh = 3.6 C).

### Error properties

- **ΔQ is exact** — all uncertainty is in Δt.
- Timing slop lives **only at the two endpoints**, each bounded by one inter-sample gap. It does
  **not** accumulate with the number of ticks.
- So the fractional bracket width ≈ gap / window-length → **shrinks as 1/T**.
- Endpoints partially cancel (if both crossings sit at similar phase in the EC cycle), so the
  *typical* error is well inside the hard bracket — `P_point` is far more stable than the bracket
  width suggests.
- **No EC-clock characterization needed.** The brackets hold whether the EC poll is regular or
  jittery. (We tried cepstrum/periodogram clock recovery — suggestive ~1.15 s period but
  unnecessary; the brackets sidestep it.)

## Validation (41 W charger, 84 edges over 117 s)

`P_point` recovered **41.8 W** at every window length (true charger ≈ 41 W). Guaranteed bracket
width vs window:

| window | guaranteed width | ≈ ± |
|--------|------------------|-----|
| 10 s   | 36.6%            | ±18% |
| 20 s   | 16.8%            | ±8%  |
| 30 s   | 10.8%            | ±5%  |
| 60 s   | 5.9%             | ±3%  |

Clean 1/T: doubling the window halves the bracket. At ~1.4 s gaps (41 W), **60 s → ±3%
guaranteed**. On battery (~3.3 s gaps) brackets are wider per window; scale window up by the same
law. (120 s+ rows plateaued only because the capture was 117 s.)

## Practical recipe

1. Lower `cache_time` (~125–250 ms in production; 0 only for short probes — every read hits the
   EC over SMBus).
2. Record `charge_now` edges (timestamp + value) and `voltage_now` (uncached; sags under load, so
   use the time-averaged value in the integral).
3. Slide a window; emit `P_point` with `[P_min, P_max]`. Choose window length from the 1/T table
   for the precision you need.
4. **Calibration**: difference bracketed BRIGHT vs DARK (or component on/off) windows and
   propagate the bounds → component power delta *with a guaranteed error bar*. Use lock-in
   (toggle + difference over many cycles) to also reject slow battery-discharge drift.

## Scripts (this dir)

- `dump-charge-edges.py` — record raw charge edges (cache lowered) → `/tmp/charge-edges.csv`
- `bracket-power.py` — the estimator above; sliding window `[P_min, P_point, P_max]` + the 1/T
  width-vs-window table → `/tmp/bracket-power.csv`
- `sample-charge.py` — charge sampler with rolling integrated power + voltage stats
- `probe-cache-time.py` / `probe-phase.py` / `probe-ec-clock.py` / `cepstrum-charge.py` —
  EC-clock investigation (informative, but not needed for the bracket method)

## Open questions for later

- Confirm the bracket on **battery** (slower ticks, wider per-window brackets) and over a long
  capture (does it keep tightening past where the 117 s file plateaued?).
- Tighten brackets using a field that changes every EC poll (e.g. `voltage_now`/`current_now` on
  battery) to mark the poll grid → bracket each crossing to one *poll* interval, not one *tick*.
- Is there a fixed gauge→EC latency offset? (Cancels in deltas, but matters for absolute power.)
- Apply lock-in + bracket propagation to produce display/wifi calibration numbers with error bars.
