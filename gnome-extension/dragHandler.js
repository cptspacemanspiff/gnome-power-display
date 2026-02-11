// Drag-to-zoom interaction handler for graph areas

import Clutter from 'gi://Clutter';
import {MARGIN} from './graphUtils.js';

export function setupDrag(area, indicator) {
    area.connect('button-press-event', (actor, event) => {
        if (event.get_button() !== 1) return Clutter.EVENT_PROPAGATE;
        const [x] = event.get_coords();
        const [ax] = actor.get_transformed_position();
        indicator._dragStart = x - ax;
        indicator._dragEnd = null;
        return Clutter.EVENT_STOP;
    });
    area.connect('motion-event', (actor, event) => {
        if (indicator._dragStart === null) return Clutter.EVENT_PROPAGATE;
        const [x] = event.get_coords();
        const [ax] = actor.get_transformed_position();
        indicator._dragEnd = x - ax;
        indicator._batteryGraphArea.queue_repaint();
        indicator._energyGraphArea.queue_repaint();
        return Clutter.EVENT_STOP;
    });
    area.connect('button-release-event', (actor, event) => {
        if (indicator._dragStart === null) return Clutter.EVENT_PROPAGATE;
        const [x] = event.get_coords();
        const [ax] = actor.get_transformed_position();
        indicator._dragEnd = x - ax;
        finishDrag(actor, indicator);
        return Clutter.EVENT_STOP;
    });
    area.connect('leave-event', () => {
        if (indicator._dragStart !== null && indicator._dragEnd !== null)
            finishDrag(area, indicator);
        else {
            indicator._dragStart = null;
            indicator._dragEnd = null;
        }
        return Clutter.EVENT_PROPAGATE;
    });
}

function finishDrag(area, indicator) {
    const start = indicator._dragStart;
    const end = indicator._dragEnd;
    indicator._dragStart = null;
    indicator._dragEnd = null;

    if (start === null || end === null) return;
    const minPx = Math.min(start, end);
    const maxPx = Math.max(start, end);
    if (maxPx - minPx < 10) return;

    const width = area.get_width();
    const gw = width - MARGIN.left - MARGIN.right;
    const {from, seconds} = indicator._getTimeRange();

    const t1 = from + ((minPx - MARGIN.left) / gw) * seconds;
    const t2 = from + ((maxPx - MARGIN.left) / gw) * seconds;
    const now = Math.floor(Date.now() / 1000);
    const clampFrom = Math.max(from, Math.floor(t1));
    const clampTo = Math.min(now, Math.ceil(t2));

    if (clampTo - clampFrom < 60) return;

    indicator._zoomTo(clampFrom, clampTo);
}
