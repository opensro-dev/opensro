/*
===========================================================================

travel-recovery-bench.mjs - actual gate transitions under faults

Places only asd2 beside the authored Jangan gate before login. Gate travel
must succeed, change relocation generation, and resume displayed movement.

===========================================================================
*/
import assert from "node:assert/strict";
import path from "node:path";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { serverGameDataRoot } from "../../../../../scripts/build/world/paths.mjs";
import { openClient, closeClient, createCaptures, measure } from "../core/client.mjs";
import { installFaults } from "../core/transport-faults.mjs";
import { parseOptions } from "../core/report.mjs";
import { walk, keepGoing } from "./scenarios.mjs";

/*
================
run
================
*/
async function run( options ) {
	const bytes = await readFile( path.join( serverGameDataRoot, "textdata", "teleportbuilding.txt" ) );
	const rows = bytes.toString( bytes[0] === 0xff && bytes[1] === 0xfe ? "utf16le" : "utf8" )
		.replace( /^\uFEFF/, "" ).split( "\n" ).map( row => row.trim().split( "\t" ) );
	const gate = rows.find( row => row[1] === "2094" );
	assert.ok( gate );
	const fixture = {
		id: "jangan-gate-recovery",
		movementMode: 3,
		startYawRadians: 0,
		start: {
			regionId: Number( gate[41] ) & 65535,
			x: Number( gate[43] ) + 150,
			y: Number( gate[44] ),
			z: Number( gate[45] )
		}
	};
	const client = await openClient( fixture, {
		spans: true,
		uncapped: false,
		frameLimit: 60,
		cpuRate: options.cpuRate,
		beforeLogin: installFaults
	} );
	const { page } = client, results = { cpuRate: options.cpuRate, scenarios: [] };
	await mkdir( options.out, { recursive: true } );
	try {
		results.scratch = await page.evaluate( () => {
			const game = __benchRuntime.gameplay();
			return { inventory: game.inventory, progression: game.progression, companions: game.cosRecords };
		} );
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
			console.log( "[travel] select real Jangan gate" );
			await page.waitForFunction( () =>
				__benchRuntime.entities().some( e => e.kind === "teleport" && e.refObjId === 2094 )
			);
			const gid = await page.evaluate( () =>
				__benchRuntime.entities().find( e => e.kind === "teleport" && e.refObjId === 2094 ).gid
			);
			await page.evaluate(
				gid => __benchRuntime.session( { kind: "gameplay", command: { kind: "select", gid } } ),
				gid
			);
			await page.waitForFunction( () => __benchRuntime.gameplay().npcConversation?.phase === "menu" );
			await page.locator( '[data-ui-id="npc-portal-open"]' ).click();
			await page.locator( '[data-ui-id="npc-portal:2"]' ).waitFor();
			const before = await page.evaluate( () => ({
				pose: __benchRuntime.gameplay().pose,
				generation: __benchRuntime.gameplay().movementTransition?.relocation
			}) );
			results.before = before;
			console.log( "[travel] paid gate teleport under transport jitter" );
			results.scenarios.push(
				await measure( page, "gate-jitter", 10000, async more => {
					await page.locator( '[data-ui-id="npc-portal:2"]' ).click();
					await page.waitForFunction(
						old => {
							const game = __benchRuntime.gameplay();
							return __benchRuntime.sessionState().phase === "world" &&
								game.pose?.regionId !== old.pose.regionId &&
								game.movementTransition?.relocation > old.generation;
						},
						before,
						{ timeout: 45000 }
					);
					await keepGoing( page, more );
				} )
			);
			results.scenarios.push( await measure( page, "after-gate-movement", 8000, more => walk( page, more ) ) );
			results.after = await page.evaluate( () => ({
				pose: __benchRuntime.gameplay().pose,
				generation: __benchRuntime.gameplay().movementTransition?.relocation
			}) );
			await page.screenshot( { path: `${options.out}/after-gate.png` } );
		} finally {
			await captures.stop( "travel" );
			await captures.finish();
		}
	} finally {
		await writeFile( `${options.out}/results.json`, JSON.stringify( results, null, 2 ) );
		await closeClient( client );
	}
}

await run(
	parseOptions(
		process.argv.slice( 2 ),
		{ cpuRate: 4, out: "temp/artifacts/travel-recovery" },
		"travel-recovery-bench.mjs [--cpu-rate N] [--out DIR]"
	)
);
