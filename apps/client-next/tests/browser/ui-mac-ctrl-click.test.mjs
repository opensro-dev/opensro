/*
===========================================================================

ui-mac-ctrl-click.test.mjs - a Mac's Ctrl+click reaches the CTRL item action

macOS turns Ctrl+click into the system's secondary click: the browser
presses the primary button with Ctrl held, sends contextmenu and no click.
The bridge must still report the CTRL activation (storage moves, quick
sale), once, and a true right press must stay a right press. The Mac
sequence is replayed as events, since the test browser runs elsewhere.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "a Mac Ctrl+click activates the slot with CTRL, once", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE } );
	try {
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const fixture = await page.evaluateHandle( async () => {
			const path = "/src/engine/runtime/platform/ui/ui.ts";
			const { createUiBridge } = await import( path );
			const events = [];
			const canvas = document.querySelector( "canvas" );
			if ( !canvas ) throw Error( "Fixture page has no canvas" );
			const bridge = createUiBridge( canvas, event => events.push( event ), () => {} );
			bridge.present( {
				title: "Storage",
				message: "",
				controls: [ {
					id: "slot:13",
					label: "slot:13",
					kind: "button",
					rect: [ 20, 20, 32, 32 ],
					draggable: true,
					carry: true,
					rightActivate: true
				} ]
			} );
			return { events };
		} );
		const replay = sequence =>
			page.evaluate( steps => {
				const target = document.querySelector( "[data-ui-id='slot:13']" );
				if ( !target ) throw Error( "slot:13 is not presented" );
				const at = { clientX: 36, clientY: 36, bubbles: true, cancelable: true, composed: true };
				for ( const step of steps ) {
					const init = { ...at, button: step.button, buttons: step.button === 2 ? 2 : 1, ctrlKey: step.ctrl };
					const event = step.type.startsWith( "pointer" ) ?
						new PointerEvent( step.type, {
							...init,
							pointerId: 1,
							pointerType: "mouse",
							isPrimary: true
						} ) :
						new MouseEvent( step.type, { ...init, detail: 1 } );
					target.dispatchEvent( event );
				}
			}, sequence );
		const activations = () =>
			fixture.evaluate( f => {
				const seen = f.events.filter( e => e.kind === "activate" || e.kind === "right-activate" );
				f.events.length = 0;
				return seen;
			} );

		// macOS: the primary press with Ctrl, then contextmenu, and no click.
		await replay( [
			{ type: "pointerdown", button: 0, ctrl: true },
			{ type: "contextmenu", button: 0, ctrl: true },
			{ type: "pointerup", button: 0, ctrl: true }
		] );
		let seen = await activations();
		assert.deepEqual( seen.map( e => [ e.kind, e.id, e.ctrl ] ), [ [ "activate", "slot:13", true ] ] );

		// A browser that also sends the click still activates once.
		await replay( [
			{ type: "pointerdown", button: 0, ctrl: true },
			{ type: "contextmenu", button: 0, ctrl: true },
			{ type: "pointerup", button: 0, ctrl: true },
			{ type: "click", button: 0, ctrl: true }
		] );
		seen = await activations();
		assert.equal( seen.filter( e => e.kind === "activate" ).length, 1, "the trailing click repeated the action" );

		// A true right press is a right press, never a CTRL activation.
		await replay( [ { type: "pointerdown", button: 2, ctrl: true }, {
			type: "contextmenu",
			button: 2,
			ctrl: true
		} ] );
		seen = await activations();
		assert.equal( seen.filter( e => e.kind === "activate" ).length, 0, "a right press activated the slot" );

		// An ordinary Ctrl+click elsewhere (a click, no contextmenu) is unchanged.
		await replay( [ { type: "pointerdown", button: 0, ctrl: true }, { type: "pointerup", button: 0, ctrl: true }, {
			type: "click",
			button: 0,
			ctrl: true
		} ] );
		seen = await activations();
		assert.deepEqual( seen.map( e => [ e.kind, e.ctrl ] ), [ [ "activate", true ] ] );
	} finally {
		await browser.close();
	}
} );
