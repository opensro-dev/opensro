/*
===========================================================================

packs.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";
import fs from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { defined } from "../helpers/defined.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createPackIndex } = await load( "src/engine/runtime/assets/worker/packs/index/index.ts" );
const { createPacks } = await load( "src/engine/runtime/assets/worker/packs/packs.ts" );
const sha = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );
function fixture( assetPath = "/assets/a.bin" ) {
	const a = Uint8Array.of( 1, 2, 3 ), b = gzipSync( Buffer.from( '{"fixture":true}' ) );
	const entries = [ {
		path: assetPath,
		offset: 0,
		length: a.length,
		mime: "application/octet-stream",
		sha256: sha( a )
	}, {
		path: "/assets/b.json.gz",
		offset: a.length,
		length: b.length,
		mime: "application/octet-stream",
		sha256: sha( b )
	} ];
	const header = Buffer.from( JSON.stringify( { format: "sro-asset-pack", version: 2, files: entries } ) );
	const bytes = new Uint8Array( 12 + header.length + a.length + b.length );
	bytes.set( Buffer.from( "SROPACK2" ) );
	new DataView( bytes.buffer ).setUint32( 8, header.length, true );
	bytes.set( header, 12 );
	bytes.set( a, 12 + header.length );
	bytes.set( b, 12 + header.length + a.length );
	const pack = { path: "/assets/packs/fixture.bin", bytes: bytes.length, sha256: sha( bytes ), assetCount: 2 };
	const manifest = {
		version: 2,
		groups: [ { name: "test", assetCount: 2, packs: [ pack ] } ],
		assets: entries.map( e => ({ ...e, packPath: pack.path }) )
	};
	return { bytes, manifest, pack };
}
test("published manifest is admitted without importing the old runtime", () => {
	const manifest = JSON.parse(
		fs.readFileSync( CLIENT_PUBLIC_ROOT + "/assets/packs/manifest.json", "utf8" )
	);
	const index = createPackIndex().manifest( manifest );
	assert.equal( index.assets.size, manifest.assets.length );
	assert.equal( index.packs.size, manifest.groups.reduce( ( count, g ) => count + g.packs.length, 0 ) );
});
test("failed manifest admission is evicted; concurrent retries share fresh work", async () => {
	for ( const failure of [ "network", "schema" ] ) {
		const f = fixture();
		let requests = 0;
		const packs = createPacks( async url => {
			if ( !url.endsWith( "manifest.json" ) ) return f.bytes;
			if ( ++requests === 1 ) {
				if ( failure === "network" ) throw new Error( "503" );
				return new TextEncoder().encode( "{}" );
			}
			return new TextEncoder().encode( JSON.stringify( f.manifest ) );
		} );
		const read = () => packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal );
		const first = await Promise.allSettled( [ read(), read() ] );
		assert.ok( first.every( r => r.status === "rejected" ) );
		assert.equal( requests, 1 );
		const recovered = await Promise.all( [ read(), read() ] );
		assert.deepEqual( [ ...recovered[0] ], [ 1, 2, 3 ] );
		assert.equal( requests, 2 );
		await read();
		assert.equal( requests, 2 );
		packs.dispose();
	}
});
test("concurrent logical assets share one verified pack and decompress JSON in the worker", async () => {
	const f = fixture(), requests = [];
	const packs = createPacks( async url => {
		requests.push( url );
		return url.endsWith( "manifest.json" ) ? new TextEncoder().encode( JSON.stringify( f.manifest ) ) : f.bytes;
	} );
	const signal = new AbortController().signal;
	const [a, b] = await Promise.all( [
		packs.read( new URL( "http://localhost/assets/a.bin" ), 100, signal ),
		packs.read( new URL( "http://localhost/assets/b.json" ), 100, signal )
	] );
	assert.deepEqual( [ ...a ], [ 1, 2, 3 ] );
	assert.deepEqual( JSON.parse( new TextDecoder().decode( b ) ), { fixture: true } );
	assert.equal( requests.length, 2 );
	a[0] = 99;
	assert.equal( (await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, signal ))[0], 1 );
	assert.equal( requests.length, 2 );
	packs.dispose();
});
test("replacement members wait behind four shared downloads and do not fail world admission", async () => {
	const fixtures = Array.from( { length: 6 }, ( _, i ) => {
		const data = Uint8Array.of( i ),
			entry = {
				path: `/assets/member-${i}.bin`,
				offset: 0,
				length: 1,
				mime: "application/octet-stream",
				sha256: sha( data )
			};
		const header = Buffer.from( JSON.stringify( { format: "sro-asset-pack", version: 2, files: [ entry ] } ) );
		const bytes = new Uint8Array( 13 + header.length );
		bytes.set( Buffer.from( "SROPACK2" ) );
		new DataView( bytes.buffer ).setUint32( 8, header.length, true );
		bytes.set( header, 12 );
		bytes.set( data, 12 + header.length );
		const pack = { path: `/assets/packs/${i}.bin`, bytes: bytes.length, sha256: sha( bytes ), assetCount: 1 };
		return { entry, pack, bytes };
	} );
	const manifest = {
		version: 2,
		groups: [ { name: "test", assetCount: 6, packs: fixtures.map( f => f.pack ) } ],
		assets: fixtures.map( f => ({ ...f.entry, packPath: f.pack.path }) )
	};
	let active = 0, maximum = 0;
	const release = [];
	const packs = createPacks( async url => {
		if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( manifest ) );
		active++;
		maximum = Math.max( active, maximum );
		await new Promise( resolve => release.push( resolve ) );
		active--;
		return defined( fixtures.find( f => url.endsWith( f.pack.path ) ) ).bytes;
	} );
	const operations = fixtures.map( f =>
		packs.read( new URL( "http://localhost" + f.entry.path ), 10, new AbortController().signal )
	);
	while ( release.length < 4 ) await new Promise( resolve => setImmediate( resolve ) );
	assert.equal( active, 4 );
	release.splice( 0 ).forEach( resolve => resolve() );
	while ( release.length < 2 ) await new Promise( resolve => setImmediate( resolve ) );
	release.splice( 0 ).forEach( resolve => resolve() );
	assert.deepEqual( (await Promise.all( operations )).map( bytes => bytes[0] ), [ 0, 1, 2, 3, 4, 5 ] );
	assert.equal( maximum, 4 );
	packs.dispose();
});
test("pack hash and header disagreement prevent publication", async () => {
	for ( const corrupt of [ "hash", "header" ] ) {
		const f = fixture();
		if ( corrupt === "hash" ) f.bytes[f.bytes.length - 1] ^= 1;
		else f.manifest.assets[0].mime = "wrong/type";
		const packs = createPacks( async url =>
			url.endsWith( "manifest.json" ) ? new TextEncoder().encode( JSON.stringify( f.manifest ) ) : f.bytes
		);
		await assert.rejects(
			packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal ),
			/mismatch|disagrees/
		);
		packs.dispose();
	}
});
test("manifest rejects duplicate and overlapping asset ranges", () => {
	const f = fixture();
	f.manifest.assets[1].offset = 0;
	assert.throws( () => createPackIndex().manifest( f.manifest ), /range/ );
	const g = fixture();
	g.manifest.assets[1].path = g.manifest.assets[0].path;
	assert.throws( () => createPackIndex().manifest( g.manifest ), /duplicate/ );
});

test("missing pack permits only hash-verified loose delivery and preserves compressed JSON", async () => {
	for ( const corrupt of [ false, true ] ) {
		const f = fixture();
		let missing = 0;
		const requests = [];
		const packs = createPacks( async ( url, limit ) => {
			requests.push( url );
			if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
			if ( url.endsWith( ".bin" ) && url.includes( "/packs/" ) ) {
				missing++;
				throw Object.assign( new Error( "missing" ), { status: 404 } );
			}
			if ( url.endsWith( "a.bin" ) ) return Uint8Array.of( corrupt ? 9 : 1, 2, 3 );
			return gzipSync( Buffer.from( '{"fixture":true}' ) );
		} );
		const signal = new AbortController().signal;
		if ( corrupt ) {
			await assert.rejects( packs.read( new URL( "http://localhost/assets/a.bin" ), 100, signal ), /SHA-256/ );
		} else {
			assert.deepEqual( [ ...await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, signal ) ], [
				1,
				2,
				3
			] );
			assert.deepEqual(
				JSON.parse(
					new TextDecoder().decode(
						await packs.read( new URL( "http://localhost/assets/b.json" ), 100, signal )
					)
				),
				{ fixture: true }
			);
			assert.equal( missing, 1 );
			assert.ok( requests.at( -1 ).endsWith( ".json.gz" ) );
		}
		packs.dispose();
	}
});
test("server failures do not bypass pack delivery", async () => {
	const f = fixture(), requests = [];
	const packs = createPacks( async url => {
		requests.push( url );
		if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
		throw Object.assign( new Error( "service unavailable" ), { status: 503 } );
	} );
	await assert.rejects(
		packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal ),
		/service unavailable/
	);
	assert.equal( requests.length, 2 );
	packs.dispose();
});

test("URL encoded filenames resolve against literal published names", async () => {
	for ( const name of [ "space name", "unicode-\u89d2\u8272" ] ) {
		const assetPath = "/assets/" + name + ".bin", f = fixture( assetPath );
		const packs = createPacks( async url =>
			url.endsWith( "manifest.json" ) ? new TextEncoder().encode( JSON.stringify( f.manifest ) ) : f.bytes
		);
		const url = new URL( "http://localhost/assets/" + encodeURIComponent( name ) + ".bin" );
		assert.deepEqual( [ ...await packs.read( url, 100, new AbortController().signal ) ], [ 1, 2, 3 ] );
		packs.dispose();
	}
});

function disk( t ) {
	const rows = new Map();
	let denied = false, quota = false;
	// A Cache takes a URL string or a Request (the store's inventory rebuild
	// passes the Requests keys() returned).
	const rowKey = request => request instanceof Request ? request.url : String( request );
	const cache = {
		async match( url ) {
			return rows.get( rowKey( url ) )?.clone();
		},
		async keys() {
			return [ ...rows.keys() ].map( url => new Request( url ) );
		},
		async delete( url ) {
			return rows.delete( rowKey( url ) );
		},
		async put( url, response ) {
			if ( denied ) throw Error( "denied" );
			if ( quota ) {
				quota = false;
				throw new DOMException( "full", "QuotaExceededError" );
			}
			rows.set( rowKey( url ), new Response( await response.arrayBuffer(), { headers: response.headers } ) );
		}
	};
	const old = Object.getOwnPropertyDescriptor( globalThis, "caches" );
	Object.defineProperty( globalThis, "caches", {
		configurable: true,
		value: {
			async open() {
				if ( denied ) throw Error( "denied" );
				return cache;
			}
		}
	} );
	t.after( () => {
		if ( old ) Object.defineProperty( globalThis, "caches", old );
		else delete globalThis.caches;
	} );
	return {
		rows,
		cache,
		deny: () => {
			denied = true;
		},
		quota: () => {
			quota = true;
		}
	};
}
test("persistent cache survives owner replacement, validates current manifest and repairs corrupt payloads", async t => {
	const d = disk( t ), f = fixture(), requests = [];
	let current = f;
	async function run() {
		const packs = createPacks( async url => {
			requests.push( url );
			return url.endsWith( "manifest.json" ) ?
				new TextEncoder().encode( JSON.stringify( current.manifest ) ) :
				current.bytes;
		} );
		const result = await packs.read(
			new URL( "http://localhost/assets/a.bin" ),
			100,
			new AbortController().signal
		);
		packs.dispose();
		return result;
	}
	await run();
	assert.equal( requests.length, 2 );
	await run();
	assert.equal( requests.length, 3, "warm owner only revalidates the manifest" );
	const key = [ ...d.rows.keys() ][0];
	d.rows.set(
		key,
		new Response( new Uint8Array( f.bytes.length ), { headers: { "content-length": String( f.bytes.length ) } } )
	);
	await run();
	assert.equal( requests.length, 5, "hash corruption repaired from network" );
	d.rows.set( key, new Response( Uint8Array.of( 1 ), { headers: { "content-length": String( f.bytes.length ) } } ) );
	await run();
	assert.equal( requests.length, 7, "truncated cache is removed before publication" );
	await run();
	assert.equal( requests.length, 8 );
	current = fixture();
	current.bytes[current.bytes.length - 1] ^= 1;
	current.pack.sha256 = sha( current.bytes );
	await run();
	assert.equal( requests.length, 10, "new content identity downloads new container" );
});
test("loose payloads survive refresh without probing missing containers", async t => {
	disk( t );
	const f = fixture(), requests = [];
	for ( let pass = 0; pass < 2; pass++ ) {
		const packs = createPacks( async url => {
			requests.push( url );
			if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
			if ( url.includes( "/packs/" ) ) throw Object.assign( Error( "missing" ), { status: 404 } );
			return Uint8Array.of( 1, 2, 3 );
		} );
		assert.deepEqual( [
			...await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal )
		], [ 1, 2, 3 ] );
		packs.dispose();
	}
	assert.equal( requests.length, 4 );
	assert.ok( requests.at( -1 ).endsWith( "manifest.json" ) );
});
test("denied persistence and quota pressure do not turn valid downloads into load failures", async t => {
	const d = disk( t ), f = fixture();
	d.quota();
	for ( let pass = 0; pass < 2; pass++ ) {
		if ( pass ) d.deny();
		const packs = createPacks( async url =>
			url.endsWith( "manifest.json" ) ? new TextEncoder().encode( JSON.stringify( f.manifest ) ) : f.bytes
		);
		assert.deepEqual( [
			...await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal )
		], [ 1, 2, 3 ] );
		packs.dispose();
	}
});

test("persistent payload eviction obeys the quota-derived budget without clearing unrelated caches", async t => {
	const d = disk( t ), old = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: { storage: { estimate: async () => ({ quota: 32 }) } }
	} );
	t.after( () => {
		if ( old ) Object.defineProperty( globalThis, "navigator", old );
		else delete globalThis.navigator;
	} );
	const { createPersistentAssets } = await load( "src/engine/runtime/assets/worker/packs/persistent.ts" ),
		owner = createPersistentAssets( quota => quota / 4 );
	await Promise.all(
		[ "a", "b", "c" ].map( key => owner.write( "http://localhost", key, Uint8Array.of( 1, 2, 3 ) ) )
	);
	assert.equal( d.rows.size, 2 );
	assert.equal( await owner.read( "http://localhost", "a", 3 ), null );
	assert.deepEqual( [ ...await owner.read( "http://localhost", "c", 3 ) ], [ 1, 2, 3 ] );
	assert.equal( owner.stats().evictions, 1 );
	await owner.write( "http://localhost", "oversized", new Uint8Array( 9 ) );
	assert.equal( d.rows.size, 2 );
});

test("the budget is half the quota between 512 MiB and 4 GiB", async () => {
	const { budgetFromQuota } = await load( "src/engine/runtime/assets/worker/packs/persistent.ts" );
	const MiB = 1024 * 1024, GiB = 1024 * MiB;
	assert.equal( budgetFromQuota( undefined ), 512 * MiB );
	assert.equal( budgetFromQuota( 100 * MiB ), 512 * MiB );
	assert.equal( budgetFromQuota( 3 * GiB ), 1.5 * GiB );
	assert.equal( budgetFromQuota( 600 * GiB ), 4 * GiB );
});

/*
================
quotaNavigator

A navigator whose storage estimate reports quota bytes; restored after t.
================
*/
function quotaNavigator( t, quota ) {
	const old = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: { storage: { estimate: async () => ({ quota }) } }
	} );
	t.after( () => {
		if ( old ) Object.defineProperty( globalThis, "navigator", old );
		else delete globalThis.navigator;
	} );
}

