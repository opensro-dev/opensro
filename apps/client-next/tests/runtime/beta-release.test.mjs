/*
===========================================================================

beta-release.test.mjs - package integrity and public serving contracts

Exercises real archives, route admission, pack completeness and the beta
compiler with synthetic assets; no licensed tree or live host is changed.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { gzipSync } from "node:zlib";
import { inspect, safeName, sha, verifyDirectory, publicIndex, releaseIdentity } from "../../tools/beta/policy.mjs";
import { projectMember } from "../../tools/beta/public-data.mjs";
import { archiveRelease, verifyArchive } from "../../tools/beta/archive.mjs";
import { serveBeta } from "../../tools/beta/serve.mjs";
import { verifyServed } from "../../tools/beta/verify.mjs";
import { defined } from "../helpers/defined.mjs";
import { runtimeTextPack } from "../helpers/runtime-text-pack.mjs";
import { REQUIRED_RUNTIME_TEXT_ASSETS } from "../../../../scripts/build/assetPackOwnership.mjs";
import { validateAssetPackIndex } from "../../../../scripts/build/assetPackIndexValidation.mjs";

test("beta gates reject exposed source and opaque compressed leaks", () => {
	for (
		const [name, text] of [
			[ "src/a.ts", "x" ],
			[ "assets/a.js.map", "{}" ],
			[ ".env", "SECRET=x" ],
			[ "private/source.json", "{}" ],
			[ "assets/a.js", "//# sourceMappingURL=data:application/json;base64,AAA" ],
			[ "assets/a.js", "const x={sourcesContent:[]};" ],
			[ "assets/a.js", '"/@fs/H:/source"' ],
			[ "assets/a.js", "globalThis.__worldProbeThing=1" ],
			[ "assets/a.json", '{"p":"H:\\\\workspace\\\\src"}' ],
			[ "assets/a.txt", "-----BEGIN PRIVATE KEY-----" ],
			[ "tests/probe.js", "x" ]
		]
	) assert.throws( () => inspect( name, Buffer.from( text ), { application: true } ), name );
	for ( const name of [ "payload/a.json.gz", "payload/digest.gz" ] ) {
		assert.throws( () => inspect( name, gzipSync( Buffer.from( '{"sourcesContent":["secret"]}' ) ) ) );
	}
	for ( const name of [ "../x", "/x", "a\\x", "a/../b", "C:/x", "a%2fb", "a//b" ] ) {
		assert.throws( () => safeName( name ) );
	}
	assert.doesNotThrow( () =>
		inspect( "assets/a.js", Buffer.from( "const q=x?a:/^\\d+$/.test(r);" ), { application: true } )
	);
});
test("group policy rejects unclassified additions and removes developer artifacts", () => {
	const index = {
		format: "sro-asset-pack-index",
		version: 2,
		groups: [ { name: "developer-labs" }, { name: "game-data" } ],
		assets: [ { group: "developer-labs" }, { group: "game-data" } ]
	};
	assert.equal( publicIndex( index ).assets.length, 1 );
	index.groups.push( { name: "new-group" } );
	assert.throws( () => publicIndex( index ) );
});
test("metadata projection preserves runtime fields without recursive key deletion", () => {
	const data = {
		format: "sro-world-object-resource-index",
		version: 1,
		reconstructionSources: [ "secret" ],
		missing: [ { imageSourcePath: "H:/private" } ],
		missingCount: 1,
		bsr: { sourcePath: "authored/path", missing: "runtime value" }
	};
	const got = JSON.parse( projectMember( "asset.json", Buffer.from( JSON.stringify( data ) ) ) );
	assert.deepEqual( got, { format: data.format, version: 1, bsr: data.bsr } );
	const other = Buffer.from( '{"format":"other","missing":"keep"}' );
	assert.equal( projectMember( "asset.json", other ), other );
});
/*
================
fixture
================
*/
async function fixture( t, paths = REQUIRED_RUNTIME_TEXT_ASSETS ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-beta-test-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	await mkdir( path.join( root, "package/application/assets" ), { recursive: true } );
	const pack = runtimeTextPack( paths );
	/** @type {Array<[string, string | Buffer, string]>} */
	const entries = [
		[ "application/index.html", '<script src="/assets/main-12345678.js"></script>', "application" ],
		[
			"application/assets/main-12345678.js",
			'document.title="ready";',
			"application"
		],
		[ "publication.json", JSON.stringify( pack.index ), "data" ],
		[ "runtime-text.bin", pack.bytes, "data" ]
	];
	const files = entries.map( ( [p, s, kind] ) => ({
		path: p,
		length: Buffer.byteLength( s ),
		sha256: sha( s ),
		kind
	}) );
	for ( const [p, s] of entries ) await writeFile( path.join( root, "package", p ), s );
	const routes = files.map( e => ({
		url: e.path === "publication.json" ?
			"/assets/packs/manifest.json" :
			e.path === "runtime-text.bin" ?
			pack.packPath :
			"/" + e.path.replace( "application/", "" ),
		file: e.path,
		length: e.length,
		offset: 0,
		mime: e.path.endsWith( ".js" ) ? "text/javascript" : "text/html"
	}) );
	const m = { format: "sro-beta-release-v1", sourceHash: sha( "fixture" ), files, routes };
	m.releaseId = releaseIdentity( m );
	await writeFile( path.join( root, "package/release.json" ), JSON.stringify( m ) );
	return { root, packageRoot: path.join( root, "package" ), m };
}

