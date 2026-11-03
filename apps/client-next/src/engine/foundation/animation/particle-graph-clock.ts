/*
===========================================================================

particle-graph-clock.ts - where a recreated particle graph's clock starts

A port history is recreated whenever its actor's model, cycle or presence
changes, and a looping effect then had to replay every 20 Hz tick from its
creation to the actor's age in one frame: an hour-old bard dance cost 1.6 s,
rare weapon glows 2 s. This module picks a later start for such a clock and
converts actor times onto it. Port-only cost bound, not a native rule; the
tick simulation itself stays in particle-graph.ts.

===========================================================================
*/

import type { ParticleEmitter, ParticleGraphState } from "@/engine/foundation/animation/particle-graph";
import { PARTICLE_TICKS_PER_SECOND } from "@/engine/foundation/animation/particle-presentation";

// The replayed window of a late-started graph, in longest root-to-leaf
// lifetime chains. Measured on the whole effect catalogue at 600 s: two
// chains kept the mean live count of 2009 of 2088 runs equal to a full
// replay.
const START_WINDOW_CHAINS = 2;

/*
================
greatestDivisor
================
*/
function greatestDivisor( a: number, b: number ): number {
	while ( b ) {
		const rest = a % b;
		a = b;
		b = rest;
	}
	return a;
}

/*
================
lifetimeChain

The longest chain of lifetimes from emitter n down through its descendants.
================
*/
function lifetimeChain( graph: readonly ParticleEmitter[], n: number, memo: number[] ): number {
	const known = memo[n];
	if ( known !== undefined ) return known;
	let longest = 0;
	for ( let c = 0; c < graph.length; c++ ) {
		if ( graph[c]!.parent === n ) longest = Math.max( longest, lifetimeChain( graph, c, memo ) );
	}
	return memo[n] = graph[n]!.frames + longest;
}

/*
================
graphStartTick

The actor tick a fresh history's clock starts at when it is first advanced
to target. Port-only cost bound, not a native rule: a port history is
recreated (model change, an actor missing from a frame, a cycle change) and
would otherwise replay every tick from 0 to the actor's age at once (a
one-hour bard dance cost 1.6 s in one frame). A graph whose roots all repeat (def.loop, or keepRoots) starts a
whole number of root periods (the lcm of the root lifetimes) before target,
at least START_WINDOW_CHAINS lifetime chains back: every element live at
target is born inside the replayed window and the roots keep their phase.
A target inside the window starts at 0, so short delays replay exactly.

Accepted approximations, against a full replay: a non-looping child or a
root emission of finite duration shows once more after the restart, and a
looping child whose period does not divide the root period changes phase.
================
*/
export function graphStartTick( graph: readonly ParticleEmitter[], target: number, keepRoots: boolean ): number {
	let repeats = false, period = 1, chain = 0;
	const memo: number[] = [];
	for ( let n = 0; n < graph.length; n++ ) {
		const def = graph[n]!;
		if ( def.loop || def.emission ) repeats = true;
		if ( def.parent >= 0 ) continue;
		// A root that ends would restart with its new clock.
		if ( !def.loop && !keepRoots || !(def.frames > 0) ) return 0;
		period = period / greatestDivisor( period, def.frames ) * def.frames;
		chain = Math.max( chain, lifetimeChain( graph, n, memo ) );
	}
	if ( !repeats ) return 0;
	const start = Math.floor( (target - START_WINDOW_CHAINS * chain) / period ) * period;
	return Number.isSafeInteger( start ) && start > 0 ? start : 0;
}

/*
================
particleGraphTime

An actor time on the graph's own clock (graphStartTick). Every reader that
sets an actor time against the graph's frame or an element's clockBirth
goes through it.
================
*/
export function particleGraphTime( history: ParticleGraphState, time: number ): number {
	return time - history.startTick / PARTICLE_TICKS_PER_SECOND;
}
