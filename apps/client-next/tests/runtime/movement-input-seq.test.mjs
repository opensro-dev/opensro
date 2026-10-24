/*
===========================================================================

movement-input-seq.test.mjs - gameplay inputs carry a sequence (#1441a)

Render-thread movement (epic #1437): every gameplay command the main thread
sends to the worker carries a monotonically increasing sequence number on
its session envelope. The worker's movement owner keeps the last one taken
and publishes it with the local movement state. A receipt or a server
correction never changes it, and a lower number never moves it back, so
the main thread can tell which inputs the worker already reflects. No
behaviour changes; the server wire is untouched.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { createInputSequence } = await import( "../../src/engine/foundation/gameplay/movement-input.ts" );
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
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

test("the input sequence issues monotonically increasing numbers", () => {
	const inputs = createInputSequence();
	assert.equal( inputs.last(), 0 );
	assert.deepEqual( [ inputs.next(), inputs.next(), inputs.next() ], [ 1, 2, 3 ] );
	assert.equal( inputs.last(), 3 );
});

test("three inputs, a receipt and a correction publish the last input taken", () => {
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
	// A refusal corrects the walk and keeps the last input taken.
	m.receive( receipt( 3, { ...pose, x: 90 }, false ), 20 );
	assert.equal( m.state().movementInputSeq, 3 );
	// A late or replayed lower sequence never moves it back.
	m.noteInput( 2 );
	assert.equal( m.state().movementInputSeq, 3 );
});
