/*
===========================================================================

frontend-stage-recovery.test.mjs - the title stage outlives a transient load failure

The live failure group behind #2862: "Asset worker timed out while loading
/assets/world/constantinople/region-694e.json" at the title screen. The
asset worker marks that failure transient, but the frontend stage turned
the first one into a permanent stage error, so the frontend flow failed
for good. The stage now retries on the bounded recovery clock the world
and navigation streams use (asset-recovery.ts), and fails only once that
sequence is exhausted or the failure is permanent.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const { createFrontendStage } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/frontend/stage/stage.ts" ) ).href
);

const TIMED_OUT = "Asset worker timed out while loading /assets/world/constantinople/region-694e.json";
// Longer than the recovery clock's largest backoff (asset-recovery.ts).
const PAST_BACKOFF_MS = 60000;

/*
================
harness

An asset owner whose results the test hands out, and a renderer that
admits whatever world it is given.
================
*/
function harness() {
	let serial = 0, sceneId = null;
	const results = new Map(), requests = [];
	const assets = {
		available: () => 4,
		request: ( url, _limit, kind ) => {
			requests.push( { url: new URL( url ).pathname, kind } );
			return ++serial;
		},
		take: id => {
			const result = results.get( id );
			results.delete( id );
			return result;
		},
		cancel: id => results.delete( id )
	};
	const renderer = {
		cancelWorldUpdate() {},
		adoptWorld: world => {
			sceneId = world.sceneId;
		},
		settleWorld: () => true,
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => ({ sceneId, pendingGroups: 0, pendingTextures: 0 })
	};
	const stage = createFrontendStage( assets, renderer, "http://localhost" );
	const answer = result => results.set( serial, { id: serial, ...result } );
	const manifest = () =>
		answer( {
			kind: "bytes",
			buffer: new TextEncoder().encode( JSON.stringify( { regionBundlePublicPath: "/assets/world/title.json" } ) )
				.buffer
		} );
	return { stage, requests, answer, manifest };
}

test("a transient world failure at the title is retried, not fatal (#2862)", () => {
	const { stage, requests, answer, manifest } = harness();
	let now = 1000;
	stage.install( "/assets/title/manifest.json", "full", "settle" );
	stage.step( now );
	manifest();
	stage.step( now );
	assert.equal( requests.length, 2, "manifest, then the world bundle" );
	answer( { kind: "error", error: TIMED_OUT, transient: true } );
	stage.step( now );
	assert.equal( stage.error(), null, "a transient failure does not fail the stage" );
	assert.match( stage.status(), /retrying/ );
	stage.step( now += 100 );
	assert.equal( requests.length, 2, "no request leaves before the retry is due" );
	stage.step( now += PAST_BACKOFF_MS );
	assert.equal( requests.length, 3, "the world bundle is requested again" );
	assert.equal( requests[2].kind, "frontend-world" );
	answer( { kind: "world", world: { sceneId: "title" } } );
	stage.step( now );
	assert.equal( stage.error(), null );
	assert.equal( stage.ready(), true, "the retried scene is drawn" );
});

test("a transient manifest failure is retried too", () => {
	const { stage, requests, answer, manifest } = harness();
	let now = 0;
	stage.install( "/assets/title/manifest.json", "full", "settle" );
	stage.step( now );
	answer( {
		kind: "error",
		error: "Asset worker timed out while loading /assets/title/manifest.json",
		transient: true
	} );
	stage.step( now );
	assert.equal( stage.error(), null );
	stage.step( now += PAST_BACKOFF_MS );
	assert.deepEqual( requests.map( row => row.url ), [
		"/assets/title/manifest.json",
		"/assets/title/manifest.json"
	] );
	manifest();
	stage.step( now );
	assert.equal( requests.at( -1 ).kind, "frontend-world" );
});

test("an exhausted retry sequence fails the stage with the asset's message", () => {
	const { stage, requests, answer, manifest } = harness();
	let now = 0;
	stage.install( "/assets/title/manifest.json", "full", "settle" );
	stage.step( now );
	manifest();
	stage.step( now );
	// Eight automatic retries (asset-recovery.ts), then the ninth failure is final.
	for ( let failure = 0; failure < 8; failure++ ) {
		answer( { kind: "error", error: TIMED_OUT, transient: true } );
		stage.step( now );
		assert.equal( stage.error(), null, "failure " + (failure + 1) + " still retries" );
		stage.step( now += PAST_BACKOFF_MS );
	}
	assert.equal( requests.length, 10, "manifest, the world bundle and eight retries" );
	answer( { kind: "error", error: TIMED_OUT, transient: true } );
	stage.step( now );
	assert.match( stage.error(), /Asset worker timed out while loading/ );
});

test("a permanent failure still fails the stage at once", () => {
	const { stage, answer, manifest } = harness();
	stage.install( "/assets/title/manifest.json", "full", "settle" );
	stage.step( 0 );
	manifest();
	stage.step( 0 );
	answer( { kind: "error", error: "Asset hash mismatch" } );
	stage.step( 0 );
	assert.match( stage.error(), /Asset hash mismatch/ );
});

test("a completed load ends the failure sequence", () => {
	const { stage, requests, answer, manifest } = harness();
	let now = 0;
	stage.install( "/assets/title/manifest.json", "full", "settle" );
	stage.step( now );
	answer( {
		kind: "error",
		error: "Asset worker timed out while loading /assets/title/manifest.json",
		transient: true
	} );
	stage.step( now );
	stage.step( now += PAST_BACKOFF_MS );
	manifest();
	stage.step( now );
	// The manifest completed: the world bundle starts a fresh sequence of eight.
	for ( let failure = 0; failure < 8; failure++ ) {
		answer( { kind: "error", error: TIMED_OUT, transient: true } );
		stage.step( now );
		assert.equal( stage.error(), null, "world failure " + (failure + 1) + " still retries" );
		stage.step( now += PAST_BACKOFF_MS );
	}
	assert.equal( requests.filter( row => row.kind === "frontend-world" ).length, 9 );
});
