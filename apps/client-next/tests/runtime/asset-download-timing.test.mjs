/*
===========================================================================

asset-download-timing.test.mjs - finished attempts are measured for the report

Every download attempt of the shared pipe closes a timing record (#4580):
how long its headers took and how long it ran, so a bug report can tell a
slow link (a long, steady duration) from a stalled request (a first byte
that never came, an attempt that ended as a stall). The stall itself, the
abort and the retry are pinned by the resume and recovery suites; here a
fake fetch stalls to prove the stalled attempt is recorded beside the
attempt that finally delivered, and that a caller's own cancellation is
never recorded as the link's fault.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { NO_PROGRESS_MS, settle, fixture, stallingBody } = await import( "../helpers/asset-download.mjs" );

const PAYLOAD = Uint8Array.of( 1, 2, 3, 4, 5, 6 );
// A whole answer a later attempt may resume: a strong validator, its
// length, byte ranges and no content coding.
const RESUMABLE = {
	status: 200,
	headers: { ETag: '"pack-v1"', "Content-Length": String( PAYLOAD.length ), "Accept-Ranges": "bytes" }
};

/*
================
stall

Lets the current attempt's no-progress window run out, then its backoff.
================
*/
async function stall( t, backoff = 250 ) {
	t.mock.timers.tick( NO_PROGRESS_MS );
	await settle();
	t.mock.timers.tick( backoff );
	await settle();
}

/*
================
flush

Lets the progress beat carry the queued records to the owner.
================
*/
async function flush( t ) {
	t.mock.timers.tick( 1000 );
	await settle();
}

test("a delivered read records its path, first byte, duration and bytes", async t => {
	const { messages, timings } = fixture( t, async () => new Response( PAYLOAD ), PAYLOAD.length );
	await settle();
	assert.equal( messages.length, 1 );
	assert.equal( messages[0].kind, "bytes" );
	await flush( t );
	assert.equal( timings.length, 1 );
	const [record] = timings;
	assert.equal( record.path, "/asset" );
	assert.equal( record.outcome, "ok" );
	assert.equal( record.bytes, PAYLOAD.length );
	assert.ok( record.firstByteMs >= 0, `first byte ${record.firstByteMs}` );
	assert.ok( record.durationMs >= record.firstByteMs, `duration ${record.durationMs}` );
	assert.equal( record.status, undefined );
});

test("a stalled attempt is recorded as a stall beside the attempt that delivers", async t => {
	let attempts = 0;
	const { messages, timings } = fixture( t, async () => {
		if ( ++attempts === 1 ) return stallingBody( [ PAYLOAD.subarray( 0, 2 ) ], RESUMABLE );
		return new Response( PAYLOAD, RESUMABLE );
	}, PAYLOAD.length );
	await settle();
	await stall( t );
	assert.equal( messages.length, 1 );
	assert.equal( messages[0].kind, "bytes" );
	await flush( t );
	assert.deepEqual( timings.map( record => record.outcome ), [ "stall", "ok" ] );
	const [stalled, delivered] = timings;
	// The stalled attempt kept the two bytes its one chunk delivered; its
	// headers had arrived, so its first byte is a real wait, not -1.
	assert.equal( stalled.path, "/asset" );
	assert.equal( stalled.bytes, 2 );
	assert.ok( stalled.firstByteMs >= 0, `first byte ${stalled.firstByteMs}` );
	// Each record counts only its own share: the delivering attempt resumed
	// after the stalled one's two bytes and kept the remaining four.
	assert.equal( delivered.bytes, PAYLOAD.length - 2 );
});

test("a request whose headers never arrive records a first byte of -1", async t => {
	let attempts = 0;
	const { timings } = fixture( t, ( url, options ) => {
		attempts++;
		return new Promise( ( resolve, reject ) => {
			options.signal.addEventListener( "abort", () => reject( options.signal.reason ), { once: true } );
		} );
	}, PAYLOAD.length );
	await settle();
	await stall( t );
	// The retry is the second silent attempt; the first one's record says
	// the headers never came and the attempt ended as a stall.
	assert.equal( attempts, 2 );
	await flush( t );
	assert.ok( timings.length >= 1 );
	assert.equal( timings[0].path, "/asset" );
	assert.equal( timings[0].outcome, "stall" );
	assert.equal( timings[0].firstByteMs, -1 );
	assert.equal( timings[0].bytes, 0 );
});

test("a load the caller cancels leaves no timing record", async t => {
	const { loader, messages, timings } = fixture( t, ( url, options ) => {
		return new Promise( ( resolve, reject ) => {
			options.signal.addEventListener( "abort", () => reject( options.signal.reason ), { once: true } );
		} );
	}, PAYLOAD.length );
	await settle();
	loader.receive( { kind: "cancel", id: 1 } );
	await settle();
	await flush( t );
	assert.equal( messages.at( -1 )?.kind, "released" );
	assert.deepEqual( timings, [] );
});
