// Battery level and energy usage graph renderers

import {
    MARGIN, bucketSeconds, bucketize, segmentSamples, overlapsSleep, getNoDataGaps,
    COL_GREEN, COL_GREEN_FILL, COL_GREEN_CHG, COL_BLUE,
    drawBackground, drawTitle, drawMidGridLine, drawBottomAxis,
    drawYLabels, drawNoDataMessage, drawTimeAxis, drawSleepRegions,
    drawNoDataRegions, drawSelectionOverlay,
    findNearestSample, drawHoverLine, drawHoverTooltip,
} from './graphUtils.js';

export function drawBatteryGraph(area, graphData, sleepData, timeRange, dragStart, dragEnd, hoverX) {
    const cr = area.get_context();
    const [width, height] = area.get_surface_size();
    const gw = width - MARGIN.left - MARGIN.right;
    const gh = height - MARGIN.top - MARGIN.bottom;
    const {from, seconds} = timeRange;

    drawBackground(cr, width, height);
    drawTitle(cr, 'Battery Level');
    drawYLabels(cr, gw, gh, [
        {text: '100%', yFrac: 0},
        {text: '50%', yFrac: 0.5},
        {text: '0%', yFrac: 1},
    ]);
    drawMidGridLine(cr, gw, gh);
    drawTimeAxis(cr, gw, gh, from, seconds);
    drawSleepRegions(cr, gw, gh, from, seconds, sleepData);

    const samples = graphData?.battery;
    if (samples && samples.length > 0) {
        drawNoDataRegions(cr, gw, gh, from, seconds, samples, sleepData);
        const segments = segmentSamples(samples);

        // Charging indicator bar below axis
        const barY = MARGIN.top + gh + 1;
        const barH = 4;
        for (const seg of segments) {
            for (let i = 1; i < seg.length; i++) {
                if ((seg[i - 1].status === 'Charging' || seg[i - 1].status === 'Full') &&
                    !overlapsSleep(sleepData, seg[i - 1].timestamp, seg[i].timestamp)) {
                    const x1 = MARGIN.left + ((seg[i - 1].timestamp - from) / seconds) * gw;
                    const x2 = MARGIN.left + ((seg[i].timestamp - from) / seconds) * gw;
                    cr.setSourceRGBA(...COL_GREEN_CHG);
                    cr.rectangle(x1, barY, x2 - x1, barH);
                    cr.fill();
                }
            }
        }

        // Draw each segment as separate filled area + line
        for (const seg of segments) {
            if (seg.length < 2) continue;
            const toX = (s) => MARGIN.left + ((s.timestamp - from) / seconds) * gw;
            const toY = (s) => MARGIN.top + gh - (s.capacity_pct / 100) * gh;

            // Filled area
            cr.moveTo(toX(seg[0]), MARGIN.top + gh);
            for (const s of seg) cr.lineTo(toX(s), toY(s));
            cr.lineTo(toX(seg[seg.length - 1]), MARGIN.top + gh);
            cr.closePath();
            cr.setSourceRGBA(...COL_GREEN_FILL);
            cr.fill();

            // Line
            cr.setSourceRGBA(...COL_GREEN);
            cr.setLineWidth(1.5);
            cr.moveTo(toX(seg[0]), toY(seg[0]));
            for (let i = 1; i < seg.length; i++) cr.lineTo(toX(seg[i]), toY(seg[i]));
            cr.stroke();
        }
    } else {
        drawNoDataMessage(cr, width, gh);
    }

    // Hover indicator
    if (hoverX !== null && dragStart === null && samples && samples.length > 0) {
        drawHoverLine(cr, gw, gh, hoverX);
        const timestamp = from + ((hoverX - MARGIN.left) / gw) * seconds;
        const nearest = findNearestSample(samples, timestamp);
        if (nearest) {
            // Data point marker
            const sx = MARGIN.left + ((nearest.timestamp - from) / seconds) * gw;
            const sy = MARGIN.top + gh - (nearest.capacity_pct / 100) * gh;
            cr.setSourceRGBA(...COL_GREEN);
            cr.arc(sx, sy, 3, 0, 2 * Math.PI);
            cr.fill();
            // Tooltip
            const watts = (nearest.power_uw / 1e6).toFixed(1);
            const d = new Date(nearest.timestamp * 1000);
            const time = `${d.getHours()}:${d.getMinutes().toString().padStart(2, '0')}`;
            drawHoverTooltip(cr, gw, gh, hoverX, [
                `${nearest.capacity_pct}%  ${watts} W`,
                time,
            ]);
        }
    }

    drawBottomAxis(cr, gw, gh);
    drawSelectionOverlay(cr, gw, gh, dragStart, dragEnd);
    cr.$dispose();
}

