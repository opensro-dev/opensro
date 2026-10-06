import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import path from "node:path";
import { root } from "../../tools/project.mjs";
test(
	"replacement asset worker reads the installed font atlas through its verified pack",
	{ timeout: 30000 },
	async () => {
		const logical = "/assets/fonts/native-ui-font-atlas.png";
		const expected = createHash( "sha256" ).update( readPublishedAssetBytesSync( logical, CLIENT_PUBLIC_ROOT ) )
			.digest( "hex" );
		const { browser, page } = await launchProbeBrowser();
		try {
			await page.goto( "http://127.0.0.1:5180/" );
			const result = await page.evaluate( async ( { url } ) => {
				const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
				const assets = createAssets();
				try {
					const id = assets.request( url );
					const start = performance.now();
					let result;
					while ( !(result = assets.take( id )) ) {
						if ( performance.now() - start > 20000 ) throw new Error( "Asset worker timed out" );
						await new Promise( resolve => setTimeout( resolve, 10 ) );
					}
					if ( result.kind === "error" ) throw new Error( result.error );
					const digest = await crypto.subtle.digest( "SHA-256", result.buffer );
					return {
						bytes: result.buffer.byteLength,
						sha: Array.from( new Uint8Array( digest ), b => b.toString( 16 ).padStart( 2, "0" ) ).join( "" )
					};
				} finally {
					assets.dispose();
				}
			}, { url: CLIENT_BASE_URL + logical } );
			assert.equal( result.sha, expected );
			assert.ok( result.bytes > 0 );
		} finally {
			await browser.close();
		}
	}
);

test( "installed atlas decodes in the worker and draws with a device-owned texture", { timeout: 30000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", error => errors.push( error.message ) );
	try {
		await page.goto( "http://127.0.0.1:5180/" );
		const result = await page.evaluate( async url => {
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const assets = createAssets(), canvas = document.createElement( "canvas" );
			canvas.style.cssText = "position:fixed;inset:0;width:256px;height:256px";
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			try {
				const id = assets.request( url, 1 << 20, "png" );
				let result;
				const start = performance.now();
				while ( !(result = assets.take( id )) ) {
					if ( performance.now() - start > 20000 ) throw new Error( "Atlas decode timed out" );
					await new Promise( resolve => setTimeout( resolve, 10 ) );
				}
				if ( result.kind !== "image" ) throw new Error( result.error ?? "Expected decoded atlas" );
				const width = result.image.width, height = result.image.height;
				renderer.setImage( result.image );
				for ( let frame = 0; frame < 10; frame++ ) {
					renderer.frame( { width: 256, height: 256 } );
					await new Promise( resolve => requestAnimationFrame( resolve ) );
				}
				if ( renderer.phase() !== "running" ) throw new Error( renderer.error() ?? "Renderer not ready" );
				const output = canvas.toDataURL();
				return { width, height, outputBytes: output.length };
			} finally {
				renderer.dispose();
				assets.dispose();
				canvas.remove();
			}
		}, CLIENT_BASE_URL + "/assets/fonts/native-ui-font-atlas.png" );
		assert.ok( result.width > 0 && result.height > 0 );
		assert.ok( result.outputBytes > 1000 );
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
