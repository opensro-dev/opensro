/*
===========================================================================

walk-predictable.test.mjs - the worker's walkPredictable flag (GH #1440)

The main-thread mover predicts a ground click only while the worker's
latest publication says a click would walk at once. The flag must agree
with what the command path does, state by state, and must republish when
a gate opens or closes with no command (a step).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
const { createGameplay } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ) ).href
);

const POSE = { regionId: 0x6b4f, x: 60, y: 10, z: 100, angle: 0 };
// movement.ts OP_PREDICTED_MOVE: the internal frame a walk request sends.
const PREDICTED_MOVE = 9;
const STANDING = { ...POSE, gid: 7, heading: 0, appearanceState: [ 1, 0, 0 ] };
const STATES = {
	standing: STANDING,
	seated: { ...STANDING, movementMode: 4 },
	dead: { ...STANDING, appearanceState: [ 2, 0, 0 ] },
	mounted: { ...STANDING, mountedOn: 9 }
};

/*
================
started

A fresh gameplay owner in the given local state: the flag it publishes,
and whether a click then sent a walk request.
================
*/
function started( local ) {
	const sent = [], game = createGameplay( f => sent.push( f ) );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	game.step( 50, local );
	const flag = game.take()?.walkPredictable;
	try {
		game.command( { kind: "move", destination: { ...POSE, x: 300 } }, 100, undefined, local );
	} catch {
		// A mounted click without its COS authority throws; it does not walk.
	}
	game.dispose();
	return { flag, walked: sent.some( f => f.opcode === PREDICTED_MOVE ) };
}

test("walkPredictable is true exactly in the states where a click walks at once", () => {
	for ( const [name, local] of Object.entries( STATES ) ) {
		const { flag, walked } = started( local );
		assert.equal( flag, name === "standing", name + ": published flag" );
		// Mounted walks are left to the worker on purpose (the flag is false
		// although the worker may walk the COS), every other state agrees.
		if ( name !== "mounted" ) assert.equal( walked, flag, name + ": the command path agrees" );
	}
});

/*
================
pickupFlag

A fresh gameplay owner in the given local state: the pickupPredictable
flag it publishes (#1567). The mover predicts a pickup leg only under
this flag, whose gate is the worker's run-up gate (pickupPredictableNow),
not the ground click's: any mount closes it even where a rider's ground
walk is predictable.
================
*/
function pickupFlag( local ) {
	const game = createGameplay( () => {} );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	game.step( 50, local );
	const flag = game.take()?.pickupPredictable;
	game.dispose();
	return flag;
}

test("pickupPredictable is the run-up's own gate, state by state", () => {
	for ( const [name, local] of Object.entries( STATES ) ) {
		assert.equal( pickupFlag( local ), name === "standing", name + ": published flag" );
	}
});

test("a gate that closes with no command republishes the flag on the next step", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( STANDING );
	game.step( 50, STANDING );
	assert.equal( game.take()?.walkPredictable, true );
	assert.equal( game.take(), null, "nothing changed" );
	game.step( 66, STATES.seated );
	assert.equal( game.take()?.walkPredictable, false );
	game.step( 83, STANDING );
	assert.equal( game.take()?.walkPredictable, true );
	game.dispose();
});
