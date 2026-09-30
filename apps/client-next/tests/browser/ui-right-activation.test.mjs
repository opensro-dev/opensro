/*
===========================================================================

ui-right-activation.test.mjs - captured icon use through real browser input

Exercise production hit testing and pointer lifetime. A release over another
icon, outside the UI or after retirement must never use an unintended item.
===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "right release belongs to the pressed live icon and preserves modifiers", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE } );
	try {
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
			const controls = [
				{ id: "first", label: "First icon", kind: "button", rect: [ 20, 20, 100, 40 ], rightActivate: true },
				{ id: "second", label: "Second icon", kind: "button", rect: [ 160, 20, 100, 40 ], rightActivate: true }
			];
			bridge.present( { title: "Icon input", message: "", controls } );
			return { bridge, controls, events };
		} );
		await page.mouse.move( 50, 40 );
		await page.mouse.down( { button: "right" } );
		await page.mouse.move( 190, 40 );
		await page.mouse.up( { button: "right" } );
		assert.deepEqual( await fixture.evaluate( f => f.events.filter( e => e.kind === "right-activate" ) ), [] );
		await page.keyboard.down( "Shift" );
		await page.mouse.click( 50, 40, { button: "right" } );
		await page.keyboard.up( "Shift" );
		assert.deepEqual( await fixture.evaluate( f => f.events.filter( e => e.kind === "right-activate" ) ), [
			{ kind: "right-activate", id: "first", shift: true, ctrl: false, alt: false }
		] );
		for ( const cancellation of [ "outside", "disabled", "retired", "blur", "cancel" ] ) {
			await fixture.evaluate( f => {
				f.events.length = 0;
				f.bridge.present( { title: "Icon input", message: "", controls: f.controls } );
			} );
			await page.mouse.move( 50, 40 );
			await page.mouse.down( { button: "right" } );
			if ( cancellation === "outside" ) await page.mouse.move( 500, 400 );
			if ( cancellation === "disabled" ) {
				await fixture.evaluate( f =>
					f.bridge.present( {
						title: "Icon input",
						message: "",
						controls: f.controls.map( c => ({ ...c, disabled: true }) )
					} )
				);
			}
			if ( cancellation === "retired" ) {
				await fixture.evaluate( f => {
					f.bridge.present( { title: "Icon input", message: "", controls: [] } );
					f.bridge.present( { title: "Icon input", message: "", controls: f.controls } );
				} );
			}
			if ( cancellation === "blur" ) await page.evaluate( () => window.dispatchEvent( new Event( "blur" ) ) );
			if ( cancellation === "cancel" ) {
				await page.evaluate( () => window.dispatchEvent( new PointerEvent( "pointercancel" ) ) );
			}
			await page.mouse.up( { button: "right" } );
			assert.deepEqual(
				await fixture.evaluate( f => f.events.filter( e => e.kind === "right-activate" ) ),
				[],
				cancellation
			);
		}
		await fixture.evaluate( f => {
			f.events.length = 0;
			f.bridge.present( {
				title: "Icon input",
				message: "",
				controls: f.controls.map( c => ({ ...c, draggable: true }) )
			} );
		} );
		await page.mouse.move( 50, 40 );
		await page.mouse.down( { button: "left" } );
		await page.mouse.move( 60, 40 );
		await page.mouse.down( { button: "right" } );
		await page.mouse.up( { button: "right" } );
		await page.mouse.up( { button: "left" } );
		const chord = await fixture.evaluate( f => f.events );
		assert.equal(
			chord.filter( e => e.kind === "right-activate" ).length,
			1,
			"right button cancels a captured left drag"
		);
		assert.equal(
			chord.filter( e => e.kind === "drag-end" || e.kind === "activate" ).length,
			0,
			"cancelled carry cannot later drop or activate"
		);
		await fixture.evaluate( f => f.bridge.dispose() );
	} finally {
		await browser.close();
	}
} );
