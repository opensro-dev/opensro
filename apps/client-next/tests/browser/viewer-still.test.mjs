/*
===========================================================================

viewer-still.test.mjs - the 3D viewer draws one shipped monster as a still

Port-only, not native (docs/VIEWER.md). viewer.html draws Tiger Girl
(refObjId 1954) through the game's renderer into a transparent PNG of the
requested size: its corners are clear and its middle holds the model. An
id no catalogue knows fails with the fixed reason, not a timeout.
Run alone, never with the whole browser suite.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

// The shipped unique Tiger Girl, and an id outside every catalogue.
const TIGER_GIRL = 1954;
const UNKNOWN_MONSTER = 99999999;
const STILL_SIZE = 256;

/*
================
viewerOutcome

The page's still: the PNG's size and alpha samples, or the failure reason.
================
*/
async function viewerOutcome( page, query ) {
	await page.goto( new URL( "viewer.html?" + query, CLIENT_NEXT_BASE_URL ).href );
	return page.evaluate( async () => {
		let png;
		try {
			png = await /** @type {Promise<string>} */ (Reflect.get( window, "__viewerStill" ));
		} catch ( error ) {
			return { reason: /** @type {Error} */ (error).message };
		}
		const image = new Image();
		image.src = png;
		await image.decode();
		const canvas = new OffscreenCanvas( image.width, image.height );
		const context = /** @type {OffscreenCanvasRenderingContext2D} */ (canvas.getContext( "2d" ));
		context.drawImage( image, 0, 0 );
		const pixels = context.getImageData( 0, 0, image.width, image.height ).data;
		// The drawn model's pixel box: its longer side is the framing.
		let left = image.width, right = -1, top = image.height, bottom = -1;
		for ( let y = 0; y < image.height; y++ ) {
			for ( let x = 0; x < image.width; x++ ) {
				if ( pixels[(y * image.width + x) * 4 + 3] === 0 ) continue;
				left = Math.min( left, x );
				right = Math.max( right, x );
				top = Math.min( top, y );
				bottom = Math.max( bottom, y );
			}
		}
		return {
			width: image.width,
			height: image.height,
			corner: pixels[3],
			span: Math.max( right - left + 1, bottom - top + 1 ) / image.width
		};
	} );
}

test( "a shipped monster draws as a transparent still of the requested size", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		const still = await viewerOutcome( page, `monster=${TIGER_GIRL}&still=1&size=${STILL_SIZE}` );
		assert.equal( still.reason, undefined, still.reason );
		assert.equal( still.width, STILL_SIZE );
		assert.equal( still.height, STILL_SIZE );
		assert.equal( still.corner, 0 );
		// Framed to its box: the model spans most of the picture, inside it.
		assert.ok( still.span > 0.6 && still.span < 1, String( still.span ) );
	} finally {
		await browser.close();
	}
} );

test( "an unknown monster fails with its reason", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		assert.deepEqual( await viewerOutcome( page, `monster=${UNKNOWN_MONSTER}&still=1` ), {
			reason: "unknown-monster"
		} );
	} finally {
		await browser.close();
	}
} );
