/*
===========================================================================

world-palette-gate.test.mjs - exact rigid world palette publication

Compares gated publication with the unconditional palette copy used before
A5. Sampling, revisions and sockets must remain identical on every frame.
The renderer test checks actual draw palettes and admission invalidation.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { createWorldRenderer } = await import( "../../src/engine/runtime/renderer/world/world.ts" );

const FRAME_COUNT = 2400;
const PALETTE_OFFSET = 16;
const CUBIC_VALUE_COUNT = 27;
const CUBIC_MIDDLE_X = 12;
const CLOCK_WRAP_MS = 0x1_0000_0000;
const CLOCK_WRAP_LEAD_MS = 256;
const CLOCK_WRAP_FRAMES = 512;
const MS_PER_SECOND = 1000;
const SEEK_FRAMES = 600;
const PROFILE_FPS = 183;

/*
================
identity
================
*/
function identity() {
	return Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
}

/*
================
fixture
================
*/
function fixture() {
	/** @type {import("../../src/engine/contracts/scene.ts").WorldGroup} */
	const group = {
		id: "gate",
		center: [ 0, 0, 0 ],
		radius: 100,
		material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true },
		animation: { model: "gate", primitive: 0, clip: "step" },
		geometry: {
			positions: Float32Array.of( -10, -10, 0, 10, -10, 0, 0, 10, 0 ),
			indices: Uint32Array.of( 0, 1, 2 ),
			transform: identity(),
			instances: identity(),
			normals: new Float32Array( 9 ),
			uvs: new Float32Array( 6 ),
			bones: identity(),
			joints: new Uint32Array( 12 ),
			weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 )
		}
	};
	/** @type {import("../../src/engine/contracts/character.ts").CharacterClip[]} */
	const clips = [ { name: "empty", duration: 1, channels: [] } ];
	/** @type {import("../../src/engine/contracts/character.ts").CharacterModel} */
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		primitives: [ {
			name: "root",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			geometry: group.geometry,
			image: -1
		} ],
		images: [],
		clips
	};
	const cubicValues = new Float32Array( CUBIC_VALUE_COUNT );
	cubicValues[CUBIC_MIDDLE_X] = 10;
	for ( const interpolation of /** @type {const} */ ([ "STEP", "LINEAR", "CUBICSPLINE" ]) ) {
		clips.push( {
			name: interpolation.toLowerCase(),
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation,
				times: Float32Array.of( 0, .5, 1 ),
				values: interpolation === "CUBICSPLINE" ?
					cubicValues :
					Float32Array.of( 0, 0, 0, 10, 0, 0, 0, 0, 0 )
			} ]
		} );
	}
	return { group, model };
}

/*
================
words
================
*/
function words( data ) {
	return new Uint32Array( data.buffer, data.byteOffset, data.length );
}

/*
================
frameSeconds

Exercise the uint32 millisecond clock rolling over, then repeated seeks.
================
*/
function frameSeconds( frame ) {
	if ( frame < CLOCK_WRAP_FRAMES ) {
		return ((CLOCK_WRAP_MS - CLOCK_WRAP_LEAD_MS + frame) >>> 0) / MS_PER_SECOND;
	}
	return frame % SEEK_FRAMES / PROFILE_FPS;
}