test("startup entries survive eviction, also after a reload", async t => {
	const d = disk( t );
	quotaNavigator( t, 32 );
	const { createPersistentAssets } = await load( "src/engine/runtime/assets/worker/packs/persistent.ts" );
	const origin = "http://localhost", bytes = Uint8Array.of( 1, 2, 3 );
	const first = createPersistentAssets( quota => quota / 4 );
	first.setStartup( origin, [ "ui" ] );
	for ( const digest of [ "ui", "world-a", "world-b" ] ) await first.write( origin, digest, bytes );
	assert.ok( await first.read( origin, "ui", 3 ), "the startup entry outlives the eviction" );
	assert.equal( await first.read( origin, "world-a", 3 ), null );
	// A new owner rebuilds its inventory from Cache Storage; the manifest names the startup set again.
	const second = createPersistentAssets( quota => quota / 4 );
	second.setStartup( origin, [ "ui" ] );
	await second.write( origin, "world-c", bytes );
	assert.ok( await second.read( origin, "ui", 3 ) );
	assert.equal( d.rows.size, 2 );
});

test("a write that cannot fit beside the startup entries is skipped, never stored over budget", async t => {
	const d = disk( t );
	quotaNavigator( t, 32 );
	const { createPersistentAssets } = await load( "src/engine/runtime/assets/worker/packs/persistent.ts" );
	const origin = "http://localhost", bytes = Uint8Array.of( 1, 2, 3 );
	const store = createPersistentAssets( quota => quota / 4 );
	store.setStartup( origin, [ "ui-a", "ui-b" ] );
	for ( const digest of [ "ui-a", "ui-b", "world" ] ) await store.write( origin, digest, bytes );
	const stored = [ ...d.rows.values() ].reduce(
		( sum, response ) => sum + Number( response.headers.get( "content-length" ) ),
		0
	);
	assert.ok( stored <= 8, `stored ${stored} bytes against an 8-byte budget` );
	assert.equal( await store.read( origin, "world", 3 ), null );
	assert.ok( store.stats().skipped >= 1 );
});

