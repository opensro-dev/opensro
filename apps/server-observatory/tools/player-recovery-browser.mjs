/*
===========================================================================
player-recovery-browser.mjs - isolated recovery UI and mutation race witnesses

Serves the real dashboard on an ephemeral loopback port. All API responses
are fixtures; no credentials, shards or production operations are contacted.
The repository browser launcher owns Chrome and this probe owns its server.
===========================================================================
*/
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { launchProbeBrowser } from "../../../scripts/lib/probeBrowser.mjs";

const publicRoot = new URL( "../public/", import.meta.url );
const directory = new URL( "../temp/artifacts/player-recovery/", import.meta.url );
const calls = [], errors = [];
let pendingRead, pendingMutation, holdRead = false, holdMutation = false, failMutation = false;
let includePK = true, includeStats = true, configured = true;
const WAIT_TIMEOUT_MS = 5000;
/*
================
waitForPending
================
*/
async function waitForPending( read ) {
	const deadline = Date.now() + WAIT_TIMEOUT_MS;
	while ( !read() ) {
		if ( Date.now() >= deadline ) throw Error( "Fixture request did not arrive" );
		await new Promise( resolve => setTimeout( resolve, 10 ) );
	}
}
/*
================
snapshot
================
*/
function snapshot( name, shard, cleared = false, reset = false ) {
	return {
		shard,
		capturedAt: "2026-10-09T08:00:00Z",
		player: {
			id: name === "Viper" ? 42 : 43,
			name,
			level: 30,
			...(includeStats ? { strength: reset ? 49 : 80, intellect: 49, statPoints: reset ? 87 : 56 } : {}),
			hp: 100,
			mp: 80,
			bound: !cleared,
			...(includePK ?
				{ pk: { dailyCount: 2, totalCount: 9, penalty: cleared ? 0 : 450 } } :
				{}),
			pvpState: cleared ? 0 : 2,
			aggressions: cleared ? null : { "44": 30 },
			savedWorld: { spawn: { regionId: 25000, x: 10, y: 20, z: 30 } }
		},
		towns: [ { id: 1, code: "GATE_CH" } ]
	};
}
/*
================
serve
================
*/
const server = createServer( async ( request, response ) => {
	const path = new URL( request.url, "http://localhost" ).pathname;
	if ( path !== "/" && !/^\/[a-z-]+\.(js|css)$/.test( path ) ) {
		response.writeHead( 404 ).end();
		return;
	}
	try {
		const body = await readFile( new URL( path === "/" ? "index.html" : path.slice( 1 ), publicRoot ) );
		response.setHeader(
			"Content-Type",
			path.endsWith( ".js" ) ? "text/javascript" : path.endsWith( ".css" ) ? "text/css" : "text/html"
		);
		response.end( body );
	} catch {
		response.writeHead( 404 ).end();
	}
} );
await new Promise( resolve => server.listen( 0, "127.0.0.1", resolve ) );
const origin = `http://127.0.0.1:${server.address().port}`;
let browser;
try {
	const launched = await launchProbeBrowser( { viewport: { width: 1440, height: 1120 } } );
	browser = launched.browser;
	const page = launched.page;
	page.on( "pageerror", error => errors.push( error.message ) );
	await page.route( "**/api/**", async route => {
		const request = route.request(), url = new URL( request.url() );
		if ( url.pathname === "/api/console" ) {
			await route.fulfill( {
				json: {
					enabled: configured,
					operator: "fixture-operator",
					shards: [ { id: "a", name: "Alpha" }, { id: "b", name: "Beta" } ]
				}
			} );
		} else if ( url.pathname === "/api/player" && request.method() === "GET" ) {
			const value = snapshot( url.searchParams.get( "character" ), url.searchParams.get( "shard" ) );
			if ( holdRead ) pendingRead = () => route.fulfill( { json: value } );
			else await route.fulfill( { json: value } );
		} else if ( url.pathname === "/api/player" && request.method() === "POST" ) {
			const body = request.postDataJSON();
			calls.push( { body, shard: url.searchParams.get( "shard" ), headers: request.headers() } );
			const reply = () =>
				route.fulfill(
					failMutation ?
						{ status: 409, json: { error: "Authority refused operation" } } :
						{
							json: snapshot(
								body.character,
								url.searchParams.get( "shard" ),
								true,
								body.action === "reset-stats"
							)
						}
				);
			if ( holdMutation ) pendingMutation = reply;
			else await reply();
		} else await route.fulfill( { status: 503, json: { error: "Fixture has no live census" } } );
	} );
	await page.goto( origin + "/#recovery" );
	await page.waitForFunction( () => !document.getElementById( "recovery-inspect-button" ).disabled );
	const field = name => page.locator( "#recovery-" + name );
	/*
	================
	inspect
	================
	*/
	async function inspect( name = "Viper" ) {
		await field( "character" ).fill( name );
		await field( "inspect-button" ).click();
		await page.waitForFunction( () => !document.getElementById( "recovery-result" ).hidden );
	}
	await inspect();
	assert.match( await field( "facts" ).textContent(), /Penalty 450; daily kills 2; total kills 9/ );
	await field( "pk-reason" ).fill( "Correct audited PK record" );
	await field( "pk-confirmation" ).fill( "viper" );
	await field( "clear-pk-button" ).click();
	assert.equal( calls.length, 0 );
	assert.match( await field( "status" ).textContent(), /exact name/ );
	await field( "pk-confirmation" ).fill( "Viper" );
	await field( "pk-reason" ).fill( "     " );
	await field( "clear-pk-button" ).click();
	assert.equal( calls.length, 0 );
	await field( "shard" ).selectOption( "b" );
	assert.equal( await field( "result" ).isVisible(), false );
	assert.equal( await field( "pk-confirmation" ).inputValue(), "" );
	// A late response for Alpha must not become Beta's inspected character.
	holdRead = true;
	await field( "inspect-button" ).click();
	await page.waitForFunction( () => document.getElementById( "recovery-status" ).textContent.includes( "Reading" ) );
	await field( "character" ).fill( "Other" );
	await waitForPending( () => pendingRead );
	const lateResponse = page.waitForResponse( response => new URL( response.url() ).pathname === "/api/player" );
	await pendingRead();
	await (await lateResponse).finished();
	await page.evaluate( () =>
		new Promise( resolve => requestAnimationFrame( () => requestAnimationFrame( resolve ) ) )
	);
	assert.equal( await field( "result" ).isVisible(), false, "late inspection cannot restore an old selection" );
	holdRead = false;
	await inspect( "Other" );
	assert.equal( await field( "player-title" ).textContent(), "Other" );
	await field( "pk-reason" ).fill( "Correct audited PK record" );
	await field( "pk-confirmation" ).fill( "Other" );
	holdMutation = true;
	await field( "clear-pk-button" ).click();
	await page.waitForFunction( () => document.getElementById( "recovery-shard" ).disabled );
	assert.equal( await field( "rescue-button" ).isDisabled(), true );
	assert.equal( await field( "inspect-button" ).isDisabled(), true );
	await page.evaluate( () => {
		for ( const id of [ "recovery-clear-pk", "recovery-rescue", "recovery-inspect" ] ) {
			document.getElementById( id ).dispatchEvent( new Event( "submit", { cancelable: true } ) );
		}
	} );
	await waitForPending( () => pendingMutation );
	assert.equal( calls.length, 1, "duplicate and competing mutations are suppressed" );
	assert.deepEqual( Object.keys( calls[0].body ).sort(), [ "action", "character", "id", "reason" ] );
	assert.equal( calls[0].body.action, "clear-pk" );
	assert.equal( calls[0].body.character, "Other" );
	assert.equal( calls[0].shard, "b" );
	assert.equal( calls[0].headers["x-sro-console"], "1" );
	await pendingMutation();
	holdMutation = false;
	await page.waitForFunction( () =>
		document.getElementById( "recovery-status" ).textContent.includes( "PK cleared" )
	);
	assert.match( await field( "status" ).textContent(), /Before: Penalty 450.*After: Penalty 0/ );
	assert.match( await field( "facts" ).textContent(), /Penalty 0; daily kills 2; total kills 9/ );
	assert.match( await field( "facts" ).textContent(), /PVP stateNeutral.*Active aggressions0/ );
	assert.equal( await field( "pk-confirmation" ).inputValue(), "" );
	await mkdir( directory, { recursive: true } );
	await page.screenshot( { path: fileURLToPath( new URL( "cleared.png", directory ) ), fullPage: true } );
	await page.setViewportSize( { width: 390, height: 844 } );
	assert.ok(
		await page.evaluate( () => document.documentElement.scrollWidth <= innerWidth ),
		"mobile recovery fits"
	);
	await page.screenshot( { path: fileURLToPath( new URL( "mobile.png", directory ) ), fullPage: true } );
	await page.setViewportSize( { width: 1440, height: 1120 } );
	failMutation = true;
	await field( "pk-confirmation" ).fill( "Other" );
	await field( "clear-pk-button" ).click();
	await page.waitForFunction( () =>
		document.getElementById( "recovery-status" ).textContent.includes( "uncertain" )
	);
	assert.equal( await field( "result" ).isVisible(), false );
	assert.equal( calls.length, 2 );
	includePK = false;
	await inspect();
	assert.equal( await field( "clear-pk-button" ).isDisabled(), true );
	assert.match( await field( "facts" ).textContent(), /Unavailable on this shard/ );
	failMutation = false;
	await field( "reason" ).fill( "Rescue stranded player" );
	await field( "confirmation" ).fill( "Viper" );
	await field( "rescue-button" ).click();
	await page.waitForFunction( () =>
		document.getElementById( "recovery-status" ).textContent.includes( "was rescued" )
	);
	assert.deepEqual( Object.keys( calls[2].body ).sort(), [ "character", "id", "reason", "town" ] );
	assert.equal( calls[2].body.town, 1, "rescue retains its existing envelope" );
	assert.equal(
		new Set( calls.map( call => call.body.id ) ).size,
		calls.length,
		"every explicit operation has a fresh audit ID"
	);
	await inspect();
	assert.match( await field( "facts" ).textContent(), /STR 80; INT 49; free points 56/ );
	await field( "stats-reason" ).fill( "Player requested stat reset" );
	await field( "stats-confirmation" ).fill( "viper" );
	await field( "reset-stats-button" ).click();
	assert.equal( calls.length, 3 );
	assert.match( await field( "status" ).textContent(), /exact name/ );
	await field( "stats-confirmation" ).fill( "Viper" );
	await field( "reset-stats-button" ).click();
	await page.waitForFunction( () =>
		document.getElementById( "recovery-status" ).textContent.includes( "stats reset" )
	);
	assert.equal( calls.length, 4 );
	assert.equal( calls[3].body.action, "reset-stats" );
	assert.deepEqual( Object.keys( calls[3].body ).sort(), [ "action", "character", "id", "reason" ] );
	assert.match(
		await field( "status" ).textContent(),
		/Before: STR 80; INT 49; free points 56.*After: STR 49; INT 49; free points 87/
	);
	assert.equal( await field( "stats-confirmation" ).inputValue(), "", "a reset requires a fresh confirmation" );
	await field( "reset-stats-button" ).click();
	assert.equal( calls.length, 4 );
	includeStats = false;
	await inspect();
	assert.equal( await field( "reset-stats-button" ).isDisabled(), true );
	configured = false;
	await page.reload();
	await page.waitForFunction( () =>
		document.getElementById( "recovery-status" ).textContent.includes( "not configured" )
	);
	assert.equal( await field( "inspect-button" ).isDisabled(), true );
	assert.deepEqual( errors, [] );
	await writeFile(
		new URL( "report.json", directory ),
		JSON.stringify(
			{
				passed: true,
				errors,
				mutations: calls.map( ( { body, shard } ) => ({ body, shard }) ),
				scenarios: [
					"exact name and reason",
					"selection invalidation",
					"late inspection",
					"exclusive mutation",
					"before/after PK",
					"stat reset envelope, summary and renewed confirmation",
					"uncertain outcome",
					"older shard"
				]
			},
			null,
			2
		)
	);
	console.log(
		"PASS: clear-PK real UI, exact shard/character, stale inspection, concurrency, refusal and old-shard gating"
	);
} finally {
	await browser?.close();
	await new Promise( resolve => server.close( resolve ) );
}
