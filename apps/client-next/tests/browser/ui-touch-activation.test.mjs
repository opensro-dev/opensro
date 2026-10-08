/*
===========================================================================

ui-touch-activation.test.mjs - mobile secondary use through the real DOM bridge

Published metadata admits touch holds for scrolls, potions and equipment alike.
Real touch input must preserve taps/carry and cancel holds across lifetimes.
===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const HOLD_WAIT_MS = 650;

test( "touch hold uses secondary metadata without a primary click or carry", { timeout: 45000 }, async () => {
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
			const controls = [ "Return Scroll", "Potion", "Equipment", "Unpublished" ].map( ( label, i ) => ({
				id: "slot:" + (13 + i),
				label,
				kind: "button",
				rect: [ 20 + i * 120, 20, 100, 60 ],
				rightActivate: i < 3,
				draggable: true,
				carry: true
			}) );
			bridge.present( { title: "Touch use", message: "", controls } );
			return { bridge, controls, events };
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
		for ( let i = 0; i < 3; i++ ) {
			await fixture.evaluate( f => {
				f.events.length = 0;
			} );
			await touch( "touchStart", [ [ 1, 50 + i * 120, 50 ] ] );
			await touch( "touchMove", [ [ 1, 52.5 + i * 120, 51.5 ] ] );
			await page.waitForTimeout( HOLD_WAIT_MS );
			assert.equal(
				await fixture.evaluate( f => f.events.filter( e => e.kind === "right-activate" ).length ),
				1,
				"the hold commits before testing later pointer movement"
			);
			await touch( "touchMove", [ [ 1, 54.5 + i * 120, 51.5 ] ] );
			await page.evaluate( () =>
				new Promise( resolve => {
					requestAnimationFrame( () => requestAnimationFrame( () => resolve( null ) ) );
				} )
			);
			await touch( "touchEnd", [] );
			const events = await fixture.evaluate( f => f.events );
			assert.deepEqual( events.filter( e => e.kind === "right-activate" ), [ {
				kind: "right-activate",
				id: "slot:" + (13 + i),
				shift: false,
				ctrl: false,
				alt: false
			} ] );
			assert.deepEqual(
				events.filter( e => [ "activate", "drag", "drag-end" ].includes( e.kind ) ),
				[],
				"moving two pixels after a committed hold cannot click, lift or drop"
			);
			// No Escape, mouse move or intervening press: the next press on the
			// same icon must clear the hold's primary-click suppression itself.
			await page.touchscreen.tap( 50 + i * 120, 50 );
			assert.equal(
				await fixture.evaluate(
					( f, id ) => f.events.filter( e => e.kind === "activate" && e.id === id ).length,
					"slot:" + (13 + i)
				),
				1,
				"the first same-slot tap after a hold activates exactly once"
			);
			await page.touchscreen.tap( 700, 400 );
		}
		await fixture.evaluate( f => {
			f.events.length = 0;
		} );
		await page.mouse.click( 50, 50 );
		assert.equal(
			await fixture.evaluate( f => f.events.filter( e => e.kind === "activate" && e.id === "slot:13" ).length ),
			1,
			"retained touch-click history does not swallow a subsequent mouse click"
		);
		await page.mouse.click( 700, 400 );
		for (
			const cancellation of [
				"move",
				"second",
				"cancel",
				"disabled",
				"metadata",
				"retired",
				"blur",
				"unpublished"
			]
		) {
			await fixture.evaluate( f => {
				f.events.length = 0;
				f.bridge.present( { title: "Touch use", message: "", controls: f.controls } );
			} );
			const x = cancellation === "unpublished" ? 410 : 50;
			await touch( "touchStart", [ [ 1, x, 50 ] ] );
			if ( cancellation === "move" ) await touch( "touchMove", [ [ 1, 70, 50 ] ] );
			if ( cancellation === "second" ) await touch( "touchStart", [ [ 1, 50, 50 ], [ 2, 700, 400 ] ] );
			if ( cancellation === "cancel" ) await touch( "touchCancel", [] );
			if ( cancellation === "blur" ) await page.evaluate( () => window.dispatchEvent( new Event( "blur" ) ) );
			if ( [ "disabled", "metadata", "retired" ].includes( cancellation ) ) {
				await fixture.evaluate( ( f, reason ) => {
					f.bridge.present( {
						title: "Touch use",
						message: "",
						controls: reason === "retired" ?
							[] :
							f.controls.map( c => ({ ...c, disabled: reason === "disabled", rightActivate: false }) )
					} );
				}, cancellation );
			}
			await page.waitForTimeout( HOLD_WAIT_MS );
			if ( cancellation !== "cancel" ) await touch( "touchEnd", [] );
			assert.deepEqual(
				await fixture.evaluate( f => f.events.filter( e => e.kind === "right-activate" ) ),
				[],
				cancellation
			);
			if ( cancellation !== "unpublished" ) {
				assert.deepEqual(
					await fixture.evaluate( f => f.events.filter( e => e.kind === "activate" ) ),
					[],
					cancellation + " cannot fall back to a primary tap"
				);
			}
			await page.keyboard.press( "Escape" );
		}
		await fixture.evaluate( f => {
			f.events.length = 0;
			f.bridge.present( { title: "Touch use", message: "", controls: f.controls } );
		} );
		await page.touchscreen.tap( 50, 50 );
		await page.touchscreen.tap( 700, 400 );
		const tap = await fixture.evaluate( f => f.events );
		assert.equal( tap.filter( e => e.kind === "activate" && e.id === "slot:13" ).length, 1 );
		assert.equal(
			tap.filter( e => e.kind === "drag-end" && e.id === "slot:13" ).length,
			1,
			"ordinary tap still carries and the next press drops"
		);
		await fixture.evaluate( f => {
			f.events.length = 0;
		} );
		await touch( "touchStart", [ [ 1, 410, 50 ] ] );
		await touch( "touchMove", [ [ 1, 412, 51 ] ] );
		await touch( "touchEnd", [] );
		await page.waitForTimeout( HOLD_WAIT_MS );
		assert.equal(
			await fixture.evaluate( f => f.events.filter( e => e.kind === "activate" && e.id === "slot:16" ).length ),
			1,
			"plain metadata button admits jitter and deduplicates the later compatibility click"
		);
		await page.keyboard.press( "Escape" );
		await fixture.evaluate( f => {
			f.events.length = 0;
		} );
		await touch( "touchStart", [ [ 1, 410, 50 ] ] );
		for ( const y of [ 70, 110, 150 ] ) {
			await touch( "touchMove", [ [ 1, 410, y ] ] );
			await page.evaluate( () => new Promise( resolve => requestAnimationFrame( () => resolve( null ) ) ) );
		}
		await touch( "touchEnd", [] );
		const dragged = await fixture.evaluate( f => f.events );
		assert.equal(
			dragged.filter( e => e.kind === "drag-end" && e.id === "slot:16" ).length,
			1,
			"draggable metadata without rightActivate retains the touch stream through a vertical drag"
		);
		assert.deepEqual(
			dragged.filter( e => [ "drag-cancel", "activate", "right-activate" ].includes( e.kind ) ),
			[]
		);
		await fixture.evaluate( f => {
			f.events.length = 0;
		} );
		await touch( "touchStart", [ [ 1, 50, 50 ] ] );
		await fixture.evaluate( f => f.bridge.dispose() );
		await page.waitForTimeout( HOLD_WAIT_MS );
		await touch( "touchEnd", [] );
		assert.deepEqual( await fixture.evaluate( f => f.events.filter( e => e.kind === "right-activate" ) ), [] );
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
