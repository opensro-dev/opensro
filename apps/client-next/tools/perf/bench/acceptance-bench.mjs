/*
===========================================================================

acceptance-bench.mjs - live crowd, movement and reconnect stress evidence

Uses scratch observer asd2 and separately provisioned real peers. Transport
faults delay actual bytes; measurements come from production frame probes.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { openClient, closeClient, createCaptures, measure } from "../core/client.mjs";
import { createCrowd } from "../core/crowd.mjs";
import { installFaults } from "../core/transport-faults.mjs";
import { parseOptions } from "../core/report.mjs";
import { walk, keepGoing } from "./scenarios.mjs";

/*
================
run
================
*/
async function run( options ) {
	assert.ok(
		options.tokenPath && options.provisioningUrl,
		"Explicit local provisioning endpoint and token path required"
	);
	const fixture = MISSION_MOVEMENT_FIXTURES.movement;
	const client = await openClient( fixture, {
		spans: true,
		uncapped: false,
		frameLimit: 60,
		cpuRate: options.cpuRate,
		beforeLogin: installFaults
	} );
	let crowd;
	const { page } = client;
	const results = { cpuRate: options.cpuRate, scenarios: [], peers: [] };
	try {
		await mkdir( options.out, { recursive: true } );
		results.scratch = await page.evaluate( () => {
			const game = __benchRuntime.gameplay();
			return { inventory: game.inventory, progression: game.progression, companions: game.cosRecords };
		} );
		console.log( "[acceptance] provision authenticated crowd" );
		crowd = await createCrowd( {
			count: options.peers,
			fixture,
			tokenPath: options.tokenPath,
			provisioningUrl: options.provisioningUrl,
			journalPath: `${options.out}/crowd-cleanup.json`
		} );
		results.peers = crowd.peers;
		await page.waitForFunction(
			names => {
				const entities = __benchRuntime.entities();
				return names.every( name => entities.some( entity => entity.name === name ) );
			},
			crowd.peers.map( peer => peer.character ),
			{ timeout: 30000 }
		);
		console.log( "[acceptance] every real peer is published to observer" );
		const worker = page.workers().find( candidate => candidate.url().includes( "/simulation/worker/" ) );
		assert.ok( worker );
		await worker.evaluate( () => {
			__recoveryLink.delay = 100;
			__recoveryLink.jitter = 25;
		} );
		const captures = await createCaptures( page, {
			dir: options.out,
			cpu: true,
			trace: `${options.out}/trace.json`
		} );
		await captures.start();
		try {
			for ( const [name, drive] of [ [ "crowd-idle-jitter", keepGoing ], [ "crowd-movement-jitter", walk ] ] ) {
				console.log( `[acceptance] ${name}` );
				results.scenarios.push(
					await measure( page, name, options.seconds * 1000, more => drive( page, more ) )
				);
			}
			for ( const lane of [ "main", "worker" ] ) {
				console.log( `[acceptance] crowd ${lane} stalls with transport jitter` );
				const destination = lane === "main" ? page : worker;
				results.scenarios.push(
					await measure( page, `crowd-${lane}-stalls-jitter`, 10000, async more => {
						await Promise.all( [
							walk( page, more ),
							(async () => {
								for ( const ms of [ 50, 100, 150, 300, 1000 ] ) {
									await page.waitForTimeout( 1000 );
									await destination.evaluate( ms => {
										const until = performance.now() + ms;
										while ( performance.now() < until ) { /* Deliberate scheduling fault. */ }
									}, ms );
								}
							})()
						] );
					} )
				);
			}
			console.log( "[acceptance] authored field slope with transport jitter" );
			results.scenarios.push(
				await measure( page, "slope-jitter", 12000, async more => {
					await page.evaluate( destination =>
						__benchRuntime.session( {
							kind: "gameplay",
							command: { kind: "move", destination }
						} ), fixture.destination );
					await keepGoing( page, more );
					const end = await page.evaluate( () => __benchRuntime.gameplay().pose );
					assert.equal( end.regionId, fixture.destination.regionId );
					assert.ok(
						Math.hypot( end.x - fixture.destination.x, end.z - fixture.destination.z ) < 8,
						"live slope movement reaches the authored destination"
					);
				} )
			);
			console.log( "[acceptance] explicit disconnect and authenticated resume" );
			await page.evaluate( () => __benchRuntime.session( { kind: "disconnect" } ) );
			await page.waitForFunction( () => __benchRuntime.sessionState().phase === "disconnected" );
			await page.evaluate( () => __benchRuntime.session( { kind: "reconnect" } ) );
			await page.waitForFunction( () => __benchRuntime.sessionState().phase === "world", null, {
				timeout: 30000
			} );
			results.scenarios.push(
				await measure( page, "reconnect-movement", options.seconds * 1000, more => walk( page, more ) )
			);
			results.transport = await worker.evaluate( () => ({ ...__recoveryLink }) );
			assert.ok( results.transport.rx > 0 && results.transport.tx > 0 && results.transport.peak <= 512 );
			assert.ok( crowd.peers.every( peer => peer.ready && !peer.closed ), "all peers stay connected" );
			await page.screenshot( { path: `${options.out}/crowd.png` } );
		} finally {
			await captures.stop( "acceptance" );
			await captures.finish();
		}
		for ( const result of results.scenarios ) {
			assert.ok( result.frames > 10 && result.movement.length > 10 );
			let bodyFrames = 0;
			for ( const sample of result.movement ) {
				if ( !sample.displayed ) continue;
				assert.ok( [ sample.displayed.x, sample.displayed.y, sample.displayed.z ].every( Number.isFinite ) );
				if ( !sample.body || sample.body.mountedOn ) continue;
				const a = sample.body.pose, b = sample.displayed;
				assert.equal( a.regionId, b.regionId );
				assert.ok(
					Math.hypot( a.x - b.x, a.y - b.y, a.z - b.z ) < .001,
					"body and camera use the same presentation position"
				);
				bodyFrames++;
			}
			assert.ok( bodyFrames > 10, "actual displayed body/camera comparisons are required" );
			result.bodyFrames = bodyFrames;
			console.log( `${result.name}: ${result.fps.toFixed( 1 )} FPS, p99 ${result.p99.toFixed( 1 )}ms` );
		}
	} catch ( error ) {
		results.failure = String( error );
		throw error;
	} finally {
		await writeFile( `${options.out}/results.json`, JSON.stringify( results, null, 2 ) );
		try {
			await crowd?.close();
		} finally {
			await closeClient( client );
		}
	}
}

await run( parseOptions(
	process.argv.slice( 2 ),
	{
		cpuRate: 1,
		seconds: 8,
		peers: 12,
		tokenPath: "",
		provisioningUrl: "",
		out: "temp/artifacts/live-acceptance"
	},
	"acceptance-bench.mjs --token-path PATH --provisioning-url URL [--peers N] [--cpu-rate N] [--seconds N] [--out DIR]"
) );
