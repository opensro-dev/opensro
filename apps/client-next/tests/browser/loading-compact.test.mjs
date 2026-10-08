/*
===========================================================================

loading-compact.test.mjs - readable native loading status through rotation

Load production CSS and scene geometry together so the DOM status cannot
overlap the native progress artwork or widen the viewport with long text.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "compact native loading status fits below the artwork and restores desktop", { timeout: 30000 }, async t => {
	const { browser, page } = await launchProbeBrowser();
	t.after( () => browser.close() );
	await page.route( CLIENT_NEXT_BASE_URL + "/", route =>
		route.fulfill( {
			contentType: "text/html",
			body: `<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1">
<link rel="stylesheet" href="/src/engine/runtime/platform/loading.css">
<div id="root"><div class="sro-boot-loading" data-native="true" data-active="true">
<div class="sro-boot-loading__inner"><div class="sro-boot-loading__copy">
<div class="sro-boot-loading__label">Loading world assets</div>
<div class="sro-boot-loading__detail">Loading a very long scene resource path that must not increase the grid's intrinsic width or push status outside the screen</div>
</div></div></div></div>`
		} ) );
	await page.goto( CLIENT_NEXT_BASE_URL );
	let desktop;
	for (
		const [width, height] of [ [ 1024, 768 ], [ 360, 858 ], [ 375, 667 ], [ 667, 375 ], [ 320, 568 ], [
			1024,
			768
		] ]
	) {
		await page.setViewportSize( { width, height } );
		const result = await page.evaluate( async () => {
			const { loadingScreenQuads } = await import(
				new URL( "/src/engine/foundation/ui/mission-loading.ts", location.origin ).href
			);
			const label = document.querySelector( ".sro-boot-loading__label" );
			const detail = document.querySelector( ".sro-boot-loading__detail" );
			if ( !label || !detail ) throw Error( "Missing loading status" );
			const rows = [ label, detail ].map( element => {
				const r = element.getBoundingClientRect();
				return {
					rect: [ r.x, r.y, r.width, r.height ],
					font: parseFloat( getComputedStyle( element ).fontSize )
				};
			} );
			return {
				rows,
				width: document.documentElement.scrollWidth,
				art: loadingScreenQuads( innerWidth, innerHeight, "scene.png", .5 ).map( q => q.rect )
			};
		} );
		if ( width === 1024 ) {
			if ( desktop ) assert.deepEqual( result, desktop );
			else desktop = result;
			continue;
		}
		assert.equal( result.width, width );
		assert.ok( result.rows[0].font >= 13 && result.rows[1].font >= 12 );
		for ( const { rect: [x, y, w, h] } of result.rows ) {
			assert.ok( x >= 0 && x + w <= width );
			assert.ok( y >= result.art[4][1] + result.art[4][3] && y + h <= height );
		}
	}
} );
