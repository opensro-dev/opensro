/*
===========================================================================

dungeonBlockObjects.mjs - the objects a DOF room block places

Each DOF block lists its objects after the block record: torches and their
glow, lamps, cobwebs, sealed doors and cave-in boulders. The client creates
every one of them (CRTDunBlock object creator 0xAD4DC0); water objects
(flags & 4) are the exception, drawn as the block's water surface instead.
The record's position is in dungeon space, its rotation a vector for 0xAD05F0
and its scale a per-axis factor (see dungeonObjectMatrix in the client).

===========================================================================
*/

// Block placements use their block index as objectId (at most 4096 blocks).
export const BLOCK_OBJECT_ID_BASE = 0x10000;

/*
================
blockObjectDefinitions

One object resource definition per distinct object path, case folded the way
every JMX path is, and the objectId each path publishes under.
================
*/
export function blockObjectDefinitions( blocks ) {
	const idByPath = new Map();
	const definitions = [];
	for ( const block of blocks ) {
		for ( const object of block.blockObjects ?? [] ) {
			const key = object.path.toLowerCase();
			if ( idByPath.has( key ) ) continue;
			const objectId = BLOCK_OBJECT_ID_BASE + idByPath.size;
			idByPath.set( key, objectId );
			definitions.push( { objectId, flags: 0, sourcePath: object.path } );
		}
	}
	return { definitions, idByPath };
}

/*
================
blockObjectPlacements

The runtime placement of every non-water block object, in block then record
order, each with a uid of its own so two objects of one resource at one spot
stay distinct.
================
*/
export function blockObjectPlacements( blocks, idByPath, region ) {
	const placements = [];
	for ( const block of blocks ) {
		for ( const object of block.blockObjects ?? [] ) {
			const objectId = idByPath.get( object.path.toLowerCase() );
			if ( objectId === undefined ) throw Error( "Dungeon block object has no resource: " + object.path );
			placements.push( {
				dungeonBlock: block.index,
				objectId,
				uid: BLOCK_OBJECT_ID_BASE + placements.length,
				regionId: String( region ),
				position: { x: object.position[0], y: object.position[1], z: object.position[2] },
				yaw: 0,
				rotation: [ object.rotation[0], object.rotation[1], object.rotation[2] ],
				scaleXyz: [ object.scale[0], object.scale[1], object.scale[2] ]
			} );
		}
	}
	return placements;
}
