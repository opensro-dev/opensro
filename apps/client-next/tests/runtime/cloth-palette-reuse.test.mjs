/*
===========================================================================

cloth-palette-reuse.test.mjs - exact CPU pose reuse and cloth consumers

A cold pose cannot reuse matrices or palette products, so it executes the
original full-materialization arithmetic on every frame. Retained cloth
instances compare complete vertex bytes and deterministic RNG consumption.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { createPaletteStreams } = await import( "../../src/engine/runtime/renderer/characters/palette-streams.ts" );
const { createClothVertices } = await import( "../../src/engine/foundation/animation/cloth-vertices.ts" );

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

Separate roots, a static child and an authored matrix exercise propagation.
================
*/
/** @returns {import("../../src/engine/contracts/character.ts").CharacterModel} */
function fixture() {
	const geometry = {
		positions: Float32Array.of( 0, 0, 0, 1, 0, 0, 1, 1, 0 ),
		normals: Float32Array.of( 0, 1, 0, 0, 1, 0, 0, 1, 0 ),
		indices: Uint32Array.of( 0, 1, 2 ),
		transform: identity(),
		joints: Uint32Array.of( 0, 1, 0, 0, 1, 2, 0, 0, 2, 3, 0, 0 ),
		weights: Float32Array.of( .75, .25, 0, 0, .5, .5, 0, 0, .25, .75, 0, 0 )
	};
	/** @type {import("../../src/engine/contracts/character.ts").CharacterPrimitive} */
	const primitive = {
		name: "cape",
		node: 0,
		image: -1,
		geometry,
		joints: [ 0, 1, 2, 3 ],
		inverseBind: Float32Array.of( ...identity(), ...identity(), ...identity(), ...identity() ),
		cloth: {
			mobility: [ 0, 1, 1 ],
			pins: [ 1, 0, 2 ],
			constraints: [ [ 0, 1, 1 ], [ 1, 2, 1 ] ],
			order: [ 1, 0 ],
			force: null,
			gravity: 1,
			gravityMobility: .3,
			windMobility: .2,
			damping: .9,
			windPeriod: 3
		}
	};
	const shifted = { ...primitive, cloth: undefined, inverseBind: primitive.inverseBind.slice() };
	shifted.inverseBind[12] = .3;
	return {
		nodes: [
			{ name: "Bip01 Spine", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
			{ name: "child", parent: 0, translation: [ 0, .5, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
			{ name: "Bone01", parent: -1, translation: [ .1, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
			{
				name: "fixed",
				parent: 2,
				translation: [ 0, 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ 1, 1, 1 ],
				matrix: [ ...identity() ]
			}
		],
		primitives: [ primitive, { ...primitive, cloth: undefined }, shifted ],
		images: [],
		clips: [ {
			name: "hold",
			duration: 2,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "STEP",
				times: Float32Array.of( 0, 1, 2 ),
				values: Float32Array.of( 0, 0, 0, -0, 2, 0, .2, 0, 0 )
			} ]
		}, {
			name: "move",
			duration: 2,
			channels: [ {
				node: 0,
				path: "rotation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1, 2 ),
				values: Float32Array.of( 0, 0, 0, 1, 0, .3, 0, .95, 0, -.2, 0, .98 )
			}, {
				node: 2,
				path: "scale",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1, 2 ),
				values: Float32Array.of( 1, 1, 1, 1.2, .8, 1, 1, 1, 1, 1 )
			} ]
		}, {
			name: "cubic",
			duration: 2,
			channels: [ {
				node: 2,
				path: "translation",
				interpolation: "CUBICSPLINE",
				times: Float32Array.of( 0, 2 ),
				values: Float32Array.of( 0, 0, 0, .1, 0, 0, .2, 0, 0, .1, 0, 0, .4, .3, 0, 0, 0, 0 )
			} ]
		}, {
			name: "rounded",
			duration: 2,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 2 ),
				values: Float32Array.of( 1, 0, 0, 1 + 2 ** -23, 0, 0 )
			} ]
		} ]
	};
}

/*
================
bytes
================
*/
function bytes( data ) {
	return new Uint8Array( data.buffer, data.byteOffset, data.byteLength );
}

/*
================
randomState

Both cloth instances receive the same sequence and expose its call count.
================
*/
function randomState() {
	const state = { calls: 0, word: 0x12345678, next };
	/*
	================
	next
	================
	*/
	function next() {
		state.calls++;
		state.word = (Math.imul( state.word, 1664525 ) + 1013904223) >>> 0;
		return state.word;
	}
	return state;
}

