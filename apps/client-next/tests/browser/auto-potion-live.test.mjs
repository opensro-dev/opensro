/*
===========================================================================

auto-potion-live.test.mjs - automatic consumption on an authenticated session

Observe the real transport without replacing source or server responses.
The scratch actor starts at full vitals to exercise native accepted use with
no MP recovery room. Restore its configuration and binding after the probe.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { resolveProbeCredentials, resolveProbeDivisionId } from "../../../../scripts/lib/probeSession.mjs";
import { resetMissionMovementFixture } from "../../../../scripts/lib/missionMovementFixture.mjs";
import { openProbeAgentSession, readProbeCharacterSpawnFromSession } from "../../../../scripts/lib/probeSession.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";
import { decodeProbeAlphaFrame } from "../../../../scripts/lib/probeTransportProtocol.mjs";
import { bindPlayableRuntime, waitPlayableWorld } from "./helpers/playable-session.mjs";

const OBSERVED_OPCODES = new Set( [ 0x75bd, 0xb5bd, 0x33a6, 0x7541, 0xb541, 0x3122, 0x7338, 0xb338, 0x74b3, 0xb4b3 ] );
const DOCK_X = [ 180, 400, 615, 835 ];
const MP_REFERENCE = 11;
const HP_REFERENCE = 4;
const MP_TYPE = 0x10ec;
const REPLY_TIMEOUT_MS = 15000;

/*
================
command
================
*/
async function command( page, value ) {
	await page.evaluate( value => __playableRuntime.session( { kind: "gameplay", command: value } ), value );
}

/*
================
boot
================
*/
async function boot( page, character ) {
	await page.setViewportSize( { width: 1024, height: 768 } );
	await page.goto( CLIENT_NEXT_BASE_URL );
	await bindPlayableRuntime( page );
	await page.waitForFunction( () => __playableRuntime.sessionState()?.phase === "signed-out" );
	const { loginId, loginPassword } = resolveProbeCredentials();
	await page.evaluate( value =>
		__playableRuntime.session( {
			kind: "login",
			apiBase: location.origin + "/api",
			...value
		} ), { id: loginId, password: loginPassword, serverId: resolveProbeDivisionId() } );
	await page.waitForFunction( () => __playableRuntime.sessionState()?.characters?.length > 0 );
	await page.locator( '[data-ui-id="frontend:create"]' ).waitFor( { state: "visible", timeout: 60000 } );
	const names = await page.evaluate( () => __playableRuntime.sessionState().characters.map( row => row.name ) );
	const index = names.indexOf( character );
	assert.ok( index >= 0 && index < DOCK_X.length, "scratch character is in the normal dock roster" );
	assert.equal( names.length, 4, "this capture's dock coordinates require four actors" );
	await page.mouse.click( DOCK_X[index], 400 );
	await page.waitForFunction( () => document.querySelector( '[data-ui-id="enter"]' )?.matches( ":enabled" ) );
	await page.evaluate( character => __playableRuntime.session( { kind: "enter-world", character } ), character );
	await waitPlayableWorld( page, character );
}

/*
================
snapshot
================
*/
async function snapshot( page, phase ) {
	return page.evaluate( phase => {
		const game = __playableRuntime.gameplay();
		return {
			phase,
			at: Date.now(),
			session: __playableRuntime.sessionState()?.phase,
			settings: game.autoPotion,
			timers: game.itemCooldowns,
			pending: game.inventoryPending,
			shop: game.shop,
			conversation: game.npcConversation,
			vitals: game.vitals,
			inventory: game.inventory.map( item => ({
				slot: item.slot,
				reference: item.refObjId,
				quantity: item.quantity
			}) )
		};
	}, phase );
}

/*
================
acceptedMP
================
*/
function acceptedMP( wire ) {
	return wire.filter( row =>
		row.direction === "in" && row.opcode === 0xb5bd &&
		row.payload[0] === 1 && (row.payload[4] | row.payload[5] << 8) === MP_TYPE
	);
}

