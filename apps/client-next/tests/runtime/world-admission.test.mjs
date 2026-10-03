/*
===========================================================================

world-admission.test.mjs - worker-prepared world scenes reach the renderer

Preparation keeps every render guard, moves group streams into one
transferable arena, and the renderer adopts the received storage as is,
through a one-use lease, across GPU recovery and residency limits.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { build } from "esbuild";
import { root } from "../../tools/project.mjs";
const result = await build( {
	stdin: {
		resolveDir: root,
		contents: `
export * from './src/engine/foundation/rendering/world-scene.ts';
export * from './src/engine/runtime/assets/world-lease.ts';
export * from './src/engine/runtime/renderer/world/world.ts';
export * from './src/engine/runtime/random/random.ts';
`
	},
	bundle: true,
	format: "esm",
	platform: "node",
	write: false
} );
const {
	prepareWorldScene,
	worldSceneTransfers,
	copyWorldScene,
	worldSceneBytes,
	createWorldLease,
	createWorldRenderer,
	createPresentationRandom
} = await import( "data:text/javascript;base64," + Buffer.from( result.outputFiles[0].contents ).toString( "base64" ) );
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
/*
================
scene
================
*/
function scene() {
	return {
		id: "scene",
		originRegion: 1,
		warnings: [],
		groups: [ {
			id: "mesh",
			center: [ 0, 0, 0 ],
			radius: 10000,
			material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true },
			geometry: {
				positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
				normals: new Float32Array( 9 ),
				uvs: new Float32Array( 6 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity(),
				instances: identity()
			}
		} ]
	};
}
/*
================
transfer
================
*/
function transfer( value ) {
	const prepared = prepareWorldScene( value );
	return structuredClone( prepared, { transfer: worldSceneTransfers( prepared.scene ) } );
}

test("terrain seam plans are rebuilt at admission and transferred with exclusive ownership", () => {
	const source = scene(), group = source.groups[0];
	group.geometry.positions = new Float32Array( [ 320, 0, 20, 20, 0, 20, 40, 9, 40 ] );
	group.ranges = [ {
		cell: [ 0, 0 ],
		lod: 0,
		indexStart: 0,
		indexCount: 3,
		vertexStart: 0,
		vertexCount: 3,
		center: [ 160, 0, 160 ],
		radius: 300,
		heights: Array( 289 ).fill( 0 ),
		seamVertices: new Uint32Array( [ 999, 999 ] )
	} ];
	const prepared = prepareWorldScene( source ), copied = copyWorldScene( source );
	const plan = prepared.scene.groups[0].ranges[0].seamVertices;
	assert.deepEqual(
		[ ...plan ],
		[ 0, 33, 2, 36 ],
		"edge plus interior needing authored-height restoration; matching interior omitted"
	);
	assert.deepEqual( copied.groups[0].ranges[0].seamVertices, plan );
	assert.notEqual( copied.groups[0].ranges[0].seamVertices, plan );
	const received = structuredClone( prepared, { transfer: worldSceneTransfers( prepared.scene ) } );
	assert.equal( plan.byteLength, 0 );
	assert.deepEqual( [ ...received.scene.groups[0].ranges[0].seamVertices ], [ 0, 33, 2, 36 ] );
	const world = createWorldRenderer(),
		geometry = {
			/*
			================
			upload
			================
			*/
			upload( data ) {
				return { data };
			},
			release() {},
			updateIndices() {},
			updatePositions() {}
		},
		images = { upload() {}, release() {} };
	world.adopt( createWorldLease( received ) );
	world.camera( { eye: [ -960, 500, 10 ], target: [ 160, 0, 160 ], fov: Math.PI / 3, near: 1, far: 5000 } );
	const frame = world.prepare( geometry, images, 1 );
	assert.equal( frame.draws[0].data.positions[7], 0, "first selection restores the mismatched interior height" );
	world.dispose( geometry, images );
});

