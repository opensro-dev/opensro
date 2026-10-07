/*
===========================================================================

wall-recovery-bench.mjs - real movement against an authored object boundary

Certifies a blocked segment from published navigation before profiling. The
scratch character then submits that destination to the real server under jitter.
Displayed positions must remain on the admitted side of the boundary.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../../scripts/lib/probeEndpoints.mjs";
import { openClient, closeClient, createCaptures, measure } from "../core/client.mjs";
import { installFaults } from "../core/transport-faults.mjs";
import { parseOptions } from "../core/report.mjs";
import { keepGoing } from "./scenarios.mjs";

/*
================
certify
================
*/
async function certify() {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( `${CLIENT_NEXT_BASE_URL}/assets/skillfx/manifest.json` );
		return await page.evaluate( async () => {
			const { createNavigationResources } = await import(
				"/src/engine/runtime/assets/worker/navigation/navigation.ts"
			);
			const { createNavigation } = await import(
				"/src/engine/runtime/simulation/worker/session/world/gameplay/movement/navigation/navigation.ts"
			);
			const region = 0x6e4a;
			const read = async path => {
				const response = await fetch( path );
				if ( !response.ok ) throw Error( `Navigation read failed: ${path}` );
				return new Uint8Array( await response.arrayBuffer() );
			};
			const catalog = await (await fetch( "/assets/world/world-region-catalog.json" )).json();
			const entry = catalog.regionsById[`0x${region.toString( 16 )}`][0];
			const product = await createNavigationResources().resolve(
				await read( entry.bundlePublicPath ),
				region,
				read
			);
			const navigation = createNavigation();
			navigation.install( region, product );
			for ( const object of product.objects ) {
				if ( object.x < 300 || object.x > 1620 || object.z < 300 || object.z > 1620 ) continue;
				for ( let direction = 0; direction < 16; direction++ ) {
					const angle = direction * Math.PI / 8, dx = Math.cos( angle ) * 250, dz = Math.sin( angle ) * 250;
					const from = navigation.surface( {
						regionId: region,
						x: Math.trunc( object.x + dx ),
						y: object.y,
						z: Math.trunc( object.z + dz ),
						angle: 0
					} );
					const to = { ...from, x: Math.trunc( object.x - dx ), z: Math.trunc( object.z - dz ) };
					const query = { slide: false };
					const stop = navigation.clip( from, to, query );
					if ( !stop || query.edge === undefined ) continue;
					const travel = Math.hypot( stop.x - from.x, stop.z - from.z );
					if ( travel < 40 || travel > 280 || Math.hypot( stop.x - to.x, stop.z - to.z ) < 20 ) continue;
					return { from, to, stop, edge: query.edge, cell: query.cell, travel };
				}
			}
			throw Error( "No authored object boundary certified in the reference region" );
		} );
	} finally {
		await browser.close();
	}
}

/*
================
run
================
*/
async function run( options ) {
	const route = await certify();
	console.log( "[wall] certified", route );
	const fixture = { id: "authored-wall-recovery", movementMode: 3, start: route.from, startYawRadians: 0 };
	const client = await openClient( fixture, {
		spans: true,
		uncapped: false,
		frameLimit: 60,
		cpuRate: options.cpuRate,
		beforeLogin: installFaults
	} );
	const { page } = client, results = { route, cpuRate: options.cpuRate };
	await mkdir( options.out, { recursive: true } );
	try {
		const worker = page.workers().find( row => row.url().includes( "/simulation/worker/" ) );
		assert.ok( worker );
		await worker.evaluate( () => {
			__recoveryLink.delay = 100;
			__recoveryLink.jitter = 25;
		} );
		const captures = await createCaptures( page, {
			dir: options.out,
			trace: `${options.out}/trace.json`
		} );
		await captures.start();
		try {
			results.scenario = await measure( page, "wall-jitter", 12000, async more => {
				await page.evaluate(
					destination =>
						__benchRuntime.session( { kind: "gameplay", command: { kind: "move", destination } } ),
					route.to
				);
				await keepGoing( page, more );
			} );
			results.end = await page.evaluate( () => ({
				pose: __benchRuntime.gameplay().pose,
				phase: __benchRuntime.sessionState().phase
			}) );
			assert.equal( results.end.phase, "world" );
			assert.ok(
				Math.hypot( results.end.pose.x - route.stop.x, results.end.pose.z - route.stop.z ) < 3,
				"server stops at certified wall"
			);
			const dx = route.stop.x - route.from.x, dz = route.stop.z - route.from.z, length = Math.hypot( dx, dz );
			let compared = 0;
			for ( const row of results.scenario.movement ) {
				if ( !row.displayed ) continue;
				const x = row.displayed.x - route.from.x, z = row.displayed.z - route.from.z;
				assert.equal( row.displayed.regionId, route.from.regionId );
				assert.ok( (x * dx + z * dz) / length <= length + .1, "presentation never crosses the wall" );
				assert.ok(
					Math.abs( x * dz - z * dx ) / length < 1,
					"presentation stays inside the admitted corridor"
				);
				compared++;
			}
			assert.ok( compared > 10 );
			results.compared = compared;
			await page.screenshot( { path: `${options.out}/wall.png` } );
		} finally {
			await captures.stop( "wall" );
			await captures.finish();
		}
	} catch ( error ) {
		results.failure = String( error );
		throw error;
	} finally {
		await writeFile( `${options.out}/results.json`, JSON.stringify( results, null, 2 ) );
		await closeClient( client );
	}
}

await run(
	parseOptions(
		process.argv.slice( 2 ),
		{ cpuRate: 4, out: "temp/artifacts/wall-recovery" },
		"wall-recovery-bench.mjs [--cpu-rate N] [--out DIR]"
	)
);
