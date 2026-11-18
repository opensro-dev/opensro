/*
===========================================================================

release-gate.test.mjs - the gate's move check against what a server answers

The gate's gameplay phase sends one move and waits for the server to
confirm it. On 2026-11-18 that wait could not pass on the release it
guarded: the server had stopped sending the arrival correction the check
waited for (#3721, as the original), and the headless runner kept only the
last gameplay publication, so the acknowledgement was gone one tick after
it arrived. The game stayed closed for hours behind a healthy server.

These tests hold the check to the receipt alone, for both servers: the
native one, which sends nothing after the receipt, and the one with the
port-only position stream, which ends the walk with a correction at the
goal. The end-to-end cases run the real headless runner against a
stand-in release worker that publishes what the r202 worker published on
production: a receipt delta, then a delta on every tick naming only the
revision and the pose clock.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { bootstrapSource, startHeadlessSimulation } from "../../tools/beta/headless-simulation.mjs";
import { MOVE_DISTANCE, moveAndConfirm, moveConfirmed } from "../../tools/beta/release-gate.mjs";

const HOME = { regionId: 25256, x: 1016, y: 20, z: 458, angle: 32768 };
const GOAL = { ...HOME, x: HOME.x + MOVE_DISTANCE };
// A wait long enough for the stand-in's receipt, short enough for the cases
// that must time out.
const TEST_BUDGET_MS = 1_500;

/*
================
standInWorker

A release worker reduced to its gameplay plane. It publishes the whole
state once at start, and answers a move as the named server does:

  native    the receipt (acknowledged, the pose the server has us at: the
            start of the walk), then nothing about the move again
  stream    the same receipt, then the arrival correction at the goal
  refused   a receipt that acknowledges the move with the server's reason
  silent    no receipt at all

Whatever the answer, a delta leaves on every tick naming only the revision
and the pose clock, as the worker's own pose publication does while it
believes it walks. That stream never stops, so no publication is ever
"the last one".
================
*/
function standInWorker( answer ) {
	return `
const HOME = ${JSON.stringify( HOME )};
const ANSWER = ${JSON.stringify( answer )};
let sequence = 0, revision = 0, acknowledged = 0;
function publish( state, delta ) {
	postMessage( { kind: "world", batch: { sequence: ++sequence, events: [
		delta ? { kind: "gameplay", state: { revision: ++revision, ...state }, delta: true } :
			{ kind: "gameplay", state: { revision: ++revision, ...state } }
	] } } );
}
globalThis.onmessage = ( { data } ) => {
	if ( data.kind === "start" ) {
		publish( {
			acknowledgedMove: 0, pendingMoves: 0, error: null, pose: HOME, authoritativePose: HOME, poseAtMs: 0,
			inventory: [ { slot: 13 } ]
		}, false );
		setInterval( () => publish( { poseAtMs: Date.now() }, true ), 8 );
		return;
	}
	if ( data.kind !== "session" || data.command.kind !== "gameplay" || data.command.command.kind !== "move" ) return;
	const goal = data.command.command.destination;
	publish( { pendingMoves: 1 }, true );
	if ( ANSWER === "silent" ) return;
	setTimeout( () => {
		acknowledged++;
		if ( ANSWER === "refused" ) {
			publish( { acknowledgedMove: acknowledged, pendingMoves: 0, error: "endpointBlocked" }, true );
			return;
		}
		publish( { acknowledgedMove: acknowledged, pendingMoves: 0, error: null, moving: true, authoritativePose: HOME }, true );
		if ( ANSWER === "stream" ) {
			setTimeout( () => publish( { moving: false, authoritativePose: { ...HOME, x: goal.x, z: goal.z } }, true ), 80 );
		}
	}, 60 );
};
`;
}

/*
================
startStandIn

The real headless runner over one stand-in worker, started and seeded.
================
*/
async function startStandIn( t, answer ) {
	const directory = await mkdtemp( path.join( os.tmpdir(), "sro-gate-" ) );
	await writeFile( path.join( directory, "worker.mjs" ), standInWorker( answer ) );
	await writeFile( path.join( directory, "bootstrap.mjs" ), bootstrapSource() );
	const simulation = await startHeadlessSimulation( {
		origin: "http://127.0.0.1:1",
		workerUrl: pathToFileURL( path.join( directory, "worker.mjs" ) ).href,
		bootstrapUrl: pathToFileURL( path.join( directory, "bootstrap.mjs" ) )
	} );
	t.after( async () => {
		await simulation.stop();
		await rm( directory, { recursive: true, force: true } );
	} );
	await simulation.waitFor( state => state.gameplay?.authoritativePose ?? null, TEST_BUDGET_MS, "seed" );
	return simulation;
}

