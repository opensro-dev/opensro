/*
===========================================================================

window-position-storage.test.mjs - browser storage failures stay recoverable

The real Platform owner validates stored placement and reports denied writes
without throwing into the runtime frame or pagehide teardown.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "malformed placement and denied writes do not abort Platform teardown", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.route( CLIENT_NEXT_BASE_URL + "/", route =>
			route.fulfill( {
				contentType: "text/html",
				body: "<!doctype html><canvas></canvas><output></output>"
			} ) );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const path = "/src/engine/runtime/platform/platform.ts";
			const { createPlatform } = await import( path );
			const key = "sro:v1150:window-positions:1";
			localStorage.setItem( key, "{malformed" );
			const status = document.querySelector( "output" );
			if ( !status ) throw Error( "Missing fixture status" );
			const events = [];
			const platform = createPlatform(
				document.querySelector( "canvas" ),
				status,
				() => {},
				() => {},
				() => {},
				event => events.push( event )
			);
			const loadStatus = status.value;
			const original = Storage.prototype.setItem;
			try {
				/*
				================
				denyPlacementWrite
				================
				*/
				Storage.prototype.setItem = function denyPlacementWrite( name, value ) {
					if ( name === key ) throw new DOMException( "Fixture denied write", "QuotaExceededError" );
					return original.call( this, name, value );
				};
				platform.saveWindowPositions( { width: 1280, height: 720, windows: { gameGuide: [ 30, 40 ] } } );
				return {
					loadStatus,
					saveStatus: status.value,
					restoredPlacement: events.filter( event => event.kind === "window-positions" ).map( event =>
						event.value
					)
				};
			} finally {
				Storage.prototype.setItem = original;
				localStorage.removeItem( key );
				platform.dispose();
			}
		} );
		assert.match( result.loadStatus, /Window positions could not be restored/ );
		assert.deepEqual( result.restoredPlacement, [ null ] );
		assert.match( result.saveStatus, /Window positions could not be saved.*QuotaExceededError/ );
	} finally {
		await browser.close();
	}
} );