test( "automatic requests, receipts, full-gauge consumption, disable and reconnect", {
	skip: process.env.SRO_AUTO_POTION_LIVE !== "1",
	timeout: 180000
}, async () => {
	const character = assertCharacterAllowed( process.env.SRO_PROBE_CHARACTER ?? "asd2", {
		context: "auto-potion connected verification"
	} );
	const directory = "temp/artifacts/auto-potion-connected";
	await mkdir( directory, { recursive: true } );
	const { browser, page } = await launchProbeBrowser();
	/** @type {{ direction: string, at: number, opcode: number, payload: number[] }[]} */
	const wire = [];
	/** @type {{ verdict: string, character: string, samples: object[], errors: string[], failure?: string }} */
	const report = { verdict: "FAIL", character, samples: [], errors: [] };
	let original, binding, authority, originalSpawn, tracing = false;
	page.on( "pageerror", error => report.errors.push( String( error ) ) );
	page.on( "websocket", socket => {
		/*
		================
		record
		================
		*/
		function record( direction, event ) {
			if ( !Buffer.isBuffer( event.payload ) || event.payload.length < 2 ) return;
			const frame = decodeProbeAlphaFrame( event.payload );
			if ( OBSERVED_OPCODES.has( frame.opcode ) ) {
				wire.push( { direction, at: Date.now(), opcode: frame.opcode, payload: [ ...frame.payload ] } );
			}
		}
		socket.on( "framesent", event => record( "out", event ) );
		socket.on( "framereceived", event => record( "in", event ) );
	} );
	try {
		console.log( "[auto-potion] authenticated dock and world admission" );
		if ( process.env.SRO_POTION_SHOP_CLOSE === "1" ) {
			authority = await openProbeAgentSession();
			originalSpawn = await readProbeCharacterSpawnFromSession( authority, character );
			assert.ok( originalSpawn );
			await resetMissionMovementFixture( {
				session: authority,
				characterName: character,
				timeoutMs: 30000,
				fixture: {
					id: "potion-shop-close",
					movementMode: 3,
					start: { regionId: 27500, x: 686.56, y: 180, z: 216.67 },
					startYawRadians: 0
				}
			} );
		}
		await boot( page, character );
		await page.context().tracing.start( { screenshots: true, snapshots: true } );
		tracing = true;
		original = await page.evaluate( () => __playableRuntime.gameplay().autoPotion );
		binding = await page.evaluate( () =>
			__playableRuntime.gameplay().quickSlots.find( row => row.slot === 1 ) ??
				{ slot: 1, kind: 0, payload: 0 }
		);
		const disabled = {
			...original,
			hp: original.hp & 0x7fff,
			mp: original.mp & 0x7fff,
			cure: original.cure & 0x7fff
		};
		await command( page, { kind: "auto-potion-save", settings: disabled } );
		if ( process.env.SRO_POTION_SHOP_CLOSE === "1" ) {
			console.log( "[auto-potion] open and close a real merchant before automatic use" );
			await page.waitForFunction( () => __playableRuntime.entities().some( row => row.kind === "npc" ), null, {
				timeout: 15000
			} );
			const candidates = await page.evaluate( () => {
				const pose = __playableRuntime.gameplay().pose;
				return __playableRuntime.entities().filter( row => row.kind === "npc" ).map( row => ({
					gid: row.gid,
					distance: Math.hypot(
						row.x + ((row.regionId & 255) - (pose.regionId & 255)) * 1920 - pose.x,
						row.z + ((row.regionId >>> 8) - (pose.regionId >>> 8)) * 1920 - pose.z
					)
				}) ).sort( ( a, b ) => a.distance - b.distance ).slice( 0, 8 );
			} );
			console.log( "[auto-potion] NPC candidates", JSON.stringify( candidates ) );
			let opened = false;
			for ( const npc of candidates ) {
				await command( page, { kind: "select", gid: npc.gid } );
				await page.waitForFunction(
					gid => {
						const game = __playableRuntime.gameplay();
						return game.target === gid && !game.targetPending && game.npcConversation?.phase === "menu";
					},
					npc.gid,
					{ timeout: REPLY_TIMEOUT_MS }
				);
				if ( await page.evaluate( () => (__playableRuntime.gameplay().targetCapabilities & 1) !== 0 ) ) {
					await command( page, { kind: "shop-open", gid: npc.gid } );
					await page.waitForFunction( () => {
						const game = __playableRuntime.gameplay();
						return game.shop && !game.inventoryPending;
					} );
					opened = true;
				}
				await command( page, { kind: "npc-close" } );
				await page.waitForFunction( () => {
					const game = __playableRuntime.gameplay();
					return game.target === 0 && !game.targetPending && game.npcConversation.phase === "closed";
				} );
				if ( opened ) break;
			}
			assert.ok( opened, "a real merchant catalog was opened" );
			assert.ok( await page.evaluate( () => __playableRuntime.gameplay().shop ), "closed catalog stays cached" );
		}
		const initial = await snapshot( page, "initial" );
		report.samples.push( initial );
		const mp = initial.inventory.find( row => row.reference === MP_REFERENCE );
		const hp = initial.inventory.find( row => row.reference === HP_REFERENCE );
		assert.ok( mp && mp.quantity >= 5 && hp && hp.quantity >= 1, "scratch potion stacks required" );
		await command( page, { kind: "quickslot-set", binding: { slot: 1, kind: 0x46, payload: mp.slot - 13 } } );
		await page.waitForFunction(
			slot =>
				__playableRuntime.gameplay().quickSlots.some( row =>
					row.slot === 1 && row.kind === 0x46 && row.payload === slot - 13
				),
			mp.slot
		);
		const enabled = { ...disabled, mp: 0xe411, timing: 0x8a };
		await command( page, { kind: "auto-potion-save", settings: enabled } );
		console.log( "[auto-potion] trigger vitals through one normal HP item request" );
		await command( page, { kind: "item-use", slot: hp.slot } );
		await page.waitForFunction(
			( { slot, quantity } ) =>
				(__playableRuntime.gameplay().inventory.find( row => row.slot === slot )?.quantity ?? 0) <=
					quantity - 3,
			{ slot: mp.slot, quantity: mp.quantity },
			{ timeout: REPLY_TIMEOUT_MS }
		);
		report.samples.push( await snapshot( page, "three-automatic-uses" ) );
		await command( page, { kind: "auto-potion-save", settings: disabled } );
		await page.waitForFunction( () => !__playableRuntime.gameplay().inventoryPending );
		const stopped = await snapshot( page, "disabled" );
		report.samples.push( stopped );
		await page.waitForTimeout( 3200 );
		const after = await snapshot( page, "disabled-after-three-retries" );
		report.samples.push( after );
		assert.deepEqual( after.inventory, stopped.inventory, "disable stops further consumption" );
		const accepted = acceptedMP( wire );
		assert.ok( accepted.length >= 3, "successful automatic MP receipts observed" );
		for ( let i = 1; i < accepted.length; i++ ) {
			assert.ok( accepted[i].at - accepted[i - 1].at >= 1000, "accepted uses respect server reuse" );
		}
		assert.equal(
			mp.quantity - after.inventory.find( row => row.slot === mp.slot ).quantity,
			accepted.length,
			"one stack debit per accepted receipt"
		);
		assert.ok( wire.some( row => row.direction === "out" && row.opcode === 0x75bd && row.payload[0] === mp.slot ) );
		assert.ok( report.samples[1].timers.some( timer => timer.category === 2 ), "receipt installs MP clock" );
		console.log( "[auto-potion] disconnect and reconnect with saved disabled configuration" );
		await page.evaluate( () => __playableRuntime.session( { kind: "disconnect" } ) );
		await page.waitForFunction( () => __playableRuntime.sessionState()?.phase === "disconnected" );
		await page.evaluate( () => __playableRuntime.session( { kind: "reconnect" } ) );
		await page.waitForFunction( () => __playableRuntime.sessionState()?.phase !== "disconnected" );
		await waitPlayableWorld( page, character );
		const reconnected = await snapshot( page, "reconnected" );
		report.samples.push( reconnected );
		assert.deepEqual( reconnected.settings, disabled, "server bootstrap retains settings" );
		// The operator's existing beta profile refills HP/MP on entry. Require
		// explicit selection; an unexpected refill must fail a native-profile run.
		const expectedInventory = process.env.SRO_POTION_EXPECT_BETA_REFILL === "1" ?
			initial.inventory :
			after.inventory;
		assert.deepEqual( reconnected.inventory, expectedInventory, "reconnect follows the declared server profile" );
		const receiptsAfterReconnect = acceptedMP( wire ).length;
		await page.waitForTimeout( 2200 );
		assert.equal(
			acceptedMP( wire ).length,
			receiptsAfterReconnect,
			"reconnect cannot reactivate disabled channels"
		);
		assert.deepEqual( report.errors, [] );
		report.verdict = "PASS";
		console.log( "[auto-potion] PASS", JSON.stringify( { accepted: accepted.length, frames: wire.length } ) );
	} catch ( error ) {
		report.failure = String( error );
		throw error;
	} finally {
		if ( original ) await command( page, { kind: "auto-potion-save", settings: original } ).catch( () => {} );
		if ( binding ) await command( page, { kind: "quickslot-set", binding } ).catch( () => {} );
		await page.screenshot( { path: directory + "/screen.png" } ).catch( () => {} );
		if ( tracing ) await page.context().tracing.stop( { path: directory + "/trace.zip" } ).catch( () => {} );
		await writeFile(
			directory + "/incident.json",
			JSON.stringify(
				{
					...report,
					betaRefill: process.env.SRO_POTION_EXPECT_BETA_REFILL === "1",
					original,
					binding,
					wire
				},
				null,
				2
			)
		);
		await browser.close();
		if ( authority && originalSpawn ) {
			await resetMissionMovementFixture( {
				session: authority,
				characterName: character,
				timeoutMs: 30000,
				fixture: {
					id: "restore-potion-shop-close",
					movementMode: 3,
					start: originalSpawn,
					startYawRadians: originalSpawn.angle / 65535 * Math.PI * 2
				}
			} );
		}
	}
} );
