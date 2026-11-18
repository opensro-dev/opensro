/*
===========================================================================

persistent-inventory.test.mjs - the store's inventory rebuild (persistent.ts)

A new worker rebuilds the store's inventory before its first write, and
every write waits behind it. Reading each stored entry for its size held
writes for minutes on a store of tens of thousands, so world-entry bursts
were skipped instead of stored. The rebuild now takes sizes from the
admitted manifests and reads only entries none of them names.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPersistentAssets } = await import( "../../src/engine/runtime/assets/worker/packs/persistent.ts" );

const ORIGIN = "https://example.test";
const BYTES = Uint8Array.of( 1, 2, 3 );

/*
================
storage

Synthetic Cache Storage and storage estimate, restored after t. keys()
keeps insertion order, as Cache Storage does; matches counts every read,
and an awaited hooks.beforeMatch holds a read where a test needs it.
================
*/
function storage( t, quota = 1 << 30 ) {
	const rows = new Map();
	const counts = { matches: 0 };
	/** @type {{ beforeMatch: ( url: string ) => Promise<void> | void }} */
	const hooks = { beforeMatch: () => {} };
	const rowKey = request => request instanceof Request ? request.url : String( request );
	const cache = {
		async match( request ) {
			counts.matches++;
			await hooks.beforeMatch( rowKey( request ) );
			return rows.get( rowKey( request ) )?.clone();
		},
		async keys() {
			return [ ...rows.keys() ].map( url => new Request( url ) );
		},
		async delete( request ) {
			return rows.delete( rowKey( request ) );
		},
		async put( request, response ) {
			rows.set( rowKey( request ), new Response( await response.arrayBuffer(), { headers: response.headers } ) );
		}
	};
	const oldCaches = Object.getOwnPropertyDescriptor( globalThis, "caches" );
	const oldNavigator = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	Object.defineProperty( globalThis, "caches", { configurable: true, value: { open: async () => cache } } );
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: { storage: { estimate: async () => ({ quota }) } }
	} );
	t.after( () => {
		if ( oldCaches ) Object.defineProperty( globalThis, "caches", oldCaches );
		else Reflect.deleteProperty( globalThis, "caches" );
		if ( oldNavigator ) Object.defineProperty( globalThis, "navigator", oldNavigator );
		else Reflect.deleteProperty( globalThis, "navigator" );
	} );
	return { rows, counts, hooks };
}

/*
================
digestKey
================
*/
function digestKey( digest ) {
	return ORIGIN + "/assets/.verified/" + digest;
}

test("the rebuild reads no entry an admitted manifest sizes", async t => {
	const s = storage( t );
	const seed = createPersistentAssets();
	for ( const digest of [ "a", "b", "stale" ] ) await seed.write( ORIGIN, digest, BYTES );
	// A new worker: its manifest names a and b, not the older release's entry.
	const store = createPersistentAssets();
	store.addSizes( ORIGIN, [ [ "a", BYTES.length ], [ "b", BYTES.length ] ] );
	s.counts.matches = 0;
	await store.write( ORIGIN, "c", BYTES );
	assert.equal( s.counts.matches, 1, "only the entry no manifest names is read for its size" );
	assert.equal( store.stats().writes, 1 );
	assert.equal( s.rows.size, 4 );
});

test("an entry no admitted manifest names is evicted before any the manifest names", async t => {
	const s = storage( t );
	const seed = createPersistentAssets();
	// The unnamed entry is neither the oldest nor the newest stored.
	for ( const digest of [ "a", "stale", "b" ] ) await seed.write( ORIGIN, digest, BYTES );
	// Room for three entries: the fourth write evicts one.
	const store = createPersistentAssets( () => 3 * BYTES.length );
	store.addSizes( ORIGIN, [ [ "a", BYTES.length ], [ "b", BYTES.length ], [ "c", BYTES.length ] ] );
	await store.write( ORIGIN, "c", BYTES );
	assert.deepEqual( [ ...s.rows.keys() ].sort(), [ "a", "b", "c" ].map( digestKey ) );
	assert.equal( store.stats().evictions, 1 );
});

test("sizes admitted while the rebuild reads never count an entry twice", async t => {
	const s = storage( t );
	const seed = createPersistentAssets();
	for ( const digest of [ "a", "hd" ] ) await seed.write( ORIGIN, digest, BYTES );
	// Room for exactly three entries: a, hd and the new c fit without eviction.
	const store = createPersistentAssets( () => 3 * BYTES.length );
	store.addSizes( ORIGIN, [ [ "a", BYTES.length ] ] );
	// The HD index is admitted while the rebuild is reading hd's size.
	s.hooks.beforeMatch = url => {
		if ( url === digestKey( "hd" ) ) store.addSizes( ORIGIN, [ [ "hd", BYTES.length ] ] );
	};
	await store.write( ORIGIN, "c", BYTES );
	assert.equal( store.stats().evictions, 0, "the store holds 9 bytes, not 12" );
	assert.deepEqual( [ ...s.rows.keys() ].sort(), [ "a", "c", "hd" ].map( digestKey ) );
});
