/*
===========================================================================

world-layers.ts - which seasonal world layers the world draws

The later client's seasonal decoration (the Halloween towns, Christmas,
the Lunar New Year, the festival and OBT arches) is built into the region
bundles as placements tagged with a layer id (scripts/world_layer_import.py,
layerobjdef.txt). The v1.150 client has no layer system; it has event
groups, set by the entered tail and 0x3347 (event-environment.ts).

Inference, recorded because no v1.696 client executable is available: a
layer's id is the event group that switches it, so a layer is drawn while
its group's bit 0 is set, the same bit the id-1 event weather reads.

===========================================================================
*/

export const NO_WORLD_LAYERS: ReadonlySet<number> = new Set();

const derived = new WeakMap<Readonly<Record<number, number>>, ReadonlySet<number>>();

/*
================
activeWorldLayers

The layers to draw for the event groups. One groups record always gives
the same set object, so a caller can compare sets by identity each frame.
================
*/
export function activeWorldLayers( groups: Readonly<Record<number, number>> | undefined ): ReadonlySet<number> {
	if ( !groups ) return NO_WORLD_LAYERS;
	let layers = derived.get( groups );
	if ( !layers ) {
		const active = Object.entries( groups ).filter( ( [ , state ] ) => (state & 1) !== 0 ).map( ( [ id ] ) =>
			Number( id )
		);
		layers = active.length ? new Set( active ) : NO_WORLD_LAYERS;
		derived.set( groups, layers );
	}
	return layers;
}

/*
================
sameWorldLayers
================
*/
export function sameWorldLayers( a: ReadonlySet<number>, b: ReadonlySet<number> ): boolean {
	if ( a === b ) return true;
	if ( a.size !== b.size ) return false;
	for ( const layer of a ) if ( !b.has( layer ) ) return false;
	return true;
}
