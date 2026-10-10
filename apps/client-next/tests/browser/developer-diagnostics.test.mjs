/*
===========================================================================

developer-diagnostics.test.mjs - developer opt-in through the shipped HUD

Exercises browser storage, real pointer input, reload, viewport placement
and disposal. The build endpoint is controlled to test its displayed state.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

/*
================
ready
================
*/
async function ready( page ) {
	await page.waitForFunction( () => typeof window.sroDebug?.setDiagnostics === "function" );
	await page.waitForFunction( () => document.querySelector( "output" )?.textContent?.includes( "runtime: running" ) );
}

test(
	"tester HUD stays minimal; developer icon persists but its panel stays closed after reload",
	{ timeout: 90000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		const errors = [];
		page.on( "pageerror", error => errors.push( error.message ) );
		let requests = 0;
		await page.route( "**/title/build", route => {
			requests++;
			return route.fulfill( {
				json: { build: { revision: "abcdef0123456789", subject: "Fixture Agent build", uptimeSeconds: 42 } }
			} );
		} );
		try {
			await page.goto( CLIENT_NEXT_BASE_URL );
			await ready( page );
			const movement = await page.evaluate( () => window.sroDebug?.dumpMovement() );
			assert.ok( movement && typeof movement === "object" && "version" in movement && "events" in movement );
			assert.equal( movement.version, 1 );
			assert.ok( Array.isArray( movement.events ) );
			const dump = await page.evaluate( () => window.sroDebug?.dumpAssets() );
			assert.ok( dump && typeof dump === "object" && "assets" in dump );
			const assets = dump.assets;
			assert.ok( assets && typeof assets === "object" && "jobs" in assets && "available" in assets );
			assert.ok( Array.isArray( assets.jobs ) );
			assert.equal( assets.available, 4 - assets.jobs.length );

			assert.equal( await page.locator( "#developer-toggle" ).isVisible(), false );
			await page.locator( "#fps-toggle" ).click();
			await page.waitForFunction( () =>
				/FPS · .* ms$/.test( document.getElementById( "fps-readout" )?.textContent ?? "" )
			);
			assert.doesNotMatch( await page.locator( "#fps-readout" ).innerText(), /Client|Agent|cpu|draws/i );
			assert.equal( requests, 0 );
			await page.evaluate( () => window.sroDebug?.setDiagnostics( true ) );
			const fps = await page.locator( "#fps-toggle" ).boundingBox();
			const developer = await page.locator( "#developer-toggle" ).boundingBox();
			assert.ok( developer && fps && developer.x + developer.width <= fps.x && developer.y === fps.y );
			await page.locator( "#developer-toggle" ).click();
			await page.waitForFunction( () =>
				document.getElementById( "developer-readout" )?.textContent?.includes( "Agent abcdef0" )
			);
			assert.equal( await page.locator( "#fps-readout" ).isVisible(), false );
			assert.match( await page.locator( "#developer-readout" ).innerText(), /Fixture Agent build/ );
			assert.equal( requests, 1 );
			await mkdir( "temp/artifacts/developer-diagnostics", { recursive: true } );
			await page.screenshot( { path: "temp/artifacts/developer-diagnostics/developer.png" } );
			await page.reload();
			await ready( page );
			assert.equal( await page.locator( "#developer-toggle" ).isVisible(), true );
			assert.equal( await page.locator( "#developer-readout" ).isVisible(), false );
			assert.equal( requests, 1 );
			await page.setViewportSize( { width: 640, height: 480 } );
			await page.locator( "#developer-toggle" ).click();
			await page.waitForFunction( () =>
				document.getElementById( "developer-readout" )?.textContent?.includes( "Agent abcdef0" )
			);
			const panel = await page.locator( "#developer-readout" ).boundingBox();
			assert.ok(
				panel && panel.x >= 0 && panel.y >= 0 && panel.x + panel.width <= 640 && panel.y + panel.height <= 480
			);
			await page.screenshot( { path: "temp/artifacts/developer-diagnostics/compact.png" } );
			await page.evaluate( () => window.sroDebug?.setDiagnostics( false ) );
			assert.equal( await page.locator( "#developer-toggle" ).isVisible(), false );
			assert.equal( await page.locator( "#developer-readout" ).isVisible(), false );
			await page.reload();
			await ready( page );
			assert.equal( await page.locator( "#developer-toggle" ).isVisible(), false );
			await page.locator( "#fps-toggle" ).click();
			await page.screenshot( { path: "temp/artifacts/developer-diagnostics/player.png" } );
			await page.evaluate( async () => {
				const { runtime } = await import( "/src/bootstrap.ts" );
				runtime.dispose();
			} );
			assert.equal( await page.evaluate( () => window.sroDebug ), undefined );
			assert.equal( await page.locator( "#developer-toggle" ).count(), 0 );
			assert.deepEqual( errors, [] );
		} finally {
			await browser.close();
		}
	}
);

test( "console opt-in works when local storage is blocked", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.addInitScript( () => {
			Storage.prototype.getItem = () => {
				throw new DOMException( "Blocked", "SecurityError" );
			};
			Storage.prototype.setItem = () => {
				throw new DOMException( "Blocked", "SecurityError" );
			};
		} );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await ready( page );
		await page.evaluate( () => window.sroDebug?.setDiagnostics( true ) );
		assert.equal( await page.locator( "#developer-toggle" ).isVisible(), true );
		await page.evaluate( () => window.sroDebug?.setDiagnostics( false ) );
		assert.equal( await page.locator( "#developer-toggle" ).isVisible(), false );
	} finally {
		await browser.close();
	}
} );

/*
================
Legacy console preference migration
================
*/
test( "legacy opt-in migrates without overriding an explicit Experimental choice", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.addInitScript( () => localStorage.setItem( "sro.developerDiagnostics", "true" ) );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await ready( page );
		assert.equal( await page.locator( "#developer-toggle" ).isVisible(), true );
		assert.equal( await page.locator( "#developer-readout" ).isVisible(), false );
		await page.evaluate( () => window.sroDebug?.setDiagnostics( false ) );
		assert.equal( await page.evaluate( () => localStorage.getItem( "sro.developerDiagnostics" ) ), null );
		const stored = await page.evaluate( () =>
			JSON.parse( localStorage.getItem( "sro:v1150:experimental-options:1" ) ?? "null" )
		);
		assert.equal( stored.developerDiagnostics, false );
		assert.equal( stored.chatTimestamps, false );
		await page.reload();
		await ready( page );
		assert.equal(
			await page.locator( "#developer-toggle" ).isVisible(),
			false,
			"canonical off wins even when a legacy key remains"
		);
	} finally {
		await browser.close();
	}
} );
