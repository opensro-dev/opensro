/*
===========================================================================

creation-slider-groove.test.mjs - creation sliders ignore groove presses

#4121: the creation sliders are invisible range inputs laid over the
drawn groove between the prev/next arrows. A browser answers a press
anywhere on that groove by jumping the value to the press point, so a
click two-thirds along the weapon groove silently re-picked a new
character's daggers as the darkstaff (and its robe-only protector
followed). The bridge now refuses groove presses on creation sliders:
the choice moves only through its authored gestures (the drawn arrows
and the thumb drag), which this test drives against the live screen.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { resolveProbeCredentials } from "../../../../scripts/lib/probeSession.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "the weapon slider ignores its groove and keeps its authored gestures", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } ),
		control = id => page.locator( `[data-ui-id="${id}"]` ),
		errors = [];
	try {
		await holdProbeRuntime( page );
		page.on( "pageerror", e => errors.push( e.message ) );
		// Admission is read-only; no creation, deletion or world-entry requests are needed.
		await page.route( "**/character/list", async route => {
			const response = await route.fetch(), body = await response.json();
			await route.fulfill( { response, json: { ...body, characters: [] } } );
		} );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await control( "frontend:reveal" ).click( { timeout: 30000 } );
		await control( "login" ).waitFor();
		await page.waitForFunction( () =>
			!/** @type {HTMLInputElement | HTMLButtonElement | null} */ (document.querySelector(
				'[data-ui-id="login"]'
			))?.disabled
		);
		const { loginId, loginPassword } = resolveProbeCredentials();
		await control( "account" ).fill( loginId );
		await control( "password" ).fill( loginPassword );
		await control( "password" ).press( "Enter" );
		await control( "frontend:create" ).click( { timeout: 30000 } );
		await page.waitForFunction( () =>
			/Frontend: create\n/.test( document.querySelector( "output" )?.textContent ?? "" )
		);
		await page.mouse.click( 260, 280 );
		await control( "create:weapon" ).waitFor( { timeout: 30000 } );
		await page.waitForFunction( () =>
			!/** @type {HTMLInputElement | HTMLButtonElement | null} */ (document.querySelector(
				'[data-ui-id="create:weapon"]'
			))?.disabled
		);

		// The deliberate choice: daggers, Europe weapon choice 1.
		await control( "create:weapon" ).fill( "1" );
		assert.equal( await control( "create:weapon" ).inputValue(), "1" );

		// A press two-thirds along the groove (where choice 6 lives) must
		// not re-pick the weapon, and must not even focus the slider.
		const box = await control( "create:weapon" ).boundingBox();
		assert.ok( box );
		const midY = box.y + box.height / 2;
		await page.mouse.click( box.x + box.width * 0.67, midY );
		await page.waitForTimeout( 500 );
		assert.equal( await control( "create:weapon" ).inputValue(), "1" );
		assert.equal(
			await page.evaluate( () => document.activeElement?.getAttribute( "data-ui-id" ) ),
			null
		);

		// The authored gestures still move it: a drag from the thumb.
		const thumbX = box.x + box.width / 9;
		await page.mouse.move( thumbX, midY );
		await page.mouse.down();
		await page.mouse.move( thumbX + 33, midY, { steps: 5 } );
		await page.mouse.up();
		await page.waitForTimeout( 300 );
		const dragged = await control( "create:weapon" ).inputValue();
		assert.notEqual( dragged, "1" );

		// And the drawn arrows step it one choice at a time.
		await control( "create:weapon:prev" ).click();
		assert.equal( await control( "create:weapon" ).inputValue(), String( Number( dragged ) - 1 ) );
		await control( "create:weapon:next" ).click();
		assert.equal( await control( "create:weapon" ).inputValue(), dragged );

		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
