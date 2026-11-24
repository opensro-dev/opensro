import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "cursor editing ownership stays stable across typing and suspended game frames", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } ),
		samples = [],
		errors = [];
	page.on( "pageerror", error => errors.push( error.message ) );
	const directory = "temp/artifacts/cursor", control = id => page.locator( `[data-ui-id="${id}"]` );
	await mkdir( directory, { recursive: true } );
	try {
		await page.addInitScript( () => {
			const request = window.requestAnimationFrame.bind( window );
			window.__cursorFrames = { paused: false, delivered: 0 };
			window.requestAnimationFrame = callback =>
				request( function deliver( time ) {
					if ( window.__cursorFrames.paused ) {
						request( deliver );
						return;
					}
					window.__cursorFrames.delivered++;
					callback( time );
				} );
		} );
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await control( "frontend:reveal" ).click( { timeout: 30000 } );
		await control( "account" ).waitFor();
		// Retail cursors ship as PNG since #422 (BUG-068: Safari drew no .cur); the
		// hotspot rides the CSS, asserted below.
		const response = await page.request.get(
			new URL( "/assets/cursors/sro_client_cursor_0x95.png", CLIENT_NEXT_BASE_URL ).href
		);
		assert.equal( response.ok(), true );
		const bytes = await response.body();
		assert.deepEqual(
			[ ...bytes.subarray( 0, 8 ) ],
			[ 0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a ],
			"asset must be a PNG"
		);
		async function sample( phase ) {
			const state = await page.evaluate( () => ({
				cursor:
					getComputedStyle( document.elementFromPoint( window.__cursorPoint[0], window.__cursorPoint[1] ) )
						.cursor,
				overlay: document.querySelector( "img[data-retail-cursor]" ) !== null,
				frames: window.__cursorFrames.delivered
			}) );
			samples.push( { phase, ...state } );
			if ( phase === "running" ) assert.match( state.cursor, /sro_client_cursor_0x95\.png["']?\) 2 1, auto$/ );
			else assert.equal( state.cursor, "none" );
			assert.equal( state.overlay, false, "cursor position must not depend on DOM painting" );
			return state;
		}
		async function move( x, y ) {
			await page.mouse.move( x, y );
			await page.evaluate( point => {
				window.__cursorPoint = point;
			}, [ x, y ] );
		}
		await move( 100, 200 );
		await sample( "running" );
		await control( "account" ).fill( "CursorProbe" );
		await control( "account" ).press( "End" );
		await control( "account" ).press( "x" );
		const typing = page.locator( "[data-typing-cursor]" );
		await page.waitForFunction( () => document.querySelector( "[data-typing-cursor]" )?.naturalWidth > 0 );
		assert.equal( await typing.isVisible(), true, "typing must retain a stationary cursor" );
		const box = await typing.boundingBox();
		assert.equal( box.x, 98 );
		assert.equal( box.y, 199 );
		await page.screenshot( { path: `${directory}/typing.png` } );
		for ( let cycle = 0; cycle < 2; cycle++ ) {
			const frames = await page.evaluate( () => {
				window.__cursorFrames.paused = true;
				return window.__cursorFrames.delivered;
			} );
			for ( const [x, y] of [ [ 200, 210 ], [ 520, 460 ], [ 760, 330 ] ] ) {
				await move( x, y );
				assert.equal( (await sample( `paused-${cycle}-${x}` )).frames, frames );
				assert.equal( await typing.isVisible(), true, "editing keeps the same cursor owner during movement" );
				const box = await typing.boundingBox();
				assert.equal( box.x, x - 2 );
				assert.equal( box.y, y - 1 );
			}
			await control( "account" ).press( "x" );
			assert.equal( await typing.isVisible(), true );
			assert.equal( await page.evaluate( () => window.__cursorFrames.delivered ), frames );
			await move( 761, 330 );
			assert.equal( await typing.isVisible(), true );
			await page.screenshot( { path: `${directory}/paused-${cycle}.png` } );
			await page.evaluate( () => {
				window.__cursorFrames.paused = false;
			} );
			await page.waitForFunction( previous => window.__cursorFrames.delivered > previous, frames );
			await sample( `resumed-${cycle}` );
		}
		await control( "account" ).press( "x" );
		assert.equal( await typing.isVisible(), true );
		await page.evaluate( () => window.dispatchEvent( new Event( "blur" ) ) );
		assert.equal( await typing.isVisible(), false );
		await control( "account" ).press( "x" );
		assert.equal( await typing.isVisible(), false, "blur must discard stale pointer coordinates" );
	} finally {
		await writeFile(
			`${directory}/result.json`,
			JSON.stringify( { samples, errors, status: await page.locator( "output" ).allTextContents() }, null, 2 )
		);
		await browser.close();
	}
} );

test( "cursor adapter follows editor focus and removes all listeners on disposal", { timeout: 30000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } );
	try {
		// Isolate the production platform adapter; no simulated game/session boot.
		await page.route(
			new URL( CLIENT_NEXT_BASE_URL ).href,
			route =>
				route.fulfill( {
					contentType: "text/html",
					body:
						'<input id="editor"><textarea id="area"></textarea><div id="editable" contenteditable="true">IME</div>'
				} )
		);
		await page.goto( CLIENT_NEXT_BASE_URL );
		await page.evaluate( async () => {
			const { createCursor } = await import( "/src/engine/runtime/platform/ui/cursor.ts" );
			window.__cursorOwner = createCursor();
		} );
		const fallback = page.locator( "[data-typing-cursor]" );
		await page.waitForFunction( () => document.querySelector( "[data-typing-cursor]" )?.naturalWidth > 0 );
		await page.locator( "#editor" ).press( "x" );
		assert.equal( await fallback.isVisible(), false );
		for ( const selector of [ "#editor", "#area", "#editable" ] ) {
			await page.mouse.move( 400, 250 );
			await page.locator( selector ).press( "x" );
			assert.equal( await fallback.isVisible(), true );
			const before = await fallback.boundingBox();
			assert.equal( before.x, 398 );
			assert.equal( before.y, 249 );
			await page.locator( selector ).press( "Backspace" );
			assert.deepEqual( await fallback.boundingBox(), before );
			await page.mouse.move( 500, 350 );
			assert.equal( await fallback.isVisible(), true );
			assert.equal( (await fallback.boundingBox()).x, 498 );
			await page.locator( selector ).dispatchEvent( "compositionstart" );
			assert.equal( await fallback.isVisible(), true );
			await page.locator( "html" ).dispatchEvent( "pointerleave" );
			assert.equal( await fallback.isVisible(), false );
		}
		await page.mouse.move( 420, 250 );
		await page.locator( "#editor" ).press( "x" );
		await mkdir( "temp/artifacts/cursor", { recursive: true } );
		await page.screenshot( { path: "temp/artifacts/cursor/typing-adapter.png" } );
		await page.evaluate( () => window.__cursorOwner.dispose() );
		await page.mouse.move( 600, 300 );
		await page.locator( "#editor" ).press( "x" );
		assert.equal( await fallback.count(), 0 );
		assert.equal( await page.evaluate( () => getComputedStyle( document.documentElement ).cursor ), "auto" );
	} finally {
		await browser.close();
	}
} );
