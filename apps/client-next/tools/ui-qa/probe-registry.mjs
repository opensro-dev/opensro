/*
===========================================================================
probe-registry.mjs - observe every production panel entry without claiming parity

Blocked admission is retained with its evidence and reason, not auto-succeeded.
The registry is imported from production; no second list of windows is kept.
===========================================================================
*/
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { UI_QA_ORIGIN } from "./settings.mjs";
import { artifactDirectory, guardReviewRequests } from "./probe-support.mjs";
import { fetchCatalogIdentity } from "./catalog-client.mjs";

const extended = process.argv.includes( "--extended" );
const match = process.argv.find( value => value.startsWith( "--match=" ) )?.slice( 8 );
const directory = artifactDirectory( extended ? "extended" : "registry" );
await mkdir( directory, { recursive: true } );
const { browser, page } = await launchProbeBrowser( { viewport: { width: 1500, height: 1100 } } );
const results = [], errors = [], external = [];
page.on( "pageerror", error => errors.push( String( error ) ) );
await guardReviewRequests( page.context(), external );
let identity;
try {
	await page.goto( UI_QA_ORIGIN + "/" );
	await page.waitForFunction( () =>
		document.querySelector( "#runtime-status" )?.textContent?.includes( "controls" )
	);
	identity = await fetchCatalogIdentity();
	const entries = await page.evaluate( async extended => {
		const { journeys } = await import( "/tools/ui-qa/scenarios.ts" );
		return journeys.filter( row =>
			extended ? row.id.includes( ":" ) && !row.id.startsWith( "surface:" ) : row.id.startsWith( "surface:" )
		).map( row => ({ id: row.id, name: row.title, family: row.family }) );
	}, extended );
	for ( const { id, name, family } of entries.filter( row => !match || new RegExp( match ).test( row.id ) ) ) {
		let report = null, reason = null;
		try {
			await page.locator( `[data-journey-id="${id}"]` ).click();
			const frame = await (await page.locator( "#viewport-frame" ).elementHandle()).contentFrame();
			await frame.waitForURL(
				new URL( await page.locator( "#viewport-frame" ).getAttribute( "src" ), page.url() ).href
			);
			await frame.waitForFunction(
				() => window.__uiQa?.snapshot().ready || window.__uiQa?.snapshot().error,
				null,
				{ timeout: 30000 }
			);
			await frame.waitForFunction(
				() => {
					const value = window.__uiQa.snapshot();
					return value.error ||
						!value.entry?.busy && value.entry?.frames >= 10 && value.stats.pending === 0 &&
							value.stats.windowReady;
				},
				null,
				{ timeout: id.startsWith( "frontend:stage-" ) || id.endsWith( "-actor" ) ? 120000 : 15000 }
			);
			report = await frame.evaluate( () => window.__uiQa.snapshot() );
			reason = report.error || report.coverage?.unsupported ||
				(report.stats.failed.length ?
					"Asset failures" :
					(report.coverage ?
							!report.coverage.admitted :
							!report.entry?.evidence?.admitted ||
							id.startsWith( "surface:" ) && report.stats.panel !== id.slice( 8 )) ?
					`Requested surface lacks target-specific evidence: ${
						(report.coverage?.requiredControls?.filter( id =>
							!report.controls.some( control => control.id === id )
						) ??
							report.entry?.evidence?.missing)?.join( ", " ) || "state witness not satisfied"
					}` :
					null);
			await page.locator( "#viewport-frame" ).screenshot( {
				path: `${directory}/${id.replaceAll( /[^a-z0-9]/gi, "-" )}.png`
			} );
		} catch ( error ) {
			reason = String( error );
		}
		results.push( {
			id,
			family,
			name,
			status: reason ?
				"blocked or unverified" :
				"presentation mounted; visible entry flow, state coverage and parity unverified",
			reason,
			report
		} );
		console.log( `${reason ? "GAP" : "OPEN"} ${name}${reason ? ": " + reason : ""}` );
	}
} finally {
	const admitted = results.filter( row => !row.reason ).length;
	const finalIdentity = await fetchCatalogIdentity();
	if ( finalIdentity.fingerprint !== identity?.fingerprint ) {
		errors.push( "Source changed during sweep; rerun required" );
	}
	const families = Object.fromEntries(
		[ ...new Set( results.map( row => row.family ) ) ].map( family => [ family, {
			total: results.filter( row => row.family === family ).length,
			admitted: results.filter( row => row.family === family && !row.reason ).length
		} ] )
	);
	await writeFile(
		`${directory}/${match ? "filtered" : "coverage"}.json`,
		JSON.stringify(
			{ identity, finalIdentity, families, admitted, total: results.length, errors, external, results },
			null,
			2
		)
	);
	console.log( `${admitted}/${results.length} entries admitted; ${results.length - admitted} disclosed gaps` );
	if ( errors.length || external.length ) process.exitCode = 1;
	await browser.close();
}
