/*
===========================================================================

persistent-recovery.test.mjs - optional cache settlement and loader admission

Mock time and stalled storage exercise the shipping persistent owner and real
worker load slots. Network fallback still has to satisfy manifest integrity.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
const { createPersistentAssets } = await import( "../../src/engine/runtime/assets/worker/packs/persistent.ts" );
const { createPacks } = await import( "../../src/engine/runtime/assets/worker/packs/packs.ts" );
const { createLoader } = await import( "../../src/engine/runtime/assets/worker/loader.ts" );

/*
================
settle
================
*/
const settle = () => new Promise( resolve => setImmediate( resolve ) );
/*
================
never
================
*/
const never = () => new Promise( () => {} );
/*
================
backend
================
*/
/** @param {import("node:test").TestContext} t
 * @param {object} changes
 * @param {(() => Promise<unknown>) | undefined} open
 */
function backend( t, changes = {}, open = undefined ) {
	t.mock.timers.enable( { apis: [ "setTimeout" ] } );
	const previous = Object.getOwnPropertyDescriptor( globalThis, "caches" );
	const cache = {
		match: async () => undefined,
		keys: async () => [],
		delete: async () => true,
		put: async () => {},
		...changes
	};
	Object.defineProperty( globalThis, "caches", { configurable: true, value: { open: open ?? (async () => cache) } } );
	t.after( () => {
		if ( previous ) Object.defineProperty( globalThis, "caches", previous );
		else Reflect.deleteProperty( globalThis, "caches" );
	} );
	return cache;
}

for ( const phase of [ "open", "match", "body" ] ) {
	test(`stalled ${phase} yields at two seconds and never starts another cache operation`, async t => {
		let calls = 0;
		const hang = () => {
			calls++;
			return never();
		};
		backend( t, {
			match: phase === "match" ? hang : async () =>
				new Response(
					new ReadableStream( { pull: hang, cancel: never } ),
					{ headers: { "content-length": "3" } }
				)
		}, phase === "open" ? hang : undefined );
		const store = createPersistentAssets();
		let settled = false;
		const read = store.read( "https://example.test", "a", 3 ).then( value => {
			settled = true;
			return value;
		} );
		await settle();
		t.mock.timers.tick( 1999 );
		await settle();
		assert.equal( settled, false );
		t.mock.timers.tick( 1 );
		await settle();
		assert.equal( settled, true );
		assert.equal( await read, null );
		const before = calls;
		for ( let i = 0; i < 100; i++ ) assert.equal( await store.read( "https://example.test", "b", 3 ), null );
		assert.equal( calls, before );
	});
}

test("invalid cached size falls through without waiting for hung deletion or cancellation", async t => {
	let deletes = 0, puts = 0;
	backend( t, {
		match: async () =>
			new Response( new ReadableStream( { cancel: never } ), { headers: { "content-length": "9" } } ),
		delete: () => {
			deletes++;
			return never();
		},
		put: async () => {
			puts++;
		}
	} );
	const store = createPersistentAssets();
	assert.equal( await store.read( "https://example.test", "a", 3 ), null );
	store.enqueue( "https://example.test", "a", Uint8Array.of( 1, 2, 3 ) );
	await settle();
	assert.equal( deletes, 1 );
	assert.equal( puts, 0 );
	t.mock.timers.tick( 2000 );
	await settle();
	await store.flush();
	assert.equal( puts, 0 );
	assert.equal( store.stats().queuedBytes, 0 );
});

test("presence checks never wait for body cancellation and bound abandoned cleanup", async t => {
	let matches = 0;
	backend( t, {
		match: async () => {
			matches++;
			return new Response( new ReadableStream( { cancel: never } ) );
		}
	} );
	const store = createPersistentAssets();
	assert.equal( await store.has( "https://example.test", "a" ), true );
	for ( let i = 0; i < 100; i++ ) await store.has( "https://example.test", String( i ) );
	assert.ok( matches <= 8 );
	t.mock.timers.tick( 2000 );
	await settle();
});

