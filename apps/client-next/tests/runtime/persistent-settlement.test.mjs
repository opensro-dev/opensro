/*
===========================================================================

persistent-settlement.test.mjs - cache recovery across late native settlement

Synthetic Cache Storage exercises the shipping persistent owner and pack
installer without retail assets. Deadlines release callers without allowing
late disk mutations or abandoned bodies to escape accounting and admission.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
const { createPersistentAssets } = await import( "../../src/engine/runtime/assets/worker/packs/persistent.ts" );
const { createPacks } = await import( "../../src/engine/runtime/assets/worker/packs/packs.ts" );

const ORIGIN = "https://example.test";
const BYTES = Uint8Array.of( 1, 2, 3 );
const DEADLINE_MS = 2000;
// The inventory scan's keys() is background bookkeeping with its own deadline.
const SCAN_MS = 30000;
const NATIVE_LIMIT = 8;
const QUEUE_LIMIT = 64;

/*
================
settle
================
*/
const settle = () => new Promise( resolve => setImmediate( resolve ) );
/*
================
deferred
================
*/
/** @returns {{promise: Promise<any>, resolve: (value?: any) => void, reject: (reason?: any) => void}} */
const deferred = () => {
	let resolve = ( /** @type {any} */ _value ) => {}, reject = ( /** @type {any} */ _reason ) => {};
	const promise = new Promise( ( yes, no ) => {
		resolve = yes;
		reject = no;
	} );
	return { promise, resolve, reject };
};
/*
================
key
================
*/
const key = digest => ORIGIN + "/assets/.verified/" + digest;
/*
================
response
================
*/
const response = () => new Response( BYTES, { headers: { "content-length": String( BYTES.length ) } } );
/*
================
backend

Hooks delay or reject native operations before their visible disk effects.
Rows survive replacement of the persistent owner, as browser storage does.
================
*/
/** @param {import("node:test").TestContext} t
 * @param {Record<string, (...args: any[]) => any>} hooks
 */
function backend( t, hooks = {} ) {
	t.mock.timers.enable( { apis: [ "setTimeout" ] } );
	const rows = new Map();
	const events = [];
	const cache = {
		/*
		================
		match
		================
		*/
		async match( request ) {
			const url = typeof request === "string" ? request : request.url;
			events.push( [ "match", url ] );
			await hooks.match?.( url );
			const bytes = rows.get( url );
			return bytes ? new Response( bytes, { headers: { "content-length": String( bytes.length ) } } ) : undefined;
		},
		/*
		================
		keys
		================
		*/
		async keys() {
			events.push( [ "keys" ] );
			await hooks.keys?.();
			return [ ...rows.keys() ].map( url => new Request( url ) );
		},
		/*
		================
		put
		================
		*/
		async put( url, value ) {
			events.push( [ "put", url ] );
			await hooks.put?.( url );
			rows.set( url, new Uint8Array( await value.arrayBuffer() ) );
		},
		/*
		================
		delete
		================
		*/
		async delete( url ) {
			events.push( [ "delete", url ] );
			await hooks.delete?.( url );
			return rows.delete( url );
		}
	};
	/** @type {[string, PropertyDescriptor | undefined][]} */
	const originals = [ "caches", "navigator" ].map(
		name => [ name, Object.getOwnPropertyDescriptor( globalThis, name ) ]
	);
	Object.defineProperty( globalThis, "caches", {
		configurable: true,
		value: {
			/*
			================
			open
			================
			*/
			async open() {
				events.push( [ "open" ] );
				await hooks.open?.();
				return cache;
			}
		}
	} );
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: { storage: { estimate: async () => ({ quota: 1024 }) } }
	} );
	t.after( () => {
		for ( const [name, descriptor] of originals ) {
			if ( descriptor ) Object.defineProperty( globalThis, name, descriptor );
			else Reflect.deleteProperty( globalThis, name );
		}
	} );
	return { cache, rows, events };
}

