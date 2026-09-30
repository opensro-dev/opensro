/*
===========================================================================

effect-billboard.test.mjs - tests for effect-billboard.ts, world-math.ts,
pose-math.ts, characters.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const effectBillboard = await import( "../../src/engine/foundation/rendering/effect-billboard.ts" );
const { faceEffectPlate, faceEffectMesh } = effectBillboard;
const { identity, placement, viewProjection, cameraBasis } = await import(
	"../../src/engine/foundation/rendering/world-math.ts"
);
const { multiply } = await import( "../../src/engine/foundation/math/pose-math.ts" );
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const camera = { eye: [ 10, 7, -20 ], target: [ 0, 0, 0 ], fov: 1, near: .1, far: 100 };

test("instances of one effect retain independent material ages in one reused draw", () => {
	const owner = createCharacters();
	let uploads = 0, updates = 0, last;
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		clips: [],
		images: [],
		primitives: [ {
			name: "plate",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			materialFrames: {
				fps: 1,
				colors: new Float32Array( [ 1, 0, 0, 1, 0, 1, 0, 0 ] ),
				windows: new Float32Array( [ .5, 1, 0, 0, .5, 1, .5, 0 ] )
			},
			geometry: {
				positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity()
			}
		} ]
	};
	owner.model( "effect", model, [] );
	const actors = [ 0, 1 ].map( ( time, i ) => ({
		gid: i + 1,
		model: "effect",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		clip: "",
		time,
		loop: false,
		scale: 1
	}) );
	const gpu = {
			upload() {
				uploads++;
				return {};
			},
			updateInstances( draw, instances, opacity, appearance ) {
				updates++;
				last = appearance.slice();
				return draw;
			},
			updateBones() {},
			release() {}
		},
		images = {
			upload() {
				return {};
			},
			release() {}
		};
	owner.actors( actors );
	owner.prepare( gpu, images, 257 );
	assert.equal( uploads, 1 );
	assert.deepEqual( [ ...last ], [ 1, 0, 0, 1, .5, 1, 0, 0, 0, 1, 0, 0, .5, 1, .5, 0 ] );
	owner.prepare( gpu, images, 257 );
	assert.equal( updates, 1 );
	owner.actors( actors.map( actor => ({ ...actor, time: actor.gid === 1 ? .5 : 1 }) ) );
	owner.prepare( gpu, images, 257 );
	assert.equal( uploads, 1 );
	assert.equal( updates, 2 );
	assert.deepEqual( [ ...defined( last ).slice( 0, 4 ) ], [ .5, .5, 0, .5 ] );
	owner.invalidate();
	owner.prepare( gpu, images, 257 );
	assert.equal( uploads, 2 );
	assert.deepEqual( [ ...defined( last ).slice( 8, 12 ) ], [ 0, 1, 0, 0 ] );
	owner.dispose( gpu, images );
});
test("camera-facing plates preserve attachment translation and authored scale under actor rotation", () => {
	const instance = placement( 257, 257, 1, 2, 3, 1.3 );
	for ( let i = 0; i < 12; i++ ) instance[i] *= 2;
	const palette = identity();
	palette[0] = 4;
	palette[5] = -3;
	palette[10] = 1;
	palette[12] = 7;
	palette[13] = 8;
	palette[14] = 9;
	const before = new Float32Array( 16 );
	multiply( instance, palette, before );
	faceEffectPlate( palette, 0, instance, viewProjection( camera, 1.5 ) );
	const world = new Float32Array( 16 );
	multiply( instance, palette, world );
	assert.deepEqual( [ ...world.slice( 12 ) ], [ ...before.slice( 12 ) ] );
	const axes = cameraBasis( camera );
	for ( const [col, axis, scale] of [ [ 0, axes.right, 8 ], [ 1, axes.up, -6 ], [ 2, axes.forward, 2 ] ] ) {
		for ( let i = 0; i < 3; i++ ) {
			assert.ok( Math.abs( world[col * 4 + i] - axis[i] * scale ) < 1e-5 );
		}
	}
});
test("retained billboard draw changes on camera rotation and reuses an unchanged view", () => {
	const owner = createCharacters(), palette = identity();
	let draws = 0, bones = 0, instances = 0, last;
	owner.model( "plate", {
		nodes: [ {
			name: "plate",
			parent: -1,
			translation: [ 0, 0, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 4, 3, 1 ]
		} ],
		clips: [],
		images: [],
		primitives: [ {
			name: "plate",
			billboard: "camera",
			node: 0,
			joints: [ 0 ],
			inverseBind: palette,
			image: -1,
			geometry: {
				positions: new Float32Array( [ -.5, .5, 0, .5, .5, 0, .5, -.5, 0, -.5, -.5, 0 ] ),
				indices: new Uint32Array( [ 0, 1, 2, 0, 2, 3 ] ),
				transform: identity()
			}
		} ]
	}, [] );
	owner.actors( [ {
		gid: 1,
		model: "plate",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 1 },
		clip: "",
		time: 0,
		loop: false,
		scale: 1
	} ] );
	const gpu = {
			upload( data ) {
				draws++;
				last = data.bones.slice();
				return {};
			},
			updateInstances( draw ) {
				instances++;
				return draw;
			},
			updateBones( draw, data ) {
				bones++;
				last = data.slice();
			},
			release() {}
		},
		images = {
			upload() {
				return {};
			},
			release() {}
		};
	const first = viewProjection( camera, 1 );
	owner.prepare( gpu, images, 257, first );
	const initial = defined( last ).slice();
	owner.prepare( gpu, images, 257, first );
	assert.equal( draws, 1 );
	assert.equal( bones, 0 );
	assert.equal( instances, 0 );
	owner.prepare( gpu, images, 257, viewProjection( { ...camera, eye: [ -10, 7, -20 ] }, 1 ) );
	assert.equal( draws, 1 );
	assert.equal( bones, 1 );
	assert.notDeepEqual( last, initial );
	owner.dispose( gpu, images );
});
test("faceEffectMesh handles mesh billboard orientations and axial Y constraint", () => {
	const instance = placement( 257, 257, 0, 0, 0, 0 );
	const palette = identity();
	palette[0] = 2;
	palette[5] = 3;
	palette[10] = 4;
	faceEffectMesh( palette, 0, instance, viewProjection( camera, 1.5 ), "camera" );
	const axes = cameraBasis( camera );
	for ( let i = 0; i < 3; i++ ) {
		assert.ok( Math.abs( palette[i] - axes.right[i] * 2 ) < 1e-5 );
		assert.ok( Math.abs( palette[4 + i] - axes.up[i] * 3 ) < 1e-5 );
		assert.ok( Math.abs( palette[8 + i] - axes.forward[i] * 4 ) < 1e-5 );
	}
	const yPalette = identity();
	yPalette[0] = 2;
	yPalette[5] = 3;
	yPalette[10] = 4;
	faceEffectMesh( yPalette, 0, instance, viewProjection( camera, 1.5 ), "y" );
	assert.ok( Math.abs( yPalette[4] ) < 1e-5 );
	assert.ok( Math.abs( yPalette[5] - 3 ) < 1e-5 );
	assert.ok( Math.abs( yPalette[6] ) < 1e-5 );
	const vPalette = identity();
	vPalette[0] = 2;
	vPalette[5] = 3;
	vPalette[10] = 4;
	faceEffectMesh( vPalette, 0, instance, viewProjection( camera, 1.5 ), "v" );
	assert.ok( Math.abs( Math.hypot( vPalette[0], vPalette[1], vPalette[2] ) - 2 ) < 1e-5 );
	assert.ok( Math.abs( Math.hypot( vPalette[4], vPalette[5], vPalette[6] ) - 3 ) < 1e-5 );
	assert.ok( Math.abs( Math.hypot( vPalette[8], vPalette[9], vPalette[10] ) - 4 ) < 1e-5 );
	const degenView = identity();
	const degenPalette = identity();
	faceEffectMesh( degenPalette, 0, instance, degenView, "y" );
	assert.ok( Number.isFinite( degenPalette[0] ) );
	const topDownView = new Float32Array( [ 1, 0, 0, 0, 0, 0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1 ] );
	const topDownPalette = identity();
	faceEffectMesh( topDownPalette, 0, instance, topDownView, "y" );
	assert.ok( Number.isFinite( topDownPalette[0] ) );
	const zeroView = new Float32Array( 16 );
	const vZeroPalette = identity();
	faceEffectMesh( vZeroPalette, 0, instance, zeroView, "v" );
	assert.ok( Number.isFinite( vZeroPalette[0] ) );
});

test("ViewVBillboard follows velocity, never the camera (CEFEffect_Render b1556c)", () => {
	const { velocityBasis } = effectBillboard;
	const view = viewProjection( camera, 1.5 ), instance = identity();
	// A ring with no velocity keeps its authored orientation on world axes,
	// wherever the camera is: flat rings stay flat.
	for ( const velocity of [ undefined, [ 0, 0, 0 ], [ 0, 5, 0 ] ] ) {
		const palette = identity();
		palette[0] = 2;
		palette[5] = 3;
		palette[10] = 4;
		faceEffectMesh( palette, 0, instance, view, "v", velocity );
		assert.deepEqual(
			Array.from( palette.slice( 0, 11 ) ).map( v => Math.round( v * 1e5 ) / 1e5 ),
			[ 2, 0, 0, 0, 0, 3, 0, 0, 0, 0, 4 ],
			`velocity ${JSON.stringify( velocity )}`
		);
	}
	// A still element keeps its own rotation (here 90 degrees about X, a ring
	// stood up by SetRotation) on world axes: the instance's yaw drops out.
	const tilted = identity();
	tilted[5] = 0;
	tilted[6] = 1;
	tilted[9] = -1;
	tilted[10] = 0;
	const yawed = identity();
	yawed[0] = 0;
	yawed[2] = -1;
	yawed[8] = 1;
	yawed[10] = 0;
	faceEffectMesh( tilted, 0, yawed, view, "v" );
	const world = new Float32Array( 16 );
	for ( let col = 0; col < 3; col++ ) {
		for ( let row = 0; row < 3; row++ ) {
			for ( let k = 0; k < 3; k++ ) world[col * 4 + row] += yawed[k * 4 + row] * tilted[col * 4 + k];
		}
	}
	assert.deepEqual(
		[ world[0], world[1], world[2], world[4], world[5], world[6], world[8], world[9], world[10] ].map( v =>
			Math.round( v * 1e5 ) / 1e5 + 0
		),
		[ 1, 0, 0, 0, 0, 1, 0, -1, 0 ]
	);
	// Moving along +X: Y follows v, Z = normalize(v x up), X = normalize(v x Z).
	const [x, y, z] = velocityBasis( [ 7, 0, 0 ] );
	assert.deepEqual( y, [ 1, 0, 0 ] );
	assert.deepEqual( z.map( v => Math.round( v * 1e6 ) / 1e6 + 0 ), [ 0, 0, 1 ] );
	assert.deepEqual( x.map( v => Math.round( v * 1e6 ) / 1e6 + 0 ), [ 0, -1, 0 ] );
	const moving = identity();
	faceEffectMesh( moving, 0, instance, view, "v", [ 7, 0, 0 ] );
	assert.ok( Math.abs( moving[4] - 1 ) < 1e-6 && Math.abs( moving[5] ) < 1e-6, "element Y turns onto the velocity" );
});
