/*
===========================================================================

berserk-live.test.mjs - live Berserk: armor, potion, Tab, appearance and expiry

Drives a real session: equips a complete heavy set through authoritative
moves, fills the gauge with the native potion, activates through Tab and
waits for the server's expiry. Needs the local stack and a scratch actor.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { openProbeAgentSession, readProbeCharacterSpawnFromSession } from "../../../../scripts/lib/probeSession.mjs";
import path from "node:path";
import { serverGameDataRoot } from "../../../../scripts/build/world/paths.mjs";
import { resetMissionMovementFixture } from "../../../../scripts/lib/missionMovementFixture.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile, readFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";
import { installPursuitRecorder } from "./helpers/pursuit-recorder.mjs";

test( "live complete armor, Berserk potion, Tab, appearance and server expiry", { timeout: 300000 }, async () => {
	const character = assertCharacterAllowed( "asd2", { context: "Berserk gameplay regression" } ),
		out = "temp/artifacts/berserk-live";
	await mkdir( out, { recursive: true } );
	const session = await openProbeAgentSession(),
		original = await readProbeCharacterSpawnFromSession( session, character );
	assert.ok( original );
	const data = await readFile( path.join( serverGameDataRoot, "textdata", "teleportbuilding.txt" ) ),
		rows = data.toString( data[0] === 255 ? "utf16le" : "utf8" ).replace( /^\uFEFF/, "" ).split( "\n" ).map( r =>
			r.trim().split( "\t" )
		),
		gate = rows.find( r => r[1] === "2094" );
	assert.ok( gate );
	const start = {
		regionId: Number( gate[41] ) & 65535,
		x: Number( gate[43] ) + 150,
		y: Number( gate[44] ),
		z: Number( gate[45] )
	};
	await resetMissionMovementFixture( {
		session,
		characterName: character,
		timeoutMs: 30000,
		fixture: { id: "berserk-safe-jangan", movementMode: 3, start, startYawRadians: 0 }
	} );
	const evidence = { character, original, start, errors: [], originalEquipment: [] };
	const { browser, page } = await launchProbeBrowser();
	try {
		page.on( "pageerror", e => evidence.errors.push( String( e ) ) );
		page.on( "console", m => {
			if ( m.type() === "error" && m.text().includes( "[SRO runtime]" ) ) evidence.errors.push( m.text() );
		} );
		await installPursuitRecorder( page, [ 0x30b3, 0x3122, 0x376f, 0xb341, 0xb5bd ] );
		console.log( "[berserk] authenticated scratch boot" );
		await bootPlayableSession( page, character );
		if (
			await page.evaluate( () => {
				const g = __playableRuntime.gameplay();
				return g.vitals.some( v => v.gid === g.localGid && v.hp === 0 );
			} )
		) {
			await page.locator( '[data-ui-id="rebirth-alternate"]' ).click();
			await page.waitForFunction( () => {
				const g = __playableRuntime.gameplay();
				return g.vitals.some( v => v.gid === g.localGid && v.hp > 0 );
			} );
		}
		evidence.before = await page.evaluate( () => ({
			gauge: __playableRuntime.berserkGauge(),
			entity: __playableRuntime.entities().find( e => e.gid === __playableRuntime.gameplay().localGid )
		}) );
		evidence.originalEquipment = await page.evaluate( () =>
			__playableRuntime.gameplay().inventory.filter( i => i.slot < 6 ).map( i => ({
				slot: i.slot,
				refObjId: i.refObjId
			}) )
		);
		async function move( source, destination ) {
			await page.evaluate(
				( { source, destination } ) =>
					__playableRuntime.session( {
						kind: "gameplay",
						command: { kind: "inventory-move", source, destination, quantity: 0 }
					} ),
				{ source, destination }
			);
			await page.waitForFunction(
				( { source, destination } ) =>
					!__playableRuntime.gameplay().inventory.some( i => i.slot === source ) &&
					__playableRuntime.gameplay().inventory.some( i => i.slot === destination ),
				{ source, destination },
				{ timeout: 10000 }
			);
		}
		for ( const item of evidence.originalEquipment ) {
			const free = await page.evaluate( () => {
				const inv = __playableRuntime.gameplay().inventory;
				return Array.from( { length: 32 }, ( _, i ) => i + 13 ).find( s => !inv.some( i => i.slot === s ) );
			} );
			assert.ok( free !== undefined );
			await move( item.slot, free );
		}
		const presentation = JSON.parse(
			await readFile( CLIENT_PUBLIC_ROOT + "/assets/data/missionPresentation.json", "utf8" )
		).itemsByRefObjId;
		console.log( "[equipment] equip complete scratch armor through authoritative moves" );
		for ( const [destination, part] of [ "CA", "BA", "SA", "AA", "LA", "FA" ].entries() ) {
			const code = `ITEM_CH_M_HEAVY_01_${part}_A`,
				refObjId = Number( Object.entries( presentation ).find( ( [, r] ) => r.codename === code )[0] );
			await page.evaluate(
				code =>
					__playableRuntime.session( {
						kind: "gameplay",
						command: { kind: "gm-command", line: "/MAKEITEM " + code + " 1" }
					} ),
				code
			);
			await page.waitForFunction(
				id => __playableRuntime.entities().some( e => e.kind === "ground-item" && e.refObjId === id ),
				refObjId,
				{ timeout: 10000 }
			);
			const gid = await page.evaluate(
				id => __playableRuntime.entities().find( e => e.kind === "ground-item" && e.refObjId === id ).gid,
				refObjId
			);
			await page.evaluate(
				gid => __playableRuntime.session( { kind: "gameplay", command: { kind: "pickup", gid } } ),
				gid
			);
			await page.waitForFunction(
				id => __playableRuntime.gameplay().inventory.some( i => i.slot >= 13 && i.refObjId === id ),
				refObjId,
				{ timeout: 10000 }
			);
			const source = await page.evaluate(
				id => __playableRuntime.gameplay().inventory.find( i => i.slot >= 13 && i.refObjId === id ).slot,
				refObjId
			);
			await move( source, destination );
		}
		await page.waitForFunction(
			() => {
				const actor = __playableRuntime.characterActors().find( a =>
					a.gid === __playableRuntime.gameplay().localGid
				);
				return actor?.model.includes( "ch_m_heavy_01" ) && actor.model.includes( "FA" );
			},
			null,
			{ timeout: 15000 }
		);
		evidence.equipped = await page.evaluate( () => ({
			inventory: __playableRuntime.gameplay().inventory.filter( i => i.slot < 6 ),
			actor: __playableRuntime.characterActors().find( a => a.gid === __playableRuntime.gameplay().localGid )
		}) );
		await page.screenshot( { path: out + "/full-armor.png" } );
		console.log( "[berserk] create and pick up native potion" );
		await page.evaluate( () =>
			__playableRuntime.session( {
				kind: "gameplay",
				command: { kind: "gm-command", line: "/MAKEITEM ITEM_ETC_GNGWC_WHAN 1" }
			} )
		);
		await page.waitForFunction(
			() => __playableRuntime.entities().some( e => e.kind === "ground-item" && e.refObjId === 23274 ),
			null,
			{ timeout: 15000 }
		);
		const gid = await page.evaluate( () =>
			__playableRuntime.entities().find( e => e.kind === "ground-item" && e.refObjId === 23274 ).gid
		);
		await page.evaluate(
			gid => __playableRuntime.session( { kind: "gameplay", command: { kind: "pickup", gid } } ),
			gid
		);
		await page.waitForFunction(
			() => __playableRuntime.gameplay().inventory.some( i => i.refObjId === 23274 ),
			null,
			{ timeout: 15000 }
		);
		const slot = await page.evaluate( () =>
			__playableRuntime.gameplay().inventory.find( i => i.refObjId === 23274 ).slot
		);
		await page.evaluate(
			slot => __playableRuntime.session( { kind: "gameplay", command: { kind: "item-use", slot } } ),
			slot
		);
		await page.waitForFunction( () => __playableRuntime.berserkGauge()?.displayed === 5, null, { timeout: 10000 } );
		await page.locator( '[data-ui-id="berserk"]' ).waitFor();
		await page.screenshot( { path: out + "/ready.png" } );
		console.log( "[berserk] activate through Tab" );
		await page.keyboard.press( "Tab" );
		await page.waitForFunction(
			() =>
				__playableRuntime.entities().find( e => e.gid === __playableRuntime.gameplay().localGid )
					?.appearanceState?.[2] ===
					1,
			null,
			{ timeout: 10000 }
		);
		await page.waitForFunction(
			() => __playableRuntime.characterActors().some( a => a.model.includes( "/char/hwan/" ) ),
			null,
			{
				timeout: 15000
			}
		);
		evidence.active = await page.evaluate( () => ({
			gauge: __playableRuntime.berserkGauge(),
			entity: __playableRuntime.entities().find( e => e.gid === __playableRuntime.gameplay().localGid ),
			actors: __playableRuntime.characterActors().filter( a =>
				a.gid === __playableRuntime.gameplay().localGid || a.model.includes( "/char/hwan/" )
			).map( a => ({
				gid: a.gid,
				model: a.model,
				scale: a.scale,
				tint: a.materialTint,
				clip: a.clip,
				attachment: a.attachment
			}) )
		}) );
		assert.equal( evidence.active.gauge.authoritative, 0 );
		assert.equal( evidence.active.entity.runSpeed, 100 );
		await page.screenshot( { path: out + "/active.png" } );
		evidence.activatedAt = Date.now();
		console.log( "[berserk] wait for authoritative expiry" );
		await page.waitForFunction(
			() =>
				__playableRuntime.entities().find( e => e.gid === __playableRuntime.gameplay().localGid )
					?.appearanceState?.[2] ===
					0,
			null,
			{ timeout: 65000 }
		);
		await page.waitForFunction(
			() => !__playableRuntime.characterActors().some( a => a.model.includes( "/char/hwan/" ) ),
			null,
			{
				timeout: 5000
			}
		);
		evidence.after = await page.evaluate( () => ({
			gauge: __playableRuntime.berserkGauge(),
			entity: __playableRuntime.entities().find( e => e.gid === __playableRuntime.gameplay().localGid )
		}) );
		assert.equal( evidence.after.entity.runSpeed, 50 );
		assert.notEqual( evidence.after.entity.appearanceState?.[0], 2, "death is not timer expiry" );
		assert.ok( Date.now() - evidence.activatedAt >= 58000, "Berserk ended before60s" );
		assert.deepEqual( evidence.errors, [] );
		evidence.verdict = "PASS SUCCESS";
		await page.screenshot( { path: out + "/expired.png" } );
	} catch ( error ) {
		evidence.failure = String( error );
		throw error;
	} finally {
		evidence.wire = await page.evaluate( () =>
			globalThis.__pursuit?.events.filter( e => [ 0x30b3, 0x3122, 0x376f, 0xb341, 0xb5bd ].includes( e.opcode ) )
		).catch( () => null );
		for ( let slot = 0; slot < 6; slot++ ) {
			await page.evaluate( slot => {
				const inv = __playableRuntime.gameplay().inventory;
				if ( !inv.some( i => i.slot === slot ) ) return;
				const destination = Array.from( { length: 32 }, ( _, i ) => i + 13 ).find( s =>
					!inv.some( i => i.slot === s )
				);
				if ( destination !== undefined ) {
					__playableRuntime.session( {
						kind: "gameplay",
						command: { kind: "inventory-move", source: slot, destination, quantity: 0 }
					} );
				}
			}, slot ).catch( () => {} );
			await page.waitForFunction(
				slot => !__playableRuntime.gameplay().inventory.some( i => i.slot === slot ),
				slot,
				{ timeout: 5000 }
			).catch( () => {} );
		}
		for ( const item of evidence.originalEquipment ) {
			await page.evaluate( ( { slot, refObjId } ) => {
				const source = __playableRuntime.gameplay().inventory.find( i =>
					i.slot >= 13 && i.refObjId === refObjId
				)?.slot;
				if ( source !== undefined ) {
					__playableRuntime.session( {
						kind: "gameplay",
						command: { kind: "inventory-move", source, destination: slot, quantity: 0 }
					} );
				}
			}, item ).catch( () => {} );
			await page.waitForFunction(
				( { slot, refObjId } ) =>
					__playableRuntime.gameplay().inventory.some( i => i.slot === slot && i.refObjId === refObjId ),
				item,
				{ timeout: 5000 }
			).catch( () => {} );
		}
		await page.screenshot( { path: out + "/final.png" } ).catch( () => {} );
		await page.evaluate( () => globalThis.__playableRuntime?.session( { kind: "logout" } ) ).catch( () => {} );
		await browser.close();
		await writeFile( out + "/incident.json", JSON.stringify( evidence, null, 2 ) );
		await resetMissionMovementFixture( {
			session,
			characterName: character,
			timeoutMs: 30000,
			fixture: {
				id: "restore-berserk-origin",
				movementMode: 3,
				start: original,
				startYawRadians: original.angle / 65535 * Math.PI * 2
			}
		} );
	}
} );