export function drawEnergyGraph(area, graphData, sleepData, timeRange, dragStart, dragEnd, hoverX) {
    const cr = area.get_context();
    const [width, height] = area.get_surface_size();
    const gw = width - MARGIN.left - MARGIN.right;
    const gh = height - MARGIN.top - MARGIN.bottom;
    const {from, to, seconds} = timeRange;

    drawBackground(cr, width, height);
    drawTitle(cr, 'Energy Usage');

    const samples = graphData?.battery;
    if (!samples || samples.length === 0) {
        drawNoDataMessage(cr, width, gh);
        cr.$dispose();
        return;
    }

    const bSec = bucketSeconds(seconds);
    const buckets = bucketize(samples, from, to, bSec);
    const nBuckets = buckets.length;

    // Find max avg power for Y scale
    let maxAvg = 0;
    for (const b of buckets) {
        if (b.count > 0) {
            const avg = b.sumPower / b.count;
            maxAvg = Math.max(maxAvg, avg);
        }
    }
    if (maxAvg === 0) maxAvg = 1;
    const maxW = Math.ceil(maxAvg / 1e6 / 5) * 5;
    const maxScale = maxW * 1e6;

    drawYLabels(cr, gw, gh, [
        {text: `${maxW}W`, yFrac: 0},
        {text: `${Math.round(maxW / 2)}W`, yFrac: 0.5},
        {text: '0W', yFrac: 1},
    ]);
    drawMidGridLine(cr, gw, gh);
    drawTimeAxis(cr, gw, gh, from, seconds);
    drawNoDataRegions(cr, gw, gh, from, seconds, samples, sleepData);
    drawSleepRegions(cr, gw, gh, from, seconds, sleepData, bSec);

    // Draw bars
    const noDataGaps = getNoDataGaps(samples, from, sleepData);
    const slotWidth = gw / nBuckets;
    const maxBarWidth = 12;
    const barWidth = Math.min(slotWidth * 0.75, maxBarWidth);
    for (let i = 0; i < nBuckets; i++) {
        const b = buckets[i];
        if (b.count === 0) continue;
        const bucketEnd = b.start + bSec;
        if (overlapsSleep(sleepData, b.start, bucketEnd)) continue;
        if (noDataGaps.some(g => b.start < g.end && bucketEnd > g.start)) continue;
        const avg = b.sumPower / b.count;
        const barH = (avg / maxScale) * gh;
        const x = MARGIN.left + (i + 0.5) * slotWidth - barWidth / 2;
        const y = MARGIN.top + gh - barH;

        cr.setSourceRGBA(...(b.charging ? COL_GREEN : COL_BLUE));
        cr.rectangle(x, y, barWidth, barH);
        cr.fill();
    }

    // Hover indicator
    if (hoverX !== null && dragStart === null && samples && samples.length > 0) {
        drawHoverLine(cr, gw, gh, hoverX);
        const bucketIdx = Math.floor((hoverX - MARGIN.left) / (gw / nBuckets));
        if (bucketIdx >= 0 && bucketIdx < nBuckets) {
            const b = buckets[bucketIdx];
            if (b.count > 0) {
                const avg = (b.sumPower / b.count / 1e6).toFixed(1);
                const fmt = (epoch) => {
                    const d = new Date(epoch * 1000);
                    return `${d.getHours()}:${d.getMinutes().toString().padStart(2, '0')}`;
                };
                drawHoverTooltip(cr, gw, gh, hoverX, [
                    `${avg} W avg`,
                    `${fmt(b.start)} – ${fmt(b.start + bSec)}`,
                ]);
            }
        }
    }

    drawBottomAxis(cr, gw, gh);
    drawSelectionOverlay(cr, gw, gh, dragStart, dragEnd);
    cr.$dispose();
}
