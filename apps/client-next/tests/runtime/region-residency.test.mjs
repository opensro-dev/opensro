/*
===========================================================================

region-residency.test.mjs - outdoor terrain parts stay resident across scenes

A region crossing composes a new scene from its objects and per-region
terrain parts (world-admission.ts). The regions it keeps must keep their
GPU geometry, byte accounting and camera collision parts; only the
regions it adds are uploaded and only the regions it drops are released.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createWorldRenderer } = await import( "../../src/engine/runtime/renderer/world/world.ts" );
const { createWorldResidency } = await import( "../../src/engine/runtime/renderer/world/residency.ts" );
const { prepareWorldScene, worldSceneTransfers } = await import(
	"../../src/engine/foundation/rendering/world-scene.ts"
);
const { createWorldLease } = await import( "../../src/engine/runtime/assets/world-lease.ts" );
const { prepareCameraCollisionParts } = await import( "../../src/engine/foundation/rendering/follow-camera.ts" );

const ANCHOR = 0x0101;

/*
================
identity
================
*/
function identity() {
	return new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
}

/*
================
transfer

A prepared scene as the asset worker's message delivers it.
================
*/
function transfer( scene ) {
	const prepared = prepareWorldScene( scene );
	return structuredClone( prepared, { transfer: worldSceneTransfers( prepared.scene ) } );
}

