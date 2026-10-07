/*
===========================================================================

shadow-topology-world.test.mjs - receiver cache terrain and point lifetimes

Exercise the shipping world owner with submitted terrain, movement inside
one cell, terrain replacement, empty demand and device recovery.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createWorldRenderer } = await import( "../../src/engine/runtime/renderer/world/world.ts" );
const { characterShadowReceiver, shadowProjection } = await import(
	"../../src/engine/foundation/rendering/character-shadow.ts"
);
const { terrainCellKey } = await import( "../../src/engine/foundation/rendering/terrain-interaction.ts" );
const { identity } = await import( "../../src/engine/foundation/rendering/world-math.ts" );

/*
================
terrainScene
================
*/
/** @returns {import("../../src/engine/contracts/scene.ts").WorldScene} */
function terrainScene( height ) {
	const geometry = {
		positions: Float32Array.of( 0, height, 0, 0, height, 320, 320, height, 320, 320, height, 0 ),
		normals: new Float32Array( 12 ),
		uvs: new Float32Array( 8 ),
		indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
		instances: identity(),
		transform: identity()
	};
	return {
		id: "shadow-terrain-" + height,
		originRegion: 1,
		warnings: [],
		groups: [ {
			id: "terrain",
			center: [ 160, height, 160 ],
			radius: 240,
			material: { terrain: true, color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true },
			geometry,
			ranges: [ {
				cell: [ 0, 0 ],
				lod: 0,
				indexStart: 0,
				indexCount: 6,
				vertexStart: 0,
				vertexCount: 4,
				center: [ 160, height, 160 ],
				radius: 240,
				heights: Array( 289 ).fill( height )
			} ]
		} ]
	};
}

/*
================
World cache lifecycle
================
*/
test("receivers follow movement, terrain replacement, stitched seams and device recovery", () => {
	const world = createWorldRenderer();
	let requests = [];
	// This projection test records receiver requests, so GPU handles are opaque
	// and only the commands reached by terrain admission are implemented.
	const geometry =
		/** @type {import("../../src/engine/runtime/renderer/internal/gpu-contract.ts").GeometryCommands} */ (
			/** @type {unknown} */ ({
				upload: data => ({ data }),
				release() {},
				updateIndices() {},
				updatePositions() {},
				characterShadows( rows ) {
					requests = rows;
					return [];
				}
			})
		);
	const textures = /** @type {import("../../src/engine/runtime/renderer/internal/gpu-contract.ts").ImageCommands} */ (
		/** @type {unknown} */ ({ upload: () => ({}), release() {} })
	);
	world.camera( { eye: [ 160, 200, 400 ], target: [ 160, 0, 160 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	try {
		for ( const height of [ 0, 17 ] ) {
			const scene = terrainScene( height );
			world.scene( scene );
			assert.ok( world.prepare( geometry, textures, 1, 0 ).draws.length );
			const data = scene.groups[0].geometry;
			const surfaces = new Map( [ [ terrainCellKey( 0, 0 ), [ {
				positions: data.positions,
				indices: data.indices,
				start: 0,
				count: 6
			} ] ] ] );
			for ( const x of [ 140.01, 147, 159.99, 160.01, 159.99 ] ) {
				const projection = shadowProjection( [ x, height, 151 ], 60 );
				const candidates = [ { projection, parts: [] } ];
				world.characterShadows( candidates, geometry, textures );
				assert.equal( requests.length, 1 );
				assert.deepEqual(
					requests[0].receiver,
					characterShadowReceiver( new Map(), projection, undefined, surfaces )
				);
				const retained = requests[0].receiver;
				world.characterShadows( candidates, geometry, textures );
				assert.equal( requests[0].receiver, retained );
			}
			world.characterShadows( [], geometry, textures );
			assert.equal( requests.length, 0 );
			world.invalidate();
			world.prepare( geometry, textures, 1, 1 );
			const projection = shadowProjection( [ 147, height, 151 ], 60 );
			world.characterShadows( [ { projection, parts: [] } ], geometry, textures );
			assert.deepEqual(
				requests[0].receiver,
				characterShadowReceiver( new Map(), projection, undefined, surfaces )
			);
		}
		const base = terrainScene( 0 ), original = base.groups[0];
		const heights = Array( 289 ).fill( 0 );
		heights[1] = 20;
		const positions = Float32Array.of( 0, 0, 0, 20, 20, 0, 0, 0, 320 );
		const indices = Uint32Array.of( 0, 2, 1 );
		const stitched = {
			...original,
			geometry: {
				...original.geometry,
				positions,
				indices,
				normals: new Float32Array( 9 ),
				uvs: new Float32Array( 6 )
			},
			ranges: [ {
				...defined( original.ranges )[0],
				heights,
				seamVertices: Uint32Array.of( 1, 1 ),
				indexCount: 3,
				vertexCount: 3
			} ]
		};
		world.scene( { ...base, id: "seam-change", groups: [ stitched ] } );
		const projection = shadowProjection( [ 15, 0, 10 ], 60 );
		let previous;
		for ( const eyeZ of [ 1000, 1290 ] ) {
			world.camera( { eye: [ 10, 200, eyeZ ], target: [ 10, 0, 10 ], near: 1, far: 2000, fov: Math.PI / 3 } );
			assert.ok( world.prepare( geometry, textures, 1, 2 ).draws.length );
			// scene() copies its input before stitching; the caller's source stays
			// unchanged. The expected submitted triangle follows the native seam.
			const expectedPositions = positions.slice();
			expectedPositions[4] = eyeZ === 1000 ? 20 : 0;
			world.characterShadows( [ { projection, parts: [] } ], geometry, textures );
			const expected = characterShadowReceiver(
				new Map(),
				projection,
				undefined,
				new Map( [ [
					terrainCellKey( 0, 0 ),
					[ { positions: expectedPositions, indices, start: 0, count: 3 } ]
				] ] )
			);
			assert.deepEqual( requests[0].receiver, expected );
			assert.notEqual( requests[0].receiver, previous, "an unchanged point cannot retain old stitched heights" );
			previous = requests[0].receiver;
		}
	} finally {
		world.dispose( geometry, textures );
	}
});
