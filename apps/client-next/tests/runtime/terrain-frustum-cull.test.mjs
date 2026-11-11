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
const { viewProjection, prepareViewFrustum, visibleFrustumSphere } = await load(
	"src/engine/foundation/rendering/world-math.ts"
);
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
ranges' centres and radii are the real cell bounds the exactness walk
below tests against each view's frustum.
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

test("the terrain choice keys on the eye cell alone; the view never re-chooses (#3177)", () => {
	const world = createWorldRenderer();
	const owner = geometryOwner();
	// Every index upload the world performs, in order: the choice a frame
	// submits is the last of these for its draw.
	const submitted = [];
	const geometry = {
		...owner.commands,
		updateIndices( draw, indices ) {
			submitted.push( new Uint32Array( indices ) );
			return owner.commands.updateIndices( draw, indices );
		}
	};
	const textures = { upload: () => ({}), release() {} };
	const group = terrainGroup();
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
		groups: [ group ]
	};
	world.scene( scene );
	const views = [
		// The eye stays over the grid's centre cell for every view, so the
		// LOD rule alone cannot drop a range: under the old cell-and-view
		// choice only the frustum could. A narrow fov quarter-turn faces
		// one 320-metre column of the 3x3 grid.
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
	// Every view submits the whole grid the eye cell selects (9 cells of
	// 512 triangles): nothing about the choice follows the view any more.
	assert.deepEqual(
		counts,
		[ 4608, 4608, 4608 ],
		`Every view of one cell submits its whole choice: ${counts.join( ", " )}`
	);
	// One upload carried the choice; turning the camera inside the cell
	// uploaded nothing more (the old cell-and-view choice uploaded per view).
	assert.equal( submitted.length, 1, `${submitted.length} index uploads for three views of one cell` );
	// Exactness (PROFILING.md section 4, GPU-move form): what each frame
	// shows inside the frustum is unchanged. Every range the old
	// cell-and-view rule would have shown - at the cell's LOD, sphere
	// against the view frustum - is present in the submission, whole.
	const chosen = new Set( submitted[0] );
	for ( const camera of views ) {
		const frustum = prepareViewFrustum( viewProjection( camera, 1.6 ) );
		for ( const range of group.ranges ) {
			if (
				!visibleFrustumSphere( frustum, range.center[0], range.center[1], range.center[2], range.radius )
			) continue;
			for ( let i = range.indexStart; i < range.indexStart + range.indexCount; i++ ) {
				assert.ok(
					chosen.has( group.geometry.indices[i] ),
					`view ${camera.target}: a range the old rule showed is missing index ${i}`
				);
			}
		}
	}
	// Crossing into the next eye cell re-chooses and re-uploads, once,
	// even though this grid's ranges sit at LOD 0 from both cells and
	// the packed indices come out identical.
	world.camera( { eye: [ 800, 60, 480 ], target: [ 800, 0, 160 ], originRegion: 257, fov: .4, near: 1, far: 5000 } );
	world.prepare( geometry, textures, 1.6, 1, 192, 120, 3500, false, false );
	assert.equal( submitted.length, 2, `${submitted.length} index uploads after crossing one cell line` );
	assert.deepEqual( submitted[1], submitted[0], "both cells pack the same ranges here" );
	world.dispose();
});
