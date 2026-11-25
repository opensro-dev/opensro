/*
===========================================================================

packs-fixture.mjs - one published pack, shared by the pack suites

A two-member SROPACK2 container (one raw member, one gzipped JSON
member) and the manifest that publishes it. The pack behaviour suite
and the recovery suite read the same verified bytes, so the fixture
has one definition.

===========================================================================
*/
import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";

/*
================
sha

The SHA-256 hex digest a pack or member is published and checked
under.
================
*/
const sha = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );

/*
================
fixture

One pack and its manifest: `/assets/a.bin` raw and `/assets/b.json.gz`
gzipped, both behind the SROPACK2 header the pack store verifies.
================
*/
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

/*
================
disk

A fake origin Cache Storage installed on globalThis for one test: every
pack reader the test creates shares it, as the windows of one site
share theirs. `deny` fails every open and put; `quota` fails the next
put for quota.
================
*/
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
		else Reflect.deleteProperty( globalThis, "caches" );
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

export { disk, fixture, sha };
