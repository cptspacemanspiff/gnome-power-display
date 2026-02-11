// Shared graph drawing utilities and constants

// Graph margins (used by all graph renderers)
export const MARGIN = {top: 18, right: 32, bottom: 16, left: 8};

// Gaps longer than this break the line
export const GAP_THRESHOLD = 30;

// Colors
export const COL_BG         = [0.12, 0.12, 0.12, 0.9];
export const COL_GRID       = [1, 1, 1, 0.08];
export const COL_AXIS       = [1, 1, 1, 0.25];
export const COL_LABEL      = [1, 1, 1, 0.5];
export const COL_TITLE      = [1, 1, 1, 0.7];
export const COL_SLEEP_BG      = [0.3, 0.35, 0.55, 0.35];
export const COL_SLEEP_EDGE    = [0.45, 0.5, 0.75, 0.5];
export const COL_SLEEP_LBL     = [0.65, 0.7, 0.9, 0.6];
export const COL_SHUTDOWN_BG   = [0.55, 0.3, 0.3, 0.35];
export const COL_SHUTDOWN_EDGE = [0.75, 0.45, 0.45, 0.5];
export const COL_SHUTDOWN_LBL  = [0.9, 0.65, 0.65, 0.6];
export const COL_GREEN      = [0.30, 0.75, 0.40, 1.0];
export const COL_GREEN_FILL = [0.30, 0.75, 0.40, 0.25];
export const COL_GREEN_CHG  = [0.30, 0.75, 0.40, 0.45];
export const COL_BLUE       = [0.35, 0.55, 0.90, 1.0];
export const COL_ORANGE     = [0.95, 0.60, 0.20, 1.0];

// Bucket size adapts to visible range
export function bucketSeconds(rangeSeconds) {
    if (rangeSeconds <= 600)    return 15;
    if (rangeSeconds <= 1800)   return 30;
    if (rangeSeconds <= 3600)   return 60;
    if (rangeSeconds <= 10800)  return 300;
    if (rangeSeconds <= 21600)  return 600;
    if (rangeSeconds <= 86400)  return 900;
    return 3600;
}

// Check if a time range overlaps with any sleep event
export function overlapsSleep(sleepData, start, end) {
    if (!sleepData) return false;
    for (const evt of sleepData) {
        if (start < evt.wake_time && end > evt.sleep_time)
            return true;
    }
    return false;
}

// Find gaps in sample data (excluding sleep regions)
export function getNoDataGaps(samples, from, sleepData) {
    if (!samples || samples.length === 0) return [];
    const gaps = [];
    if (samples[0].timestamp - from > GAP_THRESHOLD &&
        !overlapsSleep(sleepData, from, samples[0].timestamp))
        gaps.push({start: from, end: samples[0].timestamp});
    for (let i = 1; i < samples.length; i++) {
        const dt = samples[i].timestamp - samples[i - 1].timestamp;
        if (dt > GAP_THRESHOLD && !overlapsSleep(sleepData, samples[i - 1].timestamp, samples[i].timestamp))
            gaps.push({start: samples[i - 1].timestamp, end: samples[i].timestamp});
    }
    return gaps;
}

// Split samples into contiguous segments (breaking at gaps)
export function segmentSamples(samples) {
    if (!samples || samples.length === 0) return [];
    const segments = [];
    let seg = [samples[0]];
    for (let i = 1; i < samples.length; i++) {
        if (samples[i].timestamp - samples[i - 1].timestamp > GAP_THRESHOLD) {
            segments.push(seg);
            seg = [];
        }
        seg.push(samples[i]);
    }
    if (seg.length > 0) segments.push(seg);
    return segments;
}

// Group samples into time buckets for bar chart aggregation
export function bucketize(samples, from, to, bucketSec) {
    const buckets = [];
    const nBuckets = Math.ceil((to - from) / bucketSec);
    for (let i = 0; i < nBuckets; i++) {
        buckets.push({start: from + i * bucketSec, sumPower: 0, count: 0, charging: false});
    }
    for (const s of samples) {
        const idx = Math.floor((s.timestamp - from) / bucketSec);
        if (idx >= 0 && idx < nBuckets) {
            buckets[idx].sumPower += s.power_uw;
            buckets[idx].count++;
            if (s.status === 'Charging' || s.status === 'Full')
                buckets[idx].charging = true;
        }
    }
    return buckets;
}

