/*
===========================================================================

pack-mirror.test.mjs - the desktop launcher's local pack copy

On macOS and Linux the launcher cannot intercept the page's https pack
reads, so it serves its copy under its own scheme and names it in a page
global (pack-mirror.ts). The asset worker reads a pack there first and
falls back to the published origin on any failure; only the launcher's own
origins are accepted, and without the global nothing changes.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createLoader } = await import( "../../src/engine/runtime/assets/worker/loader.ts" );
const { createPackMirror, packMirrorOrigin, PACK_MIRROR_GLOBAL } = await import(
	"../../src/engine/foundation/assets/pack-mirror.ts"
);
const { settle, packedAsset } = await import( "../helpers/asset-download.mjs" );

const ORIGIN = "https://opensro.online", MIRROR = "sropack://localhost";
const PACK = ORIGIN + "/assets/packs/game-images-004-0123456789ab.bin";
// The loader checks each block with crypto.subtle.digest, which runs on the
// thread pool in real time: a count of event-loop turns is no deadline on a
// busy runner (#4017), so the wait is a wall-clock one.
const LOAD_DEADLINE_MS = 10000;

/*
================
load

Loads the fixture's one member through a loader whose page named origin as
its mirror. answer() stands in for the mirror: it gets the request and the
host's own answer, and returns what the mirror says. Returns the reads,
as "network" or "mirror" plus the path, and the delivered messages.
================
*/
async function load( t, origin, answer = ( url, options, served ) => served() ) {
	const asset = packedAsset( false ), reads = [], messages = [];
	t.mock.method( globalThis, "fetch", async ( url, options ) => {
		const parsed = new URL( url ), served = () => asset.serve( ORIGIN + parsed.pathname, options );
		if ( url.startsWith( MIRROR ) ) {
			reads.push( "mirror " + parsed.pathname );
			return answer( url, options, served );
		}
		reads.push( "network " + parsed.pathname );
		return served();
	} );
	const loader = createLoader( message => {
		if ( message.kind === "bytes" || message.kind === "error" ) messages.push( message );
	} );
	t.after( () => loader.dispose() );
	loader.receive( { kind: "pack-mirror", origin } );
	loader.receive( { kind: "load", id: 1, url: ORIGIN + asset.path, limit: asset.bytes.length } );
	const deadline = Date.now() + LOAD_DEADLINE_MS;
	while ( messages.length === 0 && Date.now() < deadline ) await settle();
	assert.equal( messages.length, 1, "the load settled" );
	assert.equal( messages[0].kind, "bytes", messages[0].error );
	assert.deepEqual( new Uint8Array( messages[0].buffer ), asset.bytes );
	return reads.filter( read => !read.endsWith( "manifest.json" ) );
}

test("a page outside the desktop app has no mirror", () => {
	assert.equal( packMirrorOrigin( {} ), null );
	assert.equal( packMirrorOrigin( { [PACK_MIRROR_GLOBAL]: MIRROR } ), MIRROR );
	// Only the launcher's own origins: never another host, never a path.
	assert.equal( packMirrorOrigin( { [PACK_MIRROR_GLOBAL]: "https://evil.example" } ), null );
	assert.equal( packMirrorOrigin( { [PACK_MIRROR_GLOBAL]: MIRROR + "/x" } ), null );
	assert.equal( packMirrorOrigin( { [PACK_MIRROR_GLOBAL]: 1 } ), null );
});

test("only published packs are mirrored, never manifests or other files", () => {
	const mirror = createPackMirror();
	assert.equal( mirror.url( PACK ), null );
	mirror.set( MIRROR );
	assert.equal( mirror.url( PACK ), MIRROR + "/assets/packs/game-images-004-0123456789ab.bin" );
	assert.equal( mirror.url( ORIGIN + "/assets/packs/manifest.json" ), null );
	assert.equal( mirror.url( ORIGIN + "/hd/packs/hd-bc7-001-0123456789ab.bin" ), null );
	assert.equal( mirror.url( ORIGIN + "/assets/interface/x-001-0123456789ab.bin" ), null );
	assert.equal( mirror.url( PACK + "?v=1" ), null );
	mirror.set( "https://evil.example" );
	assert.equal( mirror.url( PACK ), null );
});

test("pack reads go to the mirror when it has the pack; the manifest never does", async t => {
	const reads = await load( t, MIRROR );
	assert.ok( reads.length > 0 );
	assert.ok( reads.every( read => read.startsWith( "mirror " ) ), reads.join( ", " ) );
});

test("a pack the mirror does not have yet comes from the network, and the mirror stays on", async t => {
	const reads = await load( t, MIRROR, () => new Response( null, { status: 404 } ) );
	// Every read asks the mirror first, then the network.
	assert.ok( reads.length >= 4, reads.join( ", " ) );
	for ( let i = 0; i < reads.length; i += 2 ) {
		assert.ok( reads[i].startsWith( "mirror " ), reads.join( ", " ) );
		assert.ok( reads[i + 1].startsWith( "network " ), reads.join( ", " ) );
	}
});

test("a mirror that cannot be reached is turned off for the session", async t => {
	const reads = await load( t, MIRROR, () => {
		throw new TypeError( "Failed to fetch" );
	} );
	assert.ok( reads[0].startsWith( "mirror " ), reads.join( ", " ) );
	assert.ok( reads.slice( 1 ).every( read => read.startsWith( "network " ) ), reads.join( ", " ) );
	assert.ok( reads.length > 1 );
});

test("a mirror range answer that is wrong is not trusted", async t => {
	// A 200 for a range read: the loader refuses it as it would a
	// middlebox's, and the read is made again on the network.
	const reads = await load( t, MIRROR, () => new Response( new Uint8Array( 16 ) ) );
	assert.ok( reads.some( read => read.startsWith( "network " ) ), reads.join( ", " ) );
});

test("without a mirror every read goes to the published origin", async t => {
	const reads = await load( t, null );
	assert.ok( reads.every( read => read.startsWith( "network " ) ), reads.join( ", " ) );
});
