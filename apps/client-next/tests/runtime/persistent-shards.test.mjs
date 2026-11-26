/*
===========================================================================

persistent-shards.test.mjs - the store's sharded caches (persistent.ts)

Chrome refuses keys() on a cache past about 17,000 store keys ("Operation
too large"). The single v1 cache reached that on players who explored, and
from then on every write failed its inventory rebuild: nothing new was
stored and a refresh downloaded the scene again (2026-11-25, Jangan). The
store now spreads its keys over named shards, caps each below the limit,
and drops a shard that still cannot be listed. The v1 cache is emptied by
use: an entry a read needs moves to its shard, and the cache is deleted when
its time is over or its space is needed.

Its failures are counted by operation, with the latest one's reason as a
word (#4432): the bare sum climbed 412 to 773 in one report and could not
say why.

The Cache Storage here is name-aware, as a browser's is: each name is its
own cache, and keys() on one never lists another's entries.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPersistentAssets, LEGACY_KEEP_MS, MAX_SHARD_ENTRIES, failureReason } = await import(
	"../../src/engine/runtime/assets/worker/packs/persistent.ts"
);

const ORIGIN = "https://example.test";
const BYTES = Uint8Array.of( 1, 2, 3 );
const LEGACY = "sro-next-verified-v1";

/*
================
namedStorage

A Cache Storage of named caches plus a storage estimate, restored after t.
`refuseKeys` names caches whose next keys() is refused as Chrome refuses a
cache too large to list, `refusePuts` caches whose next put is refused for
quota; `deleted` records every caches.delete.
================
*/
function namedStorage( t, quota = 1 << 30 ) {
	const stores = new Map(), deleted = [], refuseKeys = new Set(), refusePuts = new Set();
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
				if ( refusePuts.delete( name ) ) throw new DOMException( "Quota exceeded.", "QuotaExceededError" );
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
			has: async name => stores.has( name ),
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
	return { stores, deleted, refuseKeys, refusePuts, named };
}