/*
================
workerFixture

A missing container exercises the supported loose-file fallback while keeping
real manifest admission, SHA-256 checking and worker scheduling in the test.
================
*/
function workerFixture( t, corrupt = false, expected = 1 ) {
	const bytes = Uint8Array.of( 1, 2, 3 );
	const digest = createHash( "sha256" ).update( bytes ).digest( "hex" );
	const packPath = "/assets/packs/test.bin";
	const manifest = {
		version: 2,
		groups: [ {
			name: "test",
			assetCount: 1,
			packs: [ { path: packPath, bytes: 100, sha256: digest, assetCount: 1 } ]
		} ],
		assets: [ {
			path: "/assets/test.bin",
			packPath,
			offset: 0,
			length: 3,
			mime: "application/octet-stream",
			sha256: digest
		} ]
	};
	t.mock.method( globalThis, "fetch", async url => {
		if ( url.endsWith( "/manifest.json" ) ) return Response.json( manifest );
		if ( url.endsWith( packPath ) ) return new Response( null, { status: 404 } );
		return new Response( corrupt ? Uint8Array.of( 9, 9, 9 ) : bytes );
	} );
	const messages = [];
	let complete = () => {};
	const completed = new Promise( resolve => {
		complete = () => resolve( undefined );
	} );
	const loader = createLoader( message => {
		if ( message.kind !== "progress" ) messages.push( message );
		if ( messages.length === expected ) complete();
	} );
	t.after( () => loader.dispose() );
	const load = id => loader.receive( { kind: "load", id, url: "https://example.test/assets/test.bin", limit: 3 } );
	return { loader, messages, load, completed };
}

for ( const phase of [ "open", "match", "body" ] ) {
	test( `four pending cache ${phase} loads cancel and release all worker slots`, { timeout: 10000 }, async t => {
		let calls = 0;
		const hang = () => {
			calls++;
			return never();
		};
		backend( t, {
			match: phase === "match" ? hang : async () =>
				new Response(
					new ReadableStream( { pull: hang, cancel: never } ),
					{ headers: { "content-length": "3" } }
				)
		}, phase === "open" ? hang : undefined );
		const { loader, messages, load, completed } = workerFixture( t, false, 8 );
		for ( let id = 1; id <= 4; id++ ) load( id );
		await settle();
		assert.ok( calls > 0 );
		assert.equal( messages.length, 0 );
		for ( let id = 1; id <= 4; id++ ) loader.receive( { kind: "cancel", id } );
		await settle();
		assert.deepEqual( messages.map( message => message.kind ), [ "released", "released", "released", "released" ] );
		const before = calls;
		for ( let id = 5; id <= 8; id++ ) load( id );
		await settle();
		t.mock.timers.tick( 2000 );
		await completed;
		assert.equal( messages.length, 8 );
		for ( const message of messages.slice( 4 ) ) {
			assert.equal( message.kind, "bytes" );
			assert.deepEqual( [ ...new Uint8Array( message.buffer ) ], [ 1, 2, 3 ] );
		}
		assert.ok( calls - before <= 8 );
	} );
}

for ( const corrupt of [ false, true ] ) {
	test( `cache timeout falls back to network with integrity ${corrupt ? "rejection" : "admission"}`, {
		timeout: 10000
	}, async t => {
		backend( t, {}, never );
		const { messages, load, completed } = workerFixture( t, corrupt );
		load( 1 );
		await settle();
		t.mock.timers.tick( 2000 );
		await completed;
		assert.equal( messages.length, 1 );
		assert.equal( messages[0].kind, corrupt ? "error" : "bytes" );
		if ( corrupt ) assert.match( messages[0].error, /SHA-256/ );
	} );
}

test("healthy cache remains usable after cancellation and a saturated burst", async t => {
	let release = () => {}, matches = 0;
	const waiting = new Promise( resolve => {
		release = () => resolve( undefined );
	} );
	let blocked = true;
	backend( t, {
		match: async () => {
			matches++;
			if ( blocked ) await waiting;
			return new Response( Uint8Array.of( 1, 2, 3 ), { headers: { "content-length": "3" } } );
		}
	} );
	const store = createPersistentAssets();
	const controller = new AbortController();
	const cancelled = store.read( "https://example.test", "cancel", 3, controller.signal );
	const rejected = assert.rejects( cancelled, { name: "AbortError" } );
	await settle();
	controller.abort();
	await rejected;
	const burst = Array.from( { length: 24 }, ( _, i ) => store.read( "https://example.test", String( i ), 3 ) );
	await settle();
	assert.ok( matches <= 8 );
	blocked = false;
	assert.ok( release );
	release();
	await Promise.all( burst );
	const before = matches;
	assert.deepEqual( await store.read( "https://example.test", "healthy", 3 ), Uint8Array.of( 1, 2, 3 ) );
	assert.equal( matches, before + 1 );
	t.mock.timers.tick( 2000 );
	await settle();
	assert.deepEqual( await store.read( "https://example.test", "still-healthy", 3 ), Uint8Array.of( 1, 2, 3 ) );
});

