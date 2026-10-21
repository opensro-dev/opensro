/*
===========================================================================

effect-room.ts - which effect actors give way when there is no room

The original keeps every decoration a character owns
(CICharactor_AppendOwnedDecoration 85C020 appends to an unbounded list).
The port bounds them twice: the effect owner's EFFECT_ACTOR_BUDGET and the
renderer's CHARACTER_ACTORS. Past either, the effects farthest from the
player give way first, so the ones a player watches keep drawing. Port-only
budgets, not native.

===========================================================================
*/

import { planarDistanceSquared, sameCoordinateSpace } from "@/engine/foundation/world/world-space";

/*
================
effectDistanceSq

The squared ground distance between two poses; outdoor regions are laid
out REGION_SIZE apart, dungeon regions only compare with themselves.
================
*/
export function effectDistanceSq(
	a: { readonly regionId: number; readonly x: number; readonly z: number; },
	b: { readonly regionId: number; readonly x: number; readonly z: number; }
): number {
	return sameCoordinateSpace( a.regionId, b.regionId ) ?
		planarDistanceSquared( b, a ) :
		Number.POSITIVE_INFINITY;
}

/*
================
trimToEffectBudget

Keeps at most room of the attached effect actors, dropping the ones
farthest from the player first and keeping the rest in order. Without a
player pose the newest entries give way. Returns how many gave way.
================
*/
export function trimToEffectBudget(
	actors: { readonly pose: { readonly regionId: number; readonly x: number; readonly z: number; }; }[],
	room: number,
	player: { readonly regionId: number; readonly x: number; readonly z: number; } | undefined
): number {
	const overflow = actors.length - Math.max( 0, room );
	if ( overflow <= 0 ) return 0;
	if ( !player ) {
		actors.length -= overflow;
		return overflow;
	}
	const far = actors.map( ( actor, index ) => ({ index, distance: effectDistanceSq( actor.pose, player ) }) )
		.sort( ( a, b ) => b.distance - a.distance || b.index - a.index )
		.slice( 0, overflow );
	const drop = new Set( far.map( row => row.index ) );
	let kept = 0;
	for ( let i = 0; i < actors.length; i++ ) if ( !drop.has( i ) ) actors[kept++] = actors[i]!;
	actors.length = kept;
	return overflow;
}

/*
================
sharedCharacterRoom

How many of the ranked characters take room when they share it with
effect actors in one nearest-first order (the Experimental shared actor
room, the owner's choice of 2026-10-14): the local player and its mount
(priority below 2) always come first, then each slot goes to whichever is
nearer, the next character or the next effect. effects is the squared
distance of each effect actor to the player, ascending; the characters
are already ranked nearest first.
================
*/
export function sharedCharacterRoom(
	characters: readonly { readonly priority: number; readonly distance: number; }[],
	effects: readonly number[],
	room: number
): number {
	let admitted = 0, effect = 0;
	for ( let taken = 0; taken < room && admitted < characters.length; taken++ ) {
		const character = characters[admitted]!;
		if ( character.priority < 2 || effect >= effects.length || character.distance <= effects[effect]! ) admitted++;
		else effect++;
	}
	return admitted;
}