// --- Drawing helpers ---

export function drawBackground(cr, width, height) {
    cr.setSourceRGBA(...COL_BG);
    cr.rectangle(0, 0, width, height);
    cr.fill();
}

export function drawTitle(cr, text) {
    cr.setSourceRGBA(...COL_TITLE);
    cr.setFontSize(10);
    cr.moveTo(MARGIN.left + 2, 13);
    cr.showText(text);
}

export function drawMidGridLine(cr, gw, gh) {
    cr.setSourceRGBA(...COL_GRID);
    cr.setLineWidth(0.5);
    cr.moveTo(MARGIN.left, MARGIN.top + gh / 2);
    cr.lineTo(MARGIN.left + gw, MARGIN.top + gh / 2);
    cr.stroke();
}

export function drawBottomAxis(cr, gw, gh) {
    cr.setSourceRGBA(...COL_AXIS);
    cr.setLineWidth(0.5);
    cr.moveTo(MARGIN.left, MARGIN.top + gh);
    cr.lineTo(MARGIN.left + gw, MARGIN.top + gh);
    cr.stroke();
}

export function drawYLabels(cr, gw, gh, labels) {
    cr.setSourceRGBA(...COL_LABEL);
    cr.setFontSize(8);
    // labels: [{text, yFrac}] where yFrac 0=top, 1=bottom
    for (const {text, yFrac} of labels) {
        const y = MARGIN.top + yFrac * gh + (yFrac === 0 ? 8 : yFrac === 1 ? 0 : 3);
        cr.moveTo(MARGIN.left + gw + 4, y);
        cr.showText(text);
    }
}

export function drawNoDataMessage(cr, width, gh) {
    cr.setSourceRGBA(1, 1, 1, 0.4);
    cr.moveTo(width / 2 - 20, MARGIN.top + gh / 2);
    cr.showText('No data');
}

export function drawTimeAxis(cr, gw, gh, from, rangeSeconds) {
    let stepSeconds, fmt;
    if (rangeSeconds <= 3600) {
        stepSeconds = 600;
        fmt = (t) => {
            const d = new Date(t * 1000);
            return `${d.getHours()}:${d.getMinutes().toString().padStart(2, '0')}`;
        };
    } else if (rangeSeconds <= 10800) {
        stepSeconds = 1800;
        fmt = (t) => {
            const d = new Date(t * 1000);
            return `${d.getHours()}:${d.getMinutes().toString().padStart(2, '0')}`;
        };
    } else if (rangeSeconds <= 21600) {
        stepSeconds = 3600;
        fmt = (t) => { const d = new Date(t * 1000); return `${d.getHours()}:00`; };
    } else if (rangeSeconds <= 86400) {
        stepSeconds = 10800;
        fmt = (t) => {
            const d = new Date(t * 1000);
            const h = d.getHours();
            if (h === 0) return '12 AM';
            if (h === 12) return '12 PM';
            return h < 12 ? `${h}` : `${h - 12}`;
        };
    } else {
        stepSeconds = 86400;
        fmt = (t) => {
            const d = new Date(t * 1000);
            return ['S','M','T','W','T','F','S'][d.getDay()];
        };
    }

    cr.setSourceRGBA(...COL_LABEL);
    cr.setFontSize(8);
    let t = Math.ceil(from / stepSeconds) * stepSeconds;
    while (t < from + rangeSeconds) {
        const x = MARGIN.left + ((t - from) / rangeSeconds) * gw;
        cr.setSourceRGBA(...COL_GRID);
        cr.setLineWidth(0.5);
        cr.moveTo(x, MARGIN.top);
        cr.lineTo(x, MARGIN.top + gh);
        cr.stroke();
        cr.setSourceRGBA(...COL_LABEL);
        cr.moveTo(x - 8, MARGIN.top + gh + 12);
        cr.showText(fmt(t));
        t += stepSeconds;
    }
}

