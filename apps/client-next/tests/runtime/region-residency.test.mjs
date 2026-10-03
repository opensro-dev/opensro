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
	const uploads = [], releases = [], written = [];
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
		updatePositions() {},
		/*
		================
		writeVertices

		A terrain layer member taking its slot (terrain-layers.ts).
		================
		*/
		writeVertices( draw, base, vertices ) {
			written.push( vertices );
		}
	};
	/** @type {any} */
	const textures = { upload: () => ({}), release() {} };
	return { geometry, textures, uploads, releases, written };
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
	// Both regions' terrain share one texture key: one layer draw, two slots.
	assert.equal( d.uploads.length, 2 );
	assert.equal( d.written.length, 2 );
	world.adopt( createWorldLease( transfer( objects( 0x0101 ) ) ), undefined, [ centre, east ] );
	settle( world, d, "objects:257" );
	assert.deepEqual( d.written.slice( 2 ), [ east.groups[0].geometry.vertices ], "only the added region is written" );
	assert.equal( d.uploads.length, 3, "the new scene uploads its objects, not its terrain" );
	const layer = d.uploads[1];
	assert.ok( !d.releases.includes( layer ), "the kept region keeps the layer" );
	assert.ok( d.releases.some( draw => draw.data.positions.length === 9 ), "the old objects are released" );
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

/*
================
layerMember

A terrain group with n vertices and its packed stream, as the worker
prepares it (only what terrain-layers.ts reads).
================
*/
/** @returns {any} */
function layerMember( n, value ) {
	return {
		id: `member:${value}`,
		terrainSector: 1,
		ranges: [],
		material: { terrain: true, texture: "/t.png", blend: false, order: 7 },
		geometry: {
			positions: new Float32Array( n * 3 ).fill( value ),
			indices: Uint32Array.from( { length: 3 }, ( _, i ) => i ),
			vertices: new Float32Array( n * 14 ).fill( value )
		}
	};
}

test("a terrain layer merges member indices at their slots and survives growth", async () => {
	const { createTerrainLayers } = await import( "../../src/engine/runtime/renderer/world/terrain-layers.ts" );
	/** @type {any} Only the anchor keys a layer. */
	const scene = { originRegion: ANCHOR };
	const layers = createTerrainLayers();
	const draws = [], writes = [], indices = new Map();
	/** @type {any} */
	const geometry = {
		/*
		================
		upload
		================
		*/
		upload( data ) {
			const draw = { id: draws.length, capacity: data.vertices.length / 14 };
			draws.push( draw );
			return draw;
		},
		/*
		================
		release
		================
		*/
		release( draw ) {
			draw.released = true;
		},
		/*
		================
		writeVertices
		================
		*/
		writeVertices( draw, base, vertices ) {
			writes.push( { draw, base, value: vertices[0] } );
		},
		/*
		================
		updateIndices
		================
		*/
		updateIndices( draw, values ) {
			indices.set( draw, [ ...values ] );
		},
		updatePositions() {}
	};
	const a = layerMember( 600, 1 ), b = layerMember( 600, 2 );
	/** @type {any} */
	const first = layers.admit( geometry, scene, a, undefined );
	assert.equal( layers.admit( geometry, scene, b, undefined ), first, "one texture key, one draw" );
	layers.select( a, Uint32Array.of( 0, 1, 2 ), 3 );
	layers.select( b, Uint32Array.of( 2, 1, 0 ), 3 );
	layers.flush( geometry );
	assert.deepEqual( indices.get( first ), [ 0, 1, 2, 602, 601, 600 ], "b's indices are offset by its slot" );
	// Past the capacity (2048 vertices): the layer rebuilds on a new draw.
	const c = layerMember( 1500, 3 );
	/** @type {any} */
	const grown = layers.admit( geometry, scene, c, undefined );
	assert.notEqual( grown, first );
	assert.ok( first.released );
	assert.equal( layers.drawOf( a ), grown, "older members resolve the rebuilt draw" );
	assert.deepEqual( writes.filter( w => w.draw === grown ).map( w => [ w.base, w.value ] ), [ [ 0, 1 ], [ 600, 2 ], [
		1200,
		3
	] ] );
	layers.remove( geometry, a );
	layers.remove( geometry, b );
	layers.remove( geometry, c );
	assert.ok( grown.released, "an empty layer releases its draw" );
});
