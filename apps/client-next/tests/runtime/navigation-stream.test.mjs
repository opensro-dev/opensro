/*
===========================================================================

navigation-stream.test.mjs - the client navigation stream's scheduling

Covers runtime/navigation/navigation.ts: which load or admission a sector
crossing keeps, when one is cancelled, and how transport failures recover.
The clipper and decoder tests stay in navigation.test.mjs.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product, pose } from "../helpers/navigation-fixture.mjs";
const { createNavigationStream } = await import(
	sourceFileUrl( "src/engine/runtime/navigation/navigation.ts" ).href
);
/*
================
streamFixture

A navigation stream over a scripted asset owner: requests are recorded,
completions are set by the test, cancellations are recorded. The catalog
publishes 0x0101 to 0x0103, one sector apart along x.
================
*/
function streamFixture() {
	let id = 0;
	const pending = new Map(), sent = [], cancelled = [], requests = [];
	const assets = {
		available: () => 4,
		/*
================
request
================
		*/
		request( url, limit, decode ) {
			requests.push( { url, decode } );
			pending.set( ++id, null );
			return id;
		},
		/*
================
take
================
		*/
		take( id ) {
			const p = pending.get( id );
			if ( p ) pending.delete( id );
			return p;
		},
		/*
================
cancel
================
		*/
		cancel( id ) {
			cancelled.push( id );
			pending.delete( id );
		}
	};
	const stream = createNavigationStream( assets, c => sent.push( c ), "https://fixture.test" );
	stream.step( pose );
	pending.set( 1, {
		kind: "bytes",
		buffer: Buffer.from(
			JSON.stringify( {
				regionsById: {
					"0x0101": [ { bundlePublicPath: "/assets/a.json" } ],
					"0x0102": [ { bundlePublicPath: "/assets/b.json" } ],
					"0x0103": [ { bundlePublicPath: "/assets/c.json" } ]
				}
			} )
		)
	} );
	stream.step( pose );
	assert.equal( requests[1].decode, "navigation" );
	assert.equal( new URL( requests[1].url ).hash, "#257" );
	return { stream, pending, sent, cancelled, requests };
}
test("a crossing keeps the load whose neighbourhood still covers the walker, then loads the new sector", () => {
	// #3928: a rider crossed a sector about every 8 s while each load took
	// longer, so cancelling at every crossing left the walk off coverage.
	const { stream, pending, sent, cancelled, requests } = streamFixture();
	const next = { ...pose, regionId: 258 };
	stream.step( next );
	assert.deepEqual( cancelled, [] );
	assert.equal( stream.phase(), "loading" );
	pending.set( 2, { kind: "navigation", product: product() } );
	stream.step( next );
	assert.equal( sent.length, 1 );
	assert.equal( sent[0].regionId, 257 );
	assert.equal( stream.phase(), "admitting" );
	stream.step( next, 257, undefined, sent[0].requestId );
	assert.equal( requests.length, 3 );
	assert.equal( new URL( requests[2].url ).hash, "#258" );
	assert.deepEqual( cancelled, [] );
	stream.dispose();
});
test("a crossing during admission keeps the admission, then loads the new sector", () => {
	const { stream, pending, sent, cancelled, requests } = streamFixture();
	pending.set( 2, { kind: "navigation", product: product() } );
	stream.step( pose );
	assert.equal( stream.phase(), "admitting" );
	const next = { ...pose, regionId: 258 };
	stream.step( next );
	assert.equal( stream.phase(), "admitting" );
	stream.step( next, 257, undefined, sent[0].requestId );
	assert.deepEqual( cancelled, [] );
	assert.equal( new URL( requests[2].url ).hash, "#258" );
	stream.dispose();
});
test("a crossing past the loading sector's neighbourhood cancels it; reset requires fresh collision admission", () => {
	const { stream, pending, sent, cancelled, requests } = streamFixture();
	const far = { ...pose, regionId: 259 };
	stream.step( far );
	assert.deepEqual( cancelled, [ 2 ] );
	pending.set( 2, { kind: "navigation", product: product() } );
	pending.set( 3, { kind: "navigation", product: product( 259 ) } );
	stream.step( far );
	assert.equal( sent.length, 1 );
	assert.equal( sent[0].regionId, 259 );
	assert.equal( stream.phase(), "admitting" );
	stream.step( far, 257 );
	assert.equal( stream.phase(), "admitting" );
	stream.step( far, 259, undefined, sent[0].requestId );
	assert.equal( stream.phase(), "ready" );
	stream.reset();
	stream.step( far );
	assert.equal( requests.length, 4 );
	stream.dispose();
	assert.ok( cancelled.includes( 4 ) );
	assert.equal( stream.error(), null );
});