export function drawSleepRegions(cr, gw, gh, from, rangeSeconds, sleepData, bucketSec) {
    if (!sleepData || sleepData.length === 0) return;
    for (const evt of sleepData) {
        let sleepStart = evt.sleep_time;
        let sleepEnd = evt.wake_time;
        if (bucketSec) {
            sleepStart = Math.floor((sleepStart - from) / bucketSec) * bucketSec + from;
            sleepEnd = Math.ceil((sleepEnd - from) / bucketSec) * bucketSec + from;
        }
        const x1 = Math.max(MARGIN.left, MARGIN.left + ((sleepStart - from) / rangeSeconds) * gw);
        const x2 = Math.min(MARGIN.left + gw, MARGIN.left + ((sleepEnd - from) / rangeSeconds) * gw);
        if (x2 <= x1) continue;
        const isShutdown = evt.type === 'shutdown';
        cr.setSourceRGBA(...(isShutdown ? COL_SHUTDOWN_BG : COL_SLEEP_BG));
        cr.rectangle(x1, MARGIN.top, x2 - x1, gh);
        cr.fill();
        // Edge lines
        cr.setSourceRGBA(...(isShutdown ? COL_SHUTDOWN_EDGE : COL_SLEEP_EDGE));
        cr.setLineWidth(0.5);
        for (const xv of [x1, x2]) {
            cr.moveTo(xv, MARGIN.top);
            cr.lineTo(xv, MARGIN.top + gh);
            cr.stroke();
        }
        // Label
        if (x2 - x1 > 28) {
            cr.setSourceRGBA(...(isShutdown ? COL_SHUTDOWN_LBL : COL_SLEEP_LBL));
            cr.setFontSize(7);
            let label;
            switch (evt.type) {
                case 'hibernate': label = 'Hibernate'; break;
                case 'suspend-then-hibernate': label = 'S2H'; break;
                case 'shutdown': label = 'Shutdown'; break;
                default: label = 'Sleep'; break;
            }
            const lx = x1 + (x2 - x1) / 2 - (label.length * 2.5);
            cr.moveTo(lx, MARGIN.top + gh / 2 + 3);
            cr.showText(label);
        }
    }
}

export function drawNoDataRegions(cr, gw, gh, from, rangeSeconds, samples, sleepData) {
    if (!samples || samples.length === 0) return;
    const gaps = getNoDataGaps(samples, from, sleepData);
    for (const gap of gaps) {
        const x1 = Math.max(MARGIN.left, MARGIN.left + ((gap.start - from) / rangeSeconds) * gw);
        const x2 = Math.min(MARGIN.left + gw, MARGIN.left + ((gap.end - from) / rangeSeconds) * gw);
        if (x2 - x1 < 2) continue;
        cr.save();
        cr.rectangle(x1, MARGIN.top, x2 - x1, gh);
        cr.clip();
        cr.setSourceRGBA(1, 1, 1, 0.04);
        cr.rectangle(x1, MARGIN.top, x2 - x1, gh);
        cr.fill();
        cr.setSourceRGBA(1, 1, 1, 0.06);
        cr.setLineWidth(0.5);
        const step = 6;
        for (let x = x1 - gh; x < x2 + gh; x += step) {
            cr.moveTo(x, MARGIN.top + gh);
            cr.lineTo(x + gh, MARGIN.top);
            cr.stroke();
        }
        cr.restore();
        if (x2 - x1 > 36) {
            cr.setSourceRGBA(1, 1, 1, 0.3);
            cr.setFontSize(7);
            cr.moveTo(x1 + (x2 - x1) / 2 - 14, MARGIN.top + gh / 2 + 3);
            cr.showText('No data');
        }
    }
}

export function drawSelectionOverlay(cr, gw, gh, dragStart, dragEnd) {
    if (dragStart === null || dragEnd === null) return;
    const x1 = Math.max(MARGIN.left, Math.min(dragStart, dragEnd));
    const x2 = Math.min(MARGIN.left + gw, Math.max(dragStart, dragEnd));
    if (x2 <= x1) return;
    cr.setSourceRGBA(0, 0, 0, 0.4);
    cr.rectangle(MARGIN.left, MARGIN.top, x1 - MARGIN.left, gh);
    cr.fill();
    cr.rectangle(x2, MARGIN.top, MARGIN.left + gw - x2, gh);
    cr.fill();
    cr.setSourceRGBA(1, 1, 1, 0.5);
    cr.setLineWidth(1);
    cr.rectangle(x1, MARGIN.top, x2 - x1, gh);
    cr.stroke();
}
