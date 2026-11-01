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
			// Native float32 additions may differ below a source-wire quantum;
			// the separate native arithmetic fixture compares bit for bit.
			Math.abs( movement.state().pose.x - expected ) < 1 / 8192,
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
test("a receipt whose server start escaped a sealed obstacle rebases a wedged client onto terrain", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const region = product( 23661 );
	const heights = new Float32Array( 97 * 97 ).fill( 3364 );
	region.navmesh.regions[0].heightMap = Buffer.from( heights.buffer ).toString( "base64" );
	region.objects = [ {
		x: 985.8694458007812,
		y: 3362.28564453125,
		z: 10.19677734375,
		yaw: -7.853983402252197,
		terrainCells: [ [ 960, 0, 1280, 320 ] ],
		mesh: {
			vertices: Float32Array.from( [
				-6.714275360107422,
				0.04216267541050911,
				4.177054405212402,
				-6.522409439086914,
				0.04221678525209427,
				-2.489809989929199,
				-0.12255165725946426,
				0.04227064177393913,
				-4.367509365081787,
				3.6409153938293457,
				0.042249832302331924,
				1.1388740539550781,
				-0.4329914152622223,
				0.042183488607406616,
				6.41970682144165
			] ),
			vertexDirections: Uint8Array.from( [ 88, 101, 50, 254, 11 ] ),
			cells: Uint16Array.from( [ 4, 3, 2, 0, 4, 2, 0, 2, 1 ] ),
			edges: Uint32Array.from( [
				2,
				3,
				0,
				65535,
				3,
				0,
				3,
				4,
				0,
				65535,
				3,
				0,
				0,
				4,
				1,
				65535,
				3,
				0,
				1,
				2,
				2,
				65535,
				3,
				0,
				0,
				1,
				2,
				65535,
				3,
				0,
				2,
				4,
				0,
				1,
				4,
				1,
				0,
				2,
				1,
				2,
				4,
				1
			] ),
			bounds: [
				-6.714275360107422,
				0.04216267541050911,
				-4.367509365081787,
				3.6409153938293457,
				0.04227064177393913,
				6.41970682144165
			],
			passThrough: false
		}
	} ];
	const movement = createMovement( () => {} );
	const wedgedInsideRock = {
		regionId: 23661,
		x: 981.6279296875,
		y: 3362.3279132062466,
		z: 10.478119850158691,
		angle: 27236
	};
	const target = { regionId: 23661, x: 932, y: 3361, z: 40, angle: 27106 };
	const serverFrom = {
		regionId: 23661,
		x: 970.7426147460938,
		y: 3363.9713375543097,
		z: 16.629905700683594,
		angle: 27236
	};
	movement.seed( wedgedInsideRock, { walkSpeed: 40, runSpeed: 100 } );
	movement.navigation( region.regionId, region );
	movement.request( target, 0 );
	for ( let now = 0; now <= 144; now += 16 ) movement.step( now );
	assert.ok( movement.state().pose.x > 981, "client prediction is trapped inside Object 4 before the receipt" );
	movement.receive(
		new TextEncoder().encode(
			JSON.stringify( {
				v: 1,
				id: 1,
				gid: 100122,
				accepted: true,
				serverTimeMs: 1000,
				world: {
					spawn: target,
					moveSegment: { from: serverFrom, startedAtMs: 1000, arrivesAtMs: 1453 }
				}
			} )
		),
		160,
		100122
	);
	movement.step( 176 );
	const afterReceipt = movement.state().pose;
	assert.ok(
		afterReceipt.x < 970 && afterReceipt.z > 16.8 && afterReceipt.y === 3364,
		`receipt left client wedged inside Object 4: ${JSON.stringify( afterReceipt )}`
	);
	for ( let now = 192; now <= 640; now += 16 ) movement.step( now );
	const arrived = movement.state().pose;
	assert.ok(
		Math.hypot( arrived.x - target.x, arrived.z - target.z ) < 1e-2,
		`rebased walk did not reach terrain target: ${JSON.stringify( arrived )}`
	);
	movement.clear();
});
// #1978: the receipt watchdog keys on inbound silence, not emission
// age alone - receipts are ordered behind the inbound stream, so one
// still missing while frames arrive is in transit, not lost.
test("a receipt lost on a silent link trips the receipt watchdog", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const movement = createMovement( () => {} ), p = product();
	p.objects = [];
	const from = { ...pose, x: 10, y: 0 }, to = { ...pose, x: 110, y: 0 };
	movement.seed( from );
	movement.navigation( 257, p );
	movement.request( to, 0 );
	assert.throws( () => movement.step( 10001 ), /Movement receipt timed out/ );
	movement.clear();
});
test("a receipt in transit behind a flowing stream does not trip the watchdog", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const movement = createMovement( () => {} ), p = product();
	p.objects = [];
	const from = { ...pose, x: 10, y: 0 }, to = { ...pose, x: 110, y: 0 };
	movement.seed( from );
	movement.navigation( 257, p );
	movement.request( to, 0 );
	// The inbound stream keeps delivering for 30 s (a long scene
	// bootstrap still draining): the receipt is behind it, not lost.
	for ( let now = 1000; now <= 30000; now += 1000 ) {
		movement.noteInboundProgress( now );
		movement.step( now );
	}
	assert.equal( movement.state().pendingMoves, 1 );
	movement.receive( receiptAt( from, to ), 30500, 7 );
	assert.equal( movement.state().pendingMoves, 0 );
	movement.step( 30516 );
	movement.clear();
});
test("the receipt backstop trips even while the stream keeps flowing", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const movement = createMovement( () => {} ), p = product();
	p.objects = [];
	const from = { ...pose, x: 10, y: 0 }, to = { ...pose, x: 110, y: 0 };
	movement.seed( from );
	movement.navigation( 257, p );
	movement.request( to, 0 );
	// Progress never stops, but 90 s without the receipt itself is
	// past the hard backstop: the answer can only be missing.
	for ( let now = 1000; now <= 90000; now += 1000 ) {
		movement.noteInboundProgress( now );
		movement.step( now );
	}
	assert.throws( () => movement.step( 90001 ), /Movement receipt timed out/ );
	movement.clear();
});
