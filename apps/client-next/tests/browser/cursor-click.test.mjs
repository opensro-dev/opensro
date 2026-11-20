import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";
for ( const lane of [ "adapter", "login" ] ) {
	test(
		lane + ": cursor is presented before editor clicks and stable during selection",
		{ timeout: 60000 },
		async t => {
			console.info( lane + ": launching" );
			const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } ),
				records = [];
			t.after( () => browser.close() );
			page.setDefaultTimeout( 10000 );
			console.info( lane + ": launched" );
			try {
				if ( lane === "adapter" ) {
					await page.route( new URL( CLIENT_NEXT_BASE_URL ).href, route =>
						route.fulfill( {
							contentType: "text/html",
							body: '<canvas style="width:1024px;height:768px"></canvas>'
						} ) );
				} else await holdProbeRuntime( page );
				console.info( lane + ": navigating" );
				await page.goto( CLIENT_NEXT_BASE_URL, { timeout: 20000 } );
				console.info( lane + ": importing" );
				if ( lane === "adapter" ) {
					await page.evaluate( async () => {
						const { createCursor } = await import( "/src/engine/runtime/platform/ui/cursor.ts" ),
							{ createUiBridge } = await import( "/src/engine/runtime/platform/ui/ui.ts" );
						window.owner = createCursor();
						window.bridge = createUiBridge( document.querySelector( "canvas" ), () => {}, () => {} );
						window.bridge.present( {
							title: "test",
							message: "",
							controls: [ {
								id: "account",
								kind: "text",
								label: "Account",
								rect: [ 100, 100, 240, 30 ],
								value: ""
							}, {
								id: "password",
								kind: "password",
								label: "Password",
								rect: [ 100, 160, 240, 30 ],
								value: ""
							} ]
						} );
					} );
				} else {
					await page.locator( '[data-ui-id="frontend:reveal"]' ).click( { timeout: 30000 } );
					await page.locator( '[data-ui-id="account"]' ).waitFor();
				}
				console.info( lane + ": image" );
				await page.waitForFunction(
					() => document.querySelector( "[data-typing-cursor]" )?.naturalWidth > 0,
					null,
					{ timeout: 10000 }
				);
				await page.evaluate( () => {
					window.__clickCursor = [];
					for (
						const type of [
							"pointerover",
							"pointerdown",
							"mousedown",
							"focusin",
							"focusout",
							"pointerup",
							"mouseup",
							"click",
							"dblclick",
							"dragstart",
							"pointerleave",
							"blur"
						]
					) {
						window.addEventListener( type, e => {
							const img = document.querySelector( "[data-typing-cursor]" );
							window.__clickCursor.push( {
								prevented: e.defaultPrevented,
								type,
								target: e.target?.dataset?.uiId ?? e.target?.tagName,
								hidden: img.hidden,
								mode: document.documentElement.dataset.sroCursorMode ?? null,
								active: document.activeElement?.dataset?.uiId
							} );
						} );
					}
				} );
				async function raster( phase ) {
					await mkdir( "temp/artifacts/cursor", { recursive: true } );
					const png =
						(await page.screenshot( { path: "temp/artifacts/cursor/" + lane + "-" + phase + ".png" } ))
							.toString( "base64" );
					const result = await page.evaluate( async png => {
						const img = document.querySelector( "[data-typing-cursor]" ),
							r = img.getBoundingClientRect(),
							c = document.createElement( "canvas" );
						c.width = innerWidth;
						c.height = innerHeight;
						const ctx = c.getContext( "2d" );
						const shot = await createImageBitmap(
							await (await fetch( "data:image/png;base64," + png )).blob()
						);
						ctx.drawImage( shot, 0, 0 );
						shot.close();
						const actual = ctx.getImageData( 0, 0, c.width, c.height ).data;
						ctx.clearRect( 0, 0, c.width, c.height );
						ctx.drawImage( img, r.x, r.y );
						const expected = ctx.getImageData( 0, 0, c.width, c.height ).data;
						let checked = 0, different = 0;
						for ( let i = 0; i < expected.length; i += 4 ) {
							if ( expected[i + 3] === 255 ) {
								checked++;
								if ( [ 0, 1, 2 ].some( k => actual[i + k] !== expected[i + k] ) ) different++;
							}
						}
						return {
							checked,
							different,
							hidden: img.hidden,
							rect: [ r.x, r.y, r.width, r.height ],
							sample: [
								...actual.slice(
									(Math.round( r.y ) * c.width + Math.round( r.x )) * 4,
									(Math.round( r.y ) * c.width + Math.round( r.x )) * 4 + 4
								)
							],
							mode: document.documentElement.dataset.sroCursorMode
						};
					}, png );
					records.push( { phase, raster: result } );
					assert.ok( result.checked > 20 );
					assert.equal( result.different, 0, phase );
				}
				console.info( lane + ": ready" );
				for ( const id of [ "account", "password", "account" ] ) {
					console.info( lane + ": " + id );
					const control = page.locator( '[data-ui-id="' + id + '"]' ), b = await control.boundingBox();
					const px = Math.round( b.x + b.width / 2 ), py = Math.round( b.y + b.height / 2 );
					await page.mouse.move( px, py );
					const before = await page.locator( "[data-typing-cursor]" ).isVisible();
					await page.mouse.click( px, py );
					await raster( "click-" + id );
					await control.fill( "CursorSelection" );
					await control.dblclick();
					await page.mouse.move( b.x + 8, b.y + b.height / 2 );
					await page.mouse.down();
					await page.mouse.move( b.x + b.width - 8, b.y + b.height / 2, { steps: 8 } );
					await page.mouse.up();
					await control.press( "ArrowLeft" );
					await page.mouse.move( b.x + 8, b.y + b.height / 2 );
					await page.mouse.down();
					await page.mouse.move( b.x + b.width - 8, b.y + b.height / 2, { steps: 8 } );
					await page.mouse.up();
					records.push( { id, before, after: await page.locator( "[data-typing-cursor]" ).isVisible() } );
				}
				const events = await page.evaluate( () => window.__clickCursor );
				assert.ok( events.some( e => e.type === "dragstart" ), "exercise real selected-text dragging" );
				assert.ok( events.filter( e => e.type === "dragstart" ).every( e => e.prevented ) );
				assert.ok(
					events.filter( e =>
						[
							"pointerdown",
							"mousedown",
							"focusin",
							"focusout",
							"pointerup",
							"mouseup",
							"click",
							"dblclick"
						].includes( e.type )
					).every( e => !e.hidden ),
					"editor gestures must never hide the cursor"
				);
			} finally {
				await mkdir( "temp/artifacts/cursor", { recursive: true } );
				await writeFile(
					"temp/artifacts/cursor/click-handoff-" + lane + ".json",
					JSON.stringify(
						{
							records,
							events: await page.evaluate( () => window.__clickCursor ?? [] )
						},
						null,
						2
					)
				);
				await browser.close();
			}
			// Before a click the image stands only while an editor is focused: a pass
			// keeps the browser's cursor (owner 2026-11-20; the hover test below pins
			// it). Every click on an editor ends with the image owning the pointer.
			assert.ok( records.filter( r => r.id ).every( r => r.after ), JSON.stringify( records ) );
		}
	);
}

