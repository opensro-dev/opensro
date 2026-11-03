/*
===========================================================================

viewer-still.test.mjs - the 3D viewer draws one shipped monster as a still

Port-only, not native (docs/guides/VIEWER.md). viewer.html draws Tiger Girl
(refObjId 1954) through the game's renderer into a transparent PNG of the
requested size: its corners are clear and its middle holds the model. An
id no catalogue knows fails with the fixed reason and the stage it
stopped at, not a timeout.
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
			const failure = /** @type {Error & { cause?: { stage?: string } }} */ (error);
			return { reason: failure.message, stage: failure.cause?.stage };
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
			reason: "unknown-monster",
			// The catalogue was read and lacks the id: it stopped at the model.
			stage: "model"
		} );
	} finally {
		await browser.close();
	}
} );

/*
================
lookQuery

A base64url P6 look: the adventurer body in the heavy set 01 with the
first spear (sockets 0..6), or one naming an item that does not exist.
================
*/
function lookQuery( unknownItem = false ) {
	const worn = [ [ 0, 287 ], [ 1, 395 ], [ 2, 359 ], [ 3, 467 ], [ 4, 431 ], [ 5, 503 ], [
		6,
		unknownItem ? 99999999 : 143
	] ];
	const look = {
		bodyRefObjId: 1907,
		worn: worn.map( ( [slot, refItemId] ) => ({ slot, refItemId, plus: 0 }) ),
		avatar: []
	};
	return "look=" + Buffer.from( JSON.stringify( look ) ).toString( "base64url" );
}

test( "a look draws its dressed character, and an unknown item is a bad request", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		const still = await viewerOutcome( page, lookQuery() + `&still=1&size=${STILL_SIZE}` );
		assert.equal( still.reason, undefined, still.reason );
		assert.equal( still.corner, 0 );
		assert.ok( still.span > 0.6 && still.span <= 1, String( still.span ) );
		assert.deepEqual( await viewerOutcome( page, lookQuery( true ) + "&still=1" ), {
			reason: "bad-request",
			stage: "model"
		} );
	} finally {
		await browser.close();
	}
} );

test(
	"the interactive view stops asking for frames once drawn, and a drag draws again",
	{ timeout: 120000 },
	async () => {
		const { browser, page } = await launchProbeBrowser( { viewport: { width: 300, height: 400 } } );
		try {
			await page.goto( new URL( "viewer.html?" + lookQuery(), CLIENT_NEXT_BASE_URL ).href );
			await page.evaluate( () => /** @type {Promise<string>} */ (Reflect.get( window, "__viewerStill" )) );
			// Count the animation frames the page asks for: none while idle.
			const frames = () =>
				page.evaluate( () =>
					new Promise( resolve => {
						let asked = 0;
						const raf = window.requestAnimationFrame.bind( window );
						window.requestAnimationFrame = callback => {
							asked++;
							return raf( callback );
						};
						setTimeout( () => {
							window.requestAnimationFrame = raf;
							resolve( asked );
						}, 1000 );
					} )
				);
			assert.equal( await frames(), 0 );
			const before = await page.screenshot( { omitBackground: true } );
			await page.mouse.move( 150, 200 );
			await page.mouse.down();
			await page.mouse.move( 250, 200, { steps: 5 } );
			await page.mouse.up();
			await page.waitForTimeout( 1000 );
			assert.equal( await frames(), 0 );
			assert.notDeepEqual( await page.screenshot( { omitBackground: true } ), before );
		} finally {
			await browser.close();
		}
	}
);

// The plain sword, its _RARE (SoS) form with an itemrare glow, and a plus.
const PLAIN_SWORD = 71;
const RARE_SWORD = 4014;
const PLUS = 7;