// Preparation moves every group stream into one arena: the receiving clone pays per buffer.
test("worker admission keeps every guard and transfers all skin buffers in one arena", () => {
	const source = scene(), g = source.groups[0].geometry;
	g.joints = new Uint32Array( 12 );
	g.weights = new Float32Array( [ 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ] );
	g.bones = identity();
	const reference = copyWorldScene( source ), prepared = prepareWorldScene( source );
	assert.equal( prepared.bytes, worldSceneBytes( source ) );
	const arenas = new Set(
		Object.values( prepared.scene.groups[0].geometry ).filter( v => ArrayBuffer.isView( v ) ).map( v => v.buffer )
	);
	assert.equal( arenas.size, 1, "group streams share one transferable arena" );
	const buffers = worldSceneTransfers( prepared.scene );
	assert.equal( new Set( buffers ).size, buffers.length );
	const received = structuredClone( prepared, { transfer: buffers } );
	for ( const buffer of buffers ) assert.equal( buffer.byteLength, 0, "sender no longer owns transferred bytes" );
	for ( const [key, value] of Object.entries( reference.groups[0].geometry ) ) {
		assert.deepEqual( received.scene.groups[0].geometry[key], value, key );
	}
	for ( const field of [ "positions", "normals", "uvs", "instances", "transform" ] ) {
		const bad = scene();
		bad.groups[0].geometry[field][0] = NaN;
		assert.throws( () => prepareWorldScene( bad ) );
	}
	for (
		const mutate of [
			s => s.groups[0].geometry.indices[2] = 999,
			s => s.groups[0].material.frames = [ "wrong" ],
			s => s.groups[0].material.color[3] = Infinity,
			s => s.groups[0].geometry.weights = new Float32Array( 12 )
		]
	) {
		const bad = scene();
		mutate( bad );
		assert.throws( () => prepareWorldScene( bad ) );
	}
});

test("adoption consumes one private lease and reuses those exact arrays across GPU recovery", () => {
	const received = transfer( scene() ), positions = received.scene.groups[0].geometry.positions;
	const lease = createWorldLease( received ), world = createWorldRenderer(), uploads = [];
	const geometry = {
			/*
			================
			upload
			================
			*/
			upload( data ) {
				uploads.push( data );
				return { data };
			},
			release() {}
		},
		textures = { upload: () => ({}), release() {} };
	assert.deepEqual( Object.keys( lease ).sort(), [ "sceneId", "takeWorld" ] );
	assert.ok( Object.isFrozen( lease ) );
	world.adopt( lease, "full" );
	world.prepare( geometry, textures, 1 );
	assert.equal( uploads[0].positions, positions, "renderer adopted, rather than copied, transferred storage" );
	assert.throws( () => world.adopt( lease ), /already consumed/ );
	assert.equal( world.stats().sceneId, "scene" );
	world.invalidate();
	world.prepare( geometry, textures, 1 );
	assert.equal( uploads[1].positions, positions );
	world.dispose( geometry, textures );
});

test("shared mutable geometry retains per-group isolation while immutable streams transfer once", () => {
	const source = scene(), first = source.groups[0];
	source.groups.push( { ...first, id: "second", geometry: { ...first.geometry } } );
	const prepared = prepareWorldScene( source ), [a, b] = prepared.scene.groups;
	assert.notEqual( a.geometry.positions, b.geometry.positions, "terrain seam writes must not mutate another group" );
	assert.equal( a.geometry.indices, b.geometry.indices, "read-only index source can remain shared" );
	a.geometry.positions[0] = 123;
	assert.equal( b.geometry.positions[0], 0 );
	assert.equal( first.geometry.positions[0], 0 );
	const moved = structuredClone( prepared, { transfer: worldSceneTransfers( prepared.scene ) } );
	moved.scene.groups[0].geometry.positions[0] = 456;
	assert.equal( moved.scene.groups[1].geometry.positions[0], 0 );
});

test("failed residency admission consumes the lease but preserves the committed world", () => {
	const value = scene(), bytes = worldSceneBytes( value ), world = createWorldRenderer( bytes + 16 );
	const geometry = { upload: () => ({}), release() {} }, textures = { upload: () => ({}), release() {} };
	world.adopt( createWorldLease( transfer( value ) ) );
	world.prepare( geometry, textures, 1 );
	const next = scene();
	next.id = "rejected";
	const lease = createWorldLease( transfer( next ) );
	assert.throws( () => world.adopt( lease ), /budget/ );
	assert.throws( () => lease.takeWorld(), /already consumed/ );
	assert.equal( world.stats().sceneId, "scene" );
	assert.equal( world.stats().pendingGroups, 0 );
	world.dispose( geometry, textures );
});