test( "press admission covers readonly editors, nested editable content, capture and pressed re-entry", {
	timeout: 30000
}, async t => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 800, height: 600 } } );
	t.after( () => browser.close() );
	await page.route(
		new URL( CLIENT_NEXT_BASE_URL ).href,
		route =>
			route.fulfill( {
				contentType: "text/html",
				body:
					'<input id="readonly" readonly value="Read only"><textarea id="area">Selection</textarea><div id="editable" contenteditable><span id="child">Nested editable</span></div><button id="button">Done</button>'
			} )
	);
	await page.goto( CLIENT_NEXT_BASE_URL );
	await page.evaluate( async () => {
		const { createCursor } = await import( "/src/engine/runtime/platform/ui/cursor.ts" );
		window.owner = createCursor();
		document.addEventListener( "pointermove", e => e.stopPropagation() );
	} );
	await page.waitForFunction( () => document.querySelector( "[data-typing-cursor]" )?.naturalWidth > 0 );
	const image = page.locator( "[data-typing-cursor]" );
	// Passing over an editor keeps the browser's cursor; its press admits the image.
	for ( const id of [ "readonly", "area", "child" ] ) {
		await page.locator( "#button" ).focus();
		await page.locator( "#" + id ).hover();
		assert.equal( await image.isVisible(), false, id + " hover" );
		await page.locator( "#" + id ).click();
		assert.equal( await image.isVisible(), true, id + " click" );
	}
	// Captured pointer events name the capturing element, not the pressed editor.
	await page.locator( "#button" ).focus();
	await page.locator( "#button" ).hover();
	assert.equal( await image.isVisible(), false );
	const b = await page.locator( "#readonly" ).boundingBox();
	await page.evaluate(
		b => document.querySelector( "#button" ).dispatchEvent(
			new PointerEvent( "pointermove", {
				bubbles: true,
				pointerType: "mouse",
				clientX: b.x + 5,
				clientY: b.y + 5
			} )
		),
		b
	);
	assert.equal( await image.isVisible(), false, "an unpressed pass" );
	// A pressed move the button names (it holds the gesture) admits nothing;
	// the same press over the editor itself does.
	await page.evaluate(
		b => document.querySelector( "#button" ).dispatchEvent(
			new PointerEvent( "pointermove", {
				bubbles: true,
				pointerType: "mouse",
				buttons: 1,
				clientX: b.x + 5,
				clientY: b.y + 5
			} )
		),
		b
	);
	assert.equal( await image.isVisible(), false, "a pressed move the button names" );
	await page.evaluate(
		b => document.querySelector( "#readonly" ).dispatchEvent(
			new PointerEvent( "pointermove", {
				bubbles: true,
				pointerType: "mouse",
				buttons: 1,
				clientX: b.x + 5,
				clientY: b.y + 5
			} )
		),
		b
	);
	assert.equal( await image.isVisible(), true, "a pressed pass over the editor" );
	await page.evaluate( () => window.dispatchEvent( new PointerEvent( "pointercancel", { pointerType: "mouse" } ) ) );
	assert.equal( await image.isVisible(), false );
	await page.evaluate(
		b => document.querySelector( "#readonly" ).dispatchEvent(
			new PointerEvent( "pointerover", {
				bubbles: true,
				pointerType: "mouse",
				clientX: b.x + 5,
				clientY: b.y + 5
			} )
		),
		b
	);
	assert.equal( await image.isVisible(), false, "unpressed re-entry" );
	await page.evaluate(
		b => document.querySelector( "#readonly" ).dispatchEvent(
			new PointerEvent( "pointerover", {
				bubbles: true,
				pointerType: "mouse",
				buttons: 1,
				clientX: b.x + 5,
				clientY: b.y + 5
			} )
		),
		b
	);
	assert.equal( await image.isVisible(), true, "pressed re-entry" );
	await page.evaluate( () => window.dispatchEvent( new PointerEvent( "pointerdown", { pointerType: "touch" } ) ) );
	assert.equal( await image.isVisible(), false );
	await page.evaluate( () => window.owner.dispose() );
	assert.equal( await image.count(), 0 );
} );
