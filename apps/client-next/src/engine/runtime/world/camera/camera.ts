/*
===========================================================================

camera.ts - SCT_SHAKECAM* playback: the screen-shake lane

Owns the one active camera script, its timer decay and the shared random
draw each shake period samples. The one fact a reader must know first:
producers stamp the presentation clock, which is monotonic by clamp
(#2561), while the host frame clock handed to step() rewinds a few
milliseconds when animation-frame and maintenance readings interleave.

===========================================================================
*/

import type { CameraScript } from "@/engine/contracts/camera-script";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";

// CGInterface 68F6D0 / 687C3E: timer 4 terminates before timer 5 can
// sample. A00BE0 polls each timer once, then restamps it to now; it does
// not catch up missed ticks. No browser timer and no private RNG stream.

/*
================
createCameraScripts
================
*/
export function createCameraScripts( random: PresentationRandom ) {
	let active: CameraScript | null = null;
	let lastMs = 0;
	let amplitude = 0;
	let x = 0;
	let y = 0;
	// The latest frame reading ever seen. The timers A00BE0 restamps never
	// move backwards, so the camera clock presents this reading, not the raw
	// one. reset() drops the shake but never rewinds the clock, exactly as a
	// world switch does not rewind the presenter's clock.
	let clockMs = 0;

	function reset() {
		active = null;
		lastMs = 0;
		amplitude = 0;
		x = 0;
		y = 0;
	}

	return {
		step( nowMs: number, events: readonly CameraScript[] ) {
			if ( !Number.isFinite( nowMs ) || nowMs < 0 ) throw Error( "Invalid camera script clock" );
			const nowClampedMs = Math.max( nowMs, clockMs );
			clockMs = nowClampedMs;
			for ( const event of events ) {
				if (
					!Number.isFinite( event.atMs ) ||
					event.atMs < 0 ||
					event.atMs > nowClampedMs ||
					![ 50, 200, 300, 400 ].includes( event.amplitude ) ||
					event.durationMs !== 500 ||
					event.periodMs !== (event.amplitude === 50 ? 20 : 25)
				) {
					throw Error( "Invalid camera script event" );
				}
				active = { ...event, atMs: Math.trunc( event.atMs ) };
				lastMs = active.atMs;
				amplitude = event.amplitude;
				x = 0;
				y = 0;
			}
			const now = Math.trunc( nowClampedMs );
			if ( active ) {
				if ( ((now - active.atMs) >>> 0) >= active.durationMs ) reset();
				else if ( ((now - lastMs) >>> 0) >= active.periodMs ) {
					// 687CA6 stores the ratio as float before x87 truncates the product.
					amplitude = Math.max(
						1,
						(amplitude -
							Math.trunc(
								Math.fround( ((now - lastMs) >>> 0) / active.durationMs ) * active.amplitude
							)) >>> 0
					);
					x = Math.fround( random.range( -amplitude, amplitude ) / 100 );
					y = Math.fround( random.range( -amplitude, amplitude ) / 100 );
					lastMs = now;
				}
			}
			return [ x, y ] as const;
		},
		reset,
		dispose: reset
	};
}