/*
================
legacyWith

The v1 cache of an earlier release, holding BYTES under each digest. It is
too large to list, as a player's was: the store must never ask it.
================
*/
async function legacyWith( named, refuseKeys, ...digests ) {
	const cache = named( LEGACY );
	for ( const digest of digests ) {
		await cache.put(
			ORIGIN + "/assets/.verified/" + digest,
			new Response( BYTES, { headers: { "content-length": String( BYTES.length ) } } )
		);
	}
	refuseKeys.add( LEGACY );
	return cache;
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

test("keys spread over shards by digest", async t => {
	const { stores } = namedStorage( t );
	const store = createPersistentAssets();
	await store.write( ORIGIN, "0a", BYTES );
	await store.write( ORIGIN, "fa", BYTES );
	await store.write( ORIGIN, "fb", BYTES );
	const zero = holding( stores, "0a" ), f = holding( stores, "fa" );
	assert.equal( zero.length, 1 );
	assert.equal( f.length, 1 );
	assert.notEqual( zero[0], f[0], "digests of different first digits share one cache" );
	assert.deepEqual( holding( stores, "fb" ), f, "one first digit spread over two caches" );
	assert.deepEqual( await store.read( ORIGIN, "fa", BYTES.length ), BYTES );
	assert.equal( stores.has( LEGACY ), false, "a player without a v1 cache was given one" );
});

test("a read the shards miss is answered by the v1 cache, and its entry moves", async t => {
	const { stores, refuseKeys, named } = namedStorage( t );
	await legacyWith( named, refuseKeys, "0a", "0b" );
	const store = createPersistentAssets();
	assert.deepEqual( await store.read( ORIGIN, "0a", BYTES.length ), BYTES, "a v1 entry was not served" );
	await store.flush();
	const [shard] = holding( stores, "0a" );
	assert.notEqual( shard, LEGACY, "the entry did not move" );
	assert.deepEqual( holding( stores, "0a" ), [ shard ], "the entry is stored twice" );
	assert.deepEqual( holding( stores, "0b" ), [ LEGACY ], "an entry no read asked for moved" );
	assert.deepEqual( await store.read( ORIGIN, "0a", BYTES.length ), BYTES );
	assert.equal( await store.read( ORIGIN, "0c", BYTES.length ), null );
	const { hits, misses, legacyHits } = store.stats();
	assert.deepEqual( { hits, misses, legacyHits }, { hits: 2, misses: 1, legacyHits: 1 } );
	assert.ok( refuseKeys.has( LEGACY ), "the v1 cache was listed" );
});

test("a v1 entry is not reported stored, so the background installer moves it", async t => {
	const { refuseKeys, named } = namedStorage( t );
	await legacyWith( named, refuseKeys, "0a" );
	const store = createPersistentAssets();
	assert.equal( await store.has( ORIGIN, "0a" ), false );
	await store.read( ORIGIN, "0a", BYTES.length );
	await store.flush();
	assert.equal( await store.has( ORIGIN, "0a" ), true );
});

test("a downloaded or rejected entry leaves the v1 cache too", async t => {
	const { refuseKeys, named } = namedStorage( t );
	const legacy = await legacyWith( named, refuseKeys, "0a", "0b", "0c" );
	const url = digest => ORIGIN + "/assets/.verified/" + digest;
	const store = createPersistentAssets();
	await store.write( ORIGIN, "0a", BYTES );
	assert.equal( legacy.rows.has( url( "0a" ) ), false, "a rewritten entry stayed" );
	await store.remove( ORIGIN, "0b" );
	assert.equal( legacy.rows.has( url( "0b" ) ), false, "a removed entry stayed" );
	assert.equal( await store.read( ORIGIN, "0b", BYTES.length ), null, "a removed entry came back" );
	assert.equal( await store.read( ORIGIN, "0c", BYTES.length + 1 ), null );
	await store.flush();
	assert.equal( legacy.rows.has( url( "0c" ) ), false, "an entry of the wrong length stayed" );
});

test("the v1 cache is deleted LEGACY_KEEP_MS after its first open", async t => {
	const { stores, deleted, refuseKeys, named } = namedStorage( t );
	t.mock.timers.enable( { apis: [ "Date" ], now: 1000 } );
	await legacyWith( named, refuseKeys, "0a", "0b" );
	await createPersistentAssets().read( ORIGIN, "0c", BYTES.length );
	// A later worker does not start the time again.
	t.mock.timers.setTime( 1000 + LEGACY_KEEP_MS - 1 );
	assert.deepEqual( await createPersistentAssets().read( ORIGIN, "0a", BYTES.length ), BYTES );
	t.mock.timers.setTime( 1000 + LEGACY_KEEP_MS );
	assert.equal( await createPersistentAssets().read( ORIGIN, "0b", BYTES.length ), null );
	assert.deepEqual( deleted, [ LEGACY ] );
	assert.equal( stores.has( LEGACY ), false );
});

test("a v1 cache that cannot be opened is an open failure, and the store still works", async t => {
	const { named } = namedStorage( t );
	await legacyWith( named, new Set(), "0a" );
	const backend = globalThis.caches;
	const realOpen = backend.open;
	backend.open = async name => {
		if ( name === LEGACY ) throw new DOMException( "backend gone", "UnknownError" );
		return realOpen( name );
	};
	const store = createPersistentAssets();
	assert.equal( await store.read( ORIGIN, "0a", BYTES.length ), null, "the v1 entry was served anyway" );
	const stats = store.stats();
	assert.equal( stats.failures.open, 1 );
	assert.equal( stats.lastFailure?.operation, "open" );
	assert.equal( stats.lastFailure?.reason, "unavailable" );
	await store.write( ORIGIN, "0a", BYTES );
	assert.deepEqual( await store.read( ORIGIN, "0a", BYTES.length ), BYTES, "the store needs its v1 cache" );
});

test("the v1 deadline survives its marker leaving the v1 cache", async t => {
	const { stores, deleted, refuseKeys, named } = namedStorage( t );
	t.mock.timers.enable( { apis: [ "Date" ], now: 1000 } );
	const legacy = await legacyWith( named, refuseKeys, "0a" );
	// The marker an earlier release stored in the v1 cache itself, half its
	// window already gone when this store first opens.
	const expires = String( 1000 + LEGACY_KEEP_MS / 2 );
	await legacy.put(
		"/assets/.verified/legacy-expiry",
		new Response( null, { headers: { "x-sro-expires": expires } } )
	);
	assert.deepEqual( await createPersistentAssets().read( ORIGIN, "0a", BYTES.length ), BYTES );
	// An older release still writing to the v1 cache evicts the marker.
	legacy.rows.delete( "/assets/.verified/legacy-expiry" );
	t.mock.timers.setTime( 1000 + LEGACY_KEEP_MS / 4 );
	assert.deepEqual( await createPersistentAssets().read( ORIGIN, "0a", BYTES.length ), BYTES );
	assert.equal( legacy.rows.has( "/assets/.verified/legacy-expiry" ), false, "the marker was rewritten" );
	t.mock.timers.setTime( 1000 + LEGACY_KEEP_MS / 2 );
	assert.equal( await createPersistentAssets().read( ORIGIN, "0a", BYTES.length ), null );
	assert.deepEqual( deleted, [ LEGACY ] );
	assert.equal( stores.has( LEGACY ), false );
});

test("a write refused for quota drops the v1 cache before any stored entry", async t => {
	const { stores, deleted, refuseKeys, refusePuts, named } = namedStorage( t );
	const store = createPersistentAssets();
	await legacyWith( named, refuseKeys, "0b" );
	await store.write( ORIGIN, "0a", BYTES );
	const [shard] = holding( stores, "0a" );
	refusePuts.add( shard );
	await store.write( ORIGIN, "0c", BYTES );
	assert.deepEqual( deleted, [ LEGACY ] );
	assert.deepEqual( holding( stores, "0a" ), [ shard ], "a stored entry was evicted" );
	assert.deepEqual( holding( stores, "0c" ), [ shard ], "the refused write was not retried" );
	const { evictions, writesStopped } = store.stats();
	assert.deepEqual( { evictions, writesStopped }, { evictions: 0, writesStopped: false } );
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

test("a write that meets a cache too large to list is a write failure of reason too-large", async t => {
	const { stores, refuseKeys } = namedStorage( t );
	const seed = createPersistentAssets();
	await seed.write( ORIGIN, "1old", BYTES );
	const [shard] = holding( stores, "1old" );
	refuseKeys.add( shard );
	const store = createPersistentAssets();
	await store.write( ORIGIN, "2first", BYTES );
	const stats = store.stats();
	assert.equal( stats.errors, 1 );
	assert.deepEqual( stats.failures, { open: 0, has: 0, read: 0, write: 1, remove: 0 } );
	assert.equal( stats.lastFailure?.operation, "write" );
	assert.equal( stats.lastFailure?.reason, "too-large", "Chrome raises it as an AbortError; the message decides" );
	assert.equal( typeof stats.lastFailure?.atMs, "number" );
});

test("a caller's own cancellation is not a store failure", async t => {
	namedStorage( t );
	const store = createPersistentAssets();
	await store.write( ORIGIN, "0a", BYTES );
	const cancelled = new AbortController();
	cancelled.abort( new DOMException( "left the area", "AbortError" ) );
	await assert.rejects( store.read( ORIGIN, "0a", BYTES.length, cancelled.signal ), { name: "AbortError" } );
	await assert.rejects( store.has( ORIGIN, "0a", cancelled.signal ), { name: "AbortError" } );
	assert.equal( store.stats().errors, 0 );
	assert.equal( store.stats().lastFailure, null );
});

test("a backend refusal counts against its own operation", async t => {
	const { named } = namedStorage( t );
	const store = createPersistentAssets();
	await store.write( ORIGIN, "0a", BYTES );
	const cache = named( "sro-next-verified-v2-0" );
	cache.match = async () => {
		throw new DOMException( "backend gone", "UnknownError" );
	};
	assert.equal( await store.read( ORIGIN, "0a", BYTES.length ), null );
	assert.equal( await store.has( ORIGIN, "0a" ), null );
	const stats = store.stats();
	assert.deepEqual( stats.failures, { open: 0, has: 1, read: 1, write: 0, remove: 0 } );
	assert.equal( stats.errors, 2 );
	assert.equal( stats.lastFailure?.operation, "has" );
	assert.equal( stats.lastFailure?.reason, "unavailable" );
});

test("failure reasons are words, never the browser's message", () => {
	assert.equal( failureReason( Error( "Cache operation timed out" ) ), "timeout" );
	assert.equal( failureReason( Error( "Cache suspended" ) ), "suspended" );
	assert.equal( failureReason( Error( "Cache queue full" ) ), "queue-full" );
	assert.equal( failureReason( new DOMException( "full", "QuotaExceededError" ) ), "quota" );
	assert.equal( failureReason( Error( "Incomplete persistent asset" ) ), "corrupt" );
	assert.equal( failureReason( new DOMException( "denied", "SecurityError" ) ), "unavailable" );
	assert.equal( failureReason( Error( "https://example.test/secret?x=1" ) ), "other" );
	assert.equal( failureReason( "not an error" ), "other" );
});