for ( const phase of [ "open", "match" ] ) {
	for ( const rejects of [ false, true ] ) {
		test(`late ${phase} ${rejects ? "rejection" : "success"} restores cache admission`, async t => {
			const gate = deferred();
			let first = true;
			const { rows, events } = backend( t, {
				[phase]: () => {
					if ( !first ) return;
					first = false;
					return gate.promise;
				}
			} );
			rows.set( key( "a" ), BYTES );
			const store = createPersistentAssets();
			const read = store.read( ORIGIN, "a", BYTES.length );
			await settle();
			t.mock.timers.tick( DEADLINE_MS );
			assert.equal( await read, null );
			const before = events.length;
			assert.equal( await store.read( ORIGIN, "a", BYTES.length ), null );
			assert.equal( events.length, before, "suspended callers must not start native work" );
			if ( rejects ) gate.reject( Error( "Late disk failure" ) );
			else gate.resolve();
			await settle();
			assert.deepEqual( await store.read( ORIGIN, "a", BYTES.length ), BYTES );
			assert.equal( events.filter( event => event[0] === "delete" ).length, 0 );
		});
	}
}

for ( const rejects of [ false, true ] ) {
	test(`expired body remains suspended until late cancellation ${rejects ? "rejects" : "settles"}`, async t => {
		const gate = deferred();
		const { cache } = backend( t );
		let matches = 0, cancels = 0;
		t.mock.method( cache, "match", async () => {
			if ( ++matches > 1 ) return response();
			return new Response(
				new ReadableStream( {
					cancel() {
						cancels++;
						return gate.promise;
					}
				} ),
				{ headers: { "content-length": "3" } }
			);
		} );
		const store = createPersistentAssets();
		const read = store.read( ORIGIN, "a", BYTES.length );
		await settle();
		t.mock.timers.tick( DEADLINE_MS );
		assert.equal( await read, null );
		await settle();
		assert.equal( cancels, 1 );
		for ( let i = 0; i < 20; i++ ) assert.equal( await store.read( ORIGIN, "a", BYTES.length ), null );
		assert.equal( matches, 1, "read timeout must transfer suspension directly to body cleanup" );
		if ( rejects ) gate.reject( Error( "Late cancel failure" ) );
		else gate.resolve();
		await settle();
		assert.deepEqual( await store.read( ORIGIN, "a", BYTES.length ), BYTES );
		assert.equal( matches, 2 );
	});
}

test("a shared slow open and healthy burst produce every cache hit", async t => {
	const opening = deferred(), matching = deferred();
	let active = 0, peak = 0;
	const { rows, events } = backend( t, {
		open: () => opening.promise,
		match: async () => {
			peak = Math.max( peak, ++active );
			await matching.promise;
			active--;
		}
	} );
	const store = createPersistentAssets();
	const reads = Array.from( { length: 32 }, ( _, i ) => {
		rows.set( key( String( i ) ), BYTES );
		return store.read( ORIGIN, String( i ), BYTES.length );
	} );
	await settle();
	assert.deepEqual( events, [ [ "open" ] ] );
	opening.resolve();
	await settle();
	assert.equal( active, NATIVE_LIMIT );
	matching.resolve();
	for ( const bytes of await Promise.all( reads ) ) assert.deepEqual( bytes, BYTES );
	assert.equal( peak, NATIVE_LIMIT );
	assert.equal( store.stats().hits, reads.length );
	assert.equal( store.stats().errors, 0 );
});

