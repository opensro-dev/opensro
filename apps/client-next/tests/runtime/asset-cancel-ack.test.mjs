/*
===========================================================================

asset-cancel-ack.test.mjs - a late cancellation never kills the asset runtime

A player stuck on "Unable to finish loading ... Asset worker timed out while
cancelling /assets/effects/programs.json" (2026-10-13): a member read waits
for its whole shared pack fetch even after its request is cancelled, so the
worker's "released" can arrive after the request's own deadline. These
tests pin both halves: the worker really cannot release early, and the
main-thread owner must survive that late release.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { defined } from "../helpers/defined.mjs";

async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createPacks } = await load( "src/engine/runtime/assets/worker/packs/packs.ts" );
const { createAssets } = await load( "src/engine/runtime/assets/assets.ts" );
const sha = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );

const PROGRAMS = "https://assets.test/assets/effects/programs.json";
// assets.ts REQUEST_DEADLINE_MS: the whole request's deadline.
const REQUEST_DEADLINE_MS = 120000;

/*
================
largePack

programs.json as production ships it: one member of a pack over
SMALL_PACK_BYTES (game-data-001, 43 MB), so packs.ts reads it by range
(member() -> blocks.read). The descriptor claims a large pack; the stub
serves the ranges from a small buffer.
================
*/
function largePack() {
	const member = new TextEncoder().encode( '{"programs":[]}' );
	const entry = {
		path: "/assets/effects/programs.json",
		offset: 0,
		length: member.length,
		mime: "application/json",
		sha256: sha( member )
	};
	const header = Buffer.from( JSON.stringify( { format: "sro-asset-pack", version: 2, files: [ entry ] } ) );
	const bytes = new Uint8Array( 12 + header.length + member.length );
	bytes.set( Buffer.from( "SROPACK2" ) );
	new DataView( bytes.buffer ).setUint32( 8, header.length, true );
	bytes.set( header, 12 );
	bytes.set( member, 12 + header.length );
	const pack = { path: "/assets/packs/game-data-001.bin", bytes: 43 << 20, sha256: sha( bytes ), assetCount: 1 };
	const manifest = {
		version: 2,
		groups: [ { name: "game-data", assetCount: 1, packs: [ pack ] } ],
		assets: [ { ...entry, packPath: pack.path } ]
	};
	return { bytes, manifest, dataStart: 12 + header.length };
}

/*
================
fixture

createAssets over a stub worker and a controllable clock.
================
*/
function fixture( t ) {
	const old = Object.getOwnPropertyDescriptor( globalThis, "Worker" );
	/** @type {{ messages: unknown[]; onmessage: ( event: unknown ) => void; } | undefined} */
	let worker;
	class Worker {
		/** @type {unknown[]} */
		messages = [];
		/** @type {( event: unknown ) => void} */
		onmessage = () => {};
		constructor() {
			worker = this;
		}
		postMessage( m ) {
			this.messages.push( m );
		}
		terminate() {}
	}
	Object.defineProperty( globalThis, "Worker", { configurable: true, value: Worker } );
	const clock = { now: 1000 };
	t.mock.method( performance, "now", () => clock.now );
	t.after( () => {
		if ( old ) Object.defineProperty( globalThis, "Worker", old );
		else Reflect.deleteProperty( globalThis, "Worker" );
	} );
	const assets = createAssets();
	t.after( () => assets.dispose() );
	return { assets, worker, clock };
}

test("a cancelled member read waits for its whole range download (packs.ts member())", async () => {
	const f = largePack();
	/** @type {( value?: unknown ) => void} */
	let finish = () => {};
	const memberRange = new Promise( resolve => finish = resolve );
	const packs = createPacks( async ( url, _limit, _signal, range ) => {
		if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
		// The pack header answers at once; the member's own range is the slow
		// 4.6 MB read a 350 ms RTT link spends minutes on.
		if ( range && range.start < f.dataStart ) return f.bytes.slice( range.start, range.end + 1 );
		await memberRange;
		return f.bytes.slice( range.start, range.end + 1 );
	} );
	const request = new AbortController();
	let settled = false;
	const read = packs.read( new URL( PROGRAMS ), 1 << 20, request.signal ).then(
		() => settled = true,
		() => settled = true
	);
	await new Promise( resolve => setTimeout( resolve, 10 ) );
	request.abort();
	await new Promise( resolve => setTimeout( resolve, 10 ) );
	// member() reads the header and blocks on the owner's lifetime signal, not
	// the request's: the cancelled read cannot settle, so the worker cannot
	// send "released" until the range download ends.
	assert.equal( settled, false, "a cancelled member read settled before its range download" );
	finish();
	await read;
	assert.equal( settled, true );
	packs.dispose();
});

test("a release that arrives after the request deadline does not fail the asset runtime", t => {
	const { assets, worker, clock } = fixture( t );
	// His case: 350 ms RTT from Thailand. The 4.6 MB programs.json member is
	// requested at loading start and is still downloading (about 60 KB/s,
	// some 77 s for the range) when the loading screen moves on at 100 s
	// and cancels it; the worker can release only when the range ends.
	const id = assets.request( PROGRAMS );
	clock.now += 100000;
	assets.cancel( id );
	assert.deepEqual( defined( worker ).messages.at( -1 ), { kind: "cancel", id } );
	// At 125 s the request's own deadline has passed; the release has not.
	clock.now += 25000;
	const health = assets.health();
	assert.equal( health.phase, "running", `asset runtime failed: ${health.error}` );
	// The range ends at about 177 s and the worker releases the slot.
	clock.now += 52000;
	assert.equal( assets.health().phase, "running" );
	defined( worker ).onmessage( { data: { kind: "released", id } } );
	assert.equal( assets.available(), 4 );
	assert.equal( assets.health().phase, "running" );
	// The late acknowledgement was reported once, never silently passed.
	assert.deepEqual( assets.takeDegradations().map( d => d.code ), [ "asset-cancel-unacknowledged" ] );
});
