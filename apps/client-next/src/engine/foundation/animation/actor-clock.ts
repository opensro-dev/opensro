/*
===========================================================================

actor-clock.ts - the one reader of a published actor's clocks

A steady actor is republished as the same frozen object while its other
fields hold; its clocks (time, layers, model animation) travel beside it in
the frame's ActorClocks (contracts/character.ts). Every reader of a
published actor's clocks asks here, so an entry is never missed; every
copy of a published actor is made here, so its entry travels with it.

===========================================================================
*/
import type { ActorClock, ActorClocks, CharacterActor } from "@/engine/contracts/character";

// The frame's own table, written while the frame is presented. A new map
// each frame, never one cleared and refilled (state-index.ts).
export type FrameClocks = Map<CharacterActor, ActorClock>;
// A copy's changes never name a clock: the clock is the entry's.
type ActorChanges = Partial<Omit<CharacterActor, keyof ActorClock>>;

/*
================
actorClock

The actor's clocks this frame: its entry when it has one, else its own.
================
*/
export function actorClock( actor: CharacterActor, clocks: ActorClocks | undefined ): ActorClock {
	return clocks?.get( actor ) ?? actor;
}

/*
================
carryClock

Republish an actor in a later table (the last frame's actor held while its
model loads): its entry, when it has one, comes with it.
================
*/
export function carryClock( actor: CharacterActor, from: ActorClocks, to: FrameClocks ) {
	const clock = from.get( actor );
	if ( clock ) to.set( actor, clock );
}

/*
================
republished

A frozen copy of a published actor with some of its other fields changed
(a fade's opacity, a rider's mount): it keeps the actor's clocks, so its
entry is copied to it.
================
*/
export function republished( actor: CharacterActor, changes: ActorChanges, clocks: FrameClocks ): CharacterActor {
	const copy = Object.freeze( { ...actor, ...changes } );
	const clock = clocks.get( actor );
	if ( clock ) clocks.set( copy, clock );
	return copy;
}

/*
================
clocked

The actor with its clocks written in, for a pose query: the renderer poses
the rows it is handed from their own fields. Only the rows a query walks
are resolved (actor-query-rows.ts), never a whole table.
================
*/
export function clocked( actor: CharacterActor, clocks: ActorClocks | undefined ): CharacterActor {
	const clock = clocks?.get( actor );
	return clock ? { ...actor, ...clock } : actor;
}