/*
================
TestMissingRuntimeTable
================
*/
test("hash-valid packages and archives reject each omitted raw runtime table", async t => {
	for ( const missing of REQUIRED_RUNTIME_TEXT_ASSETS ) {
		const paths = REQUIRED_RUNTIME_TEXT_ASSETS.filter( name => name !== missing );
		assert.doesNotThrow( () => validateAssetPackIndex( runtimeTextPack( paths ).index ) );
		const f = await fixture( t, paths );
		await assert.rejects(
			verifyDirectory( f.packageRoot ),
			error =>
				error instanceof Error && error.message.includes( "Missing required runtime asset" ) &&
				error.message.includes( missing )
		);
		await assert.rejects(
			archiveRelease( f.packageRoot, path.join( f.root, "incomplete.tar" ) ),
			/Missing required runtime asset/
		);
	}
});

/*
================
TestLooseRouteDoesNotSatisfyMembership
================
*/
test("a hash-valid loose route cannot replace the missing command pack member", async t => {
	const missing = "/assets/config/command.txt";
	const f = await fixture( t, REQUIRED_RUNTIME_TEXT_ASSETS.filter( name => name !== missing ) );
	const entry = defined( f.m.files.find( row => row.path === "runtime-text.bin" ) );
	f.m.routes.push( { url: missing, file: entry.path, offset: 0, length: entry.length, mime: "text/plain" } );
	f.m.releaseId = releaseIdentity( f.m );
	await writeFile( path.join( f.packageRoot, "release.json" ), JSON.stringify( f.m ) );
	await assert.rejects( verifyDirectory( f.packageRoot ), /Missing required runtime asset.*command\.txt/ );
});

/*
================
TestRequiredPackRoute
================
*/
test("a complete index cannot hide an unavailable required pack route", async t => {
	const f = await fixture( t );
	f.m.routes = f.m.routes.filter( row => row.file !== "runtime-text.bin" );
	f.m.releaseId = releaseIdentity( f.m );
	await writeFile( path.join( f.packageRoot, "release.json" ), JSON.stringify( f.m ) );
	await assert.rejects( verifyDirectory( f.packageRoot ), /Missing runtime pack route/ );
});
test("manifest and archive detect extra files, replacement payloads and corruption", async t => {
	const f = await fixture( t ), archive = path.join( f.root, "release.tar" );
	await archiveRelease( f.packageRoot, archive );
	await verifyArchive( archive, f.packageRoot );
	await writeFile( path.join( f.packageRoot, "unexpected.txt" ), "x" );
	await assert.rejects( () => verifyDirectory( f.packageRoot ) );
	await rm( path.join( f.packageRoot, "unexpected.txt" ) );
	const bytes = await readFile( archive );
	bytes[512] ^= 1;
	await writeFile( archive, bytes );
	await assert.rejects( () => verifyArchive( archive, f.packageRoot ) );
	await writeFile( path.join( f.packageRoot, "application/assets/main-12345678.js" ), "modified" );
	await assert.rejects( () => verifyDirectory( f.packageRoot ) );
});
test("serving adapter exposes only exact routes, retains caching and refuses source URLs", async t => {
	const f = await fixture( t ), server = await serveBeta( { root: f.packageRoot } );
	t.after( () => server.close() );
	await verifyServed( f.packageRoot, server.url );
	const r = await fetch( server.url + "/assets/main-12345678.js" );
	assert.equal( r.status, 200 );
	assert.match( r.headers.get( "cache-control" ), /immutable/ );
	await r.arrayBuffer();
	const cached = await fetch( server.url + "/assets/main-12345678.js", {
		headers: { "if-none-match": r.headers.get( "etag" ) }
	} );
	assert.equal( cached.status, 304 );
	const partial = await fetch( server.url + "/assets/main-12345678.js", { headers: { range: "bytes=0-7" } } );
	assert.equal( partial.status, 206 );
	assert.equal( await partial.text(), "document" );
	for (
		const name of [
			"/release.json",
			"/publication.json",
			"/application/index.html",
			"/private/source.json",
			"/api/development/anything"
		]
	) assert.equal( (await fetch( server.url + name )).status, 404 );
});

