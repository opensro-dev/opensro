/*
===========================================================================

dungeonBlockObjects.test.mjs - the objects a DOF room block places

Synthetic blocks, no game data: the resources and placements the dungeon
world builder derives from each block's non-water objects.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";

import {
	BLOCK_OBJECT_ID_BASE,
	blockObjectDefinitions,
	blockObjectPlacements
} from "../../build/world/assets/dungeonBlockObjects.mjs";

const object = ( path, position, rotation = [ 0, 0, 0 ], scale = [ 1, 1, 1 ] ) => ({
	index: 0,
	name: "n",
	path,
	position,
	rotation,
	scale,
	flags: 0
});
const BLOCKS = [
	{
		index: 0,
		blockObjects: [
			object( "res\\dun\\common\\Flame_A.bsr", [ 1, 2, 3 ], [ 0, 1.5, 0 ] ),
			object( "res\\dun\\common\\flame_a.bsr", [ 1, 2, 3 ], [ 0, 3, 0 ], [ 2, 2, 2 ] ),
			object( "res\\dun\\common\\out_obj_door.bsr", [ 9, 8, 7 ] )
		]
	},
	{ index: 1, blockObjects: [ object( "res\\dun\\common\\out_obj_door.bsr", [ 4, 5, 6 ] ) ] },
	{ index: 2 }
];

test("one definition per distinct path, case folded, in first-use order", () => {
	const { definitions, idByPath } = blockObjectDefinitions( BLOCKS );
	assert.deepEqual( definitions, [
		{ objectId: BLOCK_OBJECT_ID_BASE, flags: 0, sourcePath: "res\\dun\\common\\Flame_A.bsr" },
		{ objectId: BLOCK_OBJECT_ID_BASE + 1, flags: 0, sourcePath: "res\\dun\\common\\out_obj_door.bsr" }
	] );
	assert.equal( idByPath.get( "res\\dun\\common\\flame_a.bsr" ), BLOCK_OBJECT_ID_BASE );
});

test("object ids never collide with a block's own objectId", () => {
	assert.ok( BLOCK_OBJECT_ID_BASE > 4096, "a block index is at most 4095" );
});

test("every object becomes a placement in its block with its own uid", () => {
	const { idByPath } = blockObjectDefinitions( BLOCKS );
	const placements = blockObjectPlacements( BLOCKS, idByPath, 0x8001 );
	assert.equal( placements.length, 4 );
	assert.deepEqual( placements.map( p => p.dungeonBlock ), [ 0, 0, 0, 1 ] );
	assert.equal( new Set( placements.map( p => p.uid ) ).size, 4, "two objects at one spot stay distinct" );
	assert.deepEqual( placements[1], {
		dungeonBlock: 0,
		objectId: BLOCK_OBJECT_ID_BASE,
		uid: BLOCK_OBJECT_ID_BASE + 1,
		regionId: "32769",
		position: { x: 1, y: 2, z: 3 },
		yaw: 0,
		rotation: [ 0, 3, 0 ],
		scaleXyz: [ 2, 2, 2 ]
	} );
});

test("a block without objects adds nothing, and an unknown path is refused", () => {
	assert.deepEqual( blockObjectPlacements( [ { index: 2 } ], new Map(), 0x8001 ), [] );
	assert.throws( () => blockObjectPlacements( BLOCKS, new Map(), 0x8001 ), /no resource/ );
});