test("FIFO admission removes cancelled waiters before starting their backend work", async t => {
	const gates = Array.from( { length: NATIVE_LIMIT }, deferred );
	const { rows, events } = backend( t, {
		match: url => gates[Number( url.split( "/" ).pop() )]?.promise
	} );
	const store = createPersistentAssets();
	const controllers = Array.from( { length: 24 }, () => new AbortController() );
	for ( let i = 0; i < controllers.length; i++ ) rows.set( key( String( i ) ), BYTES );
	const reads = controllers.map( ( controller, i ) =>
		store.read( ORIGIN, String( i ), BYTES.length, controller.signal )
	);
	const cancelled = assert.rejects( reads[10], { name: "AbortError" } );
	await settle();
	assert.equal( events.filter( event => event[0] === "match" ).length, NATIVE_LIMIT );
	controllers[10].abort();
	await cancelled;
	gates[0].resolve();
	await settle();
	const started = events.filter( event => event[0] === "match" ).map( event =>
		Number( event[1].split( "/" ).pop() )
	);
	assert.deepEqual( started, Array.from( { length: 24 }, ( _, i ) => i ).filter( i => i !== 10 ) );
	for ( const gate of gates ) gate.resolve();
	for ( const [i, read] of reads.entries() ) if ( i !== 10 ) assert.deepEqual( await read, BYTES );
});

test("bounded queued callers expire without growing a stuck backend and cancelled slots can be reused", async t => {
	const gate = deferred();
	const { events } = backend( t, { match: () => gate.promise } );
	const store = createPersistentAssets();
	// Prime open so the bound measured below belongs to operation admission.
	assert.equal( await store.has( ORIGIN, "prime", AbortSignal.abort() ).catch( () => false ), false );
	const active = Array.from( { length: NATIVE_LIMIT }, ( _, i ) => store.read( ORIGIN, String( i ), BYTES.length ) );
	await settle();
	const controllers = Array.from( { length: QUEUE_LIMIT }, () => new AbortController() );
	const queued = controllers.map( ( controller, i ) =>
		store.read( ORIGIN, "q" + i, BYTES.length, controller.signal )
	);
	await settle();
	assert.equal( await store.read( ORIGIN, "overflow", BYTES.length ), null );
	const cancelled = assert.rejects( queued[0], { name: "AbortError" } );
	controllers[0].abort();
	await cancelled;
	let replacementSettled = false;
	const replacement = store.read( ORIGIN, "replacement", BYTES.length ).then( value => {
		replacementSettled = true;
		return value;
	} );
	await settle();
	assert.equal( replacementSettled, false, "cancelling a waiter must free queue capacity" );
	t.mock.timers.tick( DEADLINE_MS );
	for ( const read of [ ...active, ...queued.slice( 1 ), replacement ] ) assert.equal( await read, null );
	for ( let i = 0; i < 100; i++ ) assert.equal( await store.read( ORIGIN, "later", BYTES.length ), null );
	assert.equal( events.filter( event => event[0] === "match" ).length, NATIVE_LIMIT );
	gate.resolve();
	await settle();
});

test("suspended reads preserve pinned inventory and its byte budget", async t => {
	const gate = deferred();
	let block = false;
	const { rows, events } = backend( t, { match: url => block && url === key( "slow" ) ? gate.promise : undefined } );
	rows.set( key( "pinned" ), BYTES );
	const store = createPersistentAssets( () => BYTES.length );
	store.setStartup( ORIGIN, [ "pinned" ] );
	await store.write( ORIGIN, "pinned", BYTES );
	assert.equal( store.stats().pinned, 1 );
	block = true;
	const slow = store.read( ORIGIN, "slow", BYTES.length );
	await settle();
	t.mock.timers.tick( DEADLINE_MS );
	assert.equal( await slow, null );
	const before = events.length;
	assert.equal( await store.read( ORIGIN, "pinned", BYTES.length ), null );
	await store.remove( ORIGIN, "pinned" );
	assert.equal( events.length, before );
	assert.equal( store.stats().pinned, 1, "no lookup or deletion occurred while suspended" );
	gate.resolve();
	await settle();
	await store.write( ORIGIN, "extra", BYTES );
	assert.deepEqual( [ ...rows.keys() ], [ key( "pinned" ) ] );
	assert.equal( store.stats().pinned, 1 );
});

