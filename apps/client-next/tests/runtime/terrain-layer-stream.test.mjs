/*
===========================================================================

terrain-layer-stream.test.mjs - a terrain layer keeps no CPU vertex mirror

A layer's slots are written from its members' own packed streams
(streamVertices), so the layer draw needs no dynamicVertices copy of its
whole vertex buffer. Runs the real geometry owner on a fake device whose
buffers keep their bytes: every member's slot must equal its own stream,
after admission, a seam patch, a rebuild and a device-loss re-admission.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";

const { createGeometryResources } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/device/geometry.ts" ).href
);
const { createTerrainLayers } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/world/terrain-layers.ts" ).href
);
const { createRetirement } = await import( sourceFileUrl( "src/engine/runtime/renderer/device/retirement.ts" ).href );
globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;

const VERTEX_FLOATS = 14;

/*
================
geometryOwner

The production geometry owner on a device whose buffers keep their bytes.
Writes outside a frame are immediate, so a buffer's bytes are readable
straight after a command.
================
*/
function geometryOwner() {
	const retirement = createRetirement();
	const device = {
		limits: { maxBufferSize: 256 * 1024 * 1024 },
		createBuffer( descriptor ) {
			return { ...descriptor, bytes: new Uint8Array( descriptor.size ), destroy() {} };
		},
		createTexture() {
			return { createView: () => ({}), destroy() {} };
		},
		createSampler() {
			return {};
		},
		createBindGroup( { entries } ) {
			return entries;
		},
		pushErrorScope() {},
		popErrorScope: async () => null,
		queue: {
			writeTexture() {},
			writeBuffer( buffer, offset, data, start, size ) {
				buffer.bytes.set( new Uint8Array( data, start, size ), offset );
			},
			submit() {}
		}
	};
	return createGeometryResources(
		device,
		() => device,
		error => {
			throw error;
		},
		() => ({ getBindGroupLayout: () => ({}) }),
		{ texture: () => ({}), acquire() {}, drop() {} },
		{},
		{},
		{},
		undefined,
		undefined,
		"rgba8unorm",
		undefined,
		retirement.retire
	);
}

/*
================
member

A terrain group of n vertices whose packed stream counts up from seed, as
the worker prepares it (only what terrain-layers.ts reads).
================
*/
function member( n, seed ) {
	const vertices = Float32Array.from( { length: n * VERTEX_FLOATS }, ( _, i ) => seed + i );
	const positions = new Float32Array( n * 3 );
	for ( let i = 0; i < n; i++ ) positions.set( vertices.subarray( i * VERTEX_FLOATS, i * VERTEX_FLOATS + 3 ), i * 3 );
	return {
		id: `member:${seed}`,
		terrainSector: 1,
		ranges: [],
		material: { terrain: true, texture: "/t.png", blend: false, order: 7 },
		geometry: { positions, indices: Uint32Array.of( 0, 1, 2 ), vertices }
	};
}

/*
================
slotOf

The floats of group's slot in its layer draw's GPU vertex buffer.
================
*/
function slotOf( layers, group ) {
	const draw = layers.drawOf( group ), base = layers.layerOf( group ).members.get( group ).base;
	const floats = new Float32Array( draw.vertices.bytes.buffer );
	return floats.subarray(
		base * VERTEX_FLOATS,
		(base + group.geometry.vertices.length / VERTEX_FLOATS) * VERTEX_FLOATS
	);
}

const SCENE = { originRegion: 0x6a48, groups: [] };