/*
================
swordQuery

The adventurer look holding a sword with the given enhancement.
================
*/
function swordQuery( refItemId, plus = 0 ) {
	const worn = [ [ 0, 287 ], [ 1, 395 ], [ 2, 359 ], [ 3, 467 ], [ 4, 431 ], [ 5, 503 ], [ 6, refItemId ] ];
	const look = {
		bodyRefObjId: 1907,
		worn: worn.map( ( [slot, id] ) => ({ slot, refItemId: id, plus: slot === 6 ? plus : 0 }) ),
		avatar: []
	};
	return "look=" + Buffer.from( JSON.stringify( look ) ).toString( "base64url" );
}

/*
================
stillPixels

A still's RGBA pixels.
================
*/
async function stillPixels( page, query ) {
	await page.goto( new URL( "viewer.html?" + query + `&still=1&size=${STILL_SIZE}`, CLIENT_NEXT_BASE_URL ).href );
	return page.evaluate( async () => {
		const image = new Image();
		image.src = await /** @type {Promise<string>} */ (Reflect.get( window, "__viewerStill" ));
		await image.decode();
		const canvas = new OffscreenCanvas( image.width, image.height );
		const context = /** @type {OffscreenCanvasRenderingContext2D} */ (canvas.getContext( "2d" ));
		context.drawImage( image, 0, 0 );
		return Array.from( context.getImageData( 0, 0, image.width, image.height ).data );
	} );
}

/*
================
changed

Pixels that differ by more than a rounding between two stills.
================
*/
function changed( a, b ) {
	let count = 0;
	for ( let i = 0; i < a.length; i += 4 ) {
		const delta = Math.abs( a[i] - b[i] ) + Math.abs( a[i + 1] - b[i + 1] ) + Math.abs( a[i + 2] - b[i + 2] ) +
			Math.abs( a[i + 3] - b[i + 3] );
		if ( delta > 40 ) count++;
	}
	return count;
}

/*
================
violet

Lit violet pixels: the rare glow's colour, absent from the plain look.
================
*/
function violet( rgba ) {
	let count = 0;
	for ( let i = 0; i < rgba.length; i += 4 ) {
		if ( rgba[i + 3] > 0 && rgba[i + 2] > 150 && rgba[i + 2] > rgba[i + 1] + 40 && rgba[i] > rgba[i + 1] ) count++;
	}
	return count;
}

test( "the doll wears a rare weapon's glow and a plus's shimmer, as native 8EA960", { timeout: 180000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		const plain = await stillPixels( page, swordQuery( PLAIN_SWORD ) );
		const again = await stillPixels( page, swordQuery( PLAIN_SWORD ) );
		const rare = await stillPixels( page, swordQuery( RARE_SWORD ) );
		const plus = await stillPixels( page, swordQuery( PLAIN_SWORD, PLUS ) );
		// The same look settles on (nearly) the same picture.
		assert.ok( changed( plain, again ) < 100, `plain twice: ${changed( plain, again )}` );
		// itemrare.txt's effect draws at the blade (violet), and the plus's
		// enhancement glow (CRTModProgEquipPow) tints the blade.
		assert.ok( violet( rare ) > 30 && violet( plain ) < 5, `violet ${violet( rare )} vs ${violet( plain )}` );
		assert.ok( changed( plain, plus ) > 150, `plus changed ${changed( plain, plus )}` );
	} finally {
		await browser.close();
	}
} );

test( "the interactive view keeps animating a glowing doll", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 300, height: 400 } } );
	try {
		await page.goto( new URL( "viewer.html?" + swordQuery( RARE_SWORD ), CLIENT_NEXT_BASE_URL ).href );
		await page.evaluate( () => /** @type {Promise<string>} */ (Reflect.get( window, "__viewerStill" )) );
		const asked = await page.evaluate( () =>
			new Promise( resolve => {
				let count = 0;
				const raf = window.requestAnimationFrame.bind( window );
				window.requestAnimationFrame = callback => {
					count++;
					return raf( callback );
				};
				setTimeout( () => {
					window.requestAnimationFrame = raf;
					resolve( count );
				}, 1000 );
			} )
		);
		assert.ok( Number( asked ) > 10, `asked ${asked} frames` );
	} finally {
		await browser.close();
	}
} );
