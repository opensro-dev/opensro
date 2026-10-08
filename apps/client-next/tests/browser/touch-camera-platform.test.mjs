/*
===========================================================================

touch-camera-platform.test.mjs - real canvas touch admission and camera input

Browser-owned gestures must not cancel camera pinches or synthesize ground
commands. UI touch input and mouse wheel retain their separate admission.
===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "canvas pinch zooms without ground clicks and leaves UI and mouse input usable", { timeout: 45000 }, async () => {
	const { browser } = await launchProbeBrowser( { executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE } );
	try {
		const context = await browser.newContext( { hasTouch: true, viewport: { width: 800, height: 600 } } );
		const page = await context.newPage();
		const errors = [];
		page.on( "pageerror", error => errors.push( error.message ) );
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const fixture = await page.evaluateHandle( async () => {
			const platformPath = "/src/engine/runtime/platform/platform.ts",
				inputPath = "/src/engine/runtime/input/input.ts";
			const { createPlatform } = await import( platformPath );
			const { createInput } = await import( inputPath );
			const canvas = document.querySelector( "canvas" ), status = document.createElement( "output" );
			if ( !canvas ) throw Error( "Canvas fixture missing" );
			const clicks = [], ui = [], pointers = [], input = createInput();
			for ( const name of [ "pointerdown", "pointerup", "pointercancel", "click" ] ) {
				window.addEventListener( name, event =>
					pointers.push( {
						type: event.type,
						target: event.target instanceof HTMLElement ?
							event.target.dataset.uiId ?? event.target.tagName :
							"",
						prevented: event.defaultPrevented,
						primary: event instanceof PointerEvent ? event.isPrimary : null,
						pointer: event instanceof PointerEvent ? event.pointerId : null
					} ) );
			}
			canvas.style.touchAction = "pan-y";
			const platform = createPlatform(
				canvas,
				status,
				() => {},
				event => input.accept( event ),
				() => {},
				event => ui.push( event ),
				( x, y ) => x < 120 && y < 100,
				( x, y ) => clicks.push( { x, y } )
			);
			platform.presentUi( {
				title: "Touch camera",
				message: "",
				controls: [
					{ id: "ui", label: "UI", kind: "button", rect: [ 0, 0, 100, 80 ] }
				]
			} );
			return { platform, input, clicks, ui, canvas, pointers };
		} );
		const session = await context.newCDPSession( page );
		/*
		================
		touch
		================
		*/
		const touch = async ( type, points ) => {
			await session.send( "Input.dispatchTouchEvent", {
				type,
				touchPoints: points.map( ( [id, x, y] ) => ({ id, x, y }) )
			} );
			// Chrome may acknowledge a CDP move before dispatching its coalesced
			// pointer event. Let that input frame finish before reading the camera.
			await page.evaluate( () =>
				new Promise( resolve => {
					requestAnimationFrame( () => requestAnimationFrame( () => resolve( null ) ) );
				} )
			);
		};
		const before = await fixture.evaluate( f => f.input.camera() );
		await touch( "touchStart", [ [ 1, 300, 250 ] ] );
		await touch( "touchStart", [ [ 1, 300, 250 ], [ 2, 400, 250 ] ] );
		await touch( "touchMove", [ [ 1, 300, 250 ], [ 2, 440, 250 ] ] );
		const spread = await fixture.evaluate( f => f.input.camera() );
		assert.ok( spread.distance < before.distance, "spreading zooms in through the real input owner" );
		assert.equal( spread.yaw, before.yaw );
		await touch( "touchMove", [ [ 1, 300, 250 ], [ 2, 410, 250 ] ] );
		assert.ok( (await fixture.evaluate( f => f.input.camera() )).distance > spread.distance );
		// Finish the complete CDP gesture. Partial finger release is covered by
		// the touch owner tests; omitting a finger from a move is not a release.
		await touch( "touchEnd", [] );
		assert.deepEqual( await fixture.evaluate( f => f.clicks ), [] );
		await page.touchscreen.tap( 350, 300 );
		assert.equal( await fixture.evaluate( f => f.clicks.length ), 1, "single tap dispatches exactly once" );
		await touch( "touchStart", [ [ 1, 300, 250 ] ] );
		await touch( "touchMove", [ [ 1, 340, 250 ] ] );
		await touch( "touchEnd", [] );
		assert.notEqual( (await fixture.evaluate( f => f.input.camera() )).yaw, before.yaw );
		assert.equal( await fixture.evaluate( f => f.clicks.length ), 1, "orbit does not move the character" );
		const uiBefore = await fixture.evaluate( f => f.input.camera() );
		await touch( "touchStart", [ [ 1, 50, 40 ] ] );
		await touch( "touchEnd", [] );
		try {
			await page.waitForFunction( f => f.ui.some( e => e.kind === "activate" && e.id === "ui" ), fixture, {
				timeout: 2000
			} );
		} catch {
			assert.fail( JSON.stringify(
				await fixture.evaluate( f => ({
					events: f.ui.slice( -10 ),
					pointers: f.pointers.slice( -20 ),
					hit: document.elementFromPoint( 50, 40 )?.outerHTML,
					control: document.querySelector( '[data-ui-id="ui"]' )?.getBoundingClientRect().toJSON(),
					canvas: f.canvas.getBoundingClientRect().toJSON()
				}) )
			) );
		}
		assert.equal(
			await fixture.evaluate( f => f.ui.filter( e => e.kind === "activate" && e.id === "ui" ).length ),
			1
		);
		assert.deepEqual( await fixture.evaluate( f => f.input.camera() ), uiBefore );
		assert.equal( await fixture.evaluate( f => f.clicks.length ), 1 );
		for ( const uiFirst of [ false, true ] ) {
			const first = uiFirst ? [ 1, 50, 40 ] : [ 1, 300, 250 ];
			const second = uiFirst ? [ 2, 300, 250 ] : [ 2, 50, 40 ];
			await touch( "touchStart", [ first ] );
			await touch( "touchStart", [ first, second ] );
			await touch( "touchMove", [ first, [ second[0], second[1] + 20, second[2] ] ] );
			await touch( "touchEnd", [] );
			assert.deepEqual(
				await fixture.evaluate( f => f.input.camera() ),
				uiBefore,
				"mixed UI/canvas touches neither zoom nor orbit"
			);
			assert.equal( await fixture.evaluate( f => f.clicks.length ), 1 );
		}
		await touch( "touchStart", [ [ 1, 300, 250 ], [ 2, 400, 250 ] ] );
		await touch( "touchMove", [ [ 1, 300, 250 ], [ 2, 430, 250 ] ] );
		const interrupted = await fixture.evaluate( f => f.input.camera() );
		assert.ok( interrupted.distance < uiBefore.distance );
		await touch( "touchStart", [ [ 1, 300, 250 ], [ 2, 430, 250 ], [ 3, 50, 40 ] ] );
		await touch( "touchMove", [ [ 1, 280, 250 ], [ 2, 470, 250 ], [ 3, 50, 40 ] ] );
		assert.deepEqual(
			await fixture.evaluate( f => f.input.camera() ),
			interrupted,
			"a third UI-owned touch stops an already active canvas pinch"
		);
		await touch( "touchEnd", [] );
		assert.equal( await fixture.evaluate( f => f.clicks.length ), 1 );
		await page.mouse.move( 400, 400 );
		await page.mouse.wheel( 0, 120 );
		await page.waitForFunction( previous => {
			// Wait for wheel dispatch without assuming a compositor frame duration.
			return performance.now() > previous + 50;
		}, await page.evaluate( () => performance.now() ) );
		assert.ok( (await fixture.evaluate( f => f.input.camera() )).distance > interrupted.distance );
		for ( const end of [ "cancel", "blur" ] ) {
			await touch( "touchStart", [ [ 1, 300, 250 ] ] );
			if ( end === "blur" ) await page.evaluate( () => window.dispatchEvent( new Event( "blur" ) ) );
			await touch( "touchCancel", [] );
			assert.equal( await fixture.evaluate( f => f.clicks.length ), 1 );
		}
		await page.touchscreen.tap( 350, 300 );
		assert.equal( await fixture.evaluate( f => f.clicks.length ), 2, "new gesture works after cancellation" );
		await page.mouse.click( 400, 300 );
		assert.equal( await fixture.evaluate( f => f.clicks.length ), 3, "mouse dispatch is unchanged" );
		await fixture.evaluate( f => f.platform.dispose() );
		assert.equal( await fixture.evaluate( f => f.canvas.style.touchAction ), "pan-y" );
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
