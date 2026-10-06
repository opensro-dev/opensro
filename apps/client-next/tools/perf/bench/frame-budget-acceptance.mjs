/*
===========================================================================

frame-budget-acceptance.mjs - live video preference and hidden-tab lifecycle

Uses real UI controls, a real minimized Chrome window and authenticated asd2.
Hidden maintenance must keep the world session alive without GPU submissions.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { openClient, closeClient, measure, selectFrameLimit } from "../core/client.mjs";
import { keepGoing } from "./scenarios.mjs";

const OUT = "temp/artifacts/cpu-budget";
const client = await openClient( MISSION_MOVEMENT_FIXTURES.region_cross, {
	uncapped: false,
	frameLimit: 60,
	headed: true
} );
try {
	const { page, browser } = client;
	await selectFrameLimit( page, 120 );
	assert.equal(
		await page.evaluate( () => JSON.parse( localStorage.getItem( "sro:v1150:video-options:1" ) ).frameLimit ),
		120
	);
	await mkdir( OUT, { recursive: true } );
	await page.screenshot( { path: `${OUT}/video-options.png` } );
	await page.locator( '[data-ui-id="option-ok"]' ).click();
	const visible = await measure( page, "ui-selected-120", 4000, more => keepGoing( page, more ) );
	assert.ok( visible.fps > 80 && visible.fps < 124, `UI limit applied: ${visible.fps}` );
	await page.evaluate( () => {
		globalThis.__budgetSubmits = 0;
		const submit = GPUQueue.prototype.submit;
		GPUQueue.prototype.submit = function( ...args ) {
			globalThis.__budgetSubmits++;
			return submit.apply( this, args );
		};
	} );
	const cdp = await browser.newBrowserCDPSession();
	const target = await page.context().newCDPSession( page );
	// Focus emulation belongs to Playwright's own inspector session. A second
	// CDP session cannot release it. This guarded adapter changes only that
	// automation override; document visibility still comes from Chrome.
	const automationSession = page._connection?.toImpl?.( page )?.delegate?._mainFrameSession?._client;
	assert.ok( automationSession, "Update the focus adapter for this Playwright version" );
	await automationSession.send( "Emulation.setFocusEmulationEnabled", { enabled: false } );
	const { targetInfo } = await target.send( "Target.getTargetInfo" );
	const { windowId } = await cdp.send( "Browser.getWindowForTarget", { targetId: targetInfo.targetId } );
	await cdp.send( "Browser.setWindowBounds", { windowId, bounds: { windowState: "minimized" } } );
	console.log(
		"Visibility after tab switch/minimize",
		await page.evaluate( () => document.visibilityState ),
		await cdp.send( "Browser.getWindowBounds", { windowId } )
	);
	await page.waitForFunction( () => document.visibilityState === "hidden", undefined, {
		timeout: 10000,
		polling: 100
	} );
	await page.waitForTimeout( 500 );
	const before = await page.evaluate( () => ({
		submits: globalThis.__budgetSubmits,
		rows: globalThis.__benchRows.length
	}) );
	await page.waitForTimeout( 8000 );
	const hidden = await page.evaluate( () => ({
		visibility: document.visibilityState,
		phase: globalThis.__benchRuntime.sessionState().phase,
		submits: globalThis.__budgetSubmits,
		rows: globalThis.__benchRows.length
	}) );
	assert.equal( hidden.visibility, "hidden" );
	assert.equal( hidden.phase, "world" );
	assert.equal( hidden.submits, before.submits, "hidden maintenance submits no GPU frames" );
	assert.ok( hidden.rows > before.rows, "worker deliveries still drive journal maintenance" );
	await cdp.send( "Browser.setWindowBounds", { windowId, bounds: { windowState: "normal" } } );
	await page.bringToFront();
	await page.waitForFunction( () => document.visibilityState === "visible", undefined, {
		timeout: 10000,
		polling: 100
	} );
	await page.waitForTimeout( 500 );
	const resumed = await measure( page, "resumed-120", 4000, more => keepGoing( page, more ) );
	assert.ok( resumed.fps > 80 && resumed.fps < 124 );
	assert.equal( await page.evaluate( () => globalThis.__benchRuntime.sessionState().phase ), "world" );
	const compact = row => ({
		name: row.name,
		fps: row.fps,
		p95: row.p95,
		p99: row.p99,
		callbacksOver50Ms: row.callbacksOver50Ms
	});
	const result = { visible: compact( visible ), before, hidden, resumed: compact( resumed ) };
	await writeFile( `${OUT}/acceptance.json`, JSON.stringify( result, null, 2 ) );
	console.log( JSON.stringify( result ) );
} finally {
	await closeClient( client );
}
