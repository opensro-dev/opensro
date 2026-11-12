/*
===========================================================================

asset-download-resume.test.mjs - a stalled read resumes after its kept bytes

A path that stalls between bursts used to restart every attempt from the
first byte, so after three attempts the whole load failed and the player
could not enter the game at all (#3354). A retry now asks for the tail
after the bytes earlier attempts kept, and an attempt that carried the
read further spends no retry. A path that never delivers still fails.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createLoader } = await import( "../../src/engine/runtime/assets/worker/loader.ts" );
const { NO_PROGRESS_MS, settle, fixture, packedAsset, stallingBody } = await import( "../helpers/asset-download.mjs" );
const { defined } = await import( "../helpers/defined.mjs" );

const PAYLOAD = Uint8Array.of( 1, 2, 3, 4, 5, 6 );
const ETAG = '"pack-v1"';
// A whole answer a later attempt may resume: a strong validator, its
// length, byte ranges and no content coding.
const RESUMABLE = {
	status: 200,
	headers: { ETag: ETAG, "Content-Length": String( PAYLOAD.length ), "Accept-Ranges": "bytes" }
};

/*
================
tail

The 206 answer for the payload from start on, under the given validator.
================
*/
function tail( start, chunks, etag = ETAG ) {
	return stallingBody( chunks, {
		status: 206,
		headers: { ETag: etag, "Content-Range": `bytes ${start}-${PAYLOAD.length - 1}/${PAYLOAD.length}` }
	} );
}

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

test("a whole read that stalls between bursts resumes each time and completes", async t => {
	const ranges = [];
	const answers = [
		() => stallingBody( [ PAYLOAD.subarray( 0, 2 ) ], RESUMABLE ),
		() => tail( 2, [ PAYLOAD.subarray( 2, 4 ) ] ),
		() => tail( 4, [ PAYLOAD.subarray( 4, 5 ) ] ),
		() =>
			new Response( PAYLOAD.slice( 5 ), {
				status: 206,
				headers: { ETag: ETAG, "Content-Range": `bytes 5-5/${PAYLOAD.length}` }
			} )
	];
	const { messages } = fixture( t, async ( url, options ) => {
		ranges.push( options?.headers?.Range ?? null );
		return answers[ranges.length - 1]();
	}, PAYLOAD.length );
	await settle();
	// Every stall after new bytes spends no retry: four attempts, past the
	// three a read without progress gets, and each asks only for its tail.
	for ( let i = 0; i < 3; i++ ) await stall( t );
	assert.deepEqual( ranges, [ null, "bytes=2-", "bytes=4-", "bytes=5-" ] );
	assert.equal( messages.length, 1 );
	assert.equal( messages[0].kind, "bytes" );
	assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ ...PAYLOAD ] );
});

test("a read that stops delivering after a burst fails once a fresh budget is spent", async t => {
	// The burst restarts the budget, and the attempt that carried it counts
	// as its first: two more silent attempts, then the transient failure.
	let attempts = 0;
	const { messages } = fixture( t, ( url, options ) => {
		if ( ++attempts === 1 ) return Promise.resolve( stallingBody( [ PAYLOAD.subarray( 0, 2 ) ], RESUMABLE ) );
		return new Promise( ( resolve, reject ) => {
			options.signal.addEventListener( "abort", () => reject( options.signal.reason ), { once: true } );
		} );
	}, PAYLOAD.length );
	await settle();
	await stall( t );
	await stall( t, 1000 );
	assert.equal( attempts, 3 );
	assert.equal( messages.length, 0 );
	t.mock.timers.tick( NO_PROGRESS_MS );
	await settle();
	assert.equal( messages.length, 1 );
	assert.equal( messages[0].kind, "error" );
	assert.match( messages[0].error, /No download progress/ );
	assert.equal( messages[0].transient, true );
});

test("a server that answers the resume with the whole object starts the read over", async t => {
	const ranges = [];
	const { messages } = fixture( t, async ( url, options ) => {
		ranges.push( options?.headers?.Range ?? null );
		if ( ranges.length === 1 ) return stallingBody( [ PAYLOAD.subarray( 0, 3 ) ], RESUMABLE );
		return new Response( PAYLOAD, RESUMABLE );
	}, PAYLOAD.length );
	await settle();
	await stall( t );
	assert.deepEqual( ranges, [ null, "bytes=3-" ] );
	assert.equal( messages.length, 1 );
	assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ ...PAYLOAD ], "no kept prefix doubled" );
});

