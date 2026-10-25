/*
===========================================================================

debug-render-modes.test.mjs - the render family's applied gates

690C40 ids 151-155 negate one SWorld graphic option each (ground, sky,
mobj, char, lod; graphic-options.ts). The world renderer gates at draw
submission, so picking and the terrain selections never notice; a /lod
flip re-chooses every terrain cell at LOD 0; the character renderer
submits no draws while its option stands clear. The console-side flips
live in debug-camera.test.mjs.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createWorldRenderer } = await import( "../../src/engine/runtime/renderer/world/world.ts" );
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const {
	GRAPHIC_OPTION_CHARACTERS,
	GRAPHIC_OPTION_GROUND,
	GRAPHIC_OPTION_LEVEL_OF_DETAIL,
	GRAPHIC_OPTION_MAP_OBJECTS,
	GRAPHIC_OPTION_SKY,
	GRAPHIC_OPTION_WIRE_FRAME
} = await import( "../../src/engine/foundation/rendering/graphic-options.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
/*
================
group
================
*/
/**
 * @param {string} id
 * @param {import("../../src/engine/contracts/scene.ts").WorldMaterial} [material]
 * @returns {import("../../src/engine/contracts/scene.ts").WorldGroup}
 */
function group(
	id,
	material = { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true }
) {
	return {
		id,
		center: [ 0, 0, 0 ],
		radius: 100,
		material,
		geometry: {
			positions: new Float32Array( [
				-100,
				-100,
				0,
				100,
				-100,
				0,
				0,
				100,
				0,
				-100,
				100,
				0,
				100,
				100,
				0,
				0,
				-100,
				0
			] ),
			normals: new Float32Array( 18 ),
			uvs: new Float32Array( 12 ),
			indices: new Uint32Array( [ 0, 1, 2, 3, 4, 5 ] ),
			instances: identity(),
			transform: identity()
		}
	};
}
/*
================
terrainRange
================
*/
/** @returns {import("../../src/engine/contracts/scene.ts").TerrainRange} */
function terrainRange( cell, lod, indexStart, vertexStart ) {
	return {
		cell,
		lod,
		indexStart,
		indexCount: 3,
		vertexStart,
		vertexCount: 289,
		center: [ 10, 0, 10 ],
		radius: 20,
		heights: Array( 289 ).fill( 4 )
	};
}
/*
================
terrainGroup

Two cells of real 17x17 height grid (admission derives the seam plan
from the vertices): cell [0, 0] at LOD 0, cell [5, 0] at LOD 1.
================
*/
/** @returns {import("../../src/engine/contracts/scene.ts").WorldGroup} */
function terrainGroup() {
	const heights = 4, cells = [ 0, 1600 ], positions = new Float32Array( 289 * cells.length * 3 );
	for ( let cell = 0; cell < cells.length; cell++ ) {
		for ( let v = 0; v < 289; v++ ) {
			const at = (cell * 289 + v) * 3;
			positions[at] = cells[cell] + v % 17 * 20;
			positions[at + 1] = heights;
			positions[at + 2] = Math.floor( v / 17 ) * 20;
		}
	}
	return {
		id: "terrain",
		center: [ 800, heights, 160 ],
		radius: 1200,
		material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true },
		geometry: {
			positions,
			normals: new Float32Array( positions.length ),
			uvs: new Float32Array( 289 * cells.length * 2 ),
			indices: new Uint32Array( [ 0, 1, 2, 289, 290, 291 ] ),
			instances: identity(),
			transform: identity()
		},
		ranges: [ terrainRange( [ 0, 0 ], 0, 0, 0 ), terrainRange( [ 5, 0 ], 1, 3, 289 ) ]
	};
}
/*
================
worldFixture

One sky group, one terrain group and one plain prop under a standing
camera, with the index uploads recorded per terrain re-choice.
================
*/
function worldFixture() {
	const indexUploads = [];
	const geometry = /** @type {any} */ ({
		upload: () => ({}),
		release() {},
		/*
		================
		updateIndices
		================
		*/
		updateIndices( _draw, indices ) {
			indexUploads.push( indices.length );
		},
		updatePositions() {},
		updateInstances: draw => draw,
		updateBones() {}
	});
	const textures = /** @type {any} */ ({ upload: () => ({}), release() {} });
	const sky = group( "sky", { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true, sky: 1 } );
	const terrain = terrainGroup();
	const prop = group( "prop" );
	const world = createWorldRenderer();
	world.camera( {
		eye: [ 0, 0, 80 ],
		target: [ 0, 0, 0 ],
		follow: { yaw: 0, pitch: 0, distance: 80 },
		fov: 1,
		near: 1,
		far: 3500
	} );
	world.scene( {
		id: "render-modes",
		originRegion: 0,
		groups: [ sky, terrain, prop ],
		warnings: [],
		environment: { startTimeOfDay: .5, ratePerSecond: 0, tracks: {} }
	} );
	/*
	================
	prepare
	================
	*/
	function prepare() {
		return world.prepare( geometry, textures, 1 );
	}
	return { world, prepare, indexUploads };
}