test("a warm entry the manifest names becomes protected; one only an older release named does not", async t => {
	disk( t );
	quotaNavigator( t, 32 );
	const { createPersistentAssets } = await load( "src/engine/runtime/assets/worker/packs/persistent.ts" );
	const origin = "http://localhost", bytes = Uint8Array.of( 1, 2, 3 );
	const store = createPersistentAssets( quota => quota / 4 );
	store.setStartup( origin, [ "old-ui" ] );
	await store.write( origin, "old-ui", bytes );
	await store.write( origin, "now-ui", bytes );
	// The next release's manifest: now-ui is a startup entry, old-ui is not.
	store.setStartup( origin, [ "now-ui" ] );
	await store.write( origin, "world", bytes );
	assert.ok( await store.read( origin, "now-ui", 3 ), "the warm entry is protected without a rewrite" );
	assert.equal( await store.read( origin, "old-ui", 3 ), null, "the obsolete startup entry was evicted" );
});

test("large packs download verified ranges and persist only demanded members across reload", async t => {
	const storage = disk( t );
	const f = fixture();
	const padded = new Uint8Array( 16 << 20 );
	padded.set( f.bytes );
	f.bytes = padded;
	f.pack.bytes = padded.length;
	f.pack.sha256 = sha( padded );
	const requests = [];
	for ( let pass = 0; pass < 4; pass++ ) {
		if ( pass === 2 ) {
			const key = [ ...storage.rows.keys() ][0];
			storage.rows.set( key, new Response( Uint8Array.of( 9, 9, 9 ), { headers: { "content-length": "3" } } ) );
		}
		const packs = createPacks( async ( url, limit, signal, range ) => {
			if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
			assert.ok( range );
			requests.push( range );
			return f.bytes.slice( range.start, range.end + 1 );
		} );
		assert.deepEqual( [
			...await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal )
		], [ 1, 2, 3 ] );
		packs.dispose();
	}
	assert.equal(
		requests.length,
		6,
		"cold and corruption repair fetch three ranges each; both warm reloads avoid all payload requests"
	);
	assert.ok( requests.reduce( ( n, r ) => n + r.end - r.start + 1, 0 ) <= f.bytes.length / 4 );
});

