import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"native world cursors preserve extracted hotspots, pressed pickup and editing ownership",
	{ timeout: 30000 },
	async () => {
		const { browser, page } = await launchProbeBrowser( { viewport: { width: 800, height: 600 } } );
		try {
			// Isolated platform-adapter fixture. No world session or gameplay state.
			await page.route( CLIENT_NEXT_BASE_URL + "/", route =>
				route.fulfill( {
					contentType: "text/html",
					body: '<!doctype html><html><body><input id="editor"></body></html>'
				} ) );
			await page.goto( CLIENT_NEXT_BASE_URL + "/" );
			await page.evaluate( async () => {
				const { createCursor } = await import( "/src/engine/runtime/platform/ui/cursor.ts" );
				window.__cursorOwner = createCursor();
			} );
			await page.mouse.move( 300, 300 );
			for (
				const [id, x, y] of [ [ 0x97, 2, 1 ], [ 0x98, 2, 26 ], [ 0x99, 9, 6 ], [ 0xa1, 0, 0 ], [
					0xa3,
					15,
					15
				] ]
			) {
				const response = await page.request.get(
					`${CLIENT_NEXT_BASE_URL}/assets/cursors/sro_client_cursor_0x${id.toString( 16 )}.png`
				);
				assert.equal( response.ok(), true );
				// A PNG carries no hotspot; the cursor style below pins x y.
				const bytes = await response.body();
				assert.deepEqual( [ ...bytes.subarray( 0, 8 ) ], [ 0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a ] );
				await page.evaluate( id => window.__cursorOwner.world( id ), id );
				assert.match(
					await page.locator( "body" ).evaluate( el => getComputedStyle( el ).cursor ),
					new RegExp( `0x${id.toString( 16 )}\\.png.* ${x} ${y}, auto$` )
				);
			}
			await page.evaluate( () => window.__cursorOwner.world( 0x99 ) );
			await page.mouse.down();
			assert.equal( await page.evaluate( () => document.documentElement.dataset.sroWorldCursor ), "9a" );
			await page.mouse.up();
			assert.equal( await page.evaluate( () => document.documentElement.dataset.sroWorldCursor ), "99" );
			await page.locator( "#editor" ).focus();
			await page.waitForFunction( () => document.querySelector( "[data-typing-cursor]" )?.naturalWidth > 0 );
			await page.evaluate( () => window.__cursorOwner.world( 0x97 ) );
			assert.equal( await page.locator( "body" ).evaluate( el => getComputedStyle( el ).cursor ), "none" );
			await page.evaluate( () => window.__cursorOwner.dispose() );
			assert.equal( await page.evaluate( () => document.documentElement.dataset.sroWorldCursor ), undefined );
		} finally {
			await browser.close();
		}
	}
);