for ( const phase of [ "keys", "put", "delete" ] ) {
	test(`stalled cache ${phase} cannot strand publication or flush`, async t => {
		let calls = 0;
		backend( t, {
			[phase]: () => {
				calls++;
				return never();
			}
		} );
		const store = createPersistentAssets();
		if ( phase === "delete" ) void store.remove( "https://example.test", "old" );
		store.enqueue( "https://example.test", "new", Uint8Array.of( 1, 2, 3 ) );
		let flushed = false;
		const flush = store.flush().then( () => {
			flushed = true;
		} );
		await settle();
		assert.equal( calls, 1 );
		assert.equal( flushed, false );
		t.mock.timers.tick( 2000 );
		await settle();
		await flush;
		assert.equal( flushed, true );
		assert.equal( store.stats().queuedBytes, 0 );
		store.enqueue( "https://example.test", "later", Uint8Array.of( 1, 2, 3 ) );
		await store.flush();
		assert.equal( calls, 1 );
	});
}

test("late successful invalidation precedes publication of its replacement", async t => {
	let deleted, releaseDelete = () => {};
	const deletion = new Promise( resolve => {
		releaseDelete = () => resolve( undefined );
	} );
	const events = [];
	backend( t, {
		delete: async () => {
			await deletion;
			deleted = true;
			events.push( "delete" );
			return true;
		},
		put: async () => {
			assert.equal( deleted, true );
			events.push( "put" );
		}
	} );
	const store = createPersistentAssets();
	void store.remove( "https://example.test", "a" );
	store.enqueue( "https://example.test", "a", Uint8Array.of( 1, 2, 3 ) );
	await settle();
	assert.deepEqual( events, [] );
	assert.ok( releaseDelete );
	releaseDelete();
	await store.flush();
	assert.deepEqual( events, [ "delete", "put" ] );
});

for ( const phase of [ "match", "body" ] ) {
	test(`cancelling a stalled ${phase} preserves the same entry even if abandoned work later rejects`, async t => {
		let rejectRead = ( reason ) => {}, deletes = 0, first = true;
		const stalled = new Promise( ( resolve, reject ) => {
			rejectRead = reject;
		} );
		const bytes = Uint8Array.of( 1, 2, 3 );
		backend( t, {
			match: async () => {
				if ( first ) {
					first = false;
					if ( phase === "match" ) await stalled;
					else {return new Response( new ReadableStream( { pull: () => stalled } ), {
							headers: { "content-length": "3" }
						} );}
				}
				return new Response( bytes, { headers: { "content-length": "3" } } );
			},
			delete: async () => {
				deletes++;
				return true;
			}
		} );
		const store = createPersistentAssets();
		const controller = new AbortController();
		const read = store.read( "https://example.test", "same", 3, controller.signal );
		const rejected = assert.rejects( read, { name: "AbortError" } );
		await settle();
		controller.abort();
		await rejected;
		assert.ok( rejectRead );
		rejectRead( new Error( "Late storage failure" ) );
		await settle();
		await store.flush();
		assert.equal( deletes, 0 );
		assert.deepEqual( await store.read( "https://example.test", "same", 3 ), bytes );
		assert.equal( deletes, 0 );
	});
}

test("saturation and match failure never delete cached entries", async t => {
	let release = () => {}, deletes = 0, fail = false;
	const waiting = new Promise( resolve => {
		release = () => resolve( undefined );
	} );
	backend( t, {
		match: async () => {
			await waiting;
			if ( fail ) throw new Error( "Storage temporarily unavailable" );
			return new Response( Uint8Array.of( 1, 2, 3 ), { headers: { "content-length": "3" } } );
		},
		delete: async () => {
			deletes++;
			return true;
		}
	} );
	const store = createPersistentAssets();
	const burst = Array.from( { length: 24 }, () => store.read( "https://example.test", "same", 3 ) );
	await settle();
	assert.ok( release );
	release();
	await Promise.all( burst );
	await store.flush();
	assert.equal( deletes, 0 );
	fail = true;
	assert.equal( await store.read( "https://example.test", "same", 3 ), null );
	await store.flush();
	assert.equal( deletes, 0 );
	fail = false;
	assert.deepEqual( await store.read( "https://example.test", "same", 3 ), Uint8Array.of( 1, 2, 3 ) );
});