for (
	const { name, answer } of [
		{ name: "another validator", answer: () => tail( 2, [ PAYLOAD.subarray( 2 ) ], '"pack-v2"' ) },
		{ name: "416", answer: () => new Response( null, { status: 416 } ) },
		{
			name: "another range",
			answer: () =>
				new Response( PAYLOAD, {
					status: 206,
					headers: { ETag: ETAG, "Content-Range": `bytes 0-5/${PAYLOAD.length}` }
				} )
		}
	]
) {
	test(`a resume answered with ${name} is not trusted: the next attempt starts over`, async t => {
		const ranges = [];
		const { messages } = fixture( t, async ( url, options ) => {
			ranges.push( options?.headers?.Range ?? null );
			if ( ranges.length === 1 ) return stallingBody( [ PAYLOAD.subarray( 0, 2 ) ], RESUMABLE );
			if ( ranges.length === 2 ) return answer();
			return new Response( PAYLOAD, RESUMABLE );
		}, PAYLOAD.length );
		await settle();
		await stall( t );
		// The refused resume is the read's second attempt of its budget.
		t.mock.timers.tick( 1000 );
		await settle();
		assert.deepEqual( ranges, [ null, "bytes=2-", null ] );
		assert.equal( messages.length, 1 );
		assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ ...PAYLOAD ] );
	});
}

for (
	const { name, headers } of [
		{ name: "no validator", headers: { "Content-Length": "6", "Accept-Ranges": "bytes" } },
		{ name: "a weak validator", headers: { ETag: 'W/"pack-v1"', "Content-Length": "6", "Accept-Ranges": "bytes" } },
		{ name: "no byte ranges", headers: { ETag: ETAG, "Content-Length": "6" } },
		{
			name: "a content coding",
			headers: { ETag: ETAG, "Content-Length": "6", "Accept-Ranges": "bytes", "Content-Encoding": "br" }
		}
	]
) {
	test(`a whole answer with ${name} is not resumed: the retry starts over`, async t => {
		const ranges = [];
		const { messages } = fixture( t, async ( url, options ) => {
			ranges.push( options?.headers?.Range ?? null );
			if ( ranges.length === 1 ) return stallingBody( [ PAYLOAD.subarray( 0, 2 ) ], { status: 200, headers } );
			return new Response( PAYLOAD );
		}, PAYLOAD.length );
		await settle();
		await stall( t );
		assert.deepEqual( ranges, [ null, null ] );
		assert.equal( messages.length, 1 );
		assert.deepEqual( [ ...new Uint8Array( messages[0].buffer ) ], [ ...PAYLOAD ] );
	});
}

test("a ranged pack block that stalls resumes its range after the kept bytes", async t => {
	t.mock.timers.enable( { apis: [ "setTimeout" ] } );
	const asset = packedAsset( false ), messages = [], later = [];
	const total = asset.manifest.groups[0].packs[0].bytes, kept = 100;
	let stalledAt = -1, consumed = () => {}, deliver = () => {};
	// Resolved once the reader asked past the first chunk: the loader has
	// kept it, so the stall that follows must resume after it.
	/** @type {Promise<void>} */
	const keptFirst = new Promise( resolve => {
		consumed = resolve;
	} );
	/** @type {Promise<void>} */
	const delivered = new Promise( resolve => {
		deliver = resolve;
	} );
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		if ( stalledAt < 0 && asset.isMember( options ) ) {
			// The first member read delivers its first bytes, then stalls.
			const [start, end] = defined( /bytes=(\d+)-(\d+)/.exec( options.headers.Range ) ).slice( 1 ).map( Number );
			stalledAt = start;
			const block = new Uint8Array( await asset.serve( url, options ).arrayBuffer() );
			let pulls = 0;
			return new Response(
				new ReadableStream( {
					/*
					================
					pull
					================
					*/
					pull( controller ) {
						if ( pulls++ === 0 ) return controller.enqueue( block.slice( 0, kept ) );
						consumed();
						return new Promise( () => {} );
					}
				} ),
				{ status: 206, headers: { "Content-Range": `bytes ${start}-${end}/${total}` } }
			);
		}
		if ( stalledAt >= 0 ) later.push( options?.headers?.Range ?? null );
		return asset.serve( url, options );
	} );
	const loader = createLoader( message => {
		if (
			![ "progress", "job-progress", "job-decoding", "job-fetching", "hd-availability" ].includes( message.kind )
		) {
			messages.push( message );
		}
		if ( message.kind === "bytes" || message.kind === "error" ) deliver();
	} );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "load", id: 1, url: `https://example.test${asset.path}`, limit: asset.bytes.length } );
	await keptFirst;
	await stall( t );
	await delivered;
	assert.ok( later[0]?.startsWith( `bytes=${stalledAt + kept}-` ), `the retry asked ${later[0]}` );
	assert.equal( messages.length, 1 );
	assert.equal( messages[0].kind, "bytes" );
	assert.deepEqual( new Uint8Array( messages[0].buffer ), asset.bytes );
});
