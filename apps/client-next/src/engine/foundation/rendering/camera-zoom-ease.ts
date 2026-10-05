/*
===========================================================================

camera-zoom-ease.ts - glide the drawn camera distance toward the wheel zoom

Native zoom snaps: CGInterface_ZoomCameraByWheel (67B5E0) writes the new
distance and CGInterface_ApplyCameraOrbit (68FAD0) uses it the same frame.
Deliberate deviation (approved 2026-10-03): the input owner keeps that
exact distance, and the world camera draws a distance that closes on it
exponentially, so a wheel notch reads as a short glide at any frame rate.

===========================================================================
*/

// Time for the drawn distance to close about 63% of the gap.
export const ZOOM_EASE_TIME_MS = 70;
// Below this gap the drawn distance lands on the target.
const ZOOM_SNAP_DISTANCE = 0.01;
// A frame gap past this (a stalled tab, a hitch) lands at once.
const ZOOM_MAX_STEP_MS = 250;

export interface ZoomEase {
	step( target: number, nowMs: number ): number;
	reset(): void;
}

/*
================
createZoomEase
================
*/
export function createZoomEase(): ZoomEase {
	let drawn: number | null = null, lastMs: number | null = null;
	return {
		step( target, nowMs ) {
			if ( !Number.isFinite( target ) || !Number.isFinite( nowMs ) ) {
				throw Error( "Invalid camera zoom ease input" );
			}
			const elapsed = lastMs === null ? Infinity : Math.max( 0, nowMs - lastMs );
			lastMs = nowMs;
			if ( drawn === null || elapsed > ZOOM_MAX_STEP_MS ) {
				drawn = target;
				return drawn;
			}
			drawn += (target - drawn) * (1 - Math.exp( -elapsed / ZOOM_EASE_TIME_MS ));
			if ( Math.abs( target - drawn ) < ZOOM_SNAP_DISTANCE ) drawn = target;
			return drawn;
		},
		reset() {
			drawn = null;
			lastMs = null;
		}
	};
}
