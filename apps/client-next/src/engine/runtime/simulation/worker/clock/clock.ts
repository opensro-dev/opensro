/*
===========================================================================

clock.ts - the simulation worker's fixed-step scheduler

Every step runs for a fixed deadline 16 ms after the previous one. A late
wake catches up with at most four steps; a suspension longer than a second
skips ahead. Simulation time t therefore always belongs to the deadline
origin + t, which the sample publishes so presentation can place a pose on
the frame clock.

===========================================================================
*/
import { SIMULATION_STEP_MS } from "@/engine/contracts/simulation";
import type { Clock, ClockSample } from "@/engine/contracts/runtime";
/*
================
createClock
================
*/
export function createClock( step: ( skippedMs: number ) => void, fail: ( error: unknown ) => void ): Clock {
	let timer: ReturnType<typeof setTimeout> | undefined,
		disposed = false,
		started = false,
		next = 0,
		budgetYielded = false;
	let sample: ClockSample = { wakes: 0, steps: 0, stepsInWake: 0, wakeMs: 0, maxStepMs: 0, debtMs: 0, originMs: 0 };
	// Simulation time t steps at deadline origin + t (see ClockSample.originMs).
	let originMs = 0;
	/*
	================
	wake
	================
	*/
	function wake(): void {
		if ( disposed ) {
			return;
		}
		const now = performance.now();
		// Short scheduling hitches retain fixed ticks. A suspension/overload
		// longer than one second rebases the schedule to at most four ticks.
		// Advance deadline time through skipped intervals without executing
		// historical prediction, packet polling and publication for each tick.
		let skippedMs = 0;
		if ( now - next > 1000 ) {
			const skippedSteps = Math.max( 0, Math.floor( (now - next) / SIMULATION_STEP_MS ) + 1 - 4 );
			skippedMs = skippedSteps * SIMULATION_STEP_MS;
			next += skippedMs;
		} else if (
			budgetYielded &&
			sample.debtMs > 0 &&
			(now - next > sample.debtMs || sample.debtMs >= 4 * SIMULATION_STEP_MS)
		) {
			// When the previous wake exhausted its 4 ms step budget and debt
			// kept growing (or already spans a full four-step catch-up window),
			// executing catch-up steps cannot drain the backlog; rebase the
			// schedule to current wall time so simulation time does not drift
			// behind server time during live walks.
			const skippedSteps = Math.max( 0, Math.floor( (now - next) / SIMULATION_STEP_MS ) );
			skippedMs = skippedSteps * SIMULATION_STEP_MS;
			next += skippedMs;
		}
		try {
			let count = 0, end = now, maxStepMs = 0, yielded = false;
			while ( now >= next && count < 4 ) {
				const start = performance.now();
				step( skippedMs );
				skippedMs = 0;
				end = performance.now();
				maxStepMs = Math.max( maxStepMs, end - start );
				next += SIMULATION_STEP_MS;
				count++;
				if ( disposed ) break;
				if ( end - now >= 4 ) {
					yielded = true;
					break;
				}
			}
			budgetYielded = yielded;
			sample = {
				wakes: sample.wakes + 1,
				steps: sample.steps + count,
				stepsInWake: count,
				wakeMs: end - now,
				maxStepMs,
				debtMs: Math.max( 0, end - next ),
				originMs
			};
		} catch ( error ) {
			fail( error );
			return;
		}
		if ( !disposed ) timer = setTimeout( wake, Math.max( 0, next - performance.now() ) );
	}
	return {
		sample: () => ({ ...sample }),
		/*
		================
		start
		================
		*/
		start() {
			if ( started || disposed ) {
				return;
			}
			started = true;
			next = performance.now() + SIMULATION_STEP_MS;
			originMs = performance.timeOrigin + next;
			sample = { ...sample, originMs };
			timer = setTimeout( wake, SIMULATION_STEP_MS );
		},
		/*
		================
		reanchor
		================
		*/
		reanchor() {
			if ( !started || disposed ) {
				return 0;
			}
			const now = performance.now();
			const skippedSteps = Math.max( 0, Math.floor( (now - next) / SIMULATION_STEP_MS ) );
			const skippedMs = skippedSteps * SIMULATION_STEP_MS;
			next += skippedMs;
			budgetYielded = false;
			sample = { ...sample, debtMs: Math.max( 0, now - next ) };
			return skippedMs;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			disposed = true;
			if ( timer !== undefined ) {
				clearTimeout( timer );
			}
		}
	};
}
