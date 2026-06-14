#!/usr/bin/env python3
"""Cepstrum of charge-edge event times to find the fundamental EC period.

The charge edges form a quasi-periodic impulse train. Its power spectrum has a peak at the
fundamental rate PLUS a harmonic series -- which is what made the raw periodogram ambiguous.
The (real) cepstrum = IFFT(log|FFT|^2) collapses that harmonic series into a single peak at a
QUEFRENCY equal to the fundamental PERIOD (in seconds). Peak quefrency = the EC clock period.

Pipeline: edge times -> impulse train sampled at fs -> detrend -> Hann window -> power spectrum
-> log -> inverse FFT -> real cepstrum vs quefrency (seconds).

Usage:  ./cepstrum-charge.py [csv] [--fs 200] [--qmin 0.2] [--qmax 5.0]
Reads /tmp/charge-edges.csv by default (column t_s). Saves /tmp/charge-cepstrum.png.
"""

import argparse
import csv as csvmod
import sys

import numpy as np

try:
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    HAVE_PLT = True
except Exception:
    HAVE_PLT = False

DEFAULT_CSV = "/tmp/charge-edges.csv"
PNG = "/tmp/charge-cepstrum.png"


def load_times(path):
    ts = []
    with open(path) as f:
        for row in csvmod.DictReader(f):
            # accept t_s (dump-charge-edges) or edge_t_s (sample-charge)
            key = "t_s" if "t_s" in row else ("edge_t_s" if "edge_t_s" in row else None)
            if key is None:
                sys.exit(f"No t_s / edge_t_s column in {path}; columns: {list(row)}")
            ts.append(float(row[key]))
    return np.array(sorted(ts))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("csv", nargs="?", default=DEFAULT_CSV)
    ap.add_argument("--fs", type=float, default=200.0, help="impulse-train sample rate Hz (default 200)")
    ap.add_argument("--qmin", type=float, default=0.2, help="min quefrency to search, s")
    ap.add_argument("--qmax", type=float, default=5.0, help="max quefrency to search, s")
    args = ap.parse_args()

    t = load_times(args.csv)
    if len(t) < 8:
        sys.exit(f"Only {len(t)} edges -- too few for a meaningful cepstrum.")
    t = t - t[0]
    T = t[-1]
    fs = args.fs
    N = int(np.ceil(T * fs)) + 1
    print(f"{len(t)} edges over {T:.1f}s; impulse train N={N} samples @ {fs:.0f}Hz")

    # impulse train
    x = np.zeros(N)
    idx = np.clip(np.round(t * fs).astype(int), 0, N - 1)
    np.add.at(x, idx, 1.0)

    # detrend (remove DC = mean rate) and window to suppress leakage
    x = x - x.mean()
    x = x * np.hanning(N)

    # power spectrum -> log -> real cepstrum
    X = np.fft.rfft(x)
    ps = np.abs(X) ** 2
    logps = np.log(ps + 1e-12)
    cep = np.fft.irfft(logps, n=N)

    q = np.arange(N) / fs                      # quefrency axis (seconds)
    half = N // 2
    q, cep = q[:half], cep[:half]
    cmag = np.abs(cep)

    band = (q >= args.qmin) & (q <= args.qmax)
    if not band.any():
        sys.exit("Empty quefrency band; widen --qmin/--qmax.")
    qb, cb = q[band], cmag[band]

    # local maxima within the band, ranked
    peaks = [(qb[i], cb[i]) for i in range(1, len(cb) - 1)
             if cb[i] >= cb[i - 1] and cb[i] >= cb[i + 1]]
    peaks.sort(key=lambda p: p[1], reverse=True)

    print(f"\nTop cepstral peaks in [{args.qmin}, {args.qmax}]s "
          f"(quefrency = candidate fundamental period):")
    print(f"  {'period_s':>9}  {'freq_Hz':>8}  {'cepstrum':>9}")
    for qpk, cpk in peaks[:8]:
        print(f"  {qpk:>9.4f}  {1.0/qpk:>8.3f}  {cpk:>9.4f}")
    if peaks:
        qf = peaks[0][0]
        print(f"\n  => dominant quefrency {qf:.4f}s -> fundamental period {qf:.4f}s "
              f"({1/qf:.3f} Hz). Harmonics at {qf/2:.3f}, {qf/3:.3f}s collapse into this peak.")

    if HAVE_PLT:
        fig, ax = plt.subplots(2, 1, figsize=(10, 7))
        f_axis = np.fft.rfftfreq(N, 1 / fs)
        ax[0].plot(f_axis, ps, lw=0.6)
        ax[0].set(xlim=(0, min(20, fs / 2)), xlabel="frequency (Hz)", ylabel="power",
                  title=f"Power spectrum of charge-edge impulse train ({len(t)} edges)")
        ax[1].plot(q, cmag, lw=0.7)
        ax[1].axvspan(args.qmin, args.qmax, color="orange", alpha=0.12)
        if peaks:
            ax[1].axvline(peaks[0][0], color="r", ls="--", lw=1,
                          label=f"peak {peaks[0][0]:.3f}s")
            ax[1].legend()
        ax[1].set(xlim=(0, args.qmax), xlabel="quefrency (s) = period",
                  ylabel="|cepstrum|", title="Real cepstrum")
        fig.tight_layout()
        fig.savefig(PNG, dpi=110)
        print(f"\nPlot: {PNG}")
    else:
        print("\n(matplotlib unavailable; text peaks only)")


if __name__ == "__main__":
    main()
