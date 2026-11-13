/*
===========================================================================

world-layers.ts - which seasonal world layers the world draws

The later client's seasonal decoration (the Halloween towns, Christmas,
the Lunar New Year, the festival and OBT arches) is built into the region
bundles as placements tagged with a layer id (scripts/world_layer_import.py,
layerobjdef.txt). The v1.150 client has no outdoor object layers: no
layer table is read, and its only layer type, CLayerEntity (a 0x30-byte
pool at f118a0), is a dungeon's named layer read by Dof_Load (a8b880 via
adbcb0). Its seasonal switch is the event group, set by the entered tail
and 0x3347 (event-environment.ts).

Inference, recorded because no v1.696 client executable is available: a
layer's id is the event group that switches it, so a layer is drawn while
its group's bit 0 is set, the same bit the id-1 event weather reads.

===========================================================================
*/

/*
================
activeWorldLayers

The layers to draw for the event groups: each group whose bit 0 is set.
Stateless (a shared module holds no state, verify-ownership): the
renderer compares the result by value (sameWorldLayers) and keeps its
resident lists while the set is equal, so a fresh set each frame costs
one small allocation and changes nothing drawn.
================
*/
export function activeWorldLayers( groups: Readonly<Record<number, number>> | undefined ): ReadonlySet<number> {
	const layers = new Set<number>();
	if ( !groups ) return layers;
	for ( const [id, state] of Object.entries( groups ) ) if ( (state & 1) !== 0 ) layers.add( Number( id ) );
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
