/*
===========================================================================

hover-cadence.ts - how often the world cursor asks what lies under it

Port-only, not native (owner 2026-11-28: frame rate may deviate from
native where an established engine practice pays). The native client picks
the hover target once per frame, and it ran at about 60 frames a second.
The port renders far faster, and a hover pick with its scenery occlusion
test costs more than the rest of a crowd frame, so the hover target is
refreshed at most once per HOVER_PICK_INTERVAL_MS: the native refresh rate,
whatever the frame rate. Between refreshes the last answer stands (one
frame of a 60 Hz client). A click never reads it: selection runs its own
exact pick at the press.

===========================================================================
*/

// The native client's frame time at 60 frames a second.
export const HOVER_PICK_INTERVAL_MS = 1000 / 60;

/*
================
HoverCadence
================
*/
export interface HoverCadence {
	// True when the hover target must be picked again at now (ms).
	due( now: number ): boolean;
	// Forgets the last pick, so the next due() is true: hover was not
	// allowed (UI under the pointer, a hidden tab), and its answer is stale.
	reset(): void;
}

/*
================
createHoverCadence
================
*/
export function createHoverCadence( intervalMs = HOVER_PICK_INTERVAL_MS ): HoverCadence {
	let last = -Infinity;
	return {
		due( now ) {
			if ( now - last < intervalMs && now >= last ) return false;
			// Whole intervals keep the average at the interval; after a stall
			// (or a clock that stepped back) the cadence restarts at now.
			last = now - last < 2 * intervalMs && now >= last ? last + intervalMs : now;
			return true;
		},
		reset() {
			last = -Infinity;
		}
	};
}
