import { test } from "node:test";
import assert from "node:assert/strict";
import { build } from "esbuild";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { defined } from "../helpers/defined.mjs";
const compiled = await build( {
	entryPoints: [ path.join( root, "src/engine/runtime/assets/assets.ts" ) ],
	bundle: true,
	platform: "node",
	format: "esm",
	write: false,
	define: { "import.meta.url": JSON.stringify( "https://assets.test/assets.ts" ) }
} );
const { createAssets } = await import(
	"data:text/javascript;base64," + Buffer.from( compiled.outputFiles[0].contents ).toString( "base64" )
);
function fixture( t ) {
	const old = Object.getOwnPropertyDescriptor( globalThis, "Worker" );
	let worker;
	class Worker {
		messages = [];
		constructor() {
			worker = this;
		}
		postMessage( m ) {
			this.messages.push( m );
		}
		terminate() {}
	}
	Object.defineProperty( globalThis, "Worker", { configurable: true, value: Worker } );
	t.after( () => {
		if ( old ) Object.defineProperty( globalThis, "Worker", old );
		else delete globalThis.Worker;
	} );
	const assets = createAssets();
	t.after( () => assets.dispose() );
	return { assets, worker };
}
test("cancelled handles reserve capacity until the worker releases execution", t => {
	const { assets, worker } = fixture( t ),
		ids = Array.from( { length: 4 }, ( _, i ) => assets.request( "https://assets.test/" + i ) );
	assets.cancel( ids[0] );
	assert.equal( assets.available(), 0 );
	assert.throws( () => assets.request( "https://assets.test/replacement" ), /budget/ );
	assert.equal( assets.take( ids[0] ), null );
	defined( worker ).onmessage( { data: { kind: "released", id: ids[0] } } );
	assert.equal( assets.available(), 1 );
	assert.doesNotThrow( () => assets.request( "https://assets.test/replacement" ) );
});
test("duplicate URLs have independent cancellation and completion handles", t => {
	const { assets, worker } = fixture( t ),
		a = assets.request( "https://assets.test/a" ),
		b = assets.request( "https://assets.test/a" );
	assert.notEqual( a, b );
	assets.cancel( a );
	defined( worker ).onmessage( { data: { kind: "bytes", id: b, buffer: new ArrayBuffer( 1 ) } } );
	assert.equal( assets.take( a ), null );
	assert.equal( assets.take( b ).id, b );
	assert.equal( assets.take( b ), null );
	assert.deepEqual( defined( worker ).messages.filter( m => m.kind === "cancel" ).map( m => m.id ), [ a ] );
});
test("image ownership transfers only to the caller taking its handle", t => {
	const { assets, worker } = fixture( t );
	let closedA = 0, closedB = 0;
	const a = assets.request( "https://assets.test/a", 1024, "png" ),
		b = assets.request( "https://assets.test/a", 1024, "png" );
	defined( worker ).onmessage( {
		data: {
			kind: "image",
			id: a,
			image: {
				close() {
					closedA++;
				}
			}
		}
	} );
	defined( worker ).onmessage( {
		data: {
			kind: "image",
			id: b,
			image: {
				close() {
					closedB++;
				}
			}
		}
	} );
	const taken = assets.take( a );
	assets.cancel( b );
	assets.dispose();
	assert.equal( closedA, 0 );
	assert.equal( closedB, 1 );
	taken.image.close();
	assert.equal( closedA, 1 );
});

test("idle worker failure is observable independently of capacity and disposal", t => {
	const { assets, worker } = fixture( t );
	assert.deepEqual( assets.health(), { phase: "running" } );
	defined( worker ).onerror();
	assert.deepEqual( assets.health(), { phase: "failed", error: "Asset worker failed" } );
	assert.equal( assets.available(), 0 );
	assert.throws( () => assets.request( "https://assets.test/a" ), /Asset worker failed/ );
	assets.dispose();
	assert.deepEqual( assets.health(), { phase: "disposed" } );
});
test("worker failure retires owned completions and completes outstanding handles once", t => {
	const { assets, worker } = fixture( t );
	let closed = 0;
	const a = assets.request( "https://assets.test/a" ), b = assets.request( "https://assets.test/b" );
	defined( worker ).onmessage( {
		data: {
			kind: "image",
			id: a,
			image: {
				close() {
					closed++;
				}
			}
		}
	} );
	defined( worker ).onmessageerror();
	defined( worker ).onerror();
	assert.equal( closed, 1 );
	assert.equal( assets.take( a ).kind, "error" );
	assert.equal( assets.take( b ).kind, "error" );
	assert.equal( assets.take( b ), null );
});