test("retained palettes and cloth bytes equal cold full materialization over 1200 frames", () => {
	const source = fixture(), actual = createCharacterPose( source ), bank = createPaletteStreams( source, 1 );
	const actualRandom = randomState(), expectedRandom = randomState();
	const cloth = createClothVertices( source.primitives[0], actualRandom.next );
	const oracleCloth = createClothVertices( source.primitives[0], expectedRandom.next );
	let seconds = 0, admitted = false;
	const newClip = { ...source.clips[1], name: "admitted", channels: [ { ...source.clips[1].channels[0], node: 1 } ] };
	for ( let frame = 0; frame < 1200; frame++ ) {
		if ( frame === 400 ) {
			assert.equal( actual.admitClip( newClip ), true );
			admitted = true;
		}
		const model = admitted ? { ...source, clips: [ ...source.clips, newClip ] } : source;
		const expected = createCharacterPose( model );
		const volume = Math.floor( frame / 97 ) % 5, female = Math.floor( frame / 61 ) % 2 === 0;
		actual.bodyVolume( volume, female );
		expected.bodyVolume( volume, female );
		const clip = model.clips[Math.floor( frame / 40 ) % model.clips.length].name;
		const time = frame % 31 === 0 ? 86400.125 : frame % 29 === 0 ? 0 : frame * .017;
		/** @type {import("../../src/engine/contracts/character.ts").CharacterLayer[]} */
		const layers = [ { clip, time, loop: frame % 7 !== 0, weight: 1, lane: "timed" } ];
		if ( frame % 17 < 3 ) {
			layers.push( { clip: "hold", time: .3, loop: true, weight: .35, lane: "event" } );
		}
		if ( frame % 43 === 0 ) layers.length = 0;
		actual.evaluate( "", 0, true, layers, frame % 2 === 0 );
		expected.evaluate( "", 0, true, layers );
		bank.update( [ actual ] );
		for ( let p = 0; p < source.primitives.length; p++ ) {
			const primitive = source.primitives[p];
			const a = new Float32Array( primitive.joints.length * 16 + 32 ).fill( 99 ), b = a.slice();
			actual.palette( primitive, a, 16 );
			expected.palette( primitive, b, 16 );
			assert.deepEqual( bytes( a ), bytes( b ), "palette " + p + ", frame " + frame );
			assert.deepEqual( bytes( bank.streams[p].data ), bytes( b.subarray( 16, b.length - 16 ) ) );
			a.fill( -123 );
			actual.palette( primitive, a, 16 );
			assert.deepEqual( bytes( a.subarray( 16, a.length - 16 ) ), bytes( b.subarray( 16, b.length - 16 ) ) );
		}
		for ( const node of model.nodes ) {
			assert.deepEqual( bytes( actual.socket( node.name ) ), bytes( expected.socket( node.name ) ) );
		}
		seconds += [ 0, .001, .005, .016, .049, .05, .2, 1 ][frame % 8];
		const enabled = frame % 53 < 38;
		const motion = { direction: [ Math.sin( frame ), 0, Math.cos( frame ) ], speed: frame % 4 * .3 };
		const reference = new Float32Array( source.primitives[0].joints.length * 16 );
		expected.palette( source.primitives[0], reference );
		assert.deepEqual(
			bytes( cloth.update( bank.streams[0].data, seconds, enabled, motion ) ),
			bytes( oracleCloth.update( reference, seconds, enabled, motion ) ),
			"cloth frame " + frame
		);
		assert.equal( actualRandom.calls, expectedRandom.calls, "RNG frame " + frame );
		assert.equal( actualRandom.word, expectedRandom.word );
	}
});

test("different clocks with identical Float32 samples skip quaternion normalization", t => {
	const model = fixture(), pose = createCharacterPose( model ), primitive = model.primitives[0];
	const palette = new Float32Array( primitive.joints.length * 16 );
	pose.evaluate( "rounded", .1, false );
	pose.palette( primitive, palette );
	const expected = palette.slice(), revision = pose.revision(), evaluations = pose.cpuEvaluations();
	const sqrt = t.mock.method( Math, "sqrt" );
	for ( let frame = 0; frame < 100; frame++ ) {
		assert.equal( pose.evaluate( "rounded", .1 + frame * .001 + .0001, false ), true );
		pose.palette( primitive, palette );
		assert.deepEqual( bytes( palette ), bytes( expected ) );
	}
	assert.equal( sqrt.mock.callCount(), 0 );
	assert.equal( pose.revision(), revision + 100 );
	assert.equal( pose.cpuEvaluations(), evaluations + 100 );
	pose.evaluate( "rounded", 1.5, false );
	pose.palette( primitive, palette );
	assert.ok( sqrt.mock.callCount() > 0 );
	assert.notDeepEqual( bytes( palette ), bytes( expected ) );
});

test("a signed-zero-only sample change invalidates retained local matrices", () => {
	const model = fixture(),
		clip = {
			...model.clips[0],
			name: "zero",
			channels: [ { ...model.clips[0].channels[0], values: Float32Array.of( 0, 0, 0, -0, 0, 0, 0, 0, 0 ) } ]
		};
	const pose = createCharacterPose( { ...model, clips: [ clip ] } );
	pose.evaluate( "zero", .5, false );
	const before = pose.socket( model.nodes[0].name );
	assert.ok( before );
	assert.ok( Object.is( before[12], 0 ) );
	pose.evaluate( "zero", 1.5, false );
	const after = pose.socket( model.nodes[0].name );
	assert.ok( after );
	assert.ok( Object.is( after[12], -0 ) );
	assert.notDeepEqual( bytes( before ), bytes( after ) );
});
