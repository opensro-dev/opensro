/*
===========================================================================

gameplay-snapshot.ts - the shared rule for partial gameplay publications

The worker coalesces queued snapshots and presentation applies delivered
ones. Both read a publication the same way, and there are two kinds:

- A whole publication names every dynamic field. It retains only the
  three large omitted fields (skill catalogue, GM catalogue, social) and
  an omitted shop; every other field comes from the newest snapshot.
- A delta names only the fields whose content changed since the
  publication before it. A field it omits keeps its previous value - the
  same object, so a reader that keys work on a field's identity (the chat
  layout, the inventory windows) does nothing for a field that did not
  change. A field it names undefined was removed.

The delta exists because the own pose rides the gameplay plane: while the
character moves, the plane changes on every worker tick, and publishing
it whole cloned the inventory, the chat log and the storage about thirty
times a second, each copy a new object graph that invalidated every
identity-keyed layout on the main thread (the riding trace of
2026-11-11: the chat layout rebuilt on half of all frames, 7.6 ms each).

These pure helpers own no state or indexes; the worker's world core owns
the record of what it last published.

===========================================================================
*/
import type { GameplayState } from "@/engine/contracts/gameplay";

// Plain data nests a few levels (a bag row's tooltip fields); a deeper
// value is not compared, only called different.
const MAX_SAME_DEPTH = 32;

/*
================
mergeGameplaySnapshots

An omitted shop retains its previous value; an explicitly undefined shop
closes it. Preserve that distinction even when neither input names a shop:
the merged publication may follow a shop delivered in an earlier batch.
The caller discards previous at a world reset. Presentation alone rebuilds
the derived skill index after merging.

With delta, next names only what changed: everything else is previous's.
A delta with nothing before it (the first event behind a reset barrier)
stands alone; the whole publication that every reset forces follows it in
the same batch and replaces it.
================
*/
export function mergeGameplaySnapshots(
	previous: GameplayState | null | undefined,
	next: GameplayState,
	delta = false
): GameplayState {
	const gmCatalog = next.gmCatalog ?? previous?.gmCatalog;
	if ( delta && previous ) {
		return {
			...previous,
			...next,
			skillCatalog: next.skillCatalog ?? previous.skillCatalog,
			...(gmCatalog ? { gmCatalog } : {}),
			social: next.social ?? previous.social
		};
	}
	return {
		...next,
		skillCatalog: next.skillCatalog ?? previous?.skillCatalog,
		...(gmCatalog ? { gmCatalog } : {}),
		social: next.social ?? previous?.social,
		...(!("shop" in next) && previous && "shop" in previous ? { shop: previous.shop } : {})
	};
}

/*
================
samePlainData

Content equality of two publication values: primitives by Object.is,
arrays and plain objects member by member, typed arrays element by
element. Anything else (a Map, a class instance) is equal only to itself.
The walk stops at the first difference, so a changed value costs little
and an unchanged one costs one pass - far less than the structured clone,
the decode and the rebuilt layouts it saves.
================
*/
export function samePlainData( a: unknown, b: unknown, depth = 0 ): boolean {
	if ( Object.is( a, b ) ) return true;
	if ( typeof a !== "object" || typeof b !== "object" || a === null || b === null ) return false;
	if ( depth > MAX_SAME_DEPTH ) return false;
	if ( Array.isArray( a ) ) {
		if ( !Array.isArray( b ) || a.length !== b.length ) return false;
		for ( let i = 0; i < a.length; i++ ) {
			if ( !samePlainData( a[i], b[i], depth + 1 ) ) return false;
		}
		return true;
	}
	if ( Array.isArray( b ) ) return false;
	if ( ArrayBuffer.isView( a ) || ArrayBuffer.isView( b ) ) {
		if ( !ArrayBuffer.isView( a ) || !ArrayBuffer.isView( b ) || a.constructor !== b.constructor ) return false;
		const x = a as unknown as ArrayLike<unknown>, y = b as unknown as ArrayLike<unknown>;
		if ( x.length === undefined || x.length !== y.length ) return false;
		for ( let i = 0; i < x.length; i++ ) if ( !Object.is( x[i], y[i] ) ) return false;
		return true;
	}
	const proto = Object.getPrototypeOf( a );
	if ( proto !== Object.getPrototypeOf( b ) || (proto !== Object.prototype && proto !== null) ) return false;
	const left = a as Record<string, unknown>, right = b as Record<string, unknown>;
	let count = 0;
	for ( const key in left ) {
		if ( !Object.hasOwn( left, key ) ) continue;
		count++;
		if ( !Object.hasOwn( right, key ) || !samePlainData( left[key], right[key], depth + 1 ) ) return false;
	}
	for ( const key in right ) if ( Object.hasOwn( right, key ) ) count--;
	return count === 0;
}

/*
================
gameplayDelta

The fields of state whose content differs from published, and an
undefined for every published field state no longer has. published is
brought up to date in place: a changed field takes the new value, an
unchanged one keeps the object already published - the object the reader
on the other side of the clone still holds.
================
*/
export function gameplayDelta(
	published: Map<string, unknown>,
	state: Readonly<Record<string, unknown>>
): Record<string, unknown> {
	const delta: Record<string, unknown> = {};
	for ( const key in state ) {
		if ( !Object.hasOwn( state, key ) ) continue;
		const value = state[key];
		if ( published.has( key ) && samePlainData( published.get( key ), value ) ) continue;
		published.set( key, value );
		delta[key] = value;
	}
	for ( const key of published.keys() ) {
		if ( Object.hasOwn( state, key ) ) continue;
		published.delete( key );
		delta[key] = undefined;
	}
	return delta;
}