test("gated palettes match unconditional copies across wraps, rest poses and completed one-shots", () => {
	const { model } = fixture(), primitive = model.primitives[0];
	for ( const name of [ "", "empty", "step", "linear", "cubicspline" ] ) {
		for ( const loop of [ false, true ] ) {
			let actual = createCharacterPose( model ), expected = createCharacterPose( model );
			const a = new Float32Array( 48 ).fill( 123 ), b = a.slice();
			let copies = 0;
			for ( let frame = 0; frame < FRAME_COUNT; frame++ ) {
				// Return to zero repeatedly, and spend long runs beyond one-shot completion.
				const seconds = frameSeconds( frame );
				if ( frame % 401 === 0 ) {
					actual = createCharacterPose( model );
					expected = createCharacterPose( model );
					a.fill( 123 );
					b.fill( 123 );
				}
				assert.equal( actual.evaluate( name, seconds, loop ), expected.evaluate( name, seconds, loop ) );
				expected.palette( primitive, b, PALETTE_OFFSET );
				const changed = actual.palette( primitive, a, PALETTE_OFFSET, true );
				assert.equal( typeof changed, "boolean" );
				if ( changed ) copies++;
				assert.deepEqual( words( a ), words( b ), `${name}, loop ${loop}, frame ${frame}` );
				assert.equal( actual.revision(), expected.revision() );
				assert.equal( actual.cpuEvaluations(), expected.cpuEvaluations() );
				assert.deepEqual( actual.socket( "root" ), expected.socket( "root" ) );
			}
			if ( name === "" || name === "empty" || name === "step" || !loop ) {
				assert.ok( copies < FRAME_COUNT / 2, `${name}: ${copies} copies` );
			}
		}
	}
});

test("palette equality distinguishes signed zero, keeps NaN copies and preserves bounds errors", () => {
	const { model } = fixture(), primitive = model.primitives[0], pose = createCharacterPose( model );
	pose.evaluate( "empty", 0 );
	const out = new Float32Array( 16 );
	pose.palette( primitive, out );
	assert.equal( pose.palette( primitive, out, 0, true ), false );
	assert.equal( pose.palette( primitive, out, 0, false ), true, "cloth/default callers keep unconditional copies" );
	const zero = out.findIndex( value => Object.is( value, 0 ) );
	assert.ok( zero >= 0 );
	out[zero] = -0;
	assert.equal( pose.palette( primitive, out, 0, true ), true );
	assert.equal( Object.is( out[zero], 0 ), true );
	assert.throws( () => pose.palette( primitive, out, 1, true ), RangeError );
	assert.throws( () => pose.palette( primitive, out, -1, true ), RangeError );
	const nanPose = createCharacterPose( {
		...model,
		nodes: [ { ...model.nodes[0], translation: [ NaN, 0, 0 ] } ]
	} );
	nanPose.evaluate( "empty", 0 );
	nanPose.palette( primitive, out );
	assert.ok( out.some( Number.isNaN ) );
	assert.equal( nanPose.palette( primitive, out, 0, true ), true );
});

test("rigid world draws retain exact palettes across clock wrap and readmission", () => {
	const { group, model } = fixture(), oracle = createCharacterPose( model ), expected = identity();
	const scene = { id: "gate", originRegion: 0, groups: [ group ], models: { gate: model }, warnings: [] };
	const world = createWorldRenderer();
	let updates = 0;
	/** @type {any} */
	const gpu = {
		upload( geometry ) {
			return { palette: geometry.bones.slice() };
		},
		release() {},
		updateBones( draw, bones ) {
			draw.palette.set( bones );
			updates++;
		}
	};
	/** @type {any} */
	const textures = {};
	world.scene( scene );
	world.camera( { eye: [ 0, 0, 80 ], target: [ 0, 0, 0 ], fov: 1, near: 1, far: 3500 } );
	for ( let frame = 0; frame < FRAME_COUNT; frame++ ) {
		const seconds = frameSeconds( frame ), before = updates;
		if ( frame === 801 ) world.invalidate();
		if ( frame === 1601 ) world.scene( { ...scene, id: "replacement" } );
		const prepared = world.prepare( gpu, textures, 1, seconds );
		oracle.evaluate( "step", seconds );
		oracle.palette( model.primitives[0], expected );
		assert.equal( prepared.draws.length, 1 );
		/** @type {any} */
		const draw = prepared.draws[0];
		assert.deepEqual( words( draw.palette ), words( expected ), `frame ${frame}` );
		if ( frame === 0 || frame === 801 || frame === 1601 ) {
			assert.ok( updates > before, "a new draw must receive its palette even when bytes already match" );
		}
	}
	assert.ok( updates < FRAME_COUNT / 20, `only STEP transitions need uploads: ${updates}` );
	world.dispose( gpu, null );
});
