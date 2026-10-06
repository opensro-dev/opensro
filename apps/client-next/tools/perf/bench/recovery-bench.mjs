/*
===========================================================================

recovery-bench.mjs - real-session scheduling and transport recovery evidence

Uses the shared authenticated launcher and asd2 fixture. Faults delay real
WebSocket bytes and scheduling; they never manufacture gameplay replies or
rewrite a production source file. The pre-login hook runs in the existing simulation worker before its first
world connection; the production entry and message delivery stay untouched.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { openClient, closeClient, createCaptures, measure, revive } from "../core/client.mjs";
import { parseOptions } from "../core/report.mjs";
import { walk } from "./scenarios.mjs";

import { installFaults } from "../core/transport-faults.mjs";

import { captureVisual } from "../core/visual-capture.mjs";

/*
================
run
================
*/
async function run( options ) {
	const client = await openClient( MISSION_MOVEMENT_FIXTURES.region_cross, {
		spans: true,
		uncapped: false,
		cpuRate: options.cpuRate,
		beforeLogin: installFaults
	} );
	const { page } = client;
	try {
		let worker;
		for ( const candidate of page.workers() ) {
			if ( await candidate.evaluate( () => !!globalThis.__recoveryLink ) ) worker = candidate;
		}
		assert.ok( worker, "instrumented simulation worker must exist" );
		await mkdir( options.out, { recursive: true } );
		const results = [];
		for ( const lane of [ "main", "worker", "transport" ] ) {
			if ( !options.only.includes( lane ) ) continue;
			await revive( page );
			await worker.evaluate( delay => {
				globalThis.__recoveryLink.delay = delay;
				globalThis.__recoveryLink.jitter = 25;
			}, lane === "transport" ? 100 : 0 );
			const captures = await createCaptures( page, {
				dir: options.out,
				cpu: true,
				trace: options.trace ? `${options.out}/${lane}.json` : null
			} );
			/*
			================
			drive
			================
			*/
			const drive = async more => {
				const moving = walk( page, more );
				if ( lane !== "transport" ) {
					for ( const gap of [ 50, 100, 150, 300, 1000 ] ) {
						await page.waitForTimeout( 650 );
						await (lane === "main" ? page : worker).evaluate( ms => {
							const until = performance.now() + ms;
							while ( performance.now() < until ) { /* Deliberate scheduling fault. */ }
						}, gap );
					}
				}
				await moving;
			};
			await captures.start();
			const result = await measure( page, lane, 7000, drive );
			await captures.stop( lane );
			await captures.finish();
			if ( options.video && lane === "main" ) {
				result.visual = await captureVisual( page, `${options.out}/video`, async () => {
					const start = Date.now();
					await drive( () => Date.now() - start < 7000 );
				} );
			}
			result.transport = await worker.evaluate( () => ({ ...globalThis.__recoveryLink }) );
			assert.ok(
				result.transport.rx > 0 && result.transport.tx > 0,
				"real WebSocket traffic must cross the fault injector"
			);
			assert.ok( result.movement.length > 50, "record worker and displayed poses" );
			assert.ok(
				result.movement.every( row => !row.displayed || Number.isFinite( row.displayed.x ) ),
				"all displayed positions are finite"
			);
			let bodyFrames = 0;
			for ( const sample of result.movement ) {
				if ( !sample.body || !sample.displayed || sample.body.mountedOn ) continue;
				assert.ok( sample.body.clip, "local body must have an authored animation" );
				const a = sample.body.pose, b = sample.displayed;
				assert.equal( a.regionId, b.regionId );
				assert.ok(
					Math.hypot( a.x - b.x, a.y - b.y, a.z - b.z ) < .001,
					"body and camera must consume the same presentation pose"
				);
				bodyFrames++;
			}
			assert.ok( bodyFrames > 50, "actual body/camera comparisons are required" );
			result.bodyFrames = bodyFrames;
			result.session = await page.evaluate( () => globalThis.__benchRuntime.sessionState().phase );
			result.sessionError = await page.evaluate( () => globalThis.__benchRuntime.sessionState().error );
			results.push( result );
			await writeFile( `${options.out}/results.json`, JSON.stringify( results, null, 2 ) );
			assert.equal(
				result.session,
				"world",
				JSON.stringify( { error: result.sessionError, closes: result.transport.closes } )
			);
			console.log(
				JSON.stringify( {
					lane,
					cpuRate: options.cpuRate,
					frames: result.frames,
					p95: result.p95,
					p99: result.p99,
					transport: result.transport
				} )
			);
			await page.waitForTimeout( 1000 );
		}
	} finally {
		await closeClient( client );
	}
}

await run( parseOptions( process.argv.slice( 2 ), {
	cpuRate: 1,
	out: "temp/artifacts/recovery",
	trace: false,
	video: false,
	only: [ "main", "worker", "transport" ]
}, "recovery-bench.mjs [--cpu-rate 4] [--out DIR] [--only main,worker,transport] [--trace] [--video]" ) );