test("compiler keeps main and worker maps private, drops beta branches and excludes environment secrets", async t => {
	const { buildApplication } = await import( "../../tools/beta/build.mjs" );
	const { symbolicate } = await import( "../../tools/beta/symbolicate.mjs" );
	const base = await mkdtemp( path.join( os.tmpdir(), "sro-beta-compile-" ) );
	t.after( () => rm( base, { recursive: true, force: true } ) );
	const source = {
		"index.html": '<script type="module" src="/src/main.ts"></script>',
		"src/main.ts":
			`if(import.meta.env.MODE!=='beta')console.log('DEV_ONLY_MARKER');console.log(import.meta.env.VITE_BETA_SECRET);new Worker(new URL('./worker.ts',import.meta.url),{type:'module'});`,
		"src/worker.ts":
			`if(import.meta.env.MODE!=='beta')console.log('WORKER_DEV_ONLY_MARKER');postMessage('worker ready');`
	};
	for ( const [name, s] of Object.entries( source ) ) {
		await mkdir( path.dirname( path.join( base, name ) ), { recursive: true } );
		await writeFile( path.join( base, name ), s );
	}
	const previous = process.env.VITE_BETA_SECRET;
	process.env.VITE_BETA_SECRET = "NEVER_SHIP_THIS_SECRET";
	let maps;
	const directory = path.join( base, "release/package/application" );
	try {
		maps = await buildApplication( { base, directory, source } );
	} finally {
		if ( previous === undefined ) delete process.env.VITE_BETA_SECRET;
		else process.env.VITE_BETA_SECRET = previous;
	}
	const { files } = await import( "../../tools/beta/policy.mjs" );
	const names = await files( directory );
	assert.ok( names.some( n => /worker.*\.js$/.test( n ) ) );
	assert.ok( maps.size >= 2 );
	for ( const n of names ) {
		const s = await readFile( path.join( directory, n ), "utf8" );
		assert.ok( !n.endsWith( ".map" ) );
		assert.doesNotMatch( s, /DEV_ONLY_MARKER|NEVER_SHIP_THIS_SECRET|sourceMappingURL/ );
	}
	const privateRoot = path.join( base, "release/private" );
	await mkdir( path.join( privateRoot, "maps/assets" ), { recursive: true } );
	for ( const [n, b] of maps ) await writeFile( path.join( privateRoot, "maps", n ), b );
	await writeFile(
		path.join( privateRoot, "debug.json" ),
		JSON.stringify( {
			releaseId: "compile-test",
			maps: Object.fromEntries( [ ...maps ].map( ( [n, b] ) => [ n, sha( b ) ] ) )
		} )
	);
	const [name, b] = [ ...maps ].find( ( [n] ) => n.includes( "worker" ) );
	const map = JSON.parse( b );
	const line = map.mappings.split( ";" ).findIndex( s => s.length );
	assert.ok( line >= 0 );
	// Source lookup must belong to this compiler's private map, and wrong-release
	// lookup must fail before any symbol is returned.
	let location;
	for ( let column = 1; column < 500 && !location; column++ ) {
		location = await symbolicate( privateRoot, "compile-test", "/" + name.slice( 0, -4 ), line + 1, column );
	}
	assert.match( location.source, /worker\.ts$/ );
	await assert.rejects( () => symbolicate( privateRoot, "wrong", "/" + name.slice( 0, -4 ), 1, 1 ) );
	await writeFile( path.join( privateRoot, "maps", name ), "{}" );
	await assert.rejects( () => symbolicate( privateRoot, "compile-test", "/" + name.slice( 0, -4 ), 1, 1 ) );
});

