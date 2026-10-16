/*
===========================================================================
probe-specialty.mjs - current production Shop/COS mount and close behavior

Verifies the former duplicate-close blocker in the actual specialty recipe.
The guard challenge is reported separately from the scenario's network counts.
===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { UI_QA_ORIGIN } from "./settings.mjs";
import { artifactDirectory, guardReviewRequests, isLocalReviewRequest } from "./probe-support.mjs";
import { fetchCatalogIdentity } from "./catalog-client.mjs";

const directory = artifactDirectory( "specialty" );
await mkdir( directory, { recursive: true } );
const { browser, page } = await launchProbeBrowser( { viewport: { width: 1500, height: 1150 } } );
const blockedAttempts = [], observedExternal = [], errors = [], evidence = [];
page.on( "pageerror", error => errors.push( String( error ) ) );
page.on( "request", request => {
	if ( /^https?:/.test( request.url() ) && !isLocalReviewRequest( request.url() ) ) {
		observedExternal.push( request.url() );
	}
} );
await guardReviewRequests( page.context(), blockedAttempts );
let outcome = "incomplete", identity, guardChallenge;

try {
	identity = await fetchCatalogIdentity();
	await page.goto( UI_QA_ORIGIN + "/" );
	await page.waitForFunction( () =>
		document.querySelector( "#runtime-status" )?.textContent?.includes( "controls" )
	);
	await page.locator( '[data-journey-id="breadth:specialty-buy"]' ).click();
	const frame = await (await page.locator( "#viewport-frame" ).elementHandle()).contentFrame();
	await frame.waitForURL( new URL( await page.locator( "#viewport-frame" ).getAttribute( "src" ), page.url() ).href );
	await frame.waitForFunction(
		() => {
			const report = window.__uiQa?.snapshot();
			return report?.error || report?.coverage?.admitted && !report.entry?.busy && report.stats.pending === 0;
		},
		null,
		{ timeout: 60000 }
	);
	const mounted = await frame.evaluate( () => window.__uiQa.snapshot() );
	assert.equal( mounted.id, "breadth:specialty-buy" );
	assert.equal( mounted.error, null );
	assert.deepEqual( mounted.stats.failed, [] );
	assert.equal( mounted.coverage.admitted, true );
	const ids = mounted.controls.map( row => row.id );
	for (
		const id of [
			"close-window:Shop",
			"close-window:COS inventory",
			"specialty-deal-count",
			"specialty-deal-close"
		]
	) assert.ok( ids.includes( id ), id );
	assert.equal( new Set( ids ).size, ids.length );
	assert.equal( ids.includes( "close" ), false );
	evidence.push( { name: "specialty-mounted", report: mounted } );
	await page.locator( "#viewport-frame" ).screenshot( { path: `${directory}/mounted.png` } );
	await frame.locator( '[data-ui-id="specialty-deal-close"]' ).click();
	await frame.locator( '[data-ui-id="close-window:COS inventory"]' ).click();
	await frame.waitForFunction( () =>
		!window.__uiQa.snapshot().controls.some( row => row.id.startsWith( "close-window:" ) )
	);
	const closed = await frame.evaluate( () => window.__uiQa.snapshot() );
	assert.ok(
		closed.trace.some( row => row.kind === "unsupported integration" && row.detail?.command?.kind === "npc-close" )
	);
	evidence.push( { name: "transport-close-ends-local-trade", report: closed } );
	assert.deepEqual( observedExternal, [] );
	assert.deepEqual( blockedAttempts, [] );
	assert.deepEqual( errors, [] );
	const scenarioNetwork = { observedExternal: [ ...observedExternal ], blockedAttempts: [ ...blockedAttempts ] };
	// Deliberately attempt another loopback port from the owned page.
	// This proves actual request prevention; zero requests in the scenario alone
	// would only prove that the scenario did not attempt an external connection.
	const target = "http://127.0.0.1:5190/__uiqa-guard-challenge";
	const failed = page.waitForEvent( "requestfailed" );
	await assert.rejects( page.goto( target ), /ERR_BLOCKED_BY_CLIENT/ );
	const failure = (await failed).failure();
	assert.deepEqual( blockedAttempts, [ target ] );
	guardChallenge = { target, failure, blockedAttempts: [ ...blockedAttempts ], scenarioNetwork };
	assert.equal( (await page.request.get( UI_QA_ORIGIN + "/api/health" )).status(), 403 );
	assert.equal( (await page.request.get( UI_QA_ORIGIN + "/shard/test" )).status(), 403 );
	assert.equal( (await fetchCatalogIdentity()).fingerprint, identity.fingerprint );
	outcome = "passed";
	console.log(
		"PASS specialty mount, distinct closes, local trade close, denied game routes and real request guard"
	);
} catch ( error ) {
	errors.push( String( error ) );
	throw error;
} finally {
	await writeFile(
		`${directory}/evidence.json`,
		JSON.stringify(
			{ outcome, identity, errors, observedExternal, blockedAttempts, guardChallenge, evidence },
			null,
			2
		)
	);
	await browser.close();
}
