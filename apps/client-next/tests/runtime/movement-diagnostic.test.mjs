/*
===========================================================================

movement-diagnostic.test.mjs - correlate movement without changing its result

Commands and their receipts must remain identifiable even when publication
coalesces them or a duplicate receipt is ignored by the movement owner.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const FROM = { regionId: 257, x: 100, y: 0, z: 100, angle: 0 };
const TO = { ...FROM, x: 200 };

test("recording preserves movement and correlates accepted and duplicate receipts", () => {
	const events = [], recorded = createMovement( () => {}, event => events.push( event ) );
	const plain = createMovement( () => {} );
	const receipt = new TextEncoder().encode( JSON.stringify( {
		v: 1,
		id: 1,
		gid: 7,
		accepted: true,
		serverTimeMs: 100016,
		world: { spawn: TO, moveSegment: { from: FROM, startedAtMs: 100000, arrivesAtMs: 102000 } }
	} ) );
	for ( const movement of [ recorded, plain ] ) {
		const navigation = product();
		navigation.objects = [];
		movement.seed( FROM );
		movement.navigation( 257, navigation );
		movement.request( TO, 0 );
		movement.step( 16 );
		movement.receive( receipt, 16, 7 );
		movement.receive( receipt, 32, 7 );
		movement.step( 2500 );
	}
	assert.deepEqual( recorded.state(), plain.state() );
	assert.deepEqual( events.filter( event => event.event !== "correction" ), [
		{
			kind: "movement-diagnostic",
			event: "request",
			simulationAtMs: 0,
			requestId: 1,
			mode: 1,
			cos: false,
			acknowledged: 0,
			target: TO
		},
		{
			kind: "movement-diagnostic",
			event: "receipt",
			simulationAtMs: 16,
			requestId: 1,
			acknowledged: 0,
			serverTimeMs: 100016,
			accepted: true,
			stale: false,
			simulationRoundTripMs: 16,
			serverGoal: TO,
			serverFrom: FROM,
			serverDepartAtMs: 100000,
			serverArriveAtMs: 102000
		},
		{
			kind: "movement-diagnostic",
			event: "receipt",
			simulationAtMs: 32,
			requestId: 1,
			acknowledged: 1,
			serverTimeMs: 100016,
			accepted: true,
			stale: true
		}
	] );
});

test("a rejected request records its refusal error, correction and independent clock domains", () => {
	const events = [], movement = createMovement( () => {}, event => events.push( event ) );
	const navigation = product();
	navigation.objects = [];
	movement.seed( FROM );
	movement.navigation( 257, navigation );
	movement.request( TO, 100, 42 );
	movement.step( 200 );
	const predicted = { ...movement.state().pose };
	movement.receive(
		new TextEncoder().encode( JSON.stringify( {
			v: 1,
			id: 1,
			gid: 7,
			accepted: false,
			error: "staleCommand",
			serverTimeMs: 900000,
			world: { spawn: FROM }
		} ) ),
		200,
		7
	);
	const request = events.find( event => event.event === "request" );
	assert.equal( request.mode, 1 );
	assert.equal( request.cos, true );
	assert.equal( request.acknowledged, 0 );
	const receipt = events.find( event => event.event === "receipt" );
	assert.equal( receipt.simulationRoundTripMs, 100 );
	assert.equal( receipt.serverTimeMs, 900000 );
	assert.equal( receipt.acknowledged, 0 );
	assert.equal( receipt.error, "staleCommand" );
	const correction = events.find( event => event.event === "correction" );
	assert.ok( correction );
	assert.equal( correction.requestId, 1 );
	assert.equal( correction.simulationAtMs, 200 );
	assert.deepEqual( correction.before, predicted );
	assert.equal( correction.after.x, FROM.x );
	assert.ok( correction.distance > 0 );
	assert.equal( correction.source, "receipt server: rejected" );
	movement.direct( 16384, 250 );
	const directRequest = events.filter( event => event.event === "request" )[1];
	assert.equal( directRequest.requestId, 2 );
	assert.equal( directRequest.mode, 0 );
	assert.equal( directRequest.cos, false );
	assert.equal( directRequest.acknowledged, 1 );
	assert.equal( directRequest.heading, 16384 );
});
