/*
===========================================================================
probe.mjs - purposeful isolated Chrome checks for the local workbench

Exercises real input, owner reentry, iframe reset and review persistence.
Screenshots are browser evidence, not native parity baselines.
===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { UI_QA_ORIGIN } from "./settings.mjs";
import { artifactDirectory, guardReviewRequests } from "./probe-support.mjs";
import { fetchCatalog } from "./catalog-client.mjs";

const directory = artifactDirectory();
await mkdir( directory, { recursive: true } );
const { browser, page } = await launchProbeBrowser( { viewport: { width: 1500, height: 1150 } } );
const failures = [], network = [], evidence = [];
let outcome = "incomplete";
page.on( "pageerror", error => failures.push( String( error ) ) );
await guardReviewRequests( page.context(), network );

/*
================
fixture
================
*/
async function fixture() {
	await page.waitForFunction( () =>
		document.querySelector( "#viewport-frame" )?.getAttribute( "src" )?.includes( "frame.html" )
	);
	const handle = await page.locator( "#viewport-frame" ).elementHandle();
	const frame = await handle.contentFrame();
	const target = new URL( await page.locator( "#viewport-frame" ).getAttribute( "src" ), page.url() ).href;
	await frame.waitForURL( target );
	await frame.waitForFunction( () => window.__uiQa?.snapshot().ready || window.__uiQa?.snapshot().error, null, {
		timeout: 60000
	} );
	const snapshot = await frame.evaluate( () => window.__uiQa.snapshot() );
	assert.equal( snapshot.error, null, JSON.stringify( snapshot ) );
	assert.deepEqual( snapshot.stats.failed, [] );
	await page.waitForFunction( () =>
		document.querySelector( "#runtime-status" )?.textContent?.includes( "controls" )
	);
	return frame;
}

/*
================
choose
================
*/
async function choose( title ) {
	await page.locator( "#navigation button" ).filter( { hasText: title } ).click();
	return fixture();
}

/*
================
capture
================
*/
async function capture( frame, name ) {
	const snapshot = await frame.evaluate( () => window.__uiQa.snapshot() );
	evidence.push( snapshot );
	await page.screenshot( { path: `${directory}/${name}.png`, fullPage: true } );
	console.log( `${name}: ${snapshot.controls.length} controls, ${snapshot.stats.failed.length} failed assets` );
}

