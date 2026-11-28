/*
===========================================================================

native-cadence.ts - work the native client did once a frame, at its frame rate

Port-only, not native (owner 2026-11-28: frame rate may deviate from
native where an established engine practice pays). The native client ran
its per-frame work once per frame at about 60 frames a second. The port
renders far faster, and some of that work costs more than the rest of a
crowd frame, so its owners refresh it at most once per NATIVE_FRAME_MS -
the native refresh rate, whatever the frame rate - and keep the last
answer between (one frame of a 60 Hz client):

- the world cursor's hover pick (runtime.ts): a click never reads it;
  selection runs its own exact pick at the press;
- the interface's step (runtime.ts): any interface event steps it at once,
  and anchored labels are placed by the renderer every frame
  (character-labels.ts), so only its own 60 Hz content waits.

===========================================================================
*/

// The native client's frame time at 60 frames a second.
export const NATIVE_FRAME_MS = 1000 / 60;

/*
================
NativeCadence
================
*/
export interface NativeCadence {
	// True when the work must be done again at now (ms).
	due( now: number ): boolean;
	// Forgets the last refresh, so the next due() is true: its answer is
	// stale (hover was not allowed, or an interface event arrived).
	reset(): void;
}

/*
================
createNativeCadence
================
*/
export function createNativeCadence( intervalMs = NATIVE_FRAME_MS ): NativeCadence {
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
