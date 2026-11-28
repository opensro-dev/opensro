/*
===========================================================================

actor-clock.ts - the one reader of a published actor's clocks

A steady actor is republished as the same frozen object while its other
fields hold; its clocks (time, layers, model animation) travel beside it in
the frame's ActorClocks (contracts/character.ts). Every reader of a
published actor's clocks asks here, so an entry is never missed.

===========================================================================
*/
import type { ActorClock, ActorClocks, CharacterActor } from "@/engine/contracts/character";

/*
================
actorClock

The actor's clocks this frame: its entry when it has one, else its own.
================
*/
export function actorClock( actor: CharacterActor, clocks: ActorClocks | undefined ): ActorClock {
	return clocks?.get( actor ) ?? actor;
}