test("completed truncated cache body is invalidated", async t => {
	let deletes = 0;
	backend( t, {
		match: async () => new Response( Uint8Array.of( 1, 2 ), { headers: { "content-length": "3" } } ),
		delete: async () => {
			deletes++;
			return true;
		}
	} );
	const store = createPersistentAssets();
	assert.equal( await store.read( "https://example.test", "truncated", 3 ), null );
	await store.flush();
	assert.equal( deletes, 1 );
});

test("a correct header with an oversized cached body is replaced by verified network bytes", async t => {
	const bytes = Uint8Array.of( 1, 2, 3 );
	const digest = createHash( "sha256" ).update( bytes ).digest( "hex" );
	const key = "https://example.test/assets/.verified/" + digest;
	const rows = new Map( [ [ key, Uint8Array.of( 1, 2, 3, 4 ) ] ] );
	let deletes = 0, puts = 0, downloads = 0;
	backend( t, {
		match: async url => {
			const value = rows.get( typeof url === "string" ? url : url.url );
			return value ? new Response( value, { headers: { "content-length": "3" } } ) : undefined;
		},
		keys: async () => [ ...rows.keys() ].map( url => new Request( url ) ),
		delete: async url => {
			deletes++;
			return rows.delete( url );
		},
		put: async ( url, response ) => {
			puts++;
			rows.set( url, new Uint8Array( await response.arrayBuffer() ) );
		}
	} );
	workerFixture( t );
	const packs = createPacks( async url => {
		downloads++;
		const response = await fetch( url );
		if ( !response.ok ) throw Object.assign( new Error( "HTTP failure" ), { status: response.status } );
		return new Uint8Array( await response.arrayBuffer() );
	} );
	t.after( () => packs.dispose() );
	const url = new URL( "https://example.test/assets/test.bin" );
	assert.deepEqual( await packs.read( url, 3, new AbortController().signal ), bytes );
	await packs.flush();
	assert.equal( deletes, 1 );
	assert.equal( puts, 1 );
	assert.deepEqual( rows.get( key ), bytes );
	const before = downloads;
	assert.deepEqual( await packs.read( url, 3, new AbortController().signal ), bytes );
	assert.equal( downloads, before );
	assert.ok( packs.stats().hits > 0 );
});

for ( const timeout of [ false, true ] ) {
	test(`late cache match body is cancelled after ${timeout ? "timeout" : "caller cancellation"} without deletion`, async t => {
		let resolveMatch = ( response ) => {}, cancels = 0, deletes = 0;
		const pending = new Promise( resolve => {
			resolveMatch = resolve;
		} );
		backend( t, {
			match: () => pending,
			delete: async () => {
				deletes++;
				return true;
			}
		} );
		const store = createPersistentAssets();
		const controller = new AbortController();
		const read = store.read( "https://example.test", "same", 3, controller.signal );
		const result = timeout ? read : assert.rejects( read, { name: "AbortError" } );
		await settle();
		if ( timeout ) t.mock.timers.tick( 2000 );
		else controller.abort();
		await result;
		resolveMatch(
			new Response(
				new ReadableStream( {
					cancel: () => {
						cancels++;
						return never();
					}
				} ),
				{
					headers: { "content-length": "3" }
				}
			)
		);
		await settle();
		assert.equal( cancels, 1 );
		assert.equal( deletes, 0 );
		t.mock.timers.tick( 2000 );
		await settle();
		await store.flush();
		assert.equal( deletes, 0 );
	});
}

test("abandoned match cleanup has finite admission even when every cancel hangs", async t => {
	let resolveMatch = ( response ) => {}, cancels = 0, matches = 0;
	backend( t, {
		match: () => {
			matches++;
			return new Promise( resolve => {
				resolveMatch = resolve;
			} );
		}
	} );
	const store = createPersistentAssets();
	for ( let i = 0; i < 12; i++ ) {
		const controller = new AbortController();
		const read = store.read( "https://example.test", String( i ), 3, controller.signal );
		const result = read.catch( error => {
			assert.equal( error.name, "AbortError" );
		} );
		await settle();
		controller.abort();
		await result;
		resolveMatch(
			new Response(
				new ReadableStream( {
					cancel: () => {
						cancels++;
						return never();
					}
				} ),
				{
					headers: { "content-length": "3" }
				}
			)
		);
		await settle();
	}
	assert.equal( matches, 8 );
	assert.equal( cancels, 8 );
	t.mock.timers.tick( 2000 );
	await settle();
});
