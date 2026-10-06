import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { publishedAssets, assetEncoding } from "../../tools/published-assets.mjs";
import { defined } from "../helpers/defined.mjs";
test("published middleware leaves production JS and worker chunks to Vite", () => {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "next-ui-route-" ) );
	try {
		fs.mkdirSync( path.join( root, "dist/assets" ), { recursive: true } );
		fs.writeFileSync( path.join( root, "dist/assets/entry-test.js" ), "export {};" );
		let middleware;
		publishedAssets().configurePreviewServer( {
			config: { root, build: { outDir: "dist" } },
			middlewares: {
				use( fn ) {
					middleware = fn;
				}
			}
		} );
		let delegated = 0;
		defined( middleware )( { url: "/assets/entry-test.js" }, {}, () => delegated++ );
		assert.equal( delegated, 1 );
	} finally {
		fs.rmSync( root, { recursive: true, force: true } );
	}
});

test("HTTP asset delivery revalidates, compresses JSON and bounds partial responses", async t => {
	const { createServer } = await import( "node:http" );
	const { request } = await import( "node:http" );
	const { gunzipSync } = await import( "node:zlib" );
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "next-http-cache-" ) );
	t.after( () => fs.rmSync( root, { recursive: true, force: true } ) );
	fs.mkdirSync( path.join( root, "assets" ), { recursive: true } );
	const body = JSON.stringify( { text: "silkroad".repeat( 10000 ) } );
	fs.writeFileSync( path.join( root, "assets/manifest.json" ), body );
	fs.writeFileSync( path.join( root, "assets/pack.bin" ), "0123456789" );
	let middleware;
	publishedAssets( root ).configureServer( {
		config: { root, build: { outDir: "dist" } },
		middlewares: {
			use( fn ) {
				middleware = fn;
			}
		}
	} );
	const server = createServer( ( req, res ) =>
		middleware( req, res, () => {
			res.statusCode = 404;
			res.end();
		} )
	);
	await new Promise( resolve => server.listen( 0, "127.0.0.1", resolve ) );
	t.after( () => new Promise( resolve => server.close( resolve ) ) );
	const get = ( url, headers = {}, method = "GET" ) =>
		new Promise( ( resolve, reject ) => {
			const req = request( {
				hostname: "127.0.0.1",
				port: defined( server.address() ).port,
				path: url,
				headers,
				method
			}, res => {
				const parts = [];
				res.on( "data", p => parts.push( p ) );
				res.on( "end", () =>
					resolve( { status: res.statusCode, headers: res.headers, body: Buffer.concat( parts ) } ) );
			} );
			req.on( "error", reject );
			req.end();
		} );
	const { zstdCompressSync } = await import( "node:zlib" ), { createHash } = await import( "node:crypto" );
	const pack = Buffer.concat( [ Buffer.from( "SROPACK1" ), Buffer.alloc( 10000, 7 ) ] ),
		name = "/assets/compact-" + createHash( "sha256" ).update( pack ).digest( "hex" ).slice( 0, 12 ) + ".bin";
	fs.writeFileSync( path.join( root, name.slice( 1 ) + ".zst" ), zstdCompressSync( pack ) );
	const compact = await get( name, { range: "bytes=100-199" } );
	assert.equal( compact.status, 206 );
	assert.deepEqual( compact.body, pack.subarray( 100, 200 ) );
	assert.equal( compact.headers["content-range"], "bytes 100-199/" + pack.length );
	const compactHead = await get( name, {}, "HEAD" );
	assert.equal( compactHead.headers["content-length"], String( pack.length ) );
	fs.writeFileSync( path.join( root, "assets/bad-000000000000.bin.zst" ), zstdCompressSync( pack ) );
	assert.equal( (await get( "/assets/bad-000000000000.bin" )).status, 500 );
	const first = await get( "/assets/manifest.json", { "accept-encoding": "gzip" } );
	assert.equal( first.headers["content-encoding"], "gzip" );
	assert.equal( gunzipSync( first.body ).toString(), body );
	assert.ok( first.body.length < body.length / 100 );
	assert.equal( first.headers["cache-control"], "no-cache" );
	const warm = await get( "/assets/manifest.json", { "if-none-match": first.headers.etag } );
	assert.equal( warm.status, 304 );
	assert.equal( warm.body.length, 0 );
	fs.writeFileSync( path.join( root, "assets/manifest.json" ), '{"changed":true}' );
	const changed = await get( "/assets/manifest.json", { "if-none-match": first.headers.etag } );
	assert.equal( changed.status, 200 );
	assert.notEqual( changed.headers.etag, first.headers.etag );
	const range = await get( "/assets/pack.bin", { range: "bytes=2-5" } );
	assert.equal( range.status, 206 );
	assert.equal( range.headers["content-range"], "bytes 2-5/10" );
	assert.equal( range.body.toString(), "2345" );
	assert.equal( (await get( "/assets/pack.bin", { range: "bytes=20-30" } )).status, 416 );
	const head = await get( "/assets/pack.bin", {}, "HEAD" );
	assert.equal( head.body.length, 0 );
	assert.equal( head.headers["content-length"], "10" );
	assert.equal( (await get( "/assets/pack.bin", {}, "POST" )).status, 405 );
	assert.equal(
		(await get( "/assets/manifest.json", { "accept-encoding": "gzip;q=0" } )).headers["content-encoding"],
		undefined
	);
});

