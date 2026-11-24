/*
===========================================================================

packs-shared-blocks.test.mjs - one block download for every game window

Several game windows of one site share the persistent store, and before
#4268 each one that missed a new area downloaded every block of it: one
player's five windows fetched the same 1 MiB block up to eleven times per
teleport. These tests run two pack readers over one fake Cache Storage,
as two windows of the origin, and count the block reads that reach the
network.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { disk, fixture, sha } from "../helpers/packs-fixture.mjs";

const { createPacks } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/assets/worker/packs/packs.ts" ) ).href
);
const ORIGIN = "http://localhost";

/*
================
largePack

The shared fixture padded past the whole-pack limit, so its members are
read by block, and a download that counts the block reads (every ranged
read past the pack header's first bytes).
================
*/
function largePack() {
	const f = fixture(), padded = new Uint8Array( 5 << 20 );
	padded.set( f.bytes );
	f.pack.bytes = padded.length;
	f.pack.sha256 = sha( padded );
	const counts = { blocks: 0 };
	const download = async ( url, limit, signal, range ) => {
		if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
		if ( range.start > 0 ) counts.blocks++;
		return padded.slice( range.start, range.end + 1 );
	};
	return { f, counts, download };
}

/*
================
read
================
*/
function read( packs, pathname ) {
	return packs.read( new URL( ORIGIN + pathname ), 1 << 20, new AbortController().signal );
}

test("two windows that miss the same block at once download it once", async t => {
	disk( t );
	const { counts, download } = largePack();
	const first = createPacks( download ), second = createPacks( download );
	const [a, b] = await Promise.all( [ read( first, "/assets/a.bin" ), read( second, "/assets/a.bin" ) ] );
	assert.deepEqual( [ ...a ], [ 1, 2, 3 ] );
	assert.deepEqual( [ ...b ], [ 1, 2, 3 ] );
	assert.equal( counts.blocks, 1 );
	first.dispose();
	second.dispose();
});

test("a block's other members are stored by the window that fetched it", async t => {
	disk( t );
	const { counts, download } = largePack();
	const first = createPacks( download );
	await read( first, "/assets/a.bin" );
	// Another window demands the neighbour at once: it waits on the
	// block's lock, which the first holds until the block is stored.
	const second = createPacks( download );
	assert.deepEqual( JSON.parse( new TextDecoder().decode( await read( second, "/assets/b.json" ) ) ), {
		fixture: true
	} );
	assert.equal( counts.blocks, 1 );
	first.dispose();
	second.dispose();
});

test("without Web Locks each window still reads its member", async t => {
	disk( t );
	const old = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	Object.defineProperty( globalThis, "navigator", { configurable: true, value: {} } );
	t.after( () => {
		if ( old ) Object.defineProperty( globalThis, "navigator", old );
		else Reflect.deleteProperty( globalThis, "navigator" );
	} );
	const { download } = largePack();
	const first = createPacks( download ), second = createPacks( download );
	const [a, b] = await Promise.all( [ read( first, "/assets/a.bin" ), read( second, "/assets/a.bin" ) ] );
	assert.deepEqual( [ ...a ], [ 1, 2, 3 ] );
	assert.deepEqual( [ ...b ], [ 1, 2, 3 ] );
	first.dispose();
	second.dispose();
});