test("partial delivery rejects corrupt headers and members before persistent publication", async t => {
	const d = disk( t );
	for ( const corruption of [ "header", "member" ] ) {
		const f = fixture(), padded = new Uint8Array( 5 << 20 );
		padded.set( f.bytes );
		f.pack.bytes = padded.length;
		f.pack.sha256 = sha( padded );
		const packs = createPacks( async ( url, limit, signal, range ) => {
			if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
			const bytes = padded.slice( range.start, range.end + 1 );
			if ( corruption === "header" ) bytes[0] = 0;
			if ( corruption === "member" && range.start > 0 ) bytes[0] ^= 1;
			return bytes;
		} );
		await assert.rejects(
			packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal ),
			/identity|SHA-256/
		);
		assert.equal( d.rows.size, 0 );
		packs.dispose();
	}
});

test("manifest-driven batching serves hundreds of new filenames without per-asset HTTP reads", async () => {
	const count = 400, size = 4096, data = new Uint8Array( count * size );
	for ( let i = 0; i < data.length; i++ ) data[i] = i % 251;
	const entries = Array.from(
		{ length: count },
		( _, i ) => ({
			path: "/assets/generated/member-" + i + ".png",
			offset: i * size,
			length: size,
			mime: "image/png",
			sha256: sha( data.subarray( i * size, (i + 1) * size ) )
		})
	);
	const header = Buffer.from( JSON.stringify( { format: "sro-asset-pack", version: 2, files: entries } ) );
	const bytes = new Uint8Array( 8 << 20 );
	bytes.set( Buffer.from( "SROPACK2" ) );
	new DataView( bytes.buffer ).setUint32( 8, header.length, true );
	bytes.set( header, 12 );
	bytes.set( data, 12 + header.length );
	const pack = { path: "/assets/packs/generated.bin", bytes: bytes.length, sha256: sha( bytes ), assetCount: count };
	const manifest = {
		version: 2,
		groups: [ { name: "generated", assetCount: count, packs: [ pack ] } ],
		assets: entries.toReversed().map( e => ({ ...e, packPath: pack.path }) )
	};
	const requests = [];
	const packs = createPacks( async ( url, limit, signal, range ) => {
		if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( manifest ) );
		assert.ok( range );
		requests.push( range );
		return bytes.slice( range.start, range.end + 1 );
	} );
	const signal = new AbortController().signal;
	for ( let i = 0; i < count; i += 4 ) {
		await Promise.all(
			entries.slice( i, i + 4 ).map( async e =>
				assert.deepEqual(
					await packs.read( new URL( "http://localhost" + e.path ), size, signal ),
					data.subarray( e.offset, e.offset + e.length )
				)
			)
		);
	}
	assert.ok( requests.length < 10, "hundreds of members must share a few manifest-derived reads" );
	assert.ok(
		requests.reduce( ( n, r ) => n + r.end - r.start + 1, 0 ) < data.length + header.length * 2 + 24,
		"batching must not download unrelated pack padding"
	);
	packs.dispose();
});