test("encoding negotiation honors exact tokens, quality, explicit refusal and wildcard", () => {
	for (
		const [header, expected] of [
			[ undefined, "identity" ],
			[ "", "identity" ],
			[ "gzip", "gzip" ],
			[ "GZIP; q=1.000", "gzip" ],
			[ "xgzip", "identity" ],
			[ "gzip;q=0", "identity" ],
			[ "gzip;q=0.000, *;q=1", "identity" ],
			[ "*", "gzip" ],
			[ "gzip;q=0.5, identity;q=0.1", "gzip" ],
			[ "gzip;q=0.1, identity;q=0.5", "identity" ],
			[ "identity;q=0,gzip", "gzip" ],
			[ "*;q=0", null ],
			[ "gzip;q=0,identity;q=0", null ],
			[ "gzip;q=invalid", "identity" ]
		]
	) assert.equal( assetEncoding( header, true ), expected, String( header ) );
	assert.equal( assetEncoding( "gzip", false ), "identity" );
	assert.equal( assetEncoding( "identity;q=0,gzip", false ), null );
});
for ( const hook of [ "configureServer", "configurePreviewServer" ] ) {
	test( hook + " delivers byte-identical compressed GLBs and identity ranges", async t => {
		const { createServer, request } = await import( "node:http" ),
			{ gunzipSync } = await import( "node:zlib" ),
			{ createHash } = await import( "node:crypto" );
		const root = fs.mkdtempSync( path.join( os.tmpdir(), "next-glb-http-" ) );
		t.after( () => fs.rmSync( root, { recursive: true, force: true } ) );
		fs.mkdirSync( path.join( root, "assets" ) );
		const original = fs.readFileSync( CLIENT_PUBLIC_ROOT + "/assets/char/china/chinaman_monk.glb" ),
			target = path.join( root, "assets/model.glb" );
		fs.writeFileSync( target, original );
		fs.writeFileSync( path.join( root, "assets/image.png" ), Buffer.from( [ 1, 2, 3 ] ) );
		let middleware;
		const server = createServer( ( req, res ) => middleware( req, res, () => res.end() ) );
		publishedAssets( root )[hook]( {
			httpServer: server,
			config: { root, build: { outDir: "dist" } },
			middlewares: {
				use( fn ) {
					middleware = fn;
				}
			}
		} );
		await new Promise( resolve => server.listen( 0, "127.0.0.1", resolve ) );
		t.after( () => new Promise( resolve => server.close( resolve ) ) );
		const get = ( headers = {}, method = "GET", url = "/assets/model.glb" ) =>
			new Promise( ( resolve, reject ) => {
				const req = request( {
					hostname: "127.0.0.1",
					port: defined( server.address() ).port,
					path: url,
					headers,
					method
				}, res => {
					const chunks = [];
					res.on( "data", b => chunks.push( b ) );
					res.on( "end", () =>
						resolve( { status: res.statusCode, headers: res.headers, body: Buffer.concat( chunks ) } ) );
				} );
				req.on( "error", reject );
				req.end();
			} );
		const compressed = await get( { "accept-encoding": "gzip" } );
		assert.equal( compressed.headers["content-encoding"], "gzip" );
		assert.equal( compressed.headers.vary, "Accept-Encoding" );
		assert.ok( compressed.body.length < original.length * .6 );
		assert.deepEqual( gunzipSync( compressed.body ), original );
		assert.equal(
			createHash( "sha256" ).update( gunzipSync( compressed.body ) ).digest( "hex" ),
			createHash( "sha256" ).update( original ).digest( "hex" )
		);
		const identity = await get( { "accept-encoding": "gzip;q=0" } );
		assert.deepEqual( identity.body, original );
		assert.equal( identity.headers["content-encoding"], undefined );
		assert.equal( identity.headers["content-length"], String( original.length ) );
		const head = await get( { "accept-encoding": "gzip" }, "HEAD" );
		assert.equal( head.body.length, 0 );
		assert.equal( head.headers["content-encoding"], "gzip" );
		assert.equal( head.headers["content-length"], undefined );
		assert.equal( head.headers.etag, compressed.headers.etag );
		const range = await get( { "accept-encoding": "gzip", range: "bytes=8-63" } );
		assert.equal( range.status, 206 );
		assert.equal( range.headers["content-encoding"], undefined );
		assert.deepEqual( range.body, original.subarray( 8, 64 ) );
		assert.equal( range.headers["content-range"], "bytes 8-63/" + original.length );
		const warm = await get( { "accept-encoding": "gzip", "if-none-match": compressed.headers.etag } );
		assert.equal( warm.status, 304 );
		assert.equal( warm.body.length, 0 );
		assert.equal( warm.headers.vary, "Accept-Encoding" );
		assert.equal( (await get( { "accept-encoding": "gzip;q=0,identity;q=0" } )).status, 406 );
		assert.equal(
			(await get( { "accept-encoding": "gzip" }, "GET", "/assets/image.png" )).headers["content-encoding"],
			undefined
		);
		const changed = Buffer.from( original );
		changed[changed.length - 1] ^= 1;
		fs.writeFileSync( target, changed );
		const refreshed = await get( { "accept-encoding": "gzip", "if-none-match": compressed.headers.etag } );
		assert.equal( refreshed.status, 200 );
		assert.deepEqual( gunzipSync( refreshed.body ), changed );
		assert.notEqual( refreshed.headers.etag, compressed.headers.etag );
		const parallel = await Promise.all( Array.from( { length: 4 }, () => get( { "accept-encoding": "gzip" } ) ) );
		for ( const r of parallel ) {
			assert.deepEqual( gunzipSync( r.body ), changed );
		}
	} );
}

