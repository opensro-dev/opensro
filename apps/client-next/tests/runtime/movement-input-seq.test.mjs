/*
===========================================================================

movement-input-seq.test.mjs - movement inputs carry a sequence (#1441a)

Render-thread movement (epic #1437): every movement input the main thread
sends to the worker carries a monotonically increasing sequence number, the
worker records the last one it processed, and the published local movement
state carries it. A receipt or a server correction never changes it, so
the main thread can tell which inputs the worker's pose already includes.
No behaviour changes; the server wire is untouched.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { createMovementInputSequence, isMovementInput } = await import(
	"../../src/engine/foundation/gameplay/movement-input.ts"
);
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const pose = { regionId: 0x62a8, x: 50, y: 0, z: 50, angle: 0 };

/*
================
receipt

A movement receipt for request id; accepted false is the server's refusal,
which corrects the local walk.
================
*/
function receipt( id, to, accepted = true ) {
	return Buffer.from(
		JSON.stringify( {
			v: 1,
			id,
			gid: 100007,
			accepted,
			error: accepted ? undefined : "blocked",
			serverTimeMs: 1000,
			world: { spawn: to, moveSegment: { from: pose, startedAtMs: 1000, arrivesAtMs: 2000 } }
		} )
	);
}

test("the host stamps only movement inputs, monotonically", () => {
	const inputs = createMovementInputSequence();
	const query = { origin: [ 0, 0, 0 ], direction: [ 0, -1, 0 ] };
	/** @type {any} */
	const click = inputs.stamp( /** @type {any} */ ({ kind: "ground-move", query }) );
	/** @type {any} */
	const move = inputs.stamp( { kind: "move", destination: pose } );
	/** @type {any} */
	const other = inputs.stamp( { kind: "select", gid: 7 } );
	/** @type {any} */
	const again = inputs.stamp( /** @type {any} */ ({ kind: "ground-move", query }) );
	assert.equal( click.inputSeq, 1 );
	assert.equal( move.inputSeq, 2 );
	assert.equal( "inputSeq" in other, false );
	assert.equal( again.inputSeq, 3 );
	assert.equal( inputs.last(), 3 );
	assert.equal( isMovementInput( { kind: "select", gid: 7 } ), false );
});

test("three inputs, a receipt and a correction publish the last processed sequence", () => {
	const m = createMovement( () => {} );
	m.seed( pose );
	assert.equal( m.state().movementInputSeq, 0 );
	m.noteInput( 1 );
	m.request( { ...pose, x: 70 }, 0 );
	assert.equal( m.state().movementInputSeq, 1 );
	m.noteInput( 2 );
	m.request( { ...pose, x: 90 }, 1 );
	m.noteInput( 3 );
	m.request( { ...pose, x: 110 }, 2 );
	assert.equal( m.state().movementInputSeq, 3 );
	// A receipt applies the server's answer to an input; it is not an input.
	m.receive( receipt( 1, { ...pose, x: 70 } ), 10 );
	assert.equal( m.state().movementInputSeq, 3 );
	// A refusal corrects the walk and keeps the last applied input sequence.
	m.receive( receipt( 3, { ...pose, x: 90 }, false ), 20 );
	assert.equal( m.state().movementInputSeq, 3 );
	// A late or replayed lower sequence never moves it back.
	m.noteInput( 2 );
	assert.equal( m.state().movementInputSeq, 3 );
});

test("the gameplay owner records a movement input it processes and publishes it", () => {
	const game = createGameplay( () => {} );
	const local = {
		gid: 100007,
		refObjId: 1907,
		name: "walker",
		kind: /** @type {const} */ ("player"),
		regionId: pose.regionId,
		x: pose.x,
		y: pose.y,
		z: pose.z,
		heading: 0
	};
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	game.command( { kind: "move", destination: { ...pose, x: 80 }, inputSeq: 5 }, 11, undefined, local );
	assert.equal( game.take()?.movementInputSeq, 5 );
	// A command without a sequence (an internal re-issue) leaves it alone.
	game.command( { kind: "move", destination: { ...pose, x: 90 } }, 12, undefined, local );
	assert.equal( game.take()?.movementInputSeq ?? 5, 5 );
});
