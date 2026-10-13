/*
===========================================================================

layout-keys.test.mjs - the UI bridge hands hotkeys over as the layout's letter

Native binds WM_KEYDOWN virtual keys, which Windows assigns through the
active layout, so on AZERTY the key printed A is VK_A. A real KeyboardEvent
at the bridge, physical code KeyQ typing "a", must reach the HUD and the
held keys as KeyA, and come up as the same letter even when the key up
reports another character.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "an AZERTY A reaches the HUD and the held keys as KeyA", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createUiBridge } = await import( "/src/engine/runtime/platform/ui/ui.ts" );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const events = [], held = [];
			const bridge = createUiBridge( canvas, event => events.push( event ), () => {}, ( code, down ) => {
				held.push( [ code, down ] );
			} );
			try {
				bridge.present( {
					title: "Probe",
					message: "",
					controls: [ { id: "probe-key", kind: "button", label: "Probe", rect: [ 20, 20, 100, 24 ] } ]
				} );
				const button = document.querySelector( '[data-ui-id="probe-key"]' );
				if ( !button ) throw new Error( "the bridge drew no probe button" );
				button.dispatchEvent( new KeyboardEvent( "keydown", { bubbles: true, code: "KeyQ", key: "a" } ) );
				// Shift released before the key: the up reports "q".
				button.dispatchEvent( new KeyboardEvent( "keyup", { bubbles: true, code: "KeyQ", key: "q" } ) );
				return { keys: events.filter( event => event.kind === "key" ).map( event => event.code ), held };
			} finally {
				bridge.dispose();
				canvas.remove();
			}
		} );
		assert.deepEqual( result.keys, [ "KeyA" ] );
		assert.deepEqual( result.held, [ [ "KeyA", true ], [ "KeyA", false ] ] );
	} finally {
		await browser.close();
	}
} );