try {
	await page.goto( UI_QA_ORIGIN + "/", { waitUntil: "domcontentloaded" } );
	await page.waitForFunction(
		() => document.querySelector( "#title" )?.textContent === "Tools and availability",
		null,
		{ timeout: 60000 }
	);
	let frame = await fixture();
	assert.equal(
		await frame.locator( "#fps-toggle:visible, .sro-bug-record__button:visible, .sro-bug-launcher:visible" )
			.count(),
		3
	);
	await capture( frame, "hud-tools" );
	await frame.locator( ".sro-bug-launcher" ).click();
	assert.equal( await frame.getByRole( "dialog" ).count(), 1 );
	await page.locator( "#reset" ).click();
	frame = await fixture();
	assert.equal( await frame.getByRole( "dialog" ).count(), 0 );
	if ( process.argv.includes( "--first" ) ) {
		assert.deepEqual( failures, [] );
		console.log( "First HUD mount and reset passed" );
	} else {
		for (
			const title of process.argv.includes( "--interactions" ) ? [] : [
				"Target and companion overlap",
				"Compact viewport and minimap",
				"Modal focus and window overlap",
				"No guild and empty relations",
				"Roster, selection and overflow",
				"Unpartied master transfer",
				"Notice reading, editing and permissions",
				"Owner board and price edits",
				"Visitor open and closed",
				"Naming, pending and refusal",
				"Cancel and immediate naming reentry"
			]
		) {
			frame = await choose( title );
			const snapshot = await frame.evaluate( () => window.__uiQa.snapshot() );
			await capture( frame, snapshot.id );
		}
		frame = await choose( "Unpartied master transfer" );
		await page.locator( '[data-action="transfer"]' ).click();
		await frame.waitForFunction( () =>
			window.__uiQa.snapshot().controls.some( row => row.id === "guild-transfer-member:102" )
		);
		await frame.locator( '[data-ui-id="guild-transfer-member:102"]' ).click();
		await frame.locator( '[data-ui-id="guild-manager-yes"]' ).click();
		assert.equal( (await frame.evaluate( () => window.__uiQa.snapshot() )).state.partySelf, 0 );
		await capture( frame, "guild-transfer-confirm" );
		frame = await choose( "Cancel and immediate naming reentry" );
		const before = await frame.evaluate( () => window.__uiQa.snapshot().state.stall.namingSequence );
		await page.locator( '[data-action="reentry"]' ).click();
		await frame.waitForFunction( () => window.__uiQa.snapshot().state.queued === 2 );
		await page.locator( '[data-action="apply"]' ).click();
		await frame.waitForFunction(
			expected => window.__uiQa.snapshot().state.stall.namingSequence === expected,
			before + 1
		);
		await frame.waitForFunction( () =>
			window.__uiQa.snapshot().controls.some( row => row.id === "stall-prompt-text" )
		);
		await capture( frame, "stall-coalesced-reentry" );
		await page.locator( "#reset" ).click();
		frame = await fixture();
		assert.equal( (await frame.evaluate( () => window.__uiQa.snapshot() )).state.stall.namingSequence, before );
		await page.locator( "#visual" ).selectOption( "uncertain" );
		await page.locator( "#notes" ).fill( "Probe persistence check only; not a native correctness review." );
		await page.locator( "#save-review" ).click();
		await page.waitForFunction( () =>
			document.querySelector( "#save-status" )?.textContent?.startsWith( "Saved" )
		);
		const download = page.waitForEvent( "download" );
		await page.locator( "#export" ).click();
		await (await download).saveAs( `${directory}/review-export.json` );
		const exported = JSON.parse( await readFile( `${directory}/review-export.json`, "utf8" ) );
		for (
			const [resource, journey] of [
				[ "ifguildlevelup", "dialog:guild-level-up" ],
				[ "ifguildmasterelection", "dialog:guild-vote" ],
				[ "ifitemmall", "dialog:item-mall" ]
			]
		) {
			const row = exported.resources.find( value => value.name === resource );
			assert.ok( row.relatedJourneys.includes( journey ) );
			assert.match( row.availability, /related recipe available/ );
		}
		await page.reload();
		await fixture();
		await choose( "Cancel and immediate naming reentry" );
		assert.match( await page.locator( "#notes" ).inputValue(), /Probe persistence/ );
		await page.locator( "#inventory-tab" ).click();
		await page.locator( "#search" ).fill( "ifguildmasterelection" );
		await page.locator( '[data-journey-id="resource:ifguildmasterelection"]' ).click();
		assert.match( await page.locator( "#resource" ).textContent(), /Open related journey: Guild manager/ );
		await page.locator( "#resource button" ).click();
		assert.match( await page.locator( "#title" ).textContent(), /Guild manager.*vote/ );
		await fixture();
		await page.locator( "#inventory-tab" ).click();
		const unmapped = exported.resources.find( row => row.availability === "unmapped resource" );
		assert.ok( unmapped, "The native inventory should retain unmapped resources" );
		await page.locator( "#search" ).fill( unmapped.name );
		await page.locator( `[data-journey-id="resource:${unmapped.name}"]` ).click();
		assert.match( await page.locator( "#resource" ).textContent(), /No supported scenario/ );
		const family = exported.familyCoverage.find( row => row.id === "academy" );
		assert.ok( family.states.some( row => row.id === "breadth:academy-member" ) );
		assert.match( family.remaining, /graduation/ );
		await page.locator( "#search" ).fill( "" );
		await page.locator( "#coverage-tab" ).click();
		await page.locator( '[data-journey-id="coverage:academy"]' ).click();
		assert.match( await page.locator( "#limits" ).textContent(), /graduation/ );
		await page.locator( "#resource button" ).filter( { hasText: "member permission" } ).click();
		await fixture();
		assert.match( await page.locator( "#title" ).textContent(), /member permission/ );
		assert.deepEqual( failures, [] );
		assert.deepEqual( network, [] );
		console.log(
			"Mounts, Guild identity, coalesced Stall reentry, reset, notes/export, resource links and unmapped inventory passed"
		);
	}
	outcome = "passed";
} catch ( error ) {
	failures.push( String( error ) );
	throw error;
} finally {
	await writeFile(
		`${directory}/browser-evidence.json`,
		JSON.stringify( { outcome, failures, network, evidence }, null, 2 )
	);
	await browser.close();
}