test("every member's layer slot is its own stream: admission, a seam patch, a rebuild", () => {
	const geometry = geometryOwner().commands, layers = createTerrainLayers();
	const a = member( 600, 1000 ), b = member( 600, 50000 );
	layers.admit( geometry, SCENE, a, undefined );
	layers.admit( geometry, SCENE, b, undefined );
	assert.deepEqual( slotOf( layers, a ), a.geometry.vertices );
	assert.deepEqual( slotOf( layers, b ), b.geometry.vertices );
	// A seam patch of two ranges of b: its own stream and its slot move together.
	const positions = b.geometry.positions.slice();
	for ( const i of [ 3, 4, 10 ] ) positions.set( [ -1, -2, -3 ], i * 3 );
	layers.updatePositions( geometry, b, positions, [ [ 3, 2 ], [ 10, 1 ] ] );
	assert.deepEqual( Array.from( b.geometry.vertices.subarray( 3 * VERTEX_FLOATS, 3 * VERTEX_FLOATS + 3 ) ), [
		-1,
		-2,
		-3
	] );
	assert.deepEqual( slotOf( layers, b ), b.geometry.vertices );
	assert.deepEqual( slotOf( layers, a ), a.geometry.vertices, "a's slot is untouched" );
	// Past the capacity: the rebuilt draw re-writes every member from its stream.
	const first = layers.drawOf( a ), c = member( 1500, 900000 );
	layers.admit( geometry, SCENE, c, undefined );
	assert.notEqual( layers.drawOf( a ), first );
	for ( const group of [ a, b, c ] ) assert.deepEqual( slotOf( layers, group ), group.geometry.vertices );
});

test("after device loss a re-admitted layer holds the members' current streams", () => {
	const layers = createTerrainLayers(), a = member( 300, 7 );
	let geometry = geometryOwner().commands;
	layers.admit( geometry, SCENE, a, undefined );
	const positions = a.geometry.positions.slice();
	positions.set( [ 5, 6, 7 ], 0 );
	layers.updatePositions( geometry, a, positions, [ [ 0, 1 ] ] );
	// The device is lost: a new owner, and the layers re-admit on upload.
	layers.clear();
	geometry = geometryOwner().commands;
	layers.admit( geometry, SCENE, a, undefined );
	assert.deepEqual( Array.from( slotOf( layers, a ).subarray( 0, 3 ) ), [ 5, 6, 7 ] );
	assert.deepEqual( slotOf( layers, a ), a.geometry.vertices );
});

test("a layer draw keeps no mirror and stream writes are bounded", () => {
	const geometry = geometryOwner().commands, layers = createTerrainLayers(), a = member( 64, 1 );
	const draw = layers.admit( geometry, SCENE, a, undefined );
	// Mirror-backed writes refuse a layer draw: it was not uploaded with dynamicVertices.
	assert.throws(
		() => geometry.writeVertices( draw, 0, new Float32Array( VERTEX_FLOATS ) ),
		/without dynamicVertices/
	);
	assert.throws(
		() => geometry.updatePositions( draw, new Float32Array( 3 ), undefined, undefined, [ [ 0, 1 ] ], 0 ),
		/without dynamicVertices/
	);
	const capacity = draw.vertices.bytes.byteLength / (VERTEX_FLOATS * 4);
	assert.throws(
		() => geometry.streamVertices( draw, capacity, new Float32Array( VERTEX_FLOATS ) ),
		/Invalid vertex write/
	);
	assert.throws(
		() => geometry.streamVertices( draw, -1, new Float32Array( VERTEX_FLOATS ) ),
		/Invalid vertex write/
	);
	assert.throws(
		() => geometry.streamVertices( draw, 0, new Float32Array( VERTEX_FLOATS + 1 ) ),
		/Invalid vertex write/
	);
	assert.throws(
		() => layers.updatePositions( geometry, a, new Float32Array( 3 ), [ [ 0, 1 ] ] ),
		/Invalid position update/
	);
	const bad = a.geometry.positions.slice();
	bad[0] = NaN;
	assert.throws( () => layers.updatePositions( geometry, a, bad, [ [ 0, 1 ] ] ), /Invalid position update/ );
	assert.equal( a.geometry.vertices[0], 1, "a refused patch leaves the member's stream alone" );
	geometry.streamVertices( draw, capacity - 1, new Float32Array( VERTEX_FLOATS ) );
	geometry.release( draw );
	assert.throws(
		() => geometry.streamVertices( draw, 0, new Float32Array( VERTEX_FLOATS ) ),
		/Stale geometry handle/
	);
});