test("no receipt yet is no confirmation", () => {
	assert.equal( moveConfirmed( null, 0, GOAL ), null );
	assert.equal( moveConfirmed( { acknowledgedMove: 0, error: null, authoritativePose: GOAL }, 0, GOAL ), null );
	// A publication that names no acknowledgement (a per-tick delta read on
	// its own) acknowledged nothing, wherever its pose is.
	assert.equal( moveConfirmed( { revision: 9, poseAtMs: 1, authoritativePose: GOAL }, 0, GOAL ), null );
});

test("the receipt confirms the move wherever on the walk the server has the probe", () => {
	const native = { acknowledgedMove: 1, error: null, authoritativePose: HOME };
	assert.equal( moveConfirmed( native, 0, GOAL ), native, "the native server answers from the start of the walk" );
	const arrived = { acknowledgedMove: 1, error: null, authoritativePose: GOAL };
	assert.equal( moveConfirmed( arrived, 0, GOAL ), arrived, "the position stream ends the walk on the goal" );
	// The walk back: the server may still have the probe at the far end of
	// the first leg in the other direction.
	const far = { acknowledgedMove: 2, error: null, authoritativePose: { ...HOME, x: HOME.x - MOVE_DISTANCE } };
	assert.equal( moveConfirmed( far, 1, GOAL ), far );
});

test("a receipt that reports the probe somewhere else confirms nothing", () => {
	const away = { acknowledgedMove: 1, error: null, authoritativePose: { ...HOME, x: HOME.x + 40 } };
	assert.equal( moveConfirmed( away, 0, GOAL ), null );
	const elsewhere = { acknowledgedMove: 1, error: null, authoritativePose: { ...HOME, regionId: HOME.regionId + 1 } };
	assert.equal( moveConfirmed( elsewhere, 0, GOAL ), null );
	assert.equal( moveConfirmed( { acknowledgedMove: 1, error: null, authoritativePose: null }, 0, GOAL ), null );
});

test("a refusal fails with the server's reason, once it is this move's", () => {
	const refused = { acknowledgedMove: 1, error: "endpointBlocked", authoritativePose: HOME };
	assert.throws( () => moveConfirmed( refused, 0, GOAL ), /gameplay error: endpointBlocked/ );
	// The same state before the next move's receipt: the refusal is the
	// last move's, and the retry in the other direction must still wait.
	assert.equal( moveConfirmed( refused, 1, GOAL ), null );
});

test("the gate confirms a move on the native server, which sends nothing at arrival", async t => {
	const simulation = await startStandIn( t, "native" );
	const confirmed = await moveAndConfirm( simulation, GOAL, TEST_BUDGET_MS );
	assert.equal( confirmed.acknowledgedMove, 1 );
	assert.deepEqual( confirmed.authoritativePose, HOME );
	// And the walk back, as the gate does.
	assert.equal( (await moveAndConfirm( simulation, HOME, TEST_BUDGET_MS )).acknowledgedMove, 2 );
});

test("the gate confirms a move on the position stream's server", async t => {
	const simulation = await startStandIn( t, "stream" );
	assert.equal( (await moveAndConfirm( simulation, GOAL, TEST_BUDGET_MS )).acknowledgedMove, 1 );
});

test("the gate fails a refused move with the server's reason and what it saw", async t => {
	const simulation = await startStandIn( t, "refused" );
	await assert.rejects(
		moveAndConfirm( simulation, GOAL, TEST_BUDGET_MS ),
		/gameplay error: endpointBlocked; destination .*"acknowledgedMove":1.*world batches since the request/
	);
});

test("the gate fails a move the server never answers, with the state it last held", async t => {
	const simulation = await startStandIn( t, "silent" );
	await assert.rejects(
		moveAndConfirm( simulation, GOAL, 400 ),
		/gameplay move: timed out after 400 ms .*"acknowledgedMove":0,"pendingMoves":1.*world batches since the request/
	);
});