test("API adapter fixes upstream origin, rejects development paths and never trusts forwarded headers", async t => {
	const http = await import( "node:http" );
	const seen = [];
	const upstream = http.createServer( ( req, res ) => {
		seen.push( { url: req.url, headers: req.headers } );
		res.end( "{}" );
	} );
	await new Promise( r => upstream.listen( 0, "127.0.0.1", r ) );
	t.after( () => new Promise( r => upstream.close( r ) ) );
	const f = await fixture( t ),
		server = await serveBeta( {
			root: f.packageRoot,
			apiTarget: "http://127.0.0.1:" + defined( upstream.address() ).port
		} );
	t.after( () => server.close() );
	await fetch( server.url + "/api/title/servers", {
		headers: { "x-forwarded-host": "evil.invalid", "x-forwarded-proto": "https" }
	} );
	assert.equal( seen.length, 1 );
	assert.equal( seen[0].url, "/title/servers" );
	assert.equal( seen[0].headers["x-forwarded-proto"], "http" );
	assert.notEqual( seen[0].headers["x-forwarded-host"], "evil.invalid" );
	for ( const p of [ "/api//evil.invalid", "/api/development/x", "/api/%64ebug/x" ] ) {
		assert.equal( (await fetch( server.url + p )).status, 404 );
	}
	assert.equal( seen.length, 1 );
});

test("offline HTTP variants preserve identity, negotiate exclusions and revalidate without bodies", async t => {
	const { compressRoutes } = await import( "../../tools/beta/compression.mjs" );
	const f = await fixture( t );
	await compressRoutes( f.packageRoot, f.m );
	f.m.releaseId = releaseIdentity( f.m );
	await writeFile( path.join( f.packageRoot, "release.json" ), JSON.stringify( f.m ) );
	const server = await serveBeta( { root: f.packageRoot } );
	t.after( () => server.close() );
	const url = server.url + "/assets/main-12345678.js";
	const gz = await fetch( url, { headers: { "accept-encoding": "gzip" } } );
	assert.equal( gz.headers.get( "content-encoding" ), "gzip" );
	assert.equal( await gz.text(), 'document.title="ready";' );
	const cached = await fetch( url, {
		headers: { "accept-encoding": "gzip", "if-none-match": gz.headers.get( "etag" ) }
	} );
	assert.equal( cached.status, 304 );
	assert.equal( (await cached.arrayBuffer()).byteLength, 0 );
	const identity = await fetch( url, { headers: { "accept-encoding": "identity" } } );
	assert.equal( identity.headers.get( "content-encoding" ), null );
	assert.equal( await identity.text(), 'document.title="ready";' );
	assert.equal( (await fetch( url, { headers: { "accept-encoding": "*;q=0" } } )).status, 406 );
	await verifyServed( f.packageRoot, server.url );
	const entry = f.m.files.find( e => e.path === f.m.routes[0].gzip.file );
	const corrupt = gzipSync( Buffer.from( "different identity" ) );
	await writeFile( path.join( f.packageRoot, defined( entry ).path ), corrupt );
	defined( entry ).sha256 = sha( corrupt );
	defined( entry ).length = corrupt.length;
	f.m.routes[0].gzip.length = corrupt.length;
	f.m.releaseId = releaseIdentity( f.m );
	await writeFile( path.join( f.packageRoot, "release.json" ), JSON.stringify( f.m ) );
	await assert.rejects( () => verifyDirectory( f.packageRoot ), /differs from identity/ );
});

/*
================
native asset URLs

Only files the browser loads itself get a URL of their own; a page or
stylesheet naming any other asset would 404 once members are pack-only.
================
*/
test("a release publishes a URL only for declared browser-loaded assets", async () => {
	const { documentAssetUrls, requireNativeAssets } = await import( "../../tools/beta/build.mjs" );
	const native = new Set( [ "/assets/cursors/a.cur" ] );
	const members = new Set( [ "/assets/cursors/a.cur", "/assets/images/b.png" ] );
	const application = new Map( [
		[ "index.html", '<link href="/assets/index-abc12345.css"><img src="/assets/cursors/a.cur">' ],
		[ "assets/index-abc12345.css", "" ],
		[ "assets/entry-abc12345.js", 'read("/assets/images/b.png")' ]
	] );
	assert.deepEqual( documentAssetUrls( "assets/entry-abc12345.js", 'x("/assets/images/b.png")' ), [] );
	requireNativeAssets( application, native, members );
	application.set( "assets/index-abc12345.css", '.bar{background:url("/assets/images/b.png")}' );
	assert.throws( () => requireNativeAssets( application, native, members ), /loads \/assets\/images\/b\.png/ );
	application.set( "assets/index-abc12345.css", "" );
	assert.throws(
		() => requireNativeAssets( application, native, new Set( [ "/assets/images/b.png" ] ) ),
		/not a published member/
	);
});