for ( const phase of [ "keys", "inventory-match", "put", "delete" ] ) {
	for ( const rejects of [ false, true ] ) {
		test(`late ${phase} ${rejects ? "rejection" : "success"} rebuilds inventory before another publication`, async t => {
			const gate = deferred();
			let enabled = phase !== "delete", blocked = false;
			const hook = phase === "inventory-match" ? "match" : phase;
			const { rows, events } = backend( t, {
				[hook]: () => {
					if ( !enabled || blocked ) return;
					blocked = true;
					return gate.promise;
				}
			} );
			rows.set( key( "a" ), BYTES );
			const store = createPersistentAssets( () => 2 * BYTES.length );
			if ( phase === "delete" ) {
				await store.write( ORIGIN, "a", BYTES );
				enabled = true;
			}
			const work = phase === "delete" ? store.remove( ORIGIN, "a" ) : store.write( ORIGIN, "b", BYTES );
			await settle();
			assert.equal( blocked, true );
			t.mock.timers.tick( phase === "keys" ? SCAN_MS : DEADLINE_MS );
			await work;
			const before = events.length;
			await store.write( ORIGIN, "suppressed", BYTES );
			assert.equal( events.length, before );
			if ( rejects ) gate.reject( Error( "Late native failure" ) );
			else gate.resolve();
			await settle();
			const target = phase === "delete" ? "a" : "b";
			await store.write( ORIGIN, target, BYTES );
			await store.write( ORIGIN, "c", BYTES );
			assert.deepEqual( rows.get( key( target ) ), BYTES );
			assert.deepEqual( rows.get( key( "c" ) ), BYTES );
			assert.equal( [ ...rows.values() ].reduce( ( sum, bytes ) => sum + bytes.length, 0 ), 2 * BYTES.length );
			assert.deepEqual( await store.read( ORIGIN, target, BYTES.length ), BYTES );
		});
	}
}

test("real pack install resumes after temporary suspension and replacement owners reuse stored bytes", async t => {
	const gate = deferred();
	let block = false;
	const { rows } = backend( t, { match: () => block ? gate.promise : undefined } );
	const digest = createHash( "sha256" ).update( BYTES ).digest( "hex" );
	const packPath = "/assets/packs/startup.bin", assetPath = "/assets/startup.bin";
	const manifest = {
		version: 2,
		groups: [ {
			name: "startup",
			load: "startup",
			assetCount: 1,
			packs: [ {
				path: packPath,
				bytes: 100,
				sha256: digest,
				assetCount: 1
			} ]
		} ],
		assets: [ {
			path: assetPath,
			packPath,
			offset: 0,
			length: BYTES.length,
			mime: "application/octet-stream",
			sha256: digest
		} ]
	};
	let downloads = 0;
	/*
	================
	download
	================
	*/
	const download = async url => {
		if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( manifest ) );
		downloads++;
		if ( url.endsWith( packPath ) ) throw Object.assign( Error( "Missing optional container" ), { status: 404 } );
		assert.equal( url, ORIGIN + assetPath );
		return BYTES.slice();
	};
	const packs = createPacks( download );
	t.after( () => packs.dispose() );
	const url = new URL( ORIGIN + assetPath ), signal = new AbortController().signal;
	assert.equal( await packs.install( url, signal ), true );
	assert.deepEqual( rows.get( key( digest ) ), BYTES );
	block = true;
	const slow = packs.read( url, BYTES.length, signal );
	await settle();
	t.mock.timers.tick( DEADLINE_MS );
	assert.deepEqual( await slow, BYTES );
	const before = downloads;
	block = false;
	gate.resolve();
	await settle();
	assert.equal( await packs.install( url, signal ), false );
	assert.equal( downloads, before );
	const replacement = createPacks( download );
	t.after( () => replacement.dispose() );
	assert.equal( await replacement.install( url, signal ), false );
	assert.deepEqual( await replacement.read( url, BYTES.length, signal ), BYTES );
	assert.equal( downloads, before, "new owner must use the durable bytes without another asset download" );
});
