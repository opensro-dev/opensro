/*
===========================================================================

pose-clip-admission.test.mjs - append clips without retiring sampled poses

Compare retained owners with cold owners of the expanded immutable catalog.
Admission must preserve held/deferred playback while newly animated branches,
quaternion scratch growth and CPU fallback produce identical palette bytes.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { identity } = await import( "../../src/engine/foundation/rendering/world-math.ts" );

/*
================
channel
================
*/
/** @returns {import("../../src/engine/contracts/character.ts").CharacterChannel} */
function channel( node, path, values ) {
	return { node, path, interpolation: "LINEAR", times: Float32Array.of( 0, 1 ), values: Float32Array.from( values ) };
}

/*
================
fixture

The old clip moves only a leaf. New channels activate its static ancestors,
its child and a matrix-authored branch whose local transform stays immutable.
================
*/
function fixture() {
	/** @type {import("../../src/engine/contracts/character.ts").CharacterClip} */
	const old = {
		name: "old",
		duration: 1,
		channels: [ channel( 2, "rotation", [ 0, 0, 0, 1, 0, Math.sin( .3 ), 0, Math.cos( .3 ) ] ) ]
	};
	/** @type {import("../../src/engine/contracts/character.ts").CharacterClip} */
	const added = {
		name: "added",
		duration: 1,
		channels: [
			channel( 1, "translation", [ 0, 1, 0, 3, 4, 5 ] ),
			channel( 0, "rotation", [ 0, 0, 0, 1, 0, 0, Math.sin( .6 ), Math.cos( .6 ) ] ),
			channel( 2, "rotation", [ 0, 0, 0, 1, Math.sin( .2 ), 0, 0, Math.cos( .2 ) ] ),
			channel( 4, "scale", [ 1, 1, 1, .8, 1.2, 1.5 ] ),
			channel( 3, "translation", [ 0, 0, 0, 50, 60, 70 ] )
		]
	};
	/** @type {import("../../src/engine/contracts/character.ts").CharacterClip} */
	const duplicate = {
		name: "duplicate",
		duration: 1,
		channels: [
			channel( 1, "translation", [ 0, 1, 0, 2, 3, 4 ] ),
			channel( 1, "translation", [ 0, 2, 0, 5, 6, 7 ] )
		]
	};
	/** @type {import("../../src/engine/contracts/character.ts").CharacterClip} */
	const cubic = {
		name: "cubic",
		duration: 1,
		channels: [ {
			...channel( 1, "translation", [ 0, 0, 0, 1, 2, 3, 0, 0, 0, 0, 0, 0, 4, 5, 6, 0, 0, 0 ] ),
			interpolation: "CUBICSPLINE"
		} ]
	};
	/** @type {import("../../src/engine/contracts/character.ts").CharacterModel} */
	const model = {
		nodes: [ "Bip01", "branch", "socket", "matrix", "equipment:6:R:marker" ].map( ( name, i ) => ({
			name,
			parent: i === 0 ? -1 : i === 3 ? 1 : i - 1,
			translation: [ i / 10, i / 5, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ],
			matrix: i === 3 ? [ ...identity() ] : undefined
		}) ),
		clips: [ old ],
		images: [],
		primitives: [ {
			name: "body",
			node: 0,
			image: -1,
			joints: [ 0, 1, 2, 3, 4 ],
			inverseBind: Float32Array.from( Array.from( { length: 5 }, () => [ ...identity() ] ).flat() ),
			geometry: { positions: new Float32Array(), indices: new Uint32Array(), transform: identity() }
		} ]
	};
	return { model, old, added, duplicate, cubic };
}

/*
================
comparePose
================
*/
function comparePose( actual, expected, model ) {
	const a = new Float32Array( model.nodes.length * 16 ), b = new Float32Array( a.length );
	actual.palette( model.primitives[0], a );
	expected.palette( model.primitives[0], b );
	assert.deepEqual( new Uint32Array( a.buffer ), new Uint32Array( b.buffer ) );
	for ( const node of model.nodes ) {
		assert.deepEqual( actual.socket( node.name ), expected.socket( node.name ), node.name );
		assert.deepEqual( actual.socket( node.name, true ), expected.socket( node.name, true ), node.name );
	}
}