test("cancelled demand does not cancel a shared block or publish its member", async t => {
	const d = disk( t ), f = fixture(), padded = new Uint8Array( 8 << 20 );
	padded.set( f.bytes );
	f.pack.bytes = padded.length;
	f.pack.sha256 = sha( padded );
	const first = new AbortController(), second = new AbortController();
	let release, started;
	const ready = new Promise( resolve => started = resolve );
	const packs = createPacks( async ( url, limit, signal, range ) => {
		if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
		if ( range.start > 0 ) {
			started();
			await new Promise( resolve => release = resolve );
			assert.equal( signal.aborted, false );
		}
		return padded.slice( range.start, range.end + 1 );
	} );
	const a = packs.read( new URL( "http://localhost/assets/a.bin" ), 100, first.signal ),
		b = packs.read( new URL( "http://localhost/assets/b.json" ), 100, second.signal );
	await ready;
	first.abort();
	defined( release )();
	const result = await Promise.allSettled( [ a, b ] );
	assert.equal( result[0].status, "rejected" );
	assert.equal( result[1].status, "fulfilled" );
	assert.equal( d.rows.size, 1 );
	packs.dispose();
});

test("shared blocks preserve randomized member order, duplicate demand and failed-download retry", async () => {
	const fc = await import( "fast-check" ),
		{ createPackBlocks } = await load( "src/engine/runtime/assets/worker/packs/blocks.ts" );
	await fc.assert(
		fc.asyncProperty(
			fc.array( fc.integer( { min: 0, max: 31 } ), { minLength: 1, maxLength: 60 } ),
			async order => {
				const entries = Array.from(
					{ length: 32 },
					( _, i ) => ({ path: "/assets/random-" + i, offset: i * 64, length: 64, span: 64 })
				);
				const bytes = new Uint8Array( 12 + 32 * 64 );
				for ( let i = 0; i < bytes.length; i++ ) bytes[i] = i % 251;
				const pack = { path: "/assets/packs/random.bin", sha256: "identity", bytes: bytes.length, entries };
				let calls = 0;
				const blocks = createPackBlocks( async ( url, limit, signal, range ) => {
					if ( ++calls === 1 ) throw Error( "temporary" );
					return bytes.slice( range.start, range.end + 1 );
				}, new AbortController().signal );
				await assert.rejects( blocks.read( "http://localhost", pack, 12, entries[order[0]] ), /temporary/ );
				for ( let i = 0; i < order.length; i += 4 ) {
					const selected = order.slice( i, i + 4 );
					const results = await Promise.all(
						selected.map( id => blocks.read( "http://localhost", pack, 12, entries[id] ) )
					);
					for ( let j = 0; j < results.length; j++ ) {
						assert.deepEqual(
							results[j],
							bytes.slice( 12 + selected[j] * 64, 12 + (selected[j] + 1) * 64 )
						);
					}
				}
				assert.equal( calls, 2, "one failed request followed by one shared successful retry" );
				blocks.dispose();
				await assert.rejects( blocks.read( "http://localhost", pack, 12, entries[0] ), /disposed/ );
			}
		),
		{ numRuns: 30, seed: 20260910 }
	);
});

