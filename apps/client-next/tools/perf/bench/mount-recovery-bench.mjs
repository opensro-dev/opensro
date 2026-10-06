/*
===========================================================================

mount-recovery-bench.mjs - authenticated mount presentation under scheduling faults

Uses the operator-authorized scratch GM asd2 and its normal MAKEITEM command.
Rudolph is an authored level-five ride with a published model. No state or
network response is substituted. The temporary summoned ride is dismissed.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { openClient, closeClient, createCaptures, measure } from "../core/client.mjs";
import { installFaults } from "../core/transport-faults.mjs";
import { parseOptions } from "../core/report.mjs";
import { captureVisual } from "../core/visual-capture.mjs";
import { walk } from "./scenarios.mjs";

const RIDE_ITEM = 19571;

/*
================
run
================
*/
async function run( options ) {
	const client = await openClient( MISSION_MOVEMENT_FIXTURES.movement, {
		spans: true,
		uncapped: false,
		frameLimit: 60,
		cpuRate: options.cpuRate,
		beforeLogin: installFaults
	} );
	const { page } = client, results = { cpuRate: options.cpuRate, scenarios: [] };
	let gid;
	await mkdir( options.out, { recursive: true } );
	try {
		assert.ok(
			await page.evaluate( () => __benchRuntime.gameplay().eligibility?.gm ),
			"scratch GM privilege required"
		);
		assert.ok( !await page.evaluate( () => __benchRuntime.gameplay().activeCos ), "scratch has no existing ride" );
		const existing = await page.evaluate(
			ref => __benchRuntime.gameplay().inventory.find( row => row.refObjId === ref ),
			RIDE_ITEM
		);
		if ( !existing ) {
			const ground = await page.evaluate(
				ref => __benchRuntime.entities().some( row => row.refObjId === ref && row.groundItem ),
				RIDE_ITEM
			);
			if ( !ground ) {
				await page.evaluate( () =>
					__benchRuntime.session( {
						kind: "gameplay",
						command: { kind: "gm-command", line: "/MAKEITEM ITEM_COS_C_RUDOLPH 1" }
					} )
				);
			}
			await page.waitForFunction(
				ref => __benchRuntime.entities().some( row => row.refObjId === ref && row.groundItem ),
				RIDE_ITEM
			);
			const item = await page.evaluate(
				ref => __benchRuntime.entities().find( row => row.refObjId === ref && row.groundItem ).gid,
				RIDE_ITEM
			);
			await page.evaluate(
				gid => __benchRuntime.session( { kind: "gameplay", command: { kind: "pickup", gid } } ),
				item
			);
		}
		await page.waitForFunction(
			ref => __benchRuntime.gameplay().inventory.some( row => row.refObjId === ref ),
			RIDE_ITEM
		);
		const slot = await page.evaluate(
			ref => __benchRuntime.gameplay().inventory.find( row => row.refObjId === ref ).slot,
			RIDE_ITEM
		);
		await page.evaluate(
			slot => __benchRuntime.session( { kind: "gameplay", command: { kind: "item-use", slot } } ),
			slot
		);
		await page.waitForFunction( () => {
			const root = __benchRuntime, game = root.gameplay();
			return game.activeCos && root.entity( game.localGid )?.mountedOn === game.activeCos.gid;
		} );
		gid = await page.evaluate( () => __benchRuntime.gameplay().activeCos.gid );
		await page.waitForFunction( gid => __benchRuntime.characterActors().some( actor => actor.gid === gid ), gid );
		const worker = page.workers().find( row => row.url().includes( "/simulation/worker/" ) );
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
			for ( const lane of [ "main", "worker" ] ) {
				console.log( `[mount] ${lane} stalls and jitter` );
				const target = lane === "main" ? page : worker;
				results.scenarios.push(
					await measure( page, `mounted-${lane}-stalls`, 10000, async more => {
						await Promise.all( [
							walk( page, more ),
							(async () => {
								for ( const ms of [ 50, 100, 150, 300, 1000 ] ) {
									await page.waitForTimeout( 1000 );
									await target.evaluate( ms => {
										const end = performance.now() + ms;
										while ( performance.now() < end ) { /* Deliberate scheduling fault. */ }
									}, ms );
								}
							})()
						] );
					} )
				);
				assert.equal( await page.evaluate( () => __benchRuntime.sessionState().phase ), "world" );
			}
			await page.screenshot( { path: `${options.out}/mounted.png` } );
			for ( const scenario of results.scenarios ) {
				let compared = 0;
				for ( const row of scenario.movement ) {
					if ( !row.displayed || !row.mount ) continue;
					assert.equal( row.body.mountedOn, gid );
					const a = row.mount.pose, b = row.displayed;
					assert.equal( a.regionId, b.regionId );
					const cameraY = Math.fround( Math.fround( a.y + row.mount.height ) - 13 );
					assert.ok(
						Math.hypot( a.x - b.x, cameraY - b.y, a.z - b.z ) < .001,
						"mount and camera share presentation pose with the native riding height"
					);
					compared++;
				}
				assert.ok( compared > 10 );
				scenario.mountFrames = compared;
			}
		} finally {
			await captures.stop( "mount" );
			await captures.finish();
		}
		console.log( "[mount] separate visual capture" );
		results.visual = await captureVisual( page, options.out, async () => {
			const start = Date.now();
			await walk( page, () => Date.now() - start < 6000 );
		} );
	} catch ( error ) {
		results.failure = String( error );
		results.state = await page.evaluate( () => ({
			game: __benchRuntime.gameplay(),
			session: __benchRuntime.sessionState()
		}) );
		throw error;
	} finally {
		try {
			if ( gid ) {
				await page.evaluate(
					gid => __benchRuntime.session( { kind: "gameplay", command: { kind: "cos-clean", gid } } ),
					gid
				);
				await page.waitForFunction( () => !__benchRuntime.gameplay().activeCos );
				results.dismissed = true;
			}
		} finally {
			await writeFile( `${options.out}/results.json`, JSON.stringify( results, null, 2 ) );
			await closeClient( client );
		}
	}
}

await run(
	parseOptions(
		process.argv.slice( 2 ),
		{ cpuRate: 4, out: "temp/artifacts/mount-recovery" },
		"mount-recovery-bench.mjs [--cpu-rate N] [--out DIR]"
	)
);