test("copied and transferred worlds retain the same application-owned star RNG through replacement", () => {
	const copied = createWorldRenderer( undefined, undefined, createPresentationRandom( 42 ) ),
		adopted = createWorldRenderer( undefined, undefined, createPresentationRandom( 42 ) );
	const geometry = { upload: data => ({ data }), release() {} }, textures = { upload: () => ({}), release() {} };
	const source = scene();
	source.groups[0].material.sky = 2;
	source.environment = { startTimeOfDay: 0, ratePerSecond: 0, tracks: { starAlpha: [ { t: 0, value: 1 } ] } };
	try {
		for ( let cycle = 0; cycle < 3; cycle++ ) {
			copied.scene( structuredClone( source ) );
			adopted.adopt( createWorldLease( transfer( structuredClone( source ) ) ) );
			for ( const dt of [ 0, .125, .25 ] ) {
				const a = copied.prepare( geometry, textures, 1, cycle + dt ),
					b = adopted.prepare( geometry, textures, 1, cycle + dt );
				assert.deepEqual( a.environment, b.environment );
				assert.deepEqual( a.draws.map( d => d.data ), b.draws.map( d => d.data ) );
			}
			copied.invalidate();
			adopted.invalidate();
		}
	} finally {
		copied.dispose( geometry, textures );
		adopted.dispose( geometry, textures );
	}
});

test("failed worker world transfer publishes an error instead of stranding the request", async t => {
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [] },
		terrainTextures: { tileCatalog: { referencedTiles: [] } },
		objects: { placements: [], resources: { meshes: [], bsr: [], materialSets: [] } }
	};
	t.mock.method( globalThis, "fetch", async () => new Response( JSON.stringify( bundle ) ) );
	const compiled = await build( {
		entryPoints: [ `${root}/src/engine/runtime/assets/worker/loader.ts` ],
		bundle: true,
		platform: "node",
		format: "esm",
		write: false,
		plugins: [ {
			name: "pack-io",
			/*
			================
			setup
			================
			*/
			setup( b ) {
				b.onResolve( { filter: /\.\/packs\/packs$/ }, () => ({ path: "packs", namespace: "fixture" }) );
				b.onLoad(
					{ filter: /.*/, namespace: "fixture" },
					() => ({
						contents: `export const createPacks=()=>({worldAnimationManifests:async()=>[],dispose(){}});`
					})
				);
			}
		} ]
	} );
	const { createLoader } = await import(
		"data:text/javascript;base64," + Buffer.from( compiled.outputFiles[0].contents ).toString( "base64" )
	);
	const results = [];
	const loader = createLoader( result => {
		if ( result.kind === "world" ) throw Error( "fixture transfer failed" );
		results.push( result );
	} );
	try {
		loader.receive( {
			kind: "load",
			id: 1,
			url: "http://localhost/world.json",
			limit: 1024 * 1024,
			decode: "world"
		} );
		for ( let i = 0; i < 20 && !results.length; i++ ) await new Promise( setImmediate );
		assert.equal( results.length, 1 );
		assert.equal( results[0].kind, "error" );
		assert.match( results[0].error, /fixture transfer failed/ );
	} finally {
		loader.dispose();
	}
});

test("terrain boxes are owned and must conservatively enclose the cell and its heights", () => {
	const value = scene(),
		range = {
			cell: [ 0, 0 ],
			lod: 0,
			indexStart: 0,
			indexCount: 3,
			vertexStart: 0,
			vertexCount: 3,
			center: [ 160, 0, 160 ],
			radius: 230,
			heights: Array( 289 ).fill( 0 ),
			bounds: [ 0, 0, 0, 320, 1, 320 ]
		};
	value.groups[0].ranges = [ range ];
	const copied = copyWorldScene( value );
	assert.notEqual( copied.groups[0].ranges[0].bounds, range.bounds );
	range.bounds[1] = -10;
	assert.equal( copied.groups[0].ranges[0].bounds[1], 0 );
	for (
		const bounds of [ [ 0, 1, 0, 320, 2, 320 ], [ 1, 0, 0, 320, 1, 320 ], [ 0, 0, 0, 319, 1, 320 ], [
			0,
			0,
			0,
			320,
			1,
			NaN
		], [ 0, 0, 0, 320, 1 ] ]
	) {
		range.bounds = bounds;
		assert.throws( () => worldSceneBytes( value ), /terrain/i );
	}
});