test("slow persistence cannot delay admission or observe a transferred caller buffer", async t => {
	const d = disk( t ), f = fixture(), original = d.cache.put;
	let release, entered;
	const started = new Promise( resolve => entered = resolve ), gate = new Promise( resolve => release = resolve );
	d.cache.put = async ( ...args ) => {
		entered();
		await gate;
		return original( ...args );
	};
	const packs = createPacks( async url =>
		url.endsWith( "manifest.json" ) ? new TextEncoder().encode( JSON.stringify( f.manifest ) ) : f.bytes
	);
	const result = await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal );
	await started;
	assert.deepEqual( [ ...result ], [ 1, 2, 3 ] );
	structuredClone( result.buffer, { transfer: [ result.buffer ] } );
	defined( release )();
	await packs.flush();
	assert.equal( packs.stats().writes, 1 );
	assert.equal( packs.stats().queuedBytes, 0 );
	packs.dispose();
});

test("bounded publication drops optional writes under pressure and refreshes LRU on reads", async t => {
	const d = disk( t ), old = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	let estimates = 0;
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: {
			storage: {
				estimate: async () => {
					estimates++;
					return { quota: 32 };
				}
			}
		}
	} );
	t.after( () => {
		if ( old ) Object.defineProperty( globalThis, "navigator", old );
		else delete globalThis.navigator;
	} );
	const { createPersistentAssets } = await load( "src/engine/runtime/assets/worker/packs/persistent.ts" ),
		owner = createPersistentAssets( quota => quota / 4 );
	for ( const key of [ "a", "b" ] ) await owner.write( "http://localhost", key, Uint8Array.of( 1, 2, 3 ) );
	await owner.read( "http://localhost", "a", 3 );
	await owner.write( "http://localhost", "c", Uint8Array.of( 1, 2, 3 ) );
	assert.equal( await owner.read( "http://localhost", "b", 3 ), null );
	assert.ok( await owner.read( "http://localhost", "a", 3 ) );
	assert.equal( estimates, 1 );
	for ( let i = 0; i < 70; i++ ) owner.enqueue( "http://localhost", "queued-" + i, Uint8Array.of( 1 ) );
	assert.ok( owner.stats().skipped >= 6 );
	await owner.flush();
	assert.equal( owner.stats().queuedBytes, 0 );
});

