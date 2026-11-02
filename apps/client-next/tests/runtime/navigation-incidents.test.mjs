/*
===========================================================================

navigation-incidents.test.mjs - what a failed navigation stream reports

The navigation stream hands each terminal failure to the runtime once, for
the client incident (#2337). A failure still retrying and a worker's own
admission refusal hand out nothing.

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

test("a permanent navigation failure is handed out once, with the asset it died on", () => {
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
	const failure = stream.terminalFailure();
	assert.ok( failure, "a permanent failure is terminal at once" );
	assert.match( failure.error, /Asset HTTP 404/ );
	assert.equal( failure.path, "/assets/world/world-region-catalog.json" );
	assert.equal( failure.transient, false );
	assert.equal( failure.attempts, 0 );
	assert.equal( stream.terminalFailure(), undefined, "and only once per episode" );
	stream.dispose();
});

test("a transient navigation failure is handed out only when its retries are used up", () => {
	let id = 0;
	const assets = {
		available: () => 4,
		request: () => ++id,
		cancel() {},
		take: () => ({ kind: "error", id, error: "network down", transient: true })
	};
	const stream = createNavigationStream( assets, () => {}, "https://fixture.test" );
	let now = 0;
	stream.step( pose, undefined, undefined, undefined, now );
	// Fail the load, step past the retry delay, fail the retry: the
	// ninth failure is the one with nothing left to schedule.
	for ( let failure = 0; failure <= 8; failure++ ) {
		now += 100;
		stream.step( pose, undefined, undefined, undefined, now );
		assert.match( stream.error(), /network down/ );
		if ( failure < 8 ) {
			assert.equal( stream.terminalFailure(), undefined, "still retrying: nothing to file yet" );
			now += 30000;
			stream.step( pose, undefined, undefined, undefined, now );
		}
	}
	const failure = stream.terminalFailure();
	assert.ok( failure, "the exhausted failure is handed out" );
	assert.equal( failure.transient, true );
	assert.equal( failure.attempts, 8 );
	assert.equal( failure.path, "/assets/world/world-region-catalog.json" );
	assert.equal( stream.terminalFailure(), undefined, "and only once per episode" );
	stream.dispose();
});

test("an admission rejection hands out no asset failure", () => {
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
	step( undefined, { region: 257, requestId: sent[0].requestId, error: "Invalid navigation product" } );
	assert.equal( stream.phase(), "failed" );
	assert.equal( stream.terminalFailure(), undefined, "the worker's refusal is not an asset failure" );
	stream.dispose();
});
