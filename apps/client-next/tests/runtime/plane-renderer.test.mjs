/*
===========================================================================

plane-renderer.test.mjs - the renderer world asks the plane owner (#1793 C)

Parity for the slice-C migration: the world renderer's three region tests
(placement fades keep across outdoor scenes only, the dungeon ambient
gate, the weather emission gate) used to test the dungeon region bit
themselves. Each test re-states the former predicate with the raw bit
test ((regionId & 0x8000) !== 0) and compares it with the migrated
planeKindOf form over outdoor, cave and boundary regions, so an arm
swapped in plane.ts fails here. The scene-level behaviour itself stays
pinned by world-renderer.test.mjs.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const load = async entry => {
	return import( sourceFileUrl( entry ).href );
};
const { planeKindOf } = await load( "src/engine/foundation/world/plane.ts" );
const { createWorldRenderer } = await load( "src/engine/runtime/renderer/world/world.ts" );
const { createPresentationRandom } = await load( "src/engine/runtime/random/random.ts" );

// The former predicate: the dungeon bit the renderer tested before #1793.
const wasDungeon = regionId => (regionId & 0x8000) !== 0;

// No scene (0), outdoors (bit clear), caves (bit set), and both boundaries.
const REGIONS = [ 0, 0x0001, 0x62a8, 0x7fff, 0x8000, 0x8001, 0x8004, 0xffff ];

test("placement fades keep only across two outdoor scenes", () => {
	const former = ( previous, current ) => !wasDungeon( previous ) && !wasDungeon( current );
	const migrated = ( previous, current ) =>
		planeKindOf( previous ) === "outdoor" && planeKindOf( current ) === "outdoor";
	for ( const previous of REGIONS ) {
		for ( const current of REGIONS ) {
			assert.equal( migrated( previous, current ), former( previous, current ), `${previous} -> ${current}` );
		}
	}
});

test("the dungeon ambient gate opens for cave scenes or authored visibility", () => {
	const former = ( visibility, regionId ) => visibility !== undefined || wasDungeon( regionId );
	const migrated = ( visibility, regionId ) => visibility !== undefined || planeKindOf( regionId ) === "cave";
	for ( const regionId of REGIONS ) {
		for ( const visibility of [ undefined, [], [ [ 0 ] ] ] ) {
			assert.equal( migrated( visibility, regionId ), former( visibility, regionId ), `${regionId}` );
		}
	}
});

test("weather emits only in an outdoor scene", () => {
	const former = regionId => !wasDungeon( regionId );
	const migrated = regionId => planeKindOf( regionId ) === "outdoor";
	for ( const regionId of REGIONS ) {
		assert.equal( migrated( regionId ), former( regionId ), `${regionId}` );
	}
});

test("a cave scene without authored visibility still takes its ambient colour", () => {
	const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
	const geometry = { upload: () => ({}), release() {}, updateInstances: draw => draw },
		textures = { upload: () => ({}), release() {} };
	const frameAt = originRegion => {
		const world = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) );
		world.scene( {
			id: "ambient-scene",
			originRegion,
			warnings: [],
			groups: [ {
				id: "ambient-scene",
				center: [ 960, 0, 960 ],
				radius: 10000,
				material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true },
				geometry: {
					positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
					normals: new Float32Array( 9 ),
					uvs: new Float32Array( 6 ),
					indices: new Uint32Array( [ 0, 1, 2 ] ),
					instances: identity(),
					transform: identity()
				}
			} ]
		} );
		world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, -1 ], near: 1, far: 1000, fov: Math.PI / 3 } );
		const frame = world.prepare( geometry, textures, 1, 1 );
		world.dispose( geometry, textures );
		return frame;
	};
	// The ambient unpacks at environment[12..14]; the diffuse zeroes at [8..10].
	// A scene's authored ambient always arrives with its visibility
	// (world-scene validation), so a visibility-less cave scene takes the
	// default colour: the gate's plane test is what opens for it.
	const cave = frameAt( 0x8001 );
	assert.deepEqual( [ ...cave.environment.slice( 12, 15 ) ], [
		Math.fround( 0x1e / 255 ),
		Math.fround( 0x1e / 255 ),
		Math.fround( 0x1e / 255 )
	] );
	assert.deepEqual( [ ...cave.environment.slice( 8, 11 ) ], [ 0, 0, 0 ] );
	const outdoor = frameAt( 0x62a8 );
	assert.notDeepEqual( [ ...outdoor.environment.slice( 12, 15 ) ], [ ...cave.environment.slice( 12, 15 ) ] );
});
