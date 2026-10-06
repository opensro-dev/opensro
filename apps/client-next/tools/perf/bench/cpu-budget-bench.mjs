/*
===========================================================================

cpu-budget-bench.mjs - CPU time per second at matched render preferences

Uses the authenticated scratch-character harness and actual Chrome process
CPU counters. Rates are saved through the production video preference format.
Reports CPU core equivalents, not percentages of the entire host machine.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { openClient, closeClient, measure, selectFrameLimit } from "../core/client.mjs";
import { keepGoing } from "./scenarios.mjs";

const SECONDS = 8;
const OUT = "temp/artifacts/cpu-budget";
const results = [];

/*
================
cpuSample
================
*/
async function cpuSample( browserSession, pageSession ) {
	const { processInfo } = await browserSession.send( "SystemInfo.getProcessInfo" );
	const { metrics } = await pageSession.send( "Performance.getMetrics" );
	return {
		at: performance.now(),
		processes: processInfo,
		main: metrics.find( metric => metric.name === "TaskDuration" ).value
	};
}

/*
================
run
================
*/
async function run() {
	await mkdir( OUT, { recursive: true } );
	const client = await openClient( MISSION_MOVEMENT_FIXTURES.region_cross, {
		uncapped: false,
		frameLimit: 240,
		spans: true
	} );
	try {
		const browserSession = await client.browser.newBrowserCDPSession();
		const pageSession = await client.page.context().newCDPSession( client.page );
		await pageSession.send( "Performance.enable" );
		for ( const frameLimit of [ 240, 120, 60, 0, 60, 120, 240 ] ) {
			await selectFrameLimit( client.page, frameLimit );
			await client.page.locator( '[data-ui-id="option-ok"]' ).click();
			await client.page.waitForTimeout( 2500 );
			for ( const scenario of [ "idle" ] ) {
				const before = await cpuSample( browserSession, pageSession );
				const result = await measure(
					client.page,
					`${frameLimit || "uncapped"}/${scenario}`,
					SECONDS * 1000,
					more => keepGoing( client.page, more )
				);
				const after = await cpuSample( browserSession, pageSession );
				const elapsed = (after.at - before.at) / 1000;
				const cpuSeconds = after.processes.reduce( ( sum, process ) => {
					const previous = before.processes.find( item => item.id === process.id );
					return sum + (previous ? process.cpuTime - previous.cpuTime : 0);
				}, 0 );
				const evidence = await client.page.evaluate( () => ({
					phase: globalThis.__benchRuntime.sessionState().phase,
					preference: JSON.parse( localStorage.getItem( "sro:v1150:video-options:1" ) ).frameLimit,
					visibility: document.visibilityState
				}) );
				assert.equal( evidence.phase, "world" );
				assert.equal( evidence.preference, frameLimit );
				assert.equal( evidence.visibility, "visible" );
				if ( frameLimit ) assert.ok( result.fps <= frameLimit * 1.03, `cap exceeded: ${result.fps}` );
				const { movement, inputs, counts, ...summary } = result;
				results.push( {
					...summary,
					evidence,
					mainTaskPercent: (after.main - before.main) / elapsed * 100,
					browserCorePercent: cpuSeconds / elapsed * 100,
					inputs,
					counts
				} );
				await writeFile( `${OUT}/same-session-results.json`, JSON.stringify( results, null, 2 ) );
				console.log(
					JSON.stringify( {
						name: result.name,
						fps: result.fps,
						browserCorePercent: cpuSeconds / elapsed * 100
					} )
				);
			}
		}
	} finally {
		await closeClient( client );
	}
}

await run();