test("a dungeon load covers its own region only, so a move to another dungeon region cancels it", () => {
	// Every dungeon pair is one plane (samePlaneAdjacent), but a dungeon
	// product is admitted for its origin alone (clipper available).
	const requests = [], cancelled = [];
	const assets = {
		available: () => 4,
		/*
================
request
================
		*/
		request( url, limit, decode ) {
			requests.push( { url, decode } );
			return requests.length;
		},
		take: () => null,
		/*
================
cancel
================
		*/
		cancel( id ) {
			cancelled.push( id );
		}
	};
	const stream = createNavigationStream( assets, () => {}, "https://fixture.test" );
	stream.step( { ...pose, regionId: 0x8001 } );
	assert.equal( requests.length, 1 );
	stream.step( { ...pose, regionId: 0x8002 } );
	assert.deepEqual( cancelled, [ 1 ] );
	assert.equal( new URL( requests[1].url ).hash, "#" + 0x8002 );
	stream.dispose();
});

for ( const resume of [ "clock", "online", "region" ] ) {
	test(`navigation recovers transient transport failure via ${resume}`, () => {
		let id = 0;
		const pending = new Map(), sent = [], cancelled = [];
		const assets = {
			available: () => 4,
			request: () => ++id,
			take: key => {
				const value = pending.get( key );
				pending.delete( key );
				return value ?? null;
			},
			cancel: key => {
				cancelled.push( key );
				pending.delete( key );
			}
		};
		const stream = createNavigationStream( assets, command => sent.push( command ), "https://fixture.test" );
		let current = { ...pose, regionId: 0x8001 };
		const step = now => stream.step( current, undefined, undefined, undefined, now );
		step( 0 );
		pending.set( id, { kind: "error", id, error: "Load failed", transient: true } );
		step( 100 );
		assert.equal( stream.reconnecting(), true );
		step( 2099 );
		assert.equal( id, 1 );
		if ( resume === "online" ) stream.retryTransient();
		if ( resume === "region" ) current = { ...current, regionId: 0x8002 };
		step( resume === "clock" ? 2100 : 2099 );
		assert.equal( id, 2 );
		pending.set( id, { kind: "navigation", id, product: product( current.regionId ) } );
		step( 2200 );
		assert.equal( sent.length, 1 );
		assert.equal( stream.phase(), "admitting" );
		stream.step( current, current.regionId, undefined, sent[0].requestId, 2300 );
		assert.equal( stream.phase(), "ready" );
		assert.equal( stream.error(), null );
		stream.dispose();
		stream.retryTransient();
		step( 100000 );
		assert.equal( id, 2 );
	});
}

test("an unanswered navigation load retries, an unanswered admission does not", () => {
	let id = 0;
	const sent = [], answers = new Map();
	const assets = {
		available: () => 4,
		request: () => ++id,
		cancel() {},
		take: key => answers.get( key ) ?? null
	};
	const stream = createNavigationStream( assets, command => sent.push( command ), "https://fixture.test" );
	const dungeon = { ...pose, regionId: 0x8001 },
		step = now => stream.step( dungeon, undefined, undefined, undefined, now );
	step( 0 );
	// No answer for the 60 s load deadline: a transport symptom, retried.
	step( 60000 );
	assert.match( stream.error(), /loading timed out/ );
	assert.equal( stream.reconnecting(), true );
	step( 62000 );
	assert.equal( id, 2, "the load is requested again" );
	answers.set( 2, { kind: "navigation", id: 2, product: product( dungeon.regionId ) } );
	step( 62100 );
	assert.equal( stream.phase(), "admitting" );
	// No admission within 15 s is the simulation's answer, not the network's.
	step( 77100 );
	assert.match( stream.error(), /admitting timed out/ );
	assert.equal( stream.reconnecting(), false );
	stream.retryTransient();
	step( 1000000 );
	assert.equal( id, 2, "an admission timeout never retries by itself" );
	stream.dispose();
});

