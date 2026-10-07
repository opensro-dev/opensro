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
warpFixture

Authorized scratch-GM setup, outside the measured paid gate transition.
================
*/
async function warpFixture( page, pose ) {
	const worker = page.workers().find( row => row.url().includes( "/simulation/worker/" ) );
	assert.ok( worker );
	const ready = await worker.evaluate( () => __recoveryLink.ready );
	const before = await page.evaluate( () => __benchRuntime.gameplay().movementTransition.relocation );
	await page.evaluate( pose =>
		__benchRuntime.session( {
			kind: "gameplay",
			command: { kind: "gm-command", line: `/warp ${pose.regionId} ${pose.x} ${pose.y} ${pose.z}` }
		} ), pose );
	await page.waitForFunction(
		( { pose, before } ) => {
			const game = __benchRuntime.gameplay();
			return __benchRuntime.sessionState().phase === "world" && game?.pose?.regionId === pose.regionId &&
				game.movementTransition.relocation > before &&
				Math.hypot( game.pose.x - pose.x, game.pose.z - pose.z ) < 8;
		},
		{ pose, before },
		{ timeout: 45000 }
	);
	// The world snapshot precedes render/resource admission. Commands are
	// admitted only after the production client sends its actual world-ready frame.
	const deadline = Date.now() + 60000;
	while ( await worker.evaluate( () => __recoveryLink.ready ) === ready && Date.now() < deadline ) {
		await page.waitForTimeout( 100 );
	}
	assert.ok(
		await worker.evaluate( () => __recoveryLink.ready ) > ready,
		"world-ready acknowledgement sent after warp"
	);
}

/*
================
ensureGateFare

The fixture can be poor after earlier tests. Fund only asd2 through the
existing GM ground-item and ordinary pickup/shop-sale authorities.
================
*/
async function ensureGateFare( page, pose, fee ) {
	const before = await page.evaluate( () => Number( __benchRuntime.gameplay().progression.gold ) );
	if ( before >= fee ) return { before, after: before };
	assert.ok(
		await page.evaluate( () => __benchRuntime.gameplay().eligibility?.gm ),
		"scratch GM setup privilege required"
	);
	await warpFixture( page, { regionId: 25000, x: 1600, y: 0, z: 1078 } );
	await page.evaluate( () =>
		__benchRuntime.session( {
			kind: "gameplay",
			command: { kind: "gm-command", line: "/MAKEITEM ITEM_CH_SWORD_03_A 0" }
		} )
	);
	await page.waitForFunction( () => __benchRuntime.entities().some( row => row.refObjId === 77 && row.groundItem ) );
	const item = await page.evaluate( () =>
		__benchRuntime.entities().find( row => row.refObjId === 77 && row.groundItem ).gid
	);
	await page.evaluate(
		gid => __benchRuntime.session( { kind: "gameplay", command: { kind: "pickup", gid } } ),
		item
	);
	await page.waitForFunction( () => __benchRuntime.gameplay().inventory.some( row => row.refObjId === 77 ) );
	const slot = await page.evaluate( () =>
		__benchRuntime.gameplay().inventory.find( row => row.refObjId === 77 ).slot
	);
	const npc = await page.evaluate( () =>
		__benchRuntime.entities().find( row => row.kind === "npc" && row.refObjId === 2008 )?.gid
	);
	assert.ok( npc, "authored Jangan accessory merchant is resident" );
	await page.evaluate( gid => __benchRuntime.session( { kind: "gameplay", command: { kind: "select", gid } } ), npc );
	await page.locator( '[data-ui-id="shop-open"]' ).click();
	await page.waitForFunction( () => __benchRuntime.gameplay().shop && !__benchRuntime.gameplay().inventoryPending );
	await page.evaluate(
		slot => __benchRuntime.session( { kind: "gameplay", command: { kind: "shop-sell", slot, quantity: 1 } } ),
		slot
	);
	await page.waitForFunction( fee => Number( __benchRuntime.gameplay().progression.gold ) >= fee, fee );
	const after = await page.evaluate( () => Number( __benchRuntime.gameplay().progression.gold ) );
	await warpFixture( page, pose );
	return { before, after, item: 77 };
}

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
	const links = await readFile( path.join( serverGameDataRoot, "textdata", "teleportlink.txt" ) );
	const link = links.toString( links[0] === 0xff && links[1] === 0xfe ? "utf16le" : "utf8" )
		.replace( /^\uFEFF/, "" ).split( "\n" ).map( row => row.trim().split( "\t" ) )
		.find( row => row[1] === "1" && row[2] === "2" );
	assert.ok( link );
	const fee = Number( link[3] );
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
		results.fare = fee;
		results.supply = await ensureGateFare( page, fixture.start, fee );
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
				gold: Number( __benchRuntime.gameplay().progression.gold ),
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
				gold: Number( __benchRuntime.gameplay().progression.gold ),
				generation: __benchRuntime.gameplay().movementTransition?.relocation
			}) );
			assert.equal( results.before.gold - results.after.gold, fee, "the actual gate fare is paid exactly once" );
			await page.screenshot( { path: `${options.out}/after-gate.png` } );
		} finally {
			await captures.stop( "travel" );
			await captures.finish();
		}
	} catch ( error ) {
		results.failure = String( error );
		results.state = await page.evaluate( () => ({
			session: __benchRuntime.sessionState(),
			game: __benchRuntime.gameplay()
		}) );
		await page.screenshot( { path: `${options.out}/failure.png` } );
		throw error;
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
