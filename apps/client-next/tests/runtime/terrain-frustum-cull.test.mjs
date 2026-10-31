import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { GPU_BUFFER_USAGE, GPU_TEXTURE_USAGE } from "../helpers/strict-gpu.mjs";

async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createWorldRenderer } = await load( "src/engine/runtime/renderer/world/world.ts" );
const { createGeometryResources } = await load( "src/engine/runtime/renderer/device/geometry.ts" );
const { createRetirement } = await load( "src/engine/runtime/renderer/device/retirement.ts" );
globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
globalThis.GPUTextureUsage = GPU_TEXTURE_USAGE;

const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );

/*
================
geometryOwner

The production geometry owner on a device whose buffers keep their bytes
(the terrain-layer-stream harness): the draws it returns carry the index
arrays the world's selection wrote.
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
		createShaderModule() {
			return {};
		},
		createRenderPipeline() {
			return { getBindGroupLayout: () => ({}) };
		},
		createBindGroup() {
			return {};
		},
		queue: {
			writeBuffer() {},
			writeTexture() {},
			submit() {}
		},
		pushErrorScope() {},
		popErrorScope() {
			return Promise.resolve( null );
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
terrainGroup

One terrain group: a 3x3 grid of 320-metre cells, each a range of its
own indices, flat at y=0 (heights all zero), every range at LOD 0. The
ranges' centres and radii are the real cell bounds the frustum tests.
================
*/
function terrainGroup() {
	const GRID = 3, CELLS = 320, EDGE = 16, SIZE = 2;
	const positions = [], normals = [], uvs = [], indices = [], ranges = [];
	for ( let cz = 0; cz < GRID; cz++ ) {
		for ( let cx = 0; cx < GRID; cx++ ) {
			const indexStart = indices.length, vertexStart = positions.length / 3;
			const ox = cx * CELLS, oz = cz * CELLS;
			for ( let z = 0; z <= EDGE; z++ ) {
				for ( let x = 0; x <= EDGE; x++ ) {
					positions.push( ox + x * CELLS / EDGE, 0, oz + z * CELLS / EDGE );
					normals.push( 0, 1, 0 );
					uvs.push( x / EDGE, z / EDGE );
				}
			}
			for ( let z = 0; z < EDGE; z++ ) {
				for ( let x = 0; x < EDGE; x++ ) {
					const a = z * (EDGE + 1) + x, b = a + 1, c = a + EDGE + 1, d = c + 1;
					indices.push( a, c, b, b, c, d );
				}
			}
			const indexCount = indices.length - indexStart;
			ranges.push( {
				cell: [ cx, cz ],
				lod: 0,
				indexStart,
				indexCount,
				vertexStart,
				vertexCount: (EDGE + 1) * (EDGE + 1),
				center: [ ox + CELLS / 2, 0, oz + CELLS / 2 ],
				radius: CELLS / 2 * Math.SQRT2,
				heights: new Array( (EDGE + 1) * (EDGE + 1) ).fill( 0 )
			} );
		}
	}
	return {
		id: "terrain",
		center: [ CELLS * 1.5, 0, CELLS * 1.5 ],
		radius: GRID * CELLS,
		material: {
			color: [ .72, .62, .5, 1 ],
			unlit: false,
			objectLight: 1,
			alphaCutoff: 0,
			blend: false,
			doubleSided: true,
			terrain: true
		},
		ranges,
		geometry: {
			world: true,
			positions: new Float32Array( positions ),
			normals: new Float32Array( normals ),
			uvs: new Float32Array( uvs ),
			indices: new Uint32Array( indices ),
			instances: identity,
			transform: identity
		}
	};
}

test("the terrain choice keys on cell and view (native A2D1B0 parity)", () => {
	const world = createWorldRenderer();
	const owner = geometryOwner(), geometry = owner.commands;
	const textures = { upload: () => ({}), release() {} };
	const scene = {
		id: "terrain-frustum",
		originRegion: 257,
		warnings: [],
		environment: {
			startTimeOfDay: .5,
			ratePerSecond: 0,
			tracks: {
				color0xf0: [ { t: 0, r: 1, g: 1, b: 1 } ],
				color0x124: [ { t: 0, r: .55, g: .55, b: .55 } ]
			}
		},
		groups: [ terrainGroup() ]
	};
	world.scene( scene );
	const views = [
		// The eye stays over the grid's centre cell for every view, so the
		// LOD rule alone cannot drop a range: only the frustum can. A narrow
		// fov quarter-turn faces one 320-metre column of the 3x3 grid.
		{ eye: [ 480, 60, 480 ], target: [ 480, 0, 160 ], originRegion: 257, fov: .4, near: 1, far: 5000 },
		{ eye: [ 480, 60, 480 ], target: [ 800, 0, 480 ], originRegion: 257, fov: .4, near: 1, far: 5000 },
		{ eye: [ 480, 60, 480 ], target: [ 480, 0, 478.5 ], originRegion: 257, fov: .4, near: 1, far: 5000 }
	];
	const counts = [];
	for ( const camera of views ) {
		world.camera( camera );
		const prepared = world.prepare( geometry, textures, 1.6, 0, 192, 120, 3500, false, false );
		const terrain = prepared.draws[0];
		counts.push( terrain ? terrain.indexCount / 3 : 0 );
	}
	// The three views submit three different triangle counts: the cull
	// engages with the view, not only the cell.
	assert.ok(
		counts[0] >= counts[1] && counts[1] > counts[2] && counts[2] > 0,
		`Narrower views submit fewer triangles: ${counts.join( ", " )}`
	);
	// Returning to the first view reproduces its exact choice: same count,
	// same selection (the walk's draw indices are the choice).
	world.camera( views[0] );
	const replay = world.prepare( geometry, textures, 1.6, 1, 192, 120, 3500, false, false );
	const replayTriangles = replay.draws[0] ? replay.draws[0].indexCount / 3 : 0;
	assert.equal(
		replayTriangles,
		counts[0],
		`The first view replays its exact count (${replayTriangles} vs ${counts[0]})`
	);
	// The narrow fov faces one column: the sphere test keeps the column's
	// cells plus any whose sphere pokes into the cone, and drops the rest -
	// strictly fewer than the fixed eye's whole grid, strictly more than
	// none (the LOD rule alone cannot drop a range at this distance).
	assert.ok(
		counts[0] < 9 * 512 && counts[0] >= 3 * 512,
		`The facing view submits its column, not the grid (${counts[0]} of 4608)`
	);
	world.dispose();
});
