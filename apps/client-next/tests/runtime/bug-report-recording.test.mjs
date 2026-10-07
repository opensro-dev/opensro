/*
===========================================================================

bug-report-recording.test.mjs - the reporter's Record/Stop cycle

Drives recording.ts with a fake recorder whose starts and drains resolve
when the test says, so the orders a player can produce are exact: Record,
Stop and Record again before the first start answers; Stop during a start;
the server's cap; a recorder that dies mid-recording. Also the settings an
older Agent sends: its replayDefault no longer records anything.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createRecording } = await import( sourceFileUrl( "src/engine/runtime/bug-report/recording.ts" ).href );
const { readSettings } = await import( sourceFileUrl( "src/engine/runtime/bug-report/bug-report.ts" ).href );

const TRACK = { width: 2, height: 2, avcC: Uint8Array.of( 1 ), samples: [] };

/*
================
deferred
================
*/
function deferred() {
	let resolve;
	const promise = new Promise( done => resolve = done );
	return { promise, resolve };
}

/*
================
harness

A fake recorder whose start() and complete() wait for the test, and a
host that records what the player would see.
================
*/
function harness( { maxSeconds = 60 } = {} ) {
	const starts = [], drains = [];
	let running = false, nowMs = 0;
	const recorder = {
		stops: 0,
		start() {
			const answer = deferred();
			starts.push( answer );
			return answer.promise.then( started => {
				running = started;
				return started;
			} );
		},
		stop() {
			recorder.stops++;
			running = false;
		},
		complete() {
			const answer = deferred();
			drains.push( answer );
			return answer.promise.then( track => {
				running = false;
				return track;
			} );
		},
		running: () => running,
		watch() {},
		lastError: () => null,
		die() {
			running = false;
		}
	};
	const shown = [], failures = [], finished = [];
	const recording = createRecording( {
		recorder,
		now: () => nowMs,
		maxSeconds: () => maxSeconds,
		show: ( phase, seconds ) => shown.push( [ phase, Math.floor( seconds ) ] ),
		failed: reason => failures.push( reason ),
		finished: ( track, text ) => finished.push( { track, text } )
	} );
	return {
		recording,
		recorder,
		starts,
		drains,
		shown,
		failures,
		finished,
		advance( ms ) {
			nowMs += ms;
		}
	};
}

const settle = () => new Promise( resolve => setImmediate( resolve ) );

test("Record, Stop, Record: the first start's late answer cannot stop the second", async () => {
	const h = harness();
	h.recording.record();
	h.recording.stop( "" );
	h.recording.record();
	assert.equal( h.recording.phase(), "starting" );
	// The first start answers while the second is still pending, and false:
	// the recorder generation it belonged to was stopped.
	h.starts[0].resolve( false );
	await settle();
	assert.equal( h.recording.phase(), "starting", "the second start is still pending" );
	h.starts[1].resolve( true );
	await settle();
	assert.equal( h.recording.phase(), "recording", "the second recording keeps running" );
	assert.deepEqual( h.failures, [] );
	assert.equal( h.recorder.stops, 1, "only the Stop stopped the recorder" );
});

test("Stop while the start is pending abandons it", async () => {
	const h = harness();
	h.recording.record();
	h.recording.stop( "" );
	assert.equal( h.recording.phase(), "idle" );
	h.starts[0].resolve( true );
	await settle();
	assert.equal( h.recording.phase(), "idle", "a late successful start does not resume" );
	assert.deepEqual( h.finished, [] );
});

test("Stop drains, then opens the window with the recording and the /bug text", async () => {
	const h = harness();
	h.recording.record();
	h.starts[0].resolve( true );
	await settle();
	h.recording.stop( "the door is stuck" );
	assert.equal( h.recording.phase(), "finishing" );
	assert.equal( h.shown.at( -1 )[0], "finishing", "the control says so at once" );
	h.drains[0].resolve( TRACK );
	await settle();
	assert.equal( h.recording.phase(), "idle" );
	assert.deepEqual( h.finished, [ { track: TRACK, text: "the door is stuck" } ] );
	assert.equal( h.recording.recorded(), TRACK, "kept for a window closed by accident" );
	h.recording.sent( TRACK );
	assert.equal( h.recording.recorded(), null, "a sent recording is not offered again" );
});

test("the server's cap ends the recording on wall time", async () => {
	const h = harness( { maxSeconds: 5 } );
	h.recording.record();
	h.starts[0].resolve( true );
	await settle();
	h.advance( 4200 );
	h.recording.frame();
	assert.deepEqual( h.shown.at( -1 ), [ "recording", 4 ] );
	h.advance( 900 );
	h.recording.frame();
	assert.equal( h.recording.phase(), "finishing" );
	assert.equal( h.drains.length, 1 );
});

test("a recorder that stops on its own is reported, not left recording", async () => {
	const h = harness();
	h.recording.record();
	h.starts[0].resolve( true );
	await settle();
	h.recorder.die();
	h.recording.frame();
	assert.equal( h.recording.phase(), "idle" );
	assert.equal( h.failures.length, 1 );
});

test("nothing records while reports are off", () => {
	const h = harness( { maxSeconds: 0 } );
	h.recording.record();
	assert.equal( h.recording.phase(), "idle" );
	assert.equal( h.starts.length, 0 );
});

test("an older Agent's replayDefault parses and records nothing", () => {
	const settings = readSettings( { enabled: true, replayDefault: true, maxBytes: 1048576, replaySeconds: 30 } );
	assert.deepEqual( settings, {
		enabled: true,
		maxBytes: 1048576,
		maxDiagnosticsBytes: 0,
		replaySeconds: 30,
		destinations: []
	} );
	const h = harness( { maxSeconds: settings.replaySeconds } );
	assert.equal( h.recording.phase(), "idle" );
	assert.equal( h.starts.length, 0, "no recording starts from the settings alone" );
});