test("animation lookup derives candidates from every manifest and distrusts stale digest annotations", async () => {
	const f = fixture( "/assets/world/future-region/animated-objects.json.gz" );
	f.manifest.assets[0].animationSources = [ "native/new-feature.bsr" ];
	f.manifest.assets[0].animationDigest = f.manifest.assets[0].sha256;
	const packs = createPacks( async url => new TextEncoder().encode( JSON.stringify( f.manifest ) ) );
	assert.deepEqual(
		await packs.worldAnimationManifests( "http://localhost", new Set( [ "native/new-feature.bsr" ] ) ),
		[ "/assets/world/future-region/animated-objects.json" ]
	);
	assert.deepEqual(
		await packs.worldAnimationManifests( "http://localhost", new Set( [ "native/absent.bsr" ] ) ),
		[]
	);
	packs.dispose();
	f.manifest.assets[0].animationDigest = "0".repeat( 64 );
	const stale = createPacks( async () => new TextEncoder().encode( JSON.stringify( f.manifest ) ) );
	assert.equal(
		(await stale.worldAnimationManifests( "http://localhost", new Set( [ "new-unindexed-feature" ] ) )).length,
		1
	);
	stale.dispose();
});

test("eviction by the browser invalidates the owner inventory and permits republishing", async t => {
	const d = disk( t ),
		{ createPersistentAssets } = await load( "src/engine/runtime/assets/worker/packs/persistent.ts" ),
		owner = createPersistentAssets();
	await owner.write( "http://localhost", "asset", Uint8Array.of( 1, 2, 3 ) );
	d.rows.clear();
	assert.equal( await owner.read( "http://localhost", "asset", 3 ), null );
	owner.enqueue( "http://localhost", "asset", Uint8Array.of( 1, 2, 3 ) );
	await owner.flush();
	assert.deepEqual( [ ...await owner.read( "http://localhost", "asset", 3 ) ], [ 1, 2, 3 ] );
});

test("a verified range member is hashed once per admission including a warm persistent hit", async t => {
	disk( t );
	const f = fixture(), padded = new Uint8Array( 5 << 20 );
	padded.set( f.bytes );
	f.pack.bytes = padded.length;
	f.pack.sha256 = sha( padded );
	const packs = createPacks( async ( url, limit, signal, range ) =>
		url.endsWith( "manifest.json" ) ?
			new TextEncoder().encode( JSON.stringify( f.manifest ) ) :
			padded.slice( range.start, range.end + 1 )
	);
	await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal );
	await packs.flush();
	assert.equal( packs.stats().hashCalls, 1 );
	assert.equal( packs.stats().hashBytes, 3 );
	await packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal );
	assert.equal( packs.stats().hashCalls, 2 );
	assert.equal( packs.stats().hashBytes, 6 );
	packs.dispose();
});

test("indexed animation selection preserves the original catalog override order", async () => {
	const entries = [ "a", "b" ].map( name => ({
		path: `/assets/world/${name}/animated-objects.json`,
		offset: name === "a" ? 0 : 3,
		length: 3,
		mime: "application/json",
		sha256: "0".repeat( 64 ),
		packPath: "/assets/packs/fixture.bin",
		animationSources: name === "a" ? [ "shared" ] : [ "shared", "only-b" ],
		animationDigest: "0".repeat( 64 )
	}) );
	const manifest = {
		version: 2,
		groups: [ {
			name: "test",
			assetCount: 2,
			packs: [ { path: "/assets/packs/fixture.bin", bytes: 1024, sha256: "0".repeat( 64 ), assetCount: 2 } ]
		} ],
		assets: entries
	};
	const packs = createPacks( async () => new TextEncoder().encode( JSON.stringify( manifest ) ) );
	assert.deepEqual(
		await packs.worldAnimationManifests( "http://localhost", new Set( [ "only-b", "shared" ] ) ),
		entries.map( e => e.path )
	);
	packs.dispose();
});

