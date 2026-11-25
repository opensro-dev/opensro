/*
===========================================================================

creation-slider-wheel.test.mjs - creation sliders take no wheel input

#4121: a focused range input steps on the mouse wheel, so scrolling while
a creation slider held focus silently re-picked the choice (daggers became
the darkstaff, and its robe-only protector followed). The bridge now
refuses the wheel on range elements; this drives the live creation screen
through the same gestures and asserts the weapon choice only moves through
its own authored controls (prev/next buttons, fills, drags).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { resolveProbeCredentials } from "../../../../scripts/lib/probeSession.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "the weapon slider ignores the wheel and keeps its authored gestures", { timeout: 90000 }, async () => {
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

		// Scroll over the focused slider in both directions: the choice must
		// not move (before the fix each gesture stepped it, 1 -> 6 included).
		const box = await control( "create:weapon" ).boundingBox();
		assert.ok( box );
		await control( "create:weapon" ).click();
		await page.mouse.move( box.x + box.width / 2, box.y + box.height / 2 );
		await page.mouse.wheel( 0, -600 );
		await page.mouse.wheel( 0, 600 );
		await page.mouse.wheel( 0, -600 );
		await page.waitForTimeout( 500 );
		assert.equal( await control( "create:weapon" ).inputValue(), "1" );

		// The authored gestures still move it: a fill (the drag's event path)
		// and the prev button.
		await control( "create:weapon" ).fill( "6" );
		assert.equal( await control( "create:weapon" ).inputValue(), "6" );
		await control( "create:weapon:prev" ).click();
		assert.equal( await control( "create:weapon" ).inputValue(), "5" );

		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
