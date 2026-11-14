/*
===========================================================================

actor-query-rows.ts - the actors a socket or matrix query resolves

The character resolvers (renderer characterSocket / characterMatrix /
characterLocalMatrix) read only the queried actor and the attachment and
mount chain it resolves through (transformFor walks attachment?.gid ??
mountedOn, socket's fallback walk follows mountedOn). This module collects
exactly that closure from a displayed map, so a query carries a handful of
rows instead of a shifted copy of every actor on screen - the per-query
full sweep was the quadratic behind combat's presentation-events stage.

===========================================================================
*/

import type { CharacterActor } from "@/engine/contracts/character";

/*
================
actorQueryRows

Collect the queried root actors plus everyone their transform resolves
through. Roots already carry their own identity; a root's owners are looked
up in displayed. Chains are bounded by displayed membership, and cycles are
cut by seen, matching the resolvers' own visited/chain guards.
================
*/
export function actorQueryRows(
	displayed: ReadonlyMap<number, CharacterActor>,
	roots: readonly (CharacterActor | undefined)[]
): CharacterActor[] {
	const rows: CharacterActor[] = [];
	const queued: CharacterActor[] = [];
	for ( const root of roots ) if ( root ) queued.push( root );
	const seen = new Set<number>();
	while ( queued.length ) {
		const actor = queued.pop()!;
		if ( seen.has( actor.gid ) ) continue;
		seen.add( actor.gid );
		rows.push( actor );
		if ( actor.mountedOn !== undefined ) {
			const mount = displayed.get( actor.mountedOn );
			if ( mount ) queued.push( mount );
		}
		const attached = actor.attachment?.gid;
		if ( attached !== undefined ) {
			const owner = displayed.get( attached );
			if ( owner ) queued.push( owner );
		}
	}
	return rows;
}