test("browser eviction between a warm read and lazy inventory initialization cannot strand publication", async t => {
	const d = disk( t ),
		{ createPersistentAssets } = await load( "src/engine/runtime/assets/worker/packs/persistent.ts" );
	const seed = createPersistentAssets();
	await seed.write( "http://localhost", "old", Uint8Array.of( 1 ) );
	const owner = createPersistentAssets();
	assert.ok( await owner.read( "http://localhost", "old", 1 ) );
	d.rows.clear();
	await owner.write( "http://localhost", "new", Uint8Array.of( 2 ) );
	assert.deepEqual( [ ...await owner.read( "http://localhost", "new", 1 ) ], [ 2 ] );
});

/*
================
storedPack

An SROPACK2 pack with one gzip-stored member, padded to total bytes. The
stored bytes come from the caller so a test can corrupt or inflate them.
================
*/
function storedPack( member, stored, total = 0 ) {
	const entry = {
		path: "/assets/stored.bin",
		offset: 0,
		length: member.length,
		mime: "application/octet-stream",
		sha256: sha( member ),
		stored: { length: stored.length, encoding: "gzip" }
	};
	const header = Buffer.from( JSON.stringify( { format: "sro-asset-pack", version: 2, files: [ entry ] } ) );
	const prefix = Buffer.alloc( 12 );
	prefix.write( "SROPACK2" );
	prefix.writeUInt32LE( header.length, 8 );
	const unpadded = Buffer.concat( [ prefix, header, stored ] );
	const bytes = new Uint8Array(
		Buffer.concat( [ unpadded, Buffer.alloc( Math.max( 0, total - unpadded.length ) ) ] )
	);
	const pack = {
		path: "/assets/packs/stored-001-000000000000.bin",
		bytes: bytes.length,
		sha256: sha( bytes ),
		assetCount: 1
	};
	const manifest = {
		version: 2,
		groups: [ { name: "test", assetCount: 1, packs: [ pack ] } ],
		assets: [ { ...entry, packPath: pack.path } ]
	};
	// Whole packs answer whole; a range request gets exactly its slice.
	const download = async ( url, limit, signal, range ) =>
		url.endsWith( "manifest.json" ) ?
			new TextEncoder().encode( JSON.stringify( manifest ) ) :
			range ?
			bytes.slice( range.start, range.end + 1 ) :
			bytes;
	return { download };
}

const STORED_MEMBER = new Uint8Array( Buffer.from( "a stored pack member ".repeat( 200 ) ) );

for ( const [label, total] of [ [ "whole small pack", 0 ], [ "range-read large pack", 5 << 20 ] ] ) {
	test(`a gzip-stored member decodes from a ${label}`, async () => {
		const { download } = storedPack( STORED_MEMBER, gzipSync( STORED_MEMBER ), total );
		const packs = createPacks( download );
		const bytes = await packs.read(
			new URL( "http://localhost/assets/stored.bin" ),
			1 << 20,
			new AbortController().signal
		);
		assert.deepEqual( bytes, STORED_MEMBER );
		assert.ok( packs.stats().storedBytes < packs.stats().decodedBytes, "the member travelled compressed" );
		packs.dispose();
	});
}

test("corrupted stored bytes are refused, never returned", async () => {
	const stored = gzipSync( STORED_MEMBER );
	// Flip a byte inside the deflate data: gzip itself or the SHA-256 must catch it.
	stored[stored.length >> 1] ^= 0xff;
	const { download } = storedPack( STORED_MEMBER, stored, 5 << 20 );
	const packs = createPacks( download );
	await assert.rejects(
		packs.read( new URL( "http://localhost/assets/stored.bin" ), 1 << 20, new AbortController().signal )
	);
	packs.dispose();
});

test("a stored member that inflates past its declared length is refused", async () => {
	// The row declares the member's length; the stream decodes to far more.
	const bomb = gzipSync( Buffer.alloc( 1 << 20 ) );
	const declared = new Uint8Array( Buffer.alloc( bomb.length + 64 ) );
	const { download } = storedPack( declared, bomb, 5 << 20 );
	const packs = createPacks( download );
	await assert.rejects(
		packs.read( new URL( "http://localhost/assets/stored.bin" ), 1 << 22, new AbortController().signal ),
		/exceeds byte limit|wrong length|SHA-256/
	);
	packs.dispose();
});
