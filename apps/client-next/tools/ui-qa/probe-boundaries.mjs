/*
===========================================================================
probe-boundaries.mjs - directed review of permission, input and ledger boundaries

Runs only against the independent local workbench in an owned Chrome profile.
Uses real browser input; fixture state changes remain explicit checkpoints.
===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { UI_QA_ORIGIN } from "./settings.mjs";
import { artifactDirectory, guardReviewRequests } from "./probe-support.mjs";
import { fetchCatalog } from "./catalog-client.mjs";

const directory = artifactDirectory( "boundaries" );
await mkdir( directory, { recursive: true } );
const { browser, page } = await launchProbeBrowser( { viewport: { width: 1500, height: 1150 }, deviceScaleFactor: 2 } );
const errors = [], external = [], results = [];
let outcome = "incomplete";
page.on( "pageerror", error => errors.push( String( error ) ) );
await guardReviewRequests( page.context(), external );

/*
================
fixture
================
*/
async function fixture() {
	await page.waitForFunction( () =>
		document.querySelector( "#viewport-frame" )?.getAttribute( "src" )?.includes( "frame.html" )
	);
	const handle = await page.locator( "#viewport-frame" ).elementHandle(), frame = await handle.contentFrame();
	await frame.waitForURL( new URL( await page.locator( "#viewport-frame" ).getAttribute( "src" ), page.url() ).href );
	await frame.waitForFunction( () => window.__uiQa?.snapshot().ready || window.__uiQa?.snapshot().error );
	await frame.waitForFunction( () => window.__uiQa.snapshot().stats.pending === 0 );
	const report = await frame.evaluate( () => window.__uiQa.snapshot() );
	assert.equal( report.error, null );
	assert.deepEqual( report.stats.failed, [] );
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
checkpoint
================
*/
async function checkpoint( name ) {
	await page.locator( `[data-action="${name}"]` ).click();
}

/*
================
capture
================
*/
async function capture( frame, name ) {
	results.push( { name, report: await frame.evaluate( () => window.__uiQa.snapshot() ) } );
	await page.locator( "#viewport-frame" ).screenshot( { path: `${directory}/${name}.png` } );
	console.log( `PASS ${name}` );
}

/*
================
guild
================
*/
async function guild() {
	let frame = await choose( "Roster, selection and overflow" );
	const initial = await frame.evaluate( () =>
		window.__uiQa.snapshot().controls.filter( row => row.id.startsWith( "social-member:" ) ).map( row => row.id )
			.join()
	);
	await frame.locator( '[data-ui-id^="social-member:"]' ).first().hover();
	await page.mouse.wheel( 0, 600 );
	await frame.waitForFunction(
		before =>
			window.__uiQa.snapshot().controls.filter( row => row.id.startsWith( "social-member:" ) ).map( row =>
				row.id
			).join() !== before,
		initial
	);
	await capture( frame, "guild-roster-overflow" );
	frame = await choose( "Notice reading, editing and permissions" );
	await checkpoint( "notice" );
	await frame.waitForFunction( () =>
		window.__uiQa.snapshot().controls.some( row => row.id === "guild-notice-body" )
	);
	const first = await frame.evaluate( () =>
		window.__uiQa.snapshot().controls.find( row => row.id === "guild-notice-body" ).label
	);
	await frame.locator( '[data-ui-id="guild-notice-scroll-down"]' ).click( { clickCount: 3 } );
	await frame.waitForFunction(
		before => window.__uiQa.snapshot().controls.find( row => row.id === "guild-notice-body" ).label !== before,
		first
	);
	await capture( frame, "guild-notice-scroll" );
	await frame.locator( '[data-ui-id="guild-notice-close"]' ).click();
	await checkpoint( "notice" );
	await frame.waitForFunction(
		expected => window.__uiQa.snapshot().controls.find( row => row.id === "guild-notice-body" )?.label === expected,
		first
	);
	await frame.locator( '[data-ui-id="guild-notice-close"]' ).click();
	await checkpoint( "notice-edit" );
	const contents = frame.locator( 'textarea[data-ui-id="social-contents"]' );
	const original = await contents.inputValue();
	assert.ok( original.length <= 1023 );
	await contents.fill( "First line\nSecond line\n한글 selection" );
	await contents.press( "Control+Home" );
	for ( let i = 0; i < 5; i++ ) await contents.press( "Shift+ArrowRight" );
	await frame.waitForFunction( () => {
		const input = document.querySelector( '[data-ui-id="social-contents"]' );
		return input?.selectionStart === 0 && input.selectionEnd === 5;
	} );
	await capture( frame, "guild-multiline-selection" );
	await contents.press( "Escape" );
	await checkpoint( "notice-edit" );
	assert.equal( await contents.inputValue(), original, "cancel/reopen reads the owner's unchanged notice" );
	await contents.fill( "First line\nSecond line" );
	await frame.locator( '[data-ui-id="guild-notice"]' ).click();
	await frame.waitForFunction( () =>
		window.__uiQa.snapshot().trace.some( row =>
			row.kind === "unsupported integration" && row.detail.command?.kind === "guild-notice"
		)
	);
	await page.waitForFunction( () =>
		document.querySelector( "#activity" )?.textContent?.includes( "unsupported integration" )
	);
	await page.locator( "#reset" ).click();
	frame = await fixture();
	await checkpoint( "permission" );
	await frame.waitForFunction( () =>
		window.__uiQa.snapshot().controls.find( row => row.id === "guild-dialog:notice" )?.disabled
	);
	await checkpoint( "notice-edit" );
	await page.waitForFunction( () =>
		document.querySelector( "#activity" )?.textContent?.includes( "unavailable control" )
	);
	assert.equal( await frame.locator( 'textarea[data-ui-id="social-contents"]' ).count(), 0 );
	await capture( frame, "guild-permission-denied" );
	await page.locator( "#reset" ).click();
	frame = await fixture();
	assert.equal( (await frame.evaluate( () => window.__uiQa.snapshot() )).state.guildSelf.grade, 0 );
}

/*
================
hud
================
*/
async function hud() {
	let frame = await choose( "Compact viewport and minimap" );
	await page.locator( "#viewport" ).selectOption( "375x667" );
	frame = await fixture();
	await checkpoint( "pets" );
	await checkpoint( "pets" );
	for ( const id of [ "compact-map", "compact-pets" ] ) {
		await frame.locator( `[data-ui-id="${id}"]` ).click();
		await frame.waitForFunction( () => window.__uiQa.snapshot().stats.pending === 0 );
		await capture( frame, `hud-${id}` );
		await frame.locator( `[data-ui-id="${id}"]` ).click();
	}
	const boxes = await frame.locator( "#fps-chip button:visible" ).evaluateAll( nodes =>
		nodes.map( node => {
			const rect = node.getBoundingClientRect();
			return { id: node.id || node.className, x: rect.x, y: rect.y, right: rect.right, bottom: rect.bottom };
		} )
	);
	for ( const box of boxes ) {
		assert.ok( box.x >= 0 && box.y >= 0 && box.right <= 375 && box.bottom <= 667, JSON.stringify( box ) );
	}
	await page.locator( "#viewport" ).selectOption( "1000x700" );
	frame = await choose( "Modal focus and window overlap" );
	await checkpoint( "report" );
	const before = await frame.evaluate( () =>
		window.__uiQa.snapshot().trace.filter( row => row.kind === "world click-through" || row.detail?.kind === "key" )
			.length
	);
	await frame.getByRole( "textbox", { name: "Description" } ).fill(
		"Local fixture report only; no upload expected."
	);
	await frame.getByRole( "textbox", { name: "Description" } ).press( "w" );
	await frame.getByRole( "button", { name: "Send report", exact: true } ).click();
	await frame.waitForFunction( () =>
		document.querySelector( ".sro-bug-report__status" )?.textContent?.includes( "delivery is unsupported" )
	);
	assert.equal(
		await frame.evaluate( () =>
			window.__uiQa.snapshot().trace.filter( row =>
				row.kind === "world click-through" || row.detail?.kind === "key"
			).length
		),
		before
	);
	await capture( frame, "hud-modal-local-only" );
	await frame.getByRole( "button", { name: "Cancel", exact: true } ).click();
	frame = await choose( "Tools and availability" );
	const gap = await frame.evaluate( () => {
		const a = document.querySelector( "#developer-toggle" ).getBoundingClientRect(),
			b = document.querySelector( "#fps-toggle" ).getBoundingClientRect();
		return { x: (a.right + b.left) / 2, y: a.y + a.height / 2 };
	} );
	const outer = await page.locator( "#viewport-frame" ).boundingBox();
	await page.mouse.click( outer.x + gap.x, outer.y + gap.y );
	await frame.waitForFunction( () =>
		window.__uiQa.snapshot().trace.some( row => row.kind === "world click-through" )
	);
	await capture( frame, "hud-pointer-gap" );
}

/*
================
stall
================
*/
async function stall() {
	let frame = await choose( "Visitor open and closed" );
	await page.locator( "#backdrop" ).selectOption( "contrast" );
	frame = await fixture();
	await capture( frame, "stall-open-contrast" );
	await checkpoint( "trading" );
	await frame.waitForFunction( () => !window.__uiQa.snapshot().state.stall.open );
	await capture( frame, "stall-closed-contrast" );
	await checkpoint( "offers" );
	await frame.waitForFunction( () => window.__uiQa.snapshot().state.stall.offers.length === 0 );
	await capture( frame, "stall-empty-contrast" );
	await page.locator( "#backdrop" ).selectOption( "dark" );
	frame = await choose( "Naming, pending and refusal" );
	await frame.locator( '[data-ui-id="stall-prompt-text"]' ).fill( "Local review stall" );
	await frame.locator( '[data-ui-id="stall-prompt-ok"]' ).click();
	await checkpoint( "apply" );
	await frame.waitForFunction( () => window.__uiQa.snapshot().state.pending.length > 0 );
	assert.equal( await frame.locator( '[data-ui-id="stall-prompt-text"]' ).count(), 0 );
	await checkpoint( "refuse" );
	await frame.waitForFunction( () =>
		window.__uiQa.snapshot().trace.some( row =>
			row.kind === "production reply" && row.detail.notice?.code === 0x3b
		)
	);
	await capture( frame, "stall-refusal" );
}

/*
================
ledger
================
*/
async function ledger() {
	await choose( "Tools and availability" );
	await page.locator( "#notes" ).fill( "Unreviewed draft retained across navigation" );
	await choose( "Roster, selection and overflow" );
	await choose( "Tools and availability" );
	assert.equal( await page.locator( "#notes" ).inputValue(), "Unreviewed draft retained across navigation" );
	const current = await fetchCatalog();
	await page.evaluate( async identity => {
		const { SCENARIO_VERSION } = await import( "/tools/ui-qa/scenarios.ts" );
		localStorage.removeItem( "opensro.ui-qa.drafts.v1" );
		localStorage.setItem(
			"opensro.ui-qa.reviews.v1",
			JSON.stringify( [ {
				item: "hud-tools",
				context: `hud-tools|1000x700|dark|dpr:2|v${SCENARIO_VERSION}`,
				fingerprint: "old-source",
				identity: { ...identity, fingerprint: "old-source" },
				axes: { visual: "uncertain", interaction: "unreviewed", integration: "unsupported" },
				notes: "Historical probe record",
				status: "reviewed",
				time: "2026-10-11T00:00:00Z",
				version: SCENARIO_VERSION,
				evidence: {}
			} ] )
		);
	}, current.identity );
	await page.reload();
	await fixture();
	assert.match( await page.locator( "#review-status" ).textContent(), /Needs recheck/ );
	const download = page.waitForEvent( "download" );
	await page.locator( "#export" ).click();
	await (await download).saveAs( `${directory}/stale-export.json` );
	const exported = JSON.parse( await readFile( `${directory}/stale-export.json`, "utf8" ) );
	assert.equal( exported.reviews[0].status, "needs-recheck" );
	assert.equal( exported.resources.length, 231 );
	assert.equal( exported.panels.length, 32 );
	assert.ok( exported.resources.some( row => row.availability === "unmapped resource" ) );
	await page.locator( "#notes" ).fill( "Rechecked in isolated probe, not native parity" );
	await page.locator( "#save-review" ).click();
	await page.waitForFunction( () => document.querySelector( "#save-status" )?.textContent?.startsWith( "Saved" ) );
	assert.match(
		await page.locator( "#navigation button" ).filter( { hasText: "Tools and availability" } ).textContent(),
		/Reviewed checkpoint/
	);
	const saved = await page.evaluate( () =>
		JSON.parse( localStorage.getItem( "opensro.ui-qa.reviews.v1" ) ).at( -1 )
	);
	assert.equal( saved.identity.sourceHash, current.identity.sourceHash );
	assert.equal( saved.identity.assetHash, current.identity.assetHash );
	assert.equal( saved.evidence.id, "hud-tools" );
	assert.ok( saved.browser.includes( "Chrome" ) );
	assert.equal( (await page.request.get( UI_QA_ORIGIN + "/api/health" )).status(), 403 );
	results.push( { name: "review-drafts-staleness-export-and-local-guard", identity: current.identity } );
	console.log( "PASS review-drafts-staleness-export-and-local-guard" );
}

try {
	await page.goto( UI_QA_ORIGIN + "/" );
	await fixture();
	for ( const [name, run] of Object.entries( { guild, hud, stall, ledger } ) ) {
		if ( process.argv[2] && process.argv[2] !== name ) continue;
		await run();
	}
	assert.deepEqual( errors, [] );
	assert.deepEqual( external, [] );
	outcome = "passed";
} catch ( error ) {
	errors.push( String( error ) );
	throw error;
} finally {
	await writeFile(
		`${directory}/evidence-${process.argv[2] ?? "all"}.json`,
		JSON.stringify( { outcome, errors, external, results }, null, 2 )
	);
	await browser.close();
}
