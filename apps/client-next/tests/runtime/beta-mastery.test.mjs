/*
===========================================================================
beta-mastery.test.mjs - server budget publication and session isolation
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createTraining } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/training/training.ts"
);

test("beta mastery budget reaches the HUD snapshot and clears on native reentry", () => {
	const game = createGameplay( () => {} );
	const pose = {
		gid: 7,
		refObjId: 1907,
		kind: "player",
		name: "Tester",
		regionId: 0x6b4f,
		x: 60,
		y: 10,
		z: 100,
		heading: 0
	};
	game.bootstrap( { simulationProtocolVersion: 1, masteryTotalOverride: 5000 } );
	game.seed( pose );
	const beta = game.take();
	assert.ok( beta );
	assert.equal( beta.masteryTotalOverride, 5000 );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( pose );
	const native = game.take();
	assert.ok( native );
	assert.equal( native.masteryTotalOverride, undefined );
	game.dispose();
});

test("mastery configuration rejects malformed values and cannot survive reset or disposal", () => {
	const training = createTraining( () => {} );
	for ( const value of [ -1, 0, 1.5, "5000", null, Infinity ] ) {
		assert.throws( () => training.bootstrap( { masteryTotalOverride: value } ), /mastery total override/ );
	}
	for ( const clear of [ () => training.reset(), () => training.dispose() ] ) {
		training.bootstrap( { masteryTotalOverride: 5000 } );
		assert.equal( training.state().masteryTotalOverride, 5000 );
		clear();
		assert.equal( training.state().masteryTotalOverride, undefined );
	}
});
