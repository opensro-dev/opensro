/*
===========================================================================

slot-effects.ts - repaint clock for the animated item-slot overlays

item-slot-effects.ts computes which sprite frame each slot shows from the
simulation time. The UI repaints only when something changed, so this owner
remembers whether the last paint drew any animated slot overlay and reports
when the next 40 ms frame step (the fastest native step, 54FA60) is due.

===========================================================================
*/

const FRAME_STEP_MS = 40;

/*
================
createSlotEffectClock
================
*/
export function createSlotEffectClock() {
	let drawn = false, frame = -1;
	return {
		/*
		================
		beginPaint

		Called before a paint; overlays drawn during it call mark().
		================
		*/
		beginPaint() {
			drawn = false;
		},
		mark() {
			drawn = true;
		},
		/*
		================
		due

		Whether a repaint is needed at simulation time nowMs: only while the last
		paint drew an animated overlay and the frame step changed.
		================
		*/
		due( nowMs: number ) {
			const next = drawn ? Math.floor( nowMs / FRAME_STEP_MS ) : -1;
			if ( next === frame ) return false;
			frame = next;
			return next !== -1;
		}
	};
}
