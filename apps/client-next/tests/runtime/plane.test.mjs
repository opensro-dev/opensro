/*
===========================================================================

plane.test.mjs - the client plane owner's kind and feature table (#1793)

Pins planeKindOf and the per-plane feature table exactly: outdoors and in a
cave every field differs, so a door reading the wrong field fails here.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const load = async entry => {
	return import( sourceFileUrl( entry ).href );
};
const { featuresAt, planeKindOf } = await load( "src/engine/foundation/world/plane.ts" );

test("planeKindOf names the cave plane by the dungeon bit", () => {
	assert.equal( planeKindOf( 0x62a8 ), "outdoor" );
	assert.equal( planeKindOf( 0x8001 ), "cave" );
	assert.equal( planeKindOf( 0x8004 ), "cave" );
});

test("the feature table: outdoors and in a cave every field differs", () => {
	assert.deepEqual( { ...featuresAt( 0x62a8 ) }, {
		minimapCoordinates: true,
		reverseReturnPoints: true,
		characterPvs: false,
		terrainGrid: true,
		residentOnly: false,
		rooms: false,
		sectorFrames: true,
		signedLocalCoordinates: false,
		worldMap: true,
		floorMinimap: false,
		regionCatalog: true,
		sky: true,
		animatedWater: true,
		dungeonWater: false
	} );
	for ( const cave of [ 0x8001, 0x8004 ] ) {
		assert.deepEqual( { ...featuresAt( cave ) }, {
			minimapCoordinates: false,
			reverseReturnPoints: false,
			characterPvs: true,
			terrainGrid: false,
			residentOnly: true,
			rooms: true,
			sectorFrames: false,
			signedLocalCoordinates: true,
			worldMap: false,
			floorMinimap: true,
			regionCatalog: false,
			sky: false,
			animatedWater: false,
			dungeonWater: true
		} );
	}
});
