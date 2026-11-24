import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "editing cursor has no per-key or stationary-pointer handoff", { timeout: 30000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 800, height: 600 } } );
	try {
		await page.route(
			new URL( CLIENT_NEXT_BASE_URL ).href,
			route =>
				route.fulfill( {
					contentType: "text/html",
					body:
						'<input id="editor"><textarea id="area"></textarea><div id="editable" contenteditable="true">IME</div><input id="range" type="range"><button id="button">Done</button>'
				} )
		);
		await page.goto( CLIENT_NEXT_BASE_URL );
		await page.evaluate( async () => {
			const { createCursor } = await import( "/src/engine/runtime/platform/ui/cursor.ts" );
			window.cursorOwner = createCursor();
		} );
		await page.waitForFunction( () => document.querySelector( "[data-typing-cursor]" )?.naturalWidth > 0 );
		await page.mouse.move( 400, 250 );
		const records = [];
		for ( const selector of [ "#editor", "#area", "#editable" ] ) {
			await page.locator( selector ).focus();
			const record = await page.evaluate( () => {
				const image = document.querySelector( "[data-typing-cursor]" );
				const sample = () => ({
					hidden: image.hidden,
					cursor: getComputedStyle( document.elementFromPoint( 400, 250 ) ).cursor
				});
				const observer = new MutationObserver( () => {} );
				observer.observe( image, { attributes: true } );
				observer.observe( document.documentElement, { attributes: true } );
				const before = sample();
				document.activeElement.dispatchEvent( new KeyboardEvent( "keydown", { key: "x", bubbles: true } ) );
				const key = sample();
				window.dispatchEvent(
					new PointerEvent( "pointermove", { pointerType: "mouse", clientX: 400, clientY: 250 } )
				);
				const stationary = sample(), mutations = observer.takeRecords().length;
				observer.disconnect();
				return { before, key, stationary, mutations };
			} );
			records.push( { selector, ...record } );
		}
		await mkdir( "temp/artifacts/cursor", { recursive: true } );
		await writeFile( "temp/artifacts/cursor/focus-handoff.json", JSON.stringify( records, null, 2 ) );
		for ( const record of records ) {
			for ( const phase of [ "before", "key", "stationary" ] ) {
				assert.deepEqual( record[phase], { hidden: false, cursor: "none" }, `${record.selector} ${phase}` );
			}
		}
		assert.ok(
			records.every( record => record.mutations === 0 ),
			"keys and unchanged pointer coordinates must not rewrite cursor presentation"
		);
		// Screenshot the actual rendered cursor, not merely its CSS visibility.
		const raster = [];
		for ( const key of [ "x", "Backspace", "Shift", "ArrowLeft", "x" ] ) {
			await page.locator( "#editor" ).press( key );
			const png = (await page.screenshot( { clip: { x: 398, y: 249, width: 32, height: 32 } } )).toString(
				"base64"
			);
			const pixels = await page.evaluate( async png => {
				const image = await createImageBitmap( await (await fetch( "data:image/png;base64," + png )).blob() );
				const canvas = document.createElement( "canvas" );
				canvas.width = canvas.height = 32;
				const ctx = canvas.getContext( "2d" );
				ctx.drawImage( image, 0, 0 );
				image.close();
				const actual = ctx.getImageData( 0, 0, 32, 32 ).data;
				ctx.fillStyle = "white";
				ctx.fillRect( 0, 0, 32, 32 );
				ctx.drawImage( document.querySelector( "[data-typing-cursor]" ), 0, 0 );
				const expected = ctx.getImageData( 0, 0, 32, 32 ).data;
				return {
					different: actual.reduce( ( n, v, i ) => n + Number( v !== expected[i] ), 0 ),
					ink: actual.reduce( ( n, v, i ) => n + Number( i % 4 !== 3 && v !== 255 ), 0 )
				};
			}, png );
			raster.push( { key, ...pixels } );
			assert.equal( pixels.different, 0 );
			assert.ok( pixels.ink > 20 );
		}
		await writeFile( "temp/artifacts/cursor/focus-raster.json", JSON.stringify( raster, null, 2 ) );
		await page.locator( "#range" ).focus();
		assert.equal( await page.locator( "[data-typing-cursor]" ).isVisible(), false );
		await page.locator( "#button" ).focus();
		assert.equal( await page.locator( "[data-typing-cursor]" ).isVisible(), false );
	} finally {
		await browser.close();
	}
} );

test( "cursor admission waits for its image and a mouse position, and disposal cancels delayed admission", {
	timeout: 30000
}, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 800, height: 600 } } );
	let release;
	const gate = new Promise( resolve => {
		release = resolve;
	} );
	try {
		await page.route(
			new URL( CLIENT_NEXT_BASE_URL ).href,
			route => route.fulfill( { contentType: "text/html", body: '<input id="editor">' } )
		);
		await page.route( "**/sro_client_cursor_0x95.png", async route => {
			await gate;
			await route.continue().catch( () => {} );
		} );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await page.evaluate( async () => {
			const { createCursor } = await import( "/src/engine/runtime/platform/ui/cursor.ts" );
			window.cursorOwner = createCursor();
		} );
		await page.locator( "#editor" ).focus();
		await page.evaluate( () =>
			window.dispatchEvent(
				new PointerEvent( "pointerdown", { pointerType: "mouse", clientX: 400, clientY: 250 } )
			)
		);
		assert.equal( await page.locator( "[data-typing-cursor]" ).isVisible(), false );
		assert.notEqual(
			await page.evaluate( () => getComputedStyle( document.documentElement ).cursor ),
			"none",
			"do not hide the browser cursor before replacement admission"
		);
		await page.evaluate( () => window.cursorOwner.dispose() );
		release();
		await page.locator( "#editor" ).press( "x" );
		assert.equal( await page.locator( "[data-typing-cursor]" ).count(), 0 );
		assert.equal( await page.evaluate( () => getComputedStyle( document.documentElement ).cursor ), "auto" );
	} finally {
		release();
		await browser.close();
	}
} );
