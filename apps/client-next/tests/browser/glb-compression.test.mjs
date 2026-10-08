import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { publishedAssets } from "../../tools/published-assets.mjs";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test(
	"Chromium and the production asset worker admit compressed GLBs without changing bytes",
	{ timeout: 60000 },
	async () => {
		let middleware;
		const transfers = [];
		const server = createServer( ( req, res ) => {
			res.setHeader( "Access-Control-Allow-Origin", "*" );
			res.on(
				"finish",
				() => transfers.push( { path: req.url, encoding: res.getHeader( "Content-Encoding" ) } )
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
		try {
			// Exercise the loose-deployment fallback with current middleware, without
			// restarting the shared application server or altering installed packs.
			const legacy = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/packs/manifest.json", "utf8" ) );
			await page.context().route(
				"**/assets/packs/manifest.json",
				route =>
					route.fulfill( { status: 200, contentType: "application/json", body: JSON.stringify( legacy ) } )
			);
			await page.context().route(
				"**/assets/**/*.bin",
				route => route.fulfill( { status: 404, body: "Loose deployment fixture" } )
			);
			await page.context().route( "**/*.glb", route =>
				route.continue( {
					url: "http://127.0.0.1:" + server.address().port + new URL( route.request().url() ).pathname
				} ) );
			await page.goto( CLIENT_NEXT_BASE_URL );
			await page.waitForFunction( () =>
				document.querySelector( "output" )?.textContent?.includes( "runtime: running" )
			);
			// Equipment models are content-named, so take the first published one.
			const equipment = legacy.assets.find( e => e.path.startsWith( "/assets/char/equipment/" ) );
			assert.ok( equipment, "the build must publish an equipment model" );
			const paths = [
				"/assets/char/china/chinaman_monk.glb",
				equipment.path,
				"/assets/npc/mob/china/mangnyang.glb"
			];
			const result = await page.evaluate( async paths => {
				const { runtime } = await import( "/src/bootstrap.ts" );
				runtime.dispose();
				const hashes = [];
				for ( const path of paths ) {
					const r = await fetch( path, { cache: "no-store" } ), bytes = await r.arrayBuffer();
					hashes.push( {
						path,
						bytes: bytes.byteLength,
						hash: Array.from(
							new Uint8Array( await crypto.subtle.digest( "SHA-256", bytes ) ),
							b => b.toString( 16 ).padStart( 2, "0" )
						).join( "" )
					} );
				}
				// A clean cache makes the production worker verify the decompressed bytes
				// against the installed manifest rather than reuse an earlier admitted copy.
				for ( const name of await caches.keys() ) {
					await caches.delete( name );
				}
				const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" ),
					assets = createAssets(),
					models = [];
				try {
					for ( const path of paths ) {
						const id = assets.request( new URL( path, location.href ).href, 16 << 20, "character" ),
							start = performance.now();
						let r;
						while ( !(r = assets.take( id )) && performance.now() - start < 20000 ) {
							await new Promise( requestAnimationFrame );
						}
						if ( r?.kind !== "character" ) {
							throw Error( JSON.stringify( r ) );
						}
						models.push( {
							path,
							primitives: r.model.primitives.length,
							clips: r.model.clips.length,
							images: r.images.length
						} );
						// Native (block-compressed) textures own no bitmap; close bitmaps only.
						for ( const image of r.images ) {
							if ( !("kind" in image) ) image.close();
						}
					}
				} finally {
					assets.dispose();
				}
				return { hashes, models };
			}, paths );
			for ( const row of result.hashes ) {
				const original = await readFile( CLIENT_PUBLIC_ROOT + row.path );
				assert.equal( row.hash, createHash( "sha256" ).update( original ).digest( "hex" ) );
				assert.equal( row.bytes, original.length );
			}
			assert.ok( result.models.every( m => m.primitives > 0 ) );
			assert.ok( transfers.length >= 6 );
			assert.ok( transfers.every( t => t.encoding === "gzip" ) );
			await mkdir( "temp/artifacts/glb-compression", { recursive: true } );
			await writeFile(
				"temp/artifacts/glb-compression/browser.json",
				JSON.stringify( { result, transfers }, null, 2 )
			);
		} finally {
			await browser.close();
			await new Promise( resolve => server.close( resolve ) );
		}
	}
);