test("generated precompression serves full GET and HEAD without runtime encoding and preserves ranges", async t => {
	const { createServer, request } = await import( "node:http" ),
		{ createHash } = await import( "node:crypto" ),
		{ gzipSync, gunzipSync } = await import( "node:zlib" );
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "next-precompressed-" ) );
	t.after( () => fs.rmSync( root, { recursive: true, force: true } ) );
	fs.mkdirSync( path.join( root, "assets/packs/transport" ), { recursive: true } );
	const original = Buffer.alloc( 256 << 10, 13 ),
		encoded = gzipSync( original ),
		digest = createHash( "sha256" ).update( encoded ).digest( "hex" ),
		filename = path.join( root, "assets/model.glb" );
	fs.writeFileSync( filename, original );
	const transport = {
			path: "/assets/packs/transport/" + digest + ".gz",
			sha256: digest,
			length: encoded.length,
			encoding: "gzip"
		},
		source = fs.statSync( filename );
	fs.writeFileSync( path.join( root, transport.path ), encoded );
	fs.writeFileSync(
		path.join( root, "assets/packs/delivery.json" ),
		JSON.stringify( {
			version: 1,
			assets: [ {
				path: "/assets/model.glb",
				sourceSha256: createHash( "sha256" ).update( original ).digest( "hex" ),
				sourceStat: { size: source.size, mtimeMs: source.mtimeMs, ctimeMs: source.ctimeMs },
				transport
			} ]
		} )
	);
	let middleware;
	const server = createServer( ( req, res ) => middleware( req, res, () => res.end() ) );
	publishedAssets( root ).configureServer( {
		httpServer: server,
		config: { root, build: { outDir: "dist" } },
		middlewares: {
			use( fn ) {
				middleware = fn;
			}
		}
	} );
	await new Promise( resolve => server.listen( 0, "127.0.0.1", resolve ) );
	t.after( () => new Promise( resolve => server.close( resolve ) ) );
	const get = ( headers = {}, method = "GET", url = "/assets/model.glb" ) =>
		new Promise( ( resolve, reject ) => {
			const req = request( {
				hostname: "127.0.0.1",
				port: defined( server.address() ).port,
				path: url,
				headers,
				method
			}, res => {
				const chunks = [];
				res.on( "data", b => chunks.push( b ) );
				res.on( "end", () =>
					resolve( { status: res.statusCode, headers: res.headers, body: Buffer.concat( chunks ) } ) );
			} );
			req.on( "error", reject );
			req.end();
		} );
	const first = await get( { "accept-encoding": "gzip" } );
	assert.deepEqual( first.body, encoded );
	assert.equal( Number( first.headers["content-length"] ), encoded.length );
	const head = await get( { "accept-encoding": "gzip" }, "HEAD" );
	assert.equal( Number( head.headers["content-length"] ), encoded.length );
	assert.equal( head.body.length, 0 );
	const range = await get( { range: "bytes=4-10", "accept-encoding": "gzip" } );
	assert.equal( range.status, 206 );
	assert.deepEqual( range.body, original.subarray( 4, 11 ) );
	const direct = await get( {}, "GET", transport.path );
	assert.match( direct.headers["cache-control"], /immutable/ );
	assert.equal( direct.headers["content-encoding"], undefined );
	assert.deepEqual( direct.body, encoded );
	fs.writeFileSync( filename, Buffer.alloc( original.length, 14 ) );
	const changed = await get( { "accept-encoding": "gzip" } );
	assert.deepEqual( gunzipSync( changed.body ), Buffer.alloc( original.length, 14 ) );
	assert.equal(
		changed.headers["content-length"],
		undefined,
		"stale generated gzip must fall back to current streamed bytes"
	);
});
