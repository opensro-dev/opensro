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
import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { openClient, closeClient, createCaptures, measure, revive } from "../core/client.mjs";
import { parseOptions } from "../core/report.mjs";
import { walk } from "./scenarios.mjs";

import { installFaults } from "../core/transport-faults.mjs";

/*
================
captureVideo

CDP captures actual presented frames. Their timestamps preserve freezes in
the review video instead of speeding up a stalled run.
================
*/
async function captureVideo( page, dir ) {
	const cdp = await page.context().newCDPSession( page ), frames = [], writes = [];
	await mkdir( dir, { recursive: true } );
	cdp.on( "Page.screencastFrame", event => {
		void cdp.send( "Page.screencastFrameAck", { sessionId: event.sessionId } );
		if ( frames.length >= 300 ) return;
		const name = `frame-${String( frames.length ).padStart( 4, "0" )}.jpg`;
		frames.push( { name, at: event.metadata.timestamp } );
		writes.push( writeFile( `${dir}/${name}`, Buffer.from( event.data, "base64" ) ) );
	} );
	await cdp.send( "Page.startScreencast", {
		format: "jpeg",
		quality: 75,
		maxWidth: 960,
		maxHeight: 540,
		everyNthFrame: 12
	} );
	return async () => {
		await cdp.send( "Page.stopScreencast" );
		await Promise.all( writes );
		await cdp.detach();
		assert.ok( frames.length > 2, "capture must contain actual displayed frames" );
		const list = frames.map( ( frame, i ) =>
			`file '${frame.name}'\nduration ${Math.max( .001, (frames[i + 1]?.at ?? frame.at + .05) - frame.at )}`
		).join( "\n" );
		await writeFile( `${dir}/frames.txt`, list + "\n" );
		const encoded = spawnSync( process.env.SRO_PROBE_FFMPEG ?? "ffmpeg", [
			"-y",
			"-loglevel",
			"error",
			"-f",
			"concat",
			"-safe",
			"0",
			"-i",
			"frames.txt",
			"-fps_mode",
			"vfr",
			"-c:v",
			"libx264",
			"-pix_fmt",
			"yuv420p",
			"recovery.mp4"
		], { cwd: resolve( dir ), encoding: "utf8" } );
		assert.equal( encoded.status, 0, encoded.stderr || String( encoded.error ) );
	};
}

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
			const stopVideo = options.video && lane === "main" ?
				await captureVideo( page, `${options.out}/video` ) :
				null;
			await captures.start();
			const result = await measure( page, lane, 7000, async more => {
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
			} );
			await captures.stop( lane );
			await captures.finish();
			if ( stopVideo ) await stopVideo();
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
