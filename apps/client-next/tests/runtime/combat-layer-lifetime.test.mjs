/*
===========================================================================

combat-layer-lifetime.test.mjs - overlapping combat pose regressions

Exercises the real movement producer and pose consumer together. Fast input
may overlap more than eight native exit fades without retaining stale layers.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { changeLocomotion, locomotionLayers } = await import(
	"../../src/engine/foundation/animation/locomotion-blend.ts"
);

/*
================
modelFixture
================
*/
/** @returns {import("../../src/engine/contracts/character.ts").CharacterModel} */
function modelFixture() {
	/** @type {import("../../src/engine/contracts/character.ts").CharacterModel["nodes"][number]} */
	const node = { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] };
	/** @type {import("../../src/engine/contracts/character.ts").CharacterModel["clips"]} */
	const clips = [ "stand", "run", "hit" ].map( ( name, index ) => ({
		name,
		duration: 1,
		channels: [ {
			node: 0,
			path: "translation",
			interpolation: "LINEAR",
			times: Float32Array.of( 0, 1 ),
			values: Float32Array.of( index, 0, 0, index, 0, 0 )
		} ]
	}) );
	return { nodes: [ node ], clips, primitives: [], images: [] };
}

test("rapid movement changes and hit reactions preserve every live fade and retire them", () => {
	const pose = createCharacterPose( modelFixture() );
	let movement;
	let maximum = 0;
	for ( let frame = 0; frame < 120; frame++ ) {
		const now = frame / 120;
		movement = changeLocomotion( movement, frame % 2 ? "run" : "stand", true, now, "run" );
		/** @type {import("../../src/engine/contracts/character.ts").CharacterLayer[]} */
		const layers = [
			{ clip: "hit", time: .1, loop: false, weight: .25, lane: "event" },
			...locomotionLayers( movement, now + .001 )
		];
		maximum = Math.max( maximum, layers.length );
		pose.evaluate( "", 0, false, layers );
		const socket = pose.socket( "root" );
		assert.ok( socket );
		assert.ok( socket.every( Number.isFinite ) );
		assert.ok( socket[12] >= .5 && socket[12] <= 1.25, "event pass retains priority over timed fades" );
	}
	assert.ok( maximum > 8, "the real producer legitimately exceeds the old consumer ceiling" );
	assert.ok( movement );
	const settled = locomotionLayers( movement, 2 );
	assert.equal( settled.length, 1 );
	assert.equal( movement.outgoing.length, 0 );
	pose.evaluate( "", 0, false, settled );
	const settledSocket = pose.socket( "root" );
	assert.ok( settledSocket );
	assert.equal( settledSocket[12], 1 );
});