test("failed load submission terminates the channel and retires outstanding results", t => {
	const { assets, worker } = fixture( t );
	let closed = 0, terminated = 0;
	defined( worker ).terminate = () => terminated++;
	const a = assets.request( "https://assets.test/a" ), b = assets.request( "https://assets.test/b" );
	defined( worker ).onmessage( {
		data: {
			kind: "image",
			id: a,
			image: {
				close() {
					closed++;
				}
			}
		}
	} );
	defined( worker ).postMessage = () => {
		throw Error( "channel unavailable" );
	};
	assert.throws( () => assets.request( "https://assets.test/c" ), /channel unavailable/ );
	assert.equal( assets.health().phase, "failed" );
	assert.match( assets.health().error, /submission failed/ );
	assert.equal( terminated, 1 );
	assert.equal( closed, 1 );
	assert.equal( assets.available(), 0 );
	assert.equal( assets.take( a ).kind, "error" );
	assert.equal( assets.take( b ).kind, "error" );
	assert.equal( assets.take( 3 ), null, "failed request never exposes an orphan handle" );
	assert.throws( () => assets.request( "https://assets.test/d" ), /submission failed/ );
});

test("failed cancellation submission fails remaining jobs instead of claiming healthy capacity", t => {
	const { assets, worker } = fixture( t ),
		a = assets.request( "https://assets.test/a" ),
		b = assets.request( "https://assets.test/b" );
	defined( worker ).postMessage = () => {
		throw Error( "cancel send failed" );
	};
	assert.throws( () => assets.cancel( a ), /cancel send failed/ );
	assert.equal( assets.take( a ), null );
	assert.equal( assets.take( b ).kind, "error" );
	assert.equal( assets.health().phase, "failed" );
	assert.equal( assets.available(), 0 );
});

test("progress messages do not complete handles or release download capacity", t => {
	const { assets, worker } = fixture( t ), id = assets.request( "https://assets.test/a" );
	const progress = {
		bytesReceived: 4096,
		bytesPerSecond: 1024,
		filesReady: 3,
		filesActive: 1,
		cacheHits: 2,
		currentFile: "/assets/a"
	};
	defined( worker ).onmessage( { data: { kind: "progress", progress } } );
	assert.deepEqual( assets.progress(), progress );
	assert.equal( assets.available(), 3 );
	assert.equal( assets.take( id ), null );
	defined( worker ).onmessage( { data: { kind: "bytes", id, buffer: new ArrayBuffer( 1 ) } } );
	assert.equal( assets.take( id ).kind, "bytes" );
	assert.equal( assets.available(), 4 );
});

test("a stalled load fails its own handle visibly and the shared owner lives", t => {
	let now = 0;
	t.mock.method( performance, "now", () => now );
	const { assets } = fixture( t );
	const first = assets.request( "https://assets.test/stalled" ),
		second = assets.request( "https://assets.test/other" );
	now = 119999;
	assert.equal( assets.health().phase, "running" );
	assert.equal( assets.take( first ), null );
	now = 120000;
	// Both loads went silent for the whole stall window: each expires as a
	// transient error on its own handle, the owner never fails, and a retry
	// is a fresh request, not a thrown terminal error.
	assert.equal( assets.health().phase, "running" );
	assert.match( assets.take( first ).error, /timed out while loading \/stalled/ );
	assert.equal( assets.take( first ), null );
	const secondResult = assets.take( second );
	assert.match( secondResult.error, /timed out/ );
	assert.equal( secondResult.transient, true );
	assert.equal( assets.available(), 4 );
	assert.doesNotThrow( () => assets.request( "https://assets.test/retry" ) );
});
test("an unacknowledged cancellation is abandoned, keeps its slot and reports once", t => {
	let now = 0;
	t.mock.method( performance, "now", () => now );
	const { assets, worker } = fixture( t );
	const first = assets.request( "https://assets.test/stalled" );
	assets.cancel( first );
	now = 29999;
	assert.equal( assets.health().phase, "running" );
	assert.deepEqual( assets.takeDegradations(), [] );
	now = 30000;
	assert.equal( assets.health().phase, "running" );
	assert.equal( assets.available(), 3 );
	const reported = assets.takeDegradations();
	assert.equal( reported.length, 1 );
	assert.equal( reported[0].code, "asset-cancel-unacknowledged" );
	assert.match( reported[0].message, /\/stalled/ );
	now = 90000;
	assets.health();
	assert.deepEqual( assets.takeDegradations(), [] );
	defined( worker ).onmessage( { data: { kind: "released", id: first } } );
	assert.equal( assets.available(), 4 );
});
test("every slot held by abandoned cancellations fails the owner with a named reason", t => {
	let now = 0;
	t.mock.method( performance, "now", () => now );
	const { assets } = fixture( t );
	for ( const name of [ "a", "b", "c", "d" ] ) assets.cancel( assets.request( "https://assets.test/" + name ) );
	now = 30000;
	assert.equal( assets.health().phase, "failed" );
	assert.match( assets.health().error, /stopped acknowledging cancellations: \/a, \/b, \/c, \/d/ );
});