test("a navigation load that keeps delivering bytes is slow, not timed out", () => {
	let id = 0, bytesRead = 0;
	const sent = [], answers = new Map();
	const assets = {
		available: () => 4,
		request: () => ++id,
		cancel() {},
		take: key => answers.get( key ) ?? null,
		progress: () => ({
			bytesRead,
			bytesReceived: bytesRead,
			bytesPerSecond: 40000,
			filesReady: 0,
			filesActive: 1,
			cacheHits: 0,
			currentFile: ""
		})
	};
	const stream = createNavigationStream( assets, command => sent.push( command ), "https://fixture.test" );
	const dungeon = { ...pose, regionId: 0x8001 },
		step = now => stream.step( dungeon, undefined, undefined, undefined, now );
	step( 0 );
	// A 40 KB/s link keeps delivering: five minutes with no answer from
	// the worker, but the byte count grows at every step, so the load
	// lives and is never re-requested (GH #1244).
	for ( let now = 10000; now <= 300000; now += 10000 ) {
		bytesRead += 400000;
		step( now );
		assert.equal( stream.error(), null, "still loading at " + now );
		assert.equal( stream.phase(), "loading" );
	}
	assert.equal( id, 1, "no re-request while bytes flow" );
	// The bytes stop: one full deadline window of silence expires it.
	step( 300000 + 60000 );
	assert.match( stream.error(), /loading timed out/ );
	assert.equal( stream.reconnecting(), true );
	stream.dispose();
});

test("navigation permanent failures ignore automatic recovery", () => {
	let id = 0;
	const assets = {
		available: () => 4,
		request: () => ++id,
		cancel() {},
		take: () => ({ kind: "error", id, error: "Asset HTTP 404" })
	};
	const stream = createNavigationStream( assets, () => {}, "https://fixture.test" );
	stream.step( pose, undefined, undefined, undefined, 0 );
	stream.step( pose, undefined, undefined, undefined, 100 );
	stream.retryTransient();
	stream.step( pose, undefined, undefined, undefined, 1000000 );
	assert.equal( id, 1 );
	assert.equal( stream.reconnecting(), false );
	assert.match( stream.error(), /404/ );
	stream.retry();
	stream.step( pose );
	assert.equal( id, 2 );
	stream.dispose();
});
test("admission rejection fails immediately; retry ignores stale acknowledgements and missing replies time out", () => {
	const pending = new Map(), sent = [];
	let id = 0;
	const assets = {
		available: () => 4,
		request: () => ++id,
		cancel: () => {},
		take: i => {
			const r = pending.get( i );
			pending.delete( i );
			return r;
		}
	};
	const stream = createNavigationStream( assets, c => sent.push( c ), "https://fixture.test" );
	const step = ( region, fail, ack, now = 0 ) => stream.step( pose, region, fail, ack, now );
	step();
	pending.set( 1, {
		kind: "bytes",
		buffer: Buffer.from(
			JSON.stringify( { regionsById: { "0x0101": [ { bundlePublicPath: "/assets/a.json" } ] } } )
		)
	} );
	step();
	pending.set( 2, { kind: "navigation", product: product() } );
	step();
	const failure = { region: 257, requestId: sent[0].requestId, error: "Invalid navigation product" };
	step( undefined, failure );
	assert.equal( stream.phase(), "failed" );
	assert.equal( stream.error(), failure.error );
	stream.retry();
	step();
	pending.set( 3, { kind: "navigation", product: product() } );
	step();
	step( 257, failure, sent[0].requestId );
	assert.equal( stream.phase(), "admitting" );
	step( 257, undefined, sent[1].requestId );
	assert.equal( stream.phase(), "ready" );
	stream.reset();
	step();
	pending.set( 4, { kind: "navigation", product: product() } );
	step();
	step( undefined, undefined, undefined, 15000 );
	assert.equal( stream.phase(), "failed" );
	assert.match( stream.error(), /admitting timed out/ );
	stream.retry();
	step();
	step( undefined, undefined, undefined, 60000 );
	assert.match( stream.error(), /loading timed out/ );
});