test("ground, sky and mobj keep their groups out of the frame until they stand again", () => {
	const f = worldFixture();
	let frame = f.prepare();
	assert.equal( frame.draws.length, 3, "sky, terrain and prop all draw" );
	assert.equal( frame.sky, true );
	assert.deepEqual( f.indexUploads, [ 6 ], "both terrain ranges chosen at their distance bands" );
	// A stationary camera between flips: the early-out must not serve
	// the frame composed under the old options.
	f.world.setRenderMode( GRAPHIC_OPTION_GROUND, false );
	frame = f.prepare();
	assert.equal( frame.draws.length, 2, "the terrain draw is gone, sky and prop stay" );
	f.world.setRenderMode( GRAPHIC_OPTION_SKY, false );
	frame = f.prepare();
	assert.equal( frame.draws.length, 1, "only the prop stays" );
	assert.equal( frame.sky, false, "the device sky goes with the sky pass" );
	f.world.setRenderMode( GRAPHIC_OPTION_MAP_OBJECTS, false );
	frame = f.prepare();
	assert.equal( frame.draws.length, 0 );
	// Options this renderer does not own are ignored whole.
	f.world.setRenderMode( GRAPHIC_OPTION_WIRE_FRAME, true );
	f.world.setRenderMode( GRAPHIC_OPTION_CHARACTERS, false );
	frame = f.prepare();
	assert.equal( frame.draws.length, 0, "wireframe and characters changed nothing here" );
	f.world.setRenderMode( GRAPHIC_OPTION_GROUND, true );
	f.world.setRenderMode( GRAPHIC_OPTION_SKY, true );
	f.world.setRenderMode( GRAPHIC_OPTION_MAP_OBJECTS, true );
	frame = f.prepare();
	assert.equal( frame.draws.length, 3, "standing the options again restores every draw" );
	assert.equal( frame.sky, true );
});

test("/lod forces every terrain cell to LOD 0 until it stands again", () => {
	const f = worldFixture();
	f.prepare();
	assert.deepEqual( f.indexUploads, [ 6 ], "cells at LOD 0 and LOD 1 by distance" );
	f.world.setRenderMode( GRAPHIC_OPTION_LEVEL_OF_DETAIL, false );
	f.prepare();
	assert.equal( f.indexUploads.at( -1 ), 3, "only the LOD 0 range is chosen now" );
	f.world.setRenderMode( GRAPHIC_OPTION_LEVEL_OF_DETAIL, true );
	f.prepare();
	assert.equal( f.indexUploads.at( -1 ), 6, "the distance bands choose again" );
});

test("/char submits no character draws while its option stands clear", () => {
	const owner = createCharacters();
	const geometry = /** @type {any} */ ({
		upload: () => ({}),
		release() {},
		updatePositions() {},
		updateIndices() {},
		updateInstances: ( /** @type {any} */ draw ) => draw,
		updateTransform() {},
		writeVertices() {},
		updateMaterialColors() {},
		updateTextureTransform() {},
		updateTextureFactor() {},
		updateEquipmentGlow() {},
		presentParticles() {}
	});
	const transform = identity();
	owner.model(
		"m",
		/** @type {any} */ ({
			nodes: [ {
				name: "root",
				parent: -1,
				translation: [ 0, 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ 1, 1, 1 ]
			} ],
			images: [],
			clips: [ { name: "stand", duration: 1, channels: [] } ],
			primitives: [ {
				name: "body",
				node: 0,
				image: -1,
				joints: [ 0 ],
				inverseBind: transform,
				geometry: {
					positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
					indices: new Uint32Array( [ 0, 1, 2 ] ),
					transform
				}
			} ]
		}),
		[]
	);
	owner.actors( [ {
		gid: 1,
		model: "m",
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1,
		pose: { regionId: 257, x: 0, y: 0, z: 10, yaw: radians( 0 ) }
	} ] );
	const images = /** @type {any} */ ({ upload: () => ({}), release() {} });
	const shown = owner.prepare( geometry, images, 257 );
	assert.ok( shown.length > 0, "the actor draws while the option stands" );
	owner.setRenderMode( GRAPHIC_OPTION_GROUND, false );
	assert.ok( owner.prepare( geometry, images, 257 ).length > 0, "another owner's option changes nothing" );
	owner.setRenderMode( GRAPHIC_OPTION_CHARACTERS, false );
	assert.deepEqual( owner.prepare( geometry, images, 257 ), [], "no draws while /char stands clear" );
	owner.setRenderMode( GRAPHIC_OPTION_CHARACTERS, true );
	assert.ok( owner.prepare( geometry, images, 257 ).length > 0, "the draws return with the option" );
	owner.dispose( geometry, null );
});
