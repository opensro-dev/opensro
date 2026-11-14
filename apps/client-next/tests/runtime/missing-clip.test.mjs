/*
===========================================================================

missing-clip.test.mjs - a clip the model lacks plays the rest pose

The live renderer_failed incident "Missing animation stand" ended the
session because the pose evaluator threw on a clip the model's catalogue
lacked. A missing clip now plays the rest pose, as an unnamed layer does,
and is reported once through takeMissingClips.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCharacterPose, MAX_MISSING_CLIPS } = await import(
	"../../src/engine/foundation/animation/animation-pose.ts"
);

/*
================
model

One bone and one moving clip, "walk"; no "stand".
================
*/
function model() {
	return /** @type {import("../../src/engine/contracts/character").CharacterModel} */ (/** @type {unknown} */ ({
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		primitives: [],
		clips: [ {
			name: "walk",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: new Float32Array( [ 0, 1 ] ),
				values: new Float32Array( [ 0, 0, 0, 10, 0, 0 ] )
			} ]
		} ]
	}));
}

test("a clip the model lacks plays the rest pose and is reported once", () => {
	const pose = createCharacterPose( model() );
	assert.doesNotThrow( () => pose.evaluate( "stand", .5 ) );
	assert.equal( pose.socket( "root" )?.[12] ?? 0, 0 );
	pose.evaluate( "stand", .7 );
	assert.deepEqual( pose.takeMissingClips(), [ "stand" ] );
	assert.deepEqual( pose.takeMissingClips(), [] );
	pose.evaluate( "stand", .9 );
	assert.deepEqual( pose.takeMissingClips(), [], "a name is reported once per evaluator" );
});

test("a clip the model has still plays, and a later missing name is reported", () => {
	const pose = createCharacterPose( model() );
	pose.evaluate( "walk", .5 );
	assert.deepEqual( pose.takeMissingClips(), [] );
	pose.evaluate( "run", .5 );
	assert.deepEqual( pose.takeMissingClips(), [ "run" ] );
});

test("an evaluator records at most MAX_MISSING_CLIPS names", () => {
	const pose = createCharacterPose( model() );
	for ( let i = 0; i < MAX_MISSING_CLIPS + 4; i++ ) pose.evaluate( `clip${i}`, 0 );
	assert.equal( pose.takeMissingClips().length, MAX_MISSING_CLIPS );
});