test("clip admission preserves held pose revision, palette and old quaternion brackets", () => {
	const { model, old, added } = fixture();
	const actual = createCharacterPose( model ), expected = createCharacterPose( model );
	actual.evaluate( old.name, .25 );
	expected.evaluate( old.name, .25 );
	comparePose( actual, expected, model );
	const revision = actual.revision(), evaluations = actual.cpuEvaluations();
	assert.equal( actual.admitClip( added ), true );
	assert.equal( actual.revision(), revision );
	assert.equal( actual.cpuEvaluations(), evaluations );
	assert.equal( actual.evaluate( old.name, .25 ), false );
	comparePose( actual, expected, model );
	assert.equal( actual.cpuEvaluations(), evaluations );
	assert.equal( model.clips.length, 1, "admission owns no mutation of the source catalog" );
	for ( const time of [ .4, .6, .1, 86400.3 ] ) {
		actual.evaluate( old.name, time );
		expected.evaluate( old.name, time );
		comparePose( actual, expected, model );
	}
});

test("appended clips match fresh catalogs through new animated bones, layers and GPU fallback", () => {
	const { model, old, added, duplicate, cubic } = fixture();
	const catalog = { ...model, clips: [ old, added, duplicate, cubic ] };
	const actual = createCharacterPose( model );
	actual.evaluate( old.name, .4 );
	for ( const clip of [ added, duplicate, cubic ] ) assert.equal( actual.admitClip( clip ), true );
	for ( let frame = 0; frame < 120; frame++ ) {
		const expected = createCharacterPose( catalog );
		const clip = catalog.clips[frame % catalog.clips.length], time = frame % 11 ? frame * .037 : 86400.3;
		const volume = frame % 9 ? 2 : 3;
		actual.bodyVolume( volume );
		expected.bodyVolume( volume );
		/** @type {import("../../src/engine/contracts/character.ts").CharacterLayer[] | undefined} */
		const layers = frame % 5 ? undefined : [
			{ clip: added.name, time, loop: true, weight: .4, lane: "event" },
			{ clip: old.name, time: time / 2, loop: true, weight: .3, lane: "timed" },
			{ clip: duplicate.name, time: .2, loop: false, weight: .3, lane: "timed" }
		];
		actual.evaluate( clip.name, time, frame % 3 !== 0, layers, true );
		expected.evaluate( clip.name, time, frame % 3 !== 0, layers, true );
		assert.deepEqual( actual.gpuSample(), expected.gpuSample() );
		comparePose( actual, expected, model );
	}
});

test("admission preserves pending CPU work and duplicate names never replace active clips", () => {
	const { model, old, added } = fixture();
	const actual = createCharacterPose( model ), expected = createCharacterPose( model );
	actual.evaluate( old.name, .7, true, undefined, true );
	expected.evaluate( old.name, .7 );
	const revision = actual.revision();
	assert.equal( actual.cpuEvaluations(), 0 );
	assert.equal( actual.admitClip( added ), true );
	assert.equal( actual.admitClip( added ), false );
	assert.equal( actual.admitClip( { ...added, name: old.name } ), false );
	assert.equal( actual.revision(), revision );
	assert.equal( actual.cpuEvaluations(), 0 );
	assert.equal( actual.gpuSample()?.clip, old );
	comparePose( actual, expected, model );
	assert.equal( actual.cpuEvaluations(), 1 );
	actual.evaluate( old.name, .8 );
	expected.evaluate( old.name, .8 );
	comparePose( actual, expected, model );
});

test("an initially empty clip catalog admits its first animated branch", () => {
	const { model, added } = fixture();
	const actual = createCharacterPose( { ...model, clips: [] } );
	const expected = createCharacterPose( { ...model, clips: [ added ] } );
	actual.evaluate( "", 0 );
	// Before admission the clip is missing: it plays the rest pose and is
	// reported, never thrown (missing-clip.test.mjs).
	actual.evaluate( added.name, .5 );
	assert.deepEqual( actual.takeMissingClips(), [ added.name ] );
	assert.equal( actual.admitClip( added ), true );
	actual.evaluate( added.name, .5, true, undefined, true );
	expected.evaluate( added.name, .5 );
	assert.equal( actual.gpuSample()?.clip, added );
	comparePose( actual, expected, model );
});
