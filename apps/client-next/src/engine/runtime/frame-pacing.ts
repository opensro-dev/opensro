/*
===========================================================================

frame-pacing.ts - presentation deadlines independent of simulation ticks

RAF remains the display clock. Missed deadlines are discarded, never replayed;
worker simulation, ordered publications and input delivery keep their owners.

===========================================================================
*/
import { DEFAULT_FRAME_LIMIT, frameLimits } from "@/engine/foundation/rendering/video-options";

const CLOCK_TOLERANCE_MS = 0.25;

/*
================
createFramePacing

This browser power preference is intentionally separate from native video
record bytes. A zero limit means display-paced, without an application cap.
================
*/
export function createFramePacing() {
	let limit = DEFAULT_FRAME_LIMIT, deadline: number | undefined, visible = false;
	return {
		/*
		================
		setFrameLimit
		================
		*/
		setFrameLimit( value: number = DEFAULT_FRAME_LIMIT ) {
			const next = frameLimits().includes( value ) ? value : DEFAULT_FRAME_LIMIT;
			if ( next === limit ) return;
			limit = next;
			deadline = undefined;
		},
		/*
		================
		admit

		Hidden maintenance is already paced by worker deliveries. Returning to
		the foreground draws immediately without consuming old frame debt.
		================
		*/
		admit( now: number, foreground: boolean ): boolean {
			if ( foreground !== visible ) deadline = undefined;
			visible = foreground;
			if ( !foreground || limit === 0 ) return true;
			const interval = 1000 / limit;
			if ( deadline === undefined ) {
				deadline = now + interval;
				return true;
			}
			if ( now + CLOCK_TOLERANCE_MS < deadline ) return false;
			const missed = Math.max( 1, Math.floor( (now + CLOCK_TOLERANCE_MS - deadline) / interval ) + 1 );
			deadline += missed * interval;
			return true;
		}
	};
}
