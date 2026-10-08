import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { publishedAssets } from "../../tools/published-assets.mjs";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test( "gzip-stored pack members survive cold decode, warm worker replacement and arbitrary member reads", {
	timeout: 120000
}, async () => {
	const index = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/packs/manifest.json", "utf8" ) );
	assert.ok( index.assets.some( e => e.stored ), "build the assets with SROPACK2 stored members first" );
	let middleware;
	const requests = [];
	const server = createServer( ( req, res ) => {
		res.setHeader( "Access-Control-Allow-Origin", "*" );
		res.setHeader( "Access-Control-Expose-Headers", "Content-Range, Content-Encoding" );
		res.on(
			"finish",
			() =>
				requests.push( {
					path: req.url,
					status: res.statusCode,
					encoding: res.getHeader( "Content-Encoding" ),
					length: Number( res.getHeader( "Content-Length" ) ?? 0 )
				} )
		);
		middleware( req, res, () => {
			res.statusCode = 404;
			res.end();
		} );
	} );
	publishedAssets().configureServer( {
		httpServer: server,
		config: { root: process.cwd(), build: { outDir: "temp/artifacts/dist" } },
		middlewares: {
			use( fn ) {
				middleware = fn;
			}
		}
	} );
	await new Promise( resolve => server.listen( 0, "127.0.0.1", resolve ) );
	const { browser, page } = await launchProbeBrowser();
	const failures = [];
	page.on( "pageerror", e => failures.push( String( e ) ) );
	page.on( "requestfailed", r => failures.push( r.url() + ": " + r.failure()?.errorText ) );
	page.on( "console", m => {
		if ( m.type() === "error" ) failures.push( m.text() );
	} );
	try {
		await page.context().route( "**/assets/**", route => {
			const url = new URL( route.request().url() );
			return route.continue(
				url.pathname.startsWith( "/assets/" ) ?
					{ url: "http://127.0.0.1:" + server.address().port + url.pathname } :
					{}
			);
		} );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await page.waitForFunction( () =>
			document.querySelector( "output" )?.textContent?.includes( "runtime: running" )
		);
		await page.evaluate( async () => {
			const { runtime } = await import( "/src/bootstrap.ts" );
			runtime.dispose();
		} );
		requests.length = 0;
		// Real decode fixtures supplement the generated/randomized format tests.
		// Equipment models are content-named, so take the first stored one.
		const equipment = index.assets.find( e => e.stored && e.path.startsWith( "/assets/char/equipment/" ) );
		assert.ok( equipment, "the build must publish a gzip-stored equipment model" );
		const paths = [
			"/assets/char/china/chinaman_monk.glb",
			equipment.path,
			"/assets/npc/mob/china/mangnyang.glb"
		];
		const entries = paths.map( path => index.assets.find( e => e.path === path ) );
		assert.ok( entries.every( Boolean ), "a decode fixture is no longer published" );
		const sample = index.assets.filter( e => e.stored ).filter( ( _, i ) => i % 137 === 0 ).slice( 0, 32 ).map(
			e => ({ path: e.path, sha256: e.sha256, length: e.length })
		);
		const result = await page.evaluate( async ( { entries, sample } ) => {
			const { runtime } = await import( "/src/bootstrap.ts" );
			runtime.dispose();
			for ( const name of await caches.keys() ) {
				if ( name.startsWith( "sro-next-verified-" ) ) await caches.delete( name );
			}
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" ), passes = [];
			for ( let pass = 0; pass < 2; pass++ ) {
				const assets = createAssets(), models = [], start = performance.now();
				try {
					for ( const entry of entries ) {
						const id = assets.request( new URL( entry.path, location.href ).href, 16 << 20, "character" ),
							deadline = performance.now() + 30000;
						let row;
						while ( !(row = assets.take( id )) && performance.now() < deadline ) {
							await new Promise( requestAnimationFrame );
						}
						if ( row?.kind !== "character" ) throw Error( JSON.stringify( row ) );
						models.push( {
							path: entry.path,
							primitives: row.model.primitives.length,
							clips: row.model.clips.length,
							images: row.images.length
						} );
						// Native (block-compressed) textures own no bitmap; close bitmaps only.
						for ( const image of row.images ) if ( !("kind" in image) ) image.close();
					}
					const cache = await caches.open( "sro-next-verified-v1" ), deadline = performance.now() + 10000;
					for ( const entry of entries ) {
						while ( !await cache.match( location.origin + "/assets/.verified/" + entry.sha256 ) ) {
							if ( performance.now() > deadline ) {
								throw Error( "Cache publication timeout" );
							}
							await new Promise( requestAnimationFrame );
						}
					}
					passes.push( { models, milliseconds: performance.now() - start } );
				} finally {
					assets.dispose();
				}
			}
			const { createPacks } = await import( "/src/engine/runtime/assets/worker/packs/packs.ts" ), fetched = [];
			const packs = createPacks( async ( url, limit, signal, range ) => {
				fetched.push( url );
				const response = await fetch( url, {
					signal,
					headers: range ? { Range: `bytes=${range.start}-${range.end}` } : {}
				} );
				if ( !response.ok ) throw Error( "HTTP " + response.status );
				const bytes = new Uint8Array( await response.arrayBuffer() );
				if ( bytes.length > limit ) throw Error( "byte budget" );
				return bytes;
			} );
			const hashes = [];
			try {
				for ( const e of sample ) {
					const bytes = await packs.read(
						new URL( e.path, location.href ),
						64 << 20,
						new AbortController().signal
					);
					hashes.push( {
						path: e.path,
						hash: Array.from(
							new Uint8Array( await crypto.subtle.digest( "SHA-256", bytes ) ),
							b => b.toString( 16 ).padStart( 2, "0" )
						).join( "" )
					} );
				}
				await packs.flush();
				return { passes, hashes, stats: packs.stats(), fetched };
			} finally {
				packs.dispose();
			}
		}, { entries, sample } );
		assert.deepEqual( result.passes[0].models, result.passes[1].models );
		for ( const row of result.hashes ) assert.equal( row.hash, sample.find( e => e.path === row.path ).sha256 );
		// Members travelled compressed and the browser's DecompressionStream decoded them.
		assert.ok( result.stats.storedBytes > 0 );
		assert.ok( result.stats.storedBytes < result.stats.decodedBytes );
		assert.ok( requests.every( r => r.status === 200 || r.status === 206 || r.status === 304 ) );
		// Packs are read by range, so the server must never encode them.
		// Packs stay raw on the wire so ranges address stored bytes; only the
		// manifest (a JSON sidecar) may be served gzip-encoded.
		const packReads = requests.filter( r => /^\/assets\/packs\/.+\.bin$/.test( r.path ) );
		assert.ok( packReads.length > 0 && packReads.every( r => !r.encoding ) );
		await mkdir( "temp/artifacts/asset-delivery", { recursive: true } );
		await writeFile(
			"temp/artifacts/asset-delivery/browser.json",
			JSON.stringify( { result, requests }, null, 2 )
		);
	} catch ( error ) {
		console.error(
			JSON.stringify( {
				failures,
				requests: requests.slice( -12 ),
				body: await page.locator( "body" ).innerText().catch( () => "?" )
			} )
		);
		throw error;
	} finally {
		await browser.close();
		await new Promise( resolve => server.close( resolve ) );
	}
} );
