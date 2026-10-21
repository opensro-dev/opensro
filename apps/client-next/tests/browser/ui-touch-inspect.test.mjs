/*
===========================================================================

ui-touch-inspect.test.mjs - a touch tap keeps an item or skill tooltip up

Port-only (#1243). A touch tap on a control published with `inspect` keeps
its own action and also holds the hover after the finger lifts, until the
next touch, a scroll or the control losing the flag. A hold still uses, a
drag still drags, and mouse hover is unchanged. Real input over the
production bridge.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const HOLD_WAIT_MS = 650;

test( "a touch tap inspects, a hold uses, a drag drags, and mouse hover is unchanged", { timeout: 60000 }, async () => {
	const { browser } = await launchProbeBrowser( { executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE } );
	try {
		const context = await browser.newContext( { hasTouch: true, viewport: { width: 800, height: 600 } } );
		const page = await context.newPage();
		const errors = [];
		page.on( "pageerror", error => errors.push( error.message ) );
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const fixture = await page.evaluateHandle( async () => {
			const path = "/src/engine/runtime/platform/ui/ui.ts";
			const { createUiBridge } = await import( path );
			const events = [];
			const bridge = createUiBridge(
				document.querySelector( "canvas" ),
				event => events.push( event ),
				() => {}
			);
			const slot = ( id, x, inspect ) => ({
				id,
				label: id,
				kind: "button",
				rect: [ x, 20, 100, 60 ],
				rightActivate: true,
				draggable: true,
				carry: true,
				inspect
			});
			const publish = inspect =>
				bridge.present( {
					title: "Inspect",
					message: "",
					controls: [ slot( "slot:13", 20, inspect ), slot( "slot:14", 260, inspect ) ]
				} );
			publish( true );
			return { bridge, events, publish };
		} );
		const session = await context.newCDPSession( page );
		/*
		================
		touch
		================
		*/
		const touch = ( type, points ) =>
			session.send( "Input.dispatchTouchEvent", {
				type,
				touchPoints: points.map( ( [id, x, y] ) => ({ id, x, y }) )
			} );
		const settle = () =>
			page.evaluate( () =>
				new Promise( resolve => requestAnimationFrame( () => requestAnimationFrame( () => resolve( null ) ) ) )
			);
		const reset = () =>
			fixture.evaluate( f => {
				f.events.length = 0;
			} );
		const events = () => fixture.evaluate( f => f.events );
		const lastHover = async () => (await events()).filter( e => e.kind === "hover" ).at( -1 )?.id ?? null;

		// A tap keeps its action and holds the hover after the finger lifts.
		await reset();
		await page.touchscreen.tap( 70, 50 );
		await settle();
		let seen = await events();
		assert.equal(
			seen.filter( e => e.kind === "activate" && e.id === "slot:13" ).length,
			1,
			"the tap still activates"
		);
		assert.equal( seen.filter( e => e.kind === "right-activate" ).length, 0, "a tap never uses" );
		assert.equal( await lastHover(), "slot:13", "the tooltip stays up after the tap" );

		// The next touch elsewhere releases it.
		await page.touchscreen.tap( 700, 400 );
		await settle();
		assert.equal( await lastHover(), null, "a tap on the world releases the inspection" );

		// A tap on another slot moves the inspection there.
		await page.touchscreen.tap( 70, 50 );
		await page.touchscreen.tap( 310, 50 );
		await settle();
		assert.equal( await lastHover(), "slot:14", "the inspection follows the next tapped slot" );

		// A scroll releases it.
		await page.mouse.wheel( 0, 40 );
		await settle();
		assert.equal( await lastHover(), null, "a scroll releases the inspection" );

		// A hold still uses, and inspects nothing.
		await reset();
		await touch( "touchStart", [ [ 1, 70, 50 ] ] );
		await page.waitForTimeout( HOLD_WAIT_MS );
		await touch( "touchEnd", [] );
		await settle();
		seen = await events();
		assert.equal( seen.filter( e => e.kind === "right-activate" && e.id === "slot:13" ).length, 1, "a hold uses" );
		assert.equal( await lastHover(), null, "a hold leaves no tooltip held" );
		await page.touchscreen.tap( 700, 400 );

		// A drag still drags, and inspects nothing.
		await reset();
		await touch( "touchStart", [ [ 1, 70, 50 ] ] );
		for ( const x of [ 90, 130, 170, 200 ] ) await touch( "touchMove", [ [ 1, x, 50 ] ] );
		await touch( "touchEnd", [] );
		await settle();
		seen = await events();
		assert.ok( seen.some( e => e.kind === "drag" && e.id === "slot:13" ), "the touch drag publishes drags" );
		assert.equal( seen.filter( e => e.kind === "drag-end" ).length, 1, "the touch drag ends" );
		assert.equal( seen.filter( e => e.kind === "activate" ).length, 0, "a drag is no tap" );
		assert.equal( await lastHover(), null, "a drag leaves no tooltip held" );

		// Option off: no inspect flag, so the hover ends with the tap.
		await fixture.evaluate( f => f.publish( undefined ) );
		await reset();
		await page.touchscreen.tap( 70, 50 );
		await settle();
		seen = await events();
		assert.equal( seen.filter( e => e.kind === "activate" && e.id === "slot:13" ).length, 1 );
		assert.equal( await lastHover(), null, "without inspect the tooltip leaves with the finger" );

		// The flag turned off under a live inspection releases it.
		await fixture.evaluate( f => f.publish( true ) );
		await page.touchscreen.tap( 70, 50 );
		await settle();
		assert.equal( await lastHover(), "slot:13" );
		await fixture.evaluate( f => f.publish( undefined ) );
		await settle();
		assert.equal( await lastHover(), null, "losing the flag releases the inspection" );

		// Mouse hover is unchanged: over reports, off clears, a click holds nothing.
		await fixture.evaluate( f => f.publish( true ) );
		await reset();
		await page.mouse.move( 70, 50 );
		assert.equal( await lastHover(), "slot:13" );
		await page.mouse.click( 70, 50 );
		await page.mouse.move( 700, 400 );
		assert.equal( await lastHover(), null, "mouse hover ends when the pointer leaves" );
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
