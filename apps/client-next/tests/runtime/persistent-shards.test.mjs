/*
===========================================================================

persistent-shards.test.mjs - the store's sharded caches (persistent.ts)

Chrome refuses keys() on a cache past about 17,000 store keys ("Operation
too large"). The single v1 cache reached that on players who explored, and
from then on every write failed its inventory rebuild: nothing new was
stored and a refresh downloaded the scene again (2026-11-25, Jangan). The
store now spreads its keys over named shards, caps each below the limit,
drops a shard that still cannot be listed, and deletes the v1 cache.

The Cache Storage here is name-aware, as a browser's is: each name is its
own cache, and keys() on one never lists another's entries.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPersistentAssets, MAX_SHARD_ENTRIES } = await import(
	"../../src/engine/runtime/assets/worker/packs/persistent.ts"
);

const ORIGIN = "https://example.test";
const BYTES = Uint8Array.of( 1, 2, 3 );

/*
================
namedStorage

A Cache Storage of named caches plus a storage estimate, restored after t.
`refuseKeys` names caches whose next keys() is refused as Chrome refuses a
cache too large to list; `deleted` records every caches.delete.
================
*/
function namedStorage( t, quota = 1 << 30 ) {
	const stores = new Map(), deleted = [], refuseKeys = new Set();
	const rowKey = request => request instanceof Request ? request.url : String( request );
	/*
	================
	named
	================
	*/
	function named( name ) {
		if ( stores.has( name ) ) return stores.get( name );
		const rows = new Map();
		const cache = {
			rows,
			async match( request ) {
				return rows.get( rowKey( request ) )?.clone();
			},
			async keys() {
				if ( refuseKeys.delete( name ) ) {
					throw new DOMException( "Failed to execute 'keys' on 'Cache': Operation too large.", "AbortError" );
				}
				return [ ...rows.keys() ].map( url => new Request( url ) );
			},
			async delete( request ) {
				return rows.delete( rowKey( request ) );
			},
			async put( request, response ) {
				rows.set(
					rowKey( request ),
					new Response( await response.arrayBuffer(), { headers: response.headers } )
				);
			}
		};
		stores.set( name, cache );
		return cache;
	}
	const oldCaches = Object.getOwnPropertyDescriptor( globalThis, "caches" );
	const oldNavigator = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	Object.defineProperty( globalThis, "caches", {
		configurable: true,
		value: {
			open: async name => named( name ),
			async delete( name ) {
				deleted.push( name );
				return stores.delete( name );
			}
		}
	} );
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
	return { stores, deleted, refuseKeys, named };
}

/*
================
holding

The names of the caches that hold a stored digest.
================
*/
function holding( stores, digest ) {
	const url = ORIGIN + "/assets/.verified/" + digest;
	return [ ...stores ].filter( ( [, cache] ) => cache.rows.has( url ) ).map( ( [name] ) => name );
}

test("keys spread over shards by digest, and the v1 cache is deleted on the first open", async t => {
	const { stores, deleted, named } = namedStorage( t );
	named( "sro-next-verified-v1" );
	const store = createPersistentAssets();
	await store.write( ORIGIN, "0a", BYTES );
	await store.write( ORIGIN, "fa", BYTES );
	await store.write( ORIGIN, "fb", BYTES );
	const zero = holding( stores, "0a" ), f = holding( stores, "fa" );
	assert.equal( zero.length, 1 );
	assert.equal( f.length, 1 );
	assert.notEqual( zero[0], f[0], "digests of different first digits share one cache" );
	assert.deepEqual( holding( stores, "fb" ), f, "one first digit spread over two caches" );
	assert.ok( deleted.includes( "sro-next-verified-v1" ), "the v1 cache was kept" );
	assert.deepEqual( await store.read( ORIGIN, "fa", BYTES.length ), BYTES );
	assert.equal( stores.has( "sro-next-verified-v1" ), false );
});

test("a shard too large to list is dropped, and the store keeps writing", async t => {
	const { stores, deleted, refuseKeys } = namedStorage( t );
	const seed = createPersistentAssets();
	await seed.write( ORIGIN, "1old", BYTES );
	const [shard] = holding( stores, "1old" );
	// A new worker: its first write rebuilds the inventory and meets the refusal.
	refuseKeys.add( shard );
	const store = createPersistentAssets();
	await store.write( ORIGIN, "2first", BYTES );
	assert.ok( deleted.includes( shard ), "the unlistable shard was kept" );
	assert.equal( store.stats().resets, 1 );
	await store.write( ORIGIN, "1new", BYTES );
	await store.write( ORIGIN, "2second", BYTES );
	assert.deepEqual( await store.read( ORIGIN, "1new", BYTES.length ), BYTES, "the dropped shard stays frozen" );
	assert.deepEqual( await store.read( ORIGIN, "2second", BYTES.length ), BYTES, "writes stopped after the refusal" );
	assert.equal( await store.read( ORIGIN, "1old", BYTES.length ), null );
});

test("a shard never holds more than MAX_SHARD_ENTRIES: the least recently used leaves", async t => {
	const { stores } = namedStorage( t );
	const store = createPersistentAssets();
	const digest = n => "a" + n.toString( 16 ).padStart( 8, "0" );
	for ( let n = 0; n <= MAX_SHARD_ENTRIES; n++ ) await store.write( ORIGIN, digest( n ), BYTES );
	const [shard] = holding( stores, digest( MAX_SHARD_ENTRIES ) );
	assert.equal( stores.get( shard ).rows.size, MAX_SHARD_ENTRIES );
	assert.equal( await store.read( ORIGIN, digest( 0 ), BYTES.length ), null, "the oldest entry stayed" );
	assert.deepEqual( await store.read( ORIGIN, digest( MAX_SHARD_ENTRIES ), BYTES.length ), BYTES );
	assert.equal( store.stats().evictions, 1 );
});
