/*
===========================================================================

release-watch.test.mjs - tests for release-watch.ts and page-entry.ts

The watch runs against a scripted asset owner that answers the live page
check the way the worker does, so the tests pin when a check is requested
and what a reply means, not how the watch is written.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { pageEntryBundle } = await import( sourceFileUrl( "src/engine/foundation/assets/page-entry.ts" ).href );
const { createReleaseWatch, RELEASE_CHECK_INTERVAL_MS, RELEASE_TRIGGER_GAP_MS, RELEASE_STALE_GAP_MS } = await import(
	sourceFileUrl( "src/engine/runtime/release/release-watch.ts" ).href
);

const PAGE = "https://opensro.online/play";
const RUNNING = "/assets/index-AAAAAAAA.js";

/*
================
scriptedAssets

An asset owner whose page requests stay pending until `reply` answers them.
================
*/
function scriptedAssets() {
	const requests = [], results = new Map(), cancelled = [];
	let nextId = 1, free = 4, stale = false;
	return {
		requests,
		cancelled,
		setStale( value ) {
			stale = value;
		},
		releaseStale: () => stale,
		setFree( slots ) {
			free = slots;
		},
		reply( result ) {
			results.set( requests.at( -1 ).id, result );
		},
		request( url, bytes, decode ) {
			const id = nextId++;
			requests.push( { id, url, bytes, decode } );
			return id;
		},
		take( id ) {
			const result = results.get( id );
			if ( !result ) return null;
			results.delete( id );
			return { ...result, id };
		},
		available: () => free,
		cancel( id ) {
			cancelled.push( id );
		}
	};
}

test("the entry is the first module script, resolved to a path", () => {
	const html =
		`<head><script>var a</script><script src="/legacy.js"></script><script type="module" crossorigin src="/assets/index-BK8x2y3O.js"></script></head>`;
	assert.equal( pageEntryBundle( html, "https://opensro.online" ), "/assets/index-BK8x2y3O.js" );
	assert.equal(
		pageEntryBundle( `<script type=module src='./assets/x-1.js'>`, "https://opensro.online" ),
		"/assets/x-1.js"
	);
	assert.equal( pageEntryBundle( "<html><body>maintenance</body></html>", "https://opensro.online" ), null );
});

test("a watch checks at once, then only on its interval", () => {
	const assets = scriptedAssets();
	const watch = createReleaseWatch( assets, PAGE, RUNNING );
	watch.step( 0, false );
	assert.deepEqual( assets.requests.map( r => [ r.url, r.decode ] ), [ [ PAGE, "release" ] ] );
	assets.reply( { kind: "release", entry: RUNNING } );
	watch.step( 16, false );
	watch.step( RELEASE_CHECK_INTERVAL_MS - 1, false );
	assert.equal( assets.requests.length, 1 );
	watch.step( RELEASE_CHECK_INTERVAL_MS, false );
	assert.equal( assets.requests.length, 2 );
	assert.equal( watch.newerAvailable(), false );
});

test("a different live entry offers the update and stops checking", () => {
	const assets = scriptedAssets();
	const watch = createReleaseWatch( assets, PAGE, RUNNING );
	watch.step( 0, false );
	assets.reply( { kind: "release", entry: "/assets/index-BBBBBBBB.js" } );
	watch.step( 16, false );
	assert.equal( watch.newerAvailable(), true );
	watch.step( 10 * RELEASE_CHECK_INTERVAL_MS, true );
	assert.equal( assets.requests.length, 1 );
});

test("errors and entry-less pages are retried, never read as a release", () => {
	const assets = scriptedAssets();
	const watch = createReleaseWatch( assets, PAGE, RUNNING );
	watch.step( 0, false );
	assets.reply( { kind: "error", message: "HTTP 502" } );
	watch.step( 16, false );
	assert.equal( watch.newerAvailable(), false );
	watch.step( RELEASE_TRIGGER_GAP_MS, true );
	assets.reply( { kind: "release", entry: null } );
	watch.step( RELEASE_TRIGGER_GAP_MS + 16, false );
	assert.equal( assets.requests.length, 2 );
	assert.equal( watch.newerAvailable(), false );
});

test("triggers check early but no more than once per gap", () => {
	const assets = scriptedAssets();
	const watch = createReleaseWatch( assets, PAGE, RUNNING );
	watch.step( 0, false );
	assets.reply( { kind: "release", entry: RUNNING } );
	watch.step( 16, false );
	watch.step( RELEASE_TRIGGER_GAP_MS - 1, true );
	assert.equal( assets.requests.length, 1 );
	watch.step( RELEASE_TRIGGER_GAP_MS, true );
	assert.equal( assets.requests.length, 2 );
});

test("the check waits for free load slots and a development page never checks", () => {
	const busy = scriptedAssets();
	busy.setFree( 1 );
	const watch = createReleaseWatch( busy, PAGE, RUNNING );
	watch.step( 0, true );
	assert.equal( busy.requests.length, 0 );
	busy.setFree( 4 );
	watch.step( 16, false );
	assert.equal( busy.requests.length, 1 );
	watch.dispose();
	assert.deepEqual( busy.cancelled, [ busy.requests[0].id ] );

	const development = scriptedAssets();
	createReleaseWatch( development, PAGE, null ).step( 0, true );
	assert.equal( development.requests.length, 0 );
});

test("a listed file gone 404 checks at once, even with one free slot, and offers the update", () => {
	// A teleport two minutes after a publish asked for the replaced release's
	// region files (BR-261008-2319): the 10 minute interval was still running.
	const assets = scriptedAssets();
	const watch = createReleaseWatch( assets, PAGE, RUNNING );
	watch.step( 0, false );
	assets.reply( { kind: "release", entry: RUNNING } );
	watch.step( 16, false );
	assets.setFree( 1 );
	assets.setStale( true );
	watch.step( RELEASE_STALE_GAP_MS, false );
	assert.equal( assets.requests.length, 2, "the stale file did not check at once" );
	assets.reply( { kind: "release", entry: "/assets/index-BBBBBBBB.js" } );
	watch.step( RELEASE_STALE_GAP_MS + 16, false );
	assert.equal( watch.newerAvailable(), true );
});

test("a stale file whose page names this release rechecks only once per stale gap", () => {
	const assets = scriptedAssets();
	const watch = createReleaseWatch( assets, PAGE, RUNNING );
	assets.setStale( true );
	watch.step( 0, false );
	assets.reply( { kind: "release", entry: RUNNING } );
	watch.step( 16, false );
	watch.step( RELEASE_STALE_GAP_MS - 1, false );
	assert.equal( assets.requests.length, 1 );
	watch.step( RELEASE_STALE_GAP_MS, false );
	assert.equal( assets.requests.length, 2 );
});
