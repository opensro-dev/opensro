/*
===========================================================================
probe-reachability.mjs - meaningful input checks for the added owner paths

Real browser controls and 3D picking, with captured commands only. Presentation
success never marks a human review or certifies native/server behavior.
===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { UI_QA_ORIGIN } from "./settings.mjs";
import { artifactDirectory, guardReviewRequests } from "./probe-support.mjs";
import { fetchCatalogIdentity } from "./catalog-client.mjs";
const directory = artifactDirectory( "reachability" );
await mkdir( directory, { recursive: true } );
const { browser, page } = await launchProbeBrowser( { viewport: { width: 1500, height: 1150 } } );
const errors = [], external = [], evidence = [];
let outcome = "incomplete", identity;
page.on( "pageerror", error => errors.push( String( error ) ) );
await guardReviewRequests( page.context(), external );

/*
================
choose
================
*/
async function choose( id ) {
	await page.locator( `[data-journey-id="${id}"]` ).click();
	const frame = await (await page.locator( "#viewport-frame" ).elementHandle()).contentFrame();
	await frame.waitForURL( new URL( await page.locator( "#viewport-frame" ).getAttribute( "src" ), page.url() ).href );
	await frame.waitForFunction(
		() => {
			const report = window.__uiQa?.snapshot();
			return report?.error || report?.coverage?.admitted && !report.entry?.busy;
		},
		null,
		{ timeout: 120000 }
	);
	const value = await frame.evaluate( () => window.__uiQa.snapshot() );
	assert.equal( value.error, null );
	assert.deepEqual( value.stats.failed, [] );
	return frame;
}

/*
================
capture
================
*/
async function capture( frame, name ) {
	evidence.push( { name, snapshot: await frame.evaluate( () => window.__uiQa.snapshot() ) } );
	await page.locator( "#viewport-frame" ).screenshot( { path: `${directory}/${name}.png` } );
}

try {
	await page.goto( UI_QA_ORIGIN + "/" );
	await page.waitForFunction( () =>
		document.querySelector( "#runtime-status" )?.textContent?.includes( "controls" )
	);
	identity = await fetchCatalogIdentity();
	let frame = await choose( "reach:party-member" );
	assert.equal( await frame.locator( '[data-ui-id="party-member-kick:12"]' ).count(), 0 );
	assert.ok( await frame.locator( '[data-ui-id="party-disband"]' ).isDisabled() );
	frame = await choose( "reach:party-leader" );
	await frame.locator( '[data-ui-id="party-member-kick:12"]' ).click();
	await frame.waitForFunction( () =>
		window.__uiQa.snapshot().trace.some( row => row.detail?.command?.kind === "party-kick" )
	);
	let command = await frame.evaluate( () =>
		window.__uiQa.snapshot().trace.find( row => row.detail?.command?.kind === "party-kick" )
	);
	assert.equal( command.kind, "unsupported integration" );
	assert.equal( command.detail.command.id, 12 );
	await capture( frame, "party-command-captured" );
	frame = await choose( "reach:quest-abandon" );
	await frame.locator( '[data-ui-id="quest-abandon-no"]' ).click();
	assert.equal(
		(await frame.evaluate( () => window.__uiQa.snapshot().trace )).some( row =>
			row.detail?.command?.kind === "quest-abandon"
		),
		false
	);
	await frame.locator( '[data-ui-id="quest-abandon"]' ).click();
	await frame.locator( '[data-ui-id="quest-abandon-yes"]' ).click();
	await frame.waitForFunction( () =>
		window.__uiQa.snapshot().trace.some( row => row.detail?.command?.kind === "quest-abandon" )
	);
	await capture( frame, "quest-cancel-and-confirm" );
	frame = await choose( "reach:party-joining" );
	assert.equal( (await frame.evaluate( () => window.__uiQa.snapshot() )).controls.length, 0 );
	for ( let i = 0; i < 10; i++ ) await page.locator( "#advance" ).click();
	await frame.waitForFunction( () => window.__uiQa.snapshot().controls.length > 0 );
	await capture( frame, "party-timeout" );
	frame = await choose( "frontend:stage-race" );
	const before = await frame.evaluate( () => window.__uiQa.snapshot() );
	assert.equal( before.state.frontend.raceCenters.length, 2 );
	assert.ok( before.stage.residentGroups > 0 );
	const point = await frame.evaluate( async () => {
		const { screenPoint } = await import( "/src/engine/foundation/rendering/screen-point.ts" );
		const value = window.__uiQa.snapshot();
		return screenPoint(
			value.state.frontend.camera,
			value.state.frontend.raceCenters[0],
			value.viewport.width,
			value.viewport.height
		);
	} );
	assert.ok( point );
	await frame.locator( "canvas" ).hover( { position: { x: point[0], y: point[1] } } );
	// Keep the real pointer over the figure; clicking the outer clock button
	// would correctly clear production hover on leaving the canvas.
	await frame.evaluate( () => window.__uiQa.action( "advance" ) );
	await frame.waitForFunction( () => window.__uiQa.snapshot().state.frontend.hoveredRace === 0 );
	await capture( frame, "production-race-hover" );
	await frame.locator( "canvas" ).click( { position: { x: point[0], y: point[1] } } );
	await frame.waitForFunction( () => window.__uiQa.snapshot().state.frontend.phase === "race-zoom" );
	await capture( frame, "production-race-picked" );
	assert.deepEqual( errors, [] );
	assert.deepEqual( external, [] );
	const finalIdentity = await fetchCatalogIdentity();
	assert.equal( finalIdentity.fingerprint, identity.fingerprint );
	outcome = "passed";
	console.log(
		"Party permission/command, quest cancel/confirm, explicit-clock timeout and real 3D race hover/pick passed"
	);
} catch ( error ) {
	errors.push( String( error ) );
	throw error;
} finally {
	await writeFile(
		`${directory}/interactions.json`,
		JSON.stringify( { outcome, identity, errors, external, evidence }, null, 2 )
	);
	await browser.close();
}