/*
================
objects

The objects part of a scene centred on region, in the anchor's coordinates.
================
*/
function objects( region ) {
	return {
		id: `objects:${region}`,
		originRegion: ANCHOR,
		warnings: [],
		groups: [ {
			id: `object:${region}`,
			center: [ 0, 0, 0 ],
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
	};
}

/*
================
terrainPart

One region's terrain: a single flat cell at that region's offset from the
anchor, decoded and transferred like a worker part.
================
*/
function terrainPart( region ) {
	const cx = ((region & 255) - (ANCHOR & 255)) * 6, cz = ((region >>> 8) - (ANCHOR >>> 8)) * 6;
	const x0 = cx * 320, z0 = cz * 320, heights = new Array( 289 ).fill( 0 );
	const range = {
		bounds: [ x0, 0, z0, x0 + 320, 0, z0 + 320 ],
		cell: [ cx, cz ],
		lod: 0,
		indexStart: 0,
		indexCount: 6,
		vertexStart: 0,
		vertexCount: 4,
		center: [ x0 + 160, 0, z0 + 160 ],
		radius: 227,
		heights
	};
	const prepared = transfer( {
		id: `terrain:${region}`,
		originRegion: ANCHOR,
		warnings: [],
		groups: [ {
			id: `terrain-batch:${region}`,
			terrainSector: region,
			center: [ x0 + 160, 0, z0 + 160 ],
			radius: 10000,
			ranges: [ range ],
			material: {
				color: [ 1, 1, 1, 1 ],
				alphaCutoff: 0,
				blend: false,
				doubleSided: true,
				terrain: true,
				unlit: true
			},
			geometry: {
				positions: new Float32Array( [ x0, 0, z0, x0 + 320, 0, z0, x0, 0, z0 + 320, x0 + 320, 0, z0 + 320 ] ),
				normals: new Float32Array( 12 ),
				uvs: new Float32Array( 8 ),
				indices: new Uint32Array( [ 0, 2, 3, 0, 3, 1 ] ),
				instances: identity(),
				transform: identity()
			}
		} ]
	} );
	return { region, origin: ANCHOR, groups: prepared.scene.groups, bytes: prepared.bytes };
}

/*
================
devices

Geometry and image commands that record uploads and releases.
================
*/
function devices() {
	const uploads = [], releases = [];
	/** @type {any} Records calls; the renderer reads only these commands. */
	const geometry = {
		/*
		================
		upload
		================
		*/
		upload( data ) {
			const draw = { data };
			uploads.push( draw );
			return draw;
		},
		/*
		================
		release
		================
		*/
		release( draw ) {
			releases.push( draw );
		},
		updateInstances: draw => draw,
		updateIndices() {},
		updatePositions() {}
	};
	/** @type {any} */
	const textures = { upload: () => ({}), release() {} };
	return { geometry, textures, uploads, releases };
}

/*
================
settle
================
*/
function settle( world, d, sceneId ) {
	for ( let frame = 0; frame < 32 && world.stats().sceneId !== sceneId; frame++ ) {
		world.prepare( d.geometry, d.textures, 1, frame / 60 );
	}
	assert.equal( world.stats().sceneId, sceneId );
	assert.equal( world.stats().pendingGroups, 0 );
}

test("a crossing uploads only the terrain it adds and releases only the terrain it drops", () => {
	const world = createWorldRenderer(), d = devices();
	world.camera( {
		originRegion: ANCHOR,
		eye: [ 960, 500, 960 ],
		target: [ 960, 0, 961 ],
		near: 1,
		far: 3500,
		fov: 1
	} );
	const west = terrainPart( 0x0100 ), centre = terrainPart( 0x0101 ), east = terrainPart( 0x0102 );
	world.adopt( createWorldLease( transfer( objects( 0x0100 ) ) ), undefined, [ west, centre ] );
	settle( world, d, "objects:256" );
	assert.equal( d.uploads.length, 3 );
	world.adopt( createWorldLease( transfer( objects( 0x0101 ) ) ), undefined, [ centre, east ] );
	settle( world, d, "objects:257" );
	const added = d.uploads.slice( 3 ).map( draw => draw.data );
	assert.equal( added.length, 2, "the kept region is not uploaded again" );
	assert.ok( added.includes( east.groups[0].geometry ), "the added region is uploaded" );
	assert.ok( !added.includes( centre.groups[0].geometry ) );
	const released = d.releases.map( draw => draw.data );
	assert.ok( released.includes( west.groups[0].geometry ), "the dropped region is released" );
	assert.ok( !released.includes( centre.groups[0].geometry ), "the kept region keeps its draw" );
	assert.equal( world.stats().residentGroups, 3 );
	world.dispose( d.geometry, d.textures );
});

test("a terrain part counts once however many held scenes compose it", () => {
	const residency = createWorldResidency(), part = { region: 1, origin: 1, groups: [], bytes: 1000 };
	const a = { id: "a", originRegion: 1, warnings: [], groups: [] }, b = { ...a, id: "b" };
	assert.equal( residency.incoming( 10, [ part ] ), 1010 );
	residency.hold( a, 10, [ part ] );
	assert.equal( residency.incoming( 20, [ part ] ), 20 );
	residency.hold( b, 20, [ part ] );
	assert.equal( residency.retained(), 1030 );
	residency.release( a, [ b ] );
	assert.equal( residency.retained(), 1020 );
	residency.release( b, [] );
	assert.equal( residency.retained(), 0 );
});

test("release names only the groups no remaining scene uses", () => {
	const residency = createWorldResidency(), kept = { id: "kept" }, gone = { id: "gone" };
	/** @type {any} Only group identity matters to residency. */
	const old = { id: "old", originRegion: 1, warnings: [], groups: [ kept, gone ] };
	/** @type {any} */
	const next = { id: "next", originRegion: 1, warnings: [], groups: [ kept ] };
	residency.hold( old, 1 );
	residency.hold( next, 1 );
	assert.deepEqual( residency.release( old, [ next, old ] ), [ gone ] );
});

test("camera collision reuses a kept region's parts and projects only new ones", () => {
	const cache = new WeakMap(), centre = terrainPart( 0x0101 ), east = terrainPart( 0x0102 );
	/*
	================
	parts
	================
	*/
	function parts( terrain ) {
		const preparation = prepareCameraCollisionParts(
			{ ...objects( 1 ), groups: terrain.flatMap( p => p.groups ) },
			cache
		);
		let result = preparation.next();
		while ( !result.done ) result = preparation.next();
		return result.value;
	}
	const first = parts( [ centre ] ), second = parts( [ centre, east ] );
	assert.equal( first.length, 1 );
	assert.ok( second.includes( first[0] ), "the kept region's part is the same object" );
	assert.equal( second.length, 2 );
});
