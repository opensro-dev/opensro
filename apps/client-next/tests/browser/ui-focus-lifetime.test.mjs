/*
===========================================================================

ui-focus-lifetime.test.mjs - pointer gestures retire the previous UI focus

Use real Chromium focus and pointer capture against the production bridge.
Map dragging is the reported case; the same rule must cover world clicks
without breaking text selection or keyboard navigation.

===========================================================================
*/

import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"map drag and world clicks retire old button focus while text edits retain selection",
	{ timeout: 45000 },
	async () => {
		const { browser, page } = await launchProbeBrowser( {
			executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE
		} );
		try {
			await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
			const observed = await page.evaluateHandle( async () => {
				const modulePath = "/src/engine/runtime/platform/ui/ui.ts";
				const { createUiBridge } = await import( modulePath );
				const canvas = /** @type {HTMLCanvasElement} */ (document.getElementById( "world" ));
				const events = [], bridge = createUiBridge( canvas, event => events.push( event ), () => {} );
				bridge.present( {
					title: "Regression controls",
					message: "",
					controls: [
						{ id: "map-follow", label: "Automatic movement", kind: "button", rect: [ 20, 20, 160, 30 ] },
						{ id: "map-pan", label: "Map", kind: "region", rect: [ 20, 80, 300, 200 ], draggable: true },
						{ id: "chat", label: "Chat", kind: "text", value: "Silkroad", rect: [ 20, 320, 250, 30 ] }
					]
				} );
				return events;
			} );
			const button = page.locator( '[data-ui-id="map-follow"]' );
			const input = page.locator( '[data-ui-id="chat"]' );
			await button.click();
			assert.equal( await button.evaluate( element => element === document.activeElement ), true );
			await page.mouse.move( 120, 140 );
			await page.mouse.down();
			await page.mouse.move( 180, 190, { steps: 4 } );
			await page.mouse.up();
			assert.equal( await button.evaluate( element => element === document.activeElement ), false );
			await button.click();
			await page.mouse.click( 600, 450 );
			assert.equal( await button.evaluate( element => element === document.activeElement ), false );
			await input.click();
			await input.evaluate( element => {
				/** @type {HTMLInputElement} */ (element).setSelectionRange( 1, 4, "backward" );
				element.dispatchEvent( new Event( "input", { bubbles: true } ) );
			} );
			assert.deepEqual(
				await input.evaluate( element => {
					const edit = /** @type {HTMLInputElement} */ (element);
					return [ edit === document.activeElement, edit.selectionStart, edit.selectionEnd ];
				} ),
				[ true, 1, 4 ]
			);
			assert.deepEqual(
				await observed.evaluate( events => {
					const edit = events.findLast( event => event.kind === "edit" );
					return [ edit.start, edit.end, edit.direction ];
				} ),
				[ 1, 4, "backward" ],
				"the bridge keeps normalized endpoints and the browser's active selection direction"
			);
			await page.keyboard.press( "Tab" );
			assert.equal( await input.evaluate( element => element === document.activeElement ), false );
		} finally {
			await browser.close();
		}
	}
);
