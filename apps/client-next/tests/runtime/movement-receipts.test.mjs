/*
===========================================================================

movement-receipts.test.mjs - what a movement receipt does to the predicted walk

A click walks at once on the client; the server's receipt arrives a round
trip later. These tests pin that the receipt keeps the walking time and
progress the prediction made, settles on a stop the prediction passed, and
never puts the player back where the server stood when it answered.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product, pose } from "../helpers/navigation-fixture.mjs";
/*
================
load
================
*/
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
test("movement predicts an admitted seam before receipt and reconciles to authoritative state", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const frames = [], movement = createMovement( frame => frames.push( frame ) ), p = product();
	p.objects = [];
	p.navmesh.regions.push( { ...p.navmesh.regions[0], dx: 1 } );
	const from = { ...pose, x: 1910, y: 0 }, to = { ...pose, regionId: 258, x: 30, y: 0 };
	movement.seed( from );
	movement.navigation( 257, p );
	movement.request( to, 0 );
	movement.step( 400 );
	assert.equal( movement.state().pose.regionId, 258 );
	assert.equal( movement.state().pose.x, 10 );
	assert.equal( movement.state().pendingMoves, 1 );
	assert.equal( new DataView( frames[0].payload.buffer ).getUint16( 6, true ), 258 );
	movement.receive(
		new TextEncoder().encode(
			JSON.stringify( {
				v: 1,
				id: 1,
				gid: 7,
				accepted: false,
				serverTimeMs: 400,
				error: "blocked",
				world: { spawn: from }
			} )
		),
		400,
		7
	);
	assert.deepEqual( movement.state().pose, from );
	assert.equal( movement.state().pendingMoves, 0 );
	movement.clear();
});
test("a receipt arriving between worker steps keeps the predicted walk's elapsed time", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const movement = createMovement( () => {} ), p = product();
	p.objects = [];
	const from = { ...pose, x: 10, y: 0 }, to = { ...pose, x: 110, y: 0 };
	movement.seed( from );
	movement.navigation( 257, p );
	movement.request( to, 0 );
	// The worker steps every 16 ms; the production receipt lands 8 ms after a
	// step. The server started its walk half a round trip after the click.
	for ( let now = 0; now <= 352; now += 16 ) movement.step( now );
	movement.receive(
		new TextEncoder().encode( JSON.stringify( {
			v: 1,
			id: 1,
			gid: 7,
			accepted: true,
			serverTimeMs: 175,
			world: { spawn: to, moveSegment: { from, startedAtMs: 175, arrivesAtMs: 2175 } }
		} ) ),
		360,
		7
	);
	for ( const now of [ 360, 368, 400 ] ) {
		movement.step( now );
		const expected = 10 + 50 * now / 1000;
		assert.ok(
			Math.abs( movement.state().pose.x - expected ) < 1e-6,
			`receipt lost walking time at ${now}: ${movement.state().pose.x} vs ${expected}`
		);
	}
	movement.clear();
});
/*
================
receiptAt

The receipt of a click walk the server started half a round trip after
the click (at 175 ms) and answers at 360 ms, as on production.
================
*/
function receiptAt( from, to ) {
	return new TextEncoder().encode( JSON.stringify( {
		v: 1,
		id: 1,
		gid: 7,
		accepted: true,
		serverTimeMs: 175,
		world: { spawn: to, moveSegment: { from, startedAtMs: 175, arrivesAtMs: 2175 } }
	} ) );
}
/*
================
walkUntilReceipt

Clicks from -> to at 0 ms, steps the walk to the receipt at 360 ms and
returns each stepped pose after it.
================
*/
function walkUntilReceipt( movement, from, to, serverTo ) {
	movement.request( to, 0 );
	for ( let now = 0; now <= 352; now += 16 ) movement.step( now );
	const before = movement.state().pose;
	movement.receive( receiptAt( from, serverTo ), 360, 7 );
	const after = [];
	for ( const now of [ 360, 376, 392 ] ) {
		movement.step( now );
		after.push( movement.state().pose );
	}
	return { before, after };
}
test("a receipt whose server clip ends a fraction short keeps the predicted walk", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const movement = createMovement( () => {} ), p = product();
	p.objects = [];
	const from = { ...pose, x: 10, y: 0 }, to = { ...pose, x: 110, y: 0 };
	movement.seed( from );
	movement.navigation( 257, p );
	// The server's navigation stopped the walk 0.4 units before the client's.
	const { before, after } = walkUntilReceipt( movement, from, to, { ...to, x: 109.6 } );
	for ( const step of after ) {
		assert.ok( step.x >= before.x - 1e-6, `receipt stepped the player back from ${before.x} to ${step.x}` );
	}
	movement.clear();
});
test("a receipt whose server stop the prediction already passed settles on the stop", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const movement = createMovement( () => {} ), p = product();
	p.objects = [];
	const from = { ...pose, x: 10, y: 0 }, to = { ...pose, x: 110, y: 0 };
	movement.seed( from );
	movement.navigation( 257, p );
	// The prediction reached 28; the server stopped the walk at 20. A clear
	// route back to 20 and 8 < 10 to go do not make 28 progress toward it.
	const { before, after } = walkUntilReceipt( movement, from, to, { ...to, x: 20 } );
	assert.ok( before.x > 20, "the prediction passed the server's stop" );
	for ( const step of after ) assert.equal( step.x, 20, "settles on the server's stop, never its start" );
	movement.clear();
});
test("a receipt keeps the predicted walk after it crossed a region seam the server had not", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const movement = createMovement( () => {} ), p = product();
	p.objects = [];
	p.navmesh.regions.push( { ...p.navmesh.regions[0], dx: 1 } );
	const from = { ...pose, x: 1910, y: 0 }, to = { ...pose, regionId: 258, x: 60, y: 0 };
	movement.seed( from );
	movement.navigation( 257, p );
	const { before, after } = walkUntilReceipt( movement, from, to, to );
	assert.equal( before.regionId, 258, "the prediction crossed the seam before the receipt" );
	for ( const step of after ) {
		assert.equal( step.regionId, 258, "receipt put the player back across the seam" );
		assert.ok( step.x >= before.x - 1e-6, `receipt stepped the player back from ${before.x} to ${step.x}` );
	}
	movement.clear();
});
