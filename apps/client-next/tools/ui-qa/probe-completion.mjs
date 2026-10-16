/*
===========================================================================
probe-completion.mjs - real frontend input, readiness and portable review evidence

Checks ownership-sensitive interactions rather than enumerating server outcomes.
===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { UI_QA_ORIGIN } from "./settings.mjs";
import { artifactDirectory, guardReviewRequests } from "./probe-support.mjs";
import { fetchCatalogIdentity } from "./catalog-client.mjs";
const directory = artifactDirectory( "completion" );
await mkdir( directory, { recursive: true } );
const { browser, page } = await launchProbeBrowser( { viewport: { width: 1500, height: 1150 } } );
const errors = [], external = [], evidence = [];
let identity, outcome = "incomplete";
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
			const value = window.__uiQa?.snapshot();
			return value?.error || value?.coverage?.admitted && !value.entry?.busy;
		},
		null,
		{ timeout: 120000 }
	);
	assert.equal( (await frame.evaluate( () => window.__uiQa.snapshot() )).error, null );
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
	console.log( "PASS " + name );
}

try {
	await page.goto( UI_QA_ORIGIN + "/" );
	await page.waitForFunction( () =>
		document.querySelector( "#runtime-status" )?.textContent?.includes( "controls" )
	);
	identity = await fetchCatalogIdentity();
	let frame = await choose( "frontend:stage-populated" );
	let value = await frame.evaluate( () => window.__uiQa.snapshot() );
	assert.equal( value.characters.dock, true );
	assert.ok( value.characters.stats.draws > 0 );
	const point = await frame.evaluate( async () => {
		const { screenPoint } = await import( "/src/engine/foundation/rendering/screen-point.ts" );
		const value = window.__uiQa.snapshot(), actor = value.characters.actors.find( row => row.gid === 1 );
		return screenPoint(
			value.state.frontend.camera,
			[ actor.pose.x, actor.pose.y + (actor.height ?? 20) / 2, actor.pose.z ],
			value.viewport.width,
			value.viewport.height
		);
	} );
	assert.ok( point );
	assert.ok( point.every( Number.isFinite ), "Dock click must have finite projected coordinates" );
	await frame.locator( "canvas" ).click( { position: { x: point[0], y: point[1] } } );
	await frame.waitForFunction( () => window.__uiQa.snapshot().state.frontend.selectedCharacter === "Reviewer" );
	for ( let i = 0; i < 8; i++ ) await frame.evaluate( () => window.__uiQa.action( "advance" ) );
	await frame.waitForFunction( () => !window.__uiQa.snapshot().state.frontend.cameraMoving );
	await capture( frame, "dock-picked" );
	frame = await choose( "frontend:stage-europe" );
	assert.equal( (await frame.evaluate( () => window.__uiQa.snapshot() )).characters.preview, true );
	await frame.locator( '[data-ui-id="create:name"]' ).fill( "LocalReviewer" );
	await frame.locator( '[data-ui-id="create:female"]' ).click();
	await frame.waitForFunction( () => window.__uiQa.snapshot().state.frontend.creation.selection.gender === 1 );
	await frame.waitForFunction( () => window.__uiQa.snapshot().characters.preview, null, { timeout: 60000 } );
	await frame.locator( '[data-ui-id="create:weapon:next"]' ).click();
	await frame.waitForFunction( () => window.__uiQa.snapshot().characters.preview, null, { timeout: 60000 } );
	value = await frame.evaluate( () => window.__uiQa.snapshot() );
	// The real text owner enforces the authored twelve-character limit.
	assert.equal( value.state.frontend.creation.selection.name, "LocalReviewe" );
	await capture( frame, "creation-edited-ready" );
	frame = await choose( "frontend:stage-confirm" );
	await frame.locator( '[data-ui-id="create:confirm-cancel"]' ).click();
	for ( let i = 0; i < 3; i++ ) await frame.evaluate( () => window.__uiQa.action( "advance" ) );
	await frame.waitForFunction( () => window.__uiQa.snapshot().state.frontend.creation.phase === "editing" );
	await capture( frame, "creation-confirm-cancel" );
	frame = await choose( "inspect:inventory-actor" );
	await frame.locator( '[data-ui-id="doll-right"]' ).click();
	await capture( frame, "inventory-doll-input" );
	frame = await choose( "inspect:overhead-actor" );
	value = await frame.evaluate( () => window.__uiQa.snapshot() );
	assert.ok( value.characters.stats.draws > 0 && value.sceneAnchors.includes( 1 ) );
	await capture( frame, "projected-overhead" );
	frame = await choose( "inspect:stall-network" );
	value = await frame.evaluate( () => window.__uiQa.snapshot() );
	assert.equal( value.state.stall.network.rows[0].price, "500" );
	await page.locator( "#notes" ).fill(
		"Synthetic populated listing; portable exact integer evidence. Human parity unreviewed."
	);
	await page.locator( "#save-review" ).click();
	await page.waitForFunction( () => document.querySelector( "#save-status" ).textContent.includes( "Saved" ) );
	const download = page.waitForEvent( "download" );
	await page.locator( "#export" ).click();
	await (await download).saveAs( `${directory}/review-export.json` );
	const exported = JSON.parse( await readFile( `${directory}/review-export.json`, "utf8" ) );
	assert.equal( exported.prototypeScope.A.panels.length, 32 );
	assert.equal( exported.prototypeScope.A.remaining[0].id, "breadth:specialty-buy" );
	assert.equal( exported.prototypeScope.B.remaining.length, 0 );
	const missing = await page.evaluate( async () => {
		const { journeys } = await import( "/tools/ui-qa/scenarios.ts" );
		const { prototypeScope } = await import( "/tools/ui-qa/prototype-scope.ts" );
		return prototypeScope.A.panels.flatMap( row => [ row.entry, row.inspect ] ).filter( id =>
			!journeys.some( row => row.id === id )
		);
	} );
	assert.deepEqual( missing, [] );
	await capture( frame, "exact-integer-export" );
	const finalIdentity = await fetchCatalogIdentity();
	assert.equal( finalIdentity.fingerprint, identity.fingerprint );
	assert.deepEqual( errors, [] );
	assert.deepEqual( external, [] );
	outcome = "passed";
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
