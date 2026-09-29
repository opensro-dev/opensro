/*
===========================================================================

recall-appointment-live.test.mjs - live Dimensional Gate recall appointment

Selects the Jangan gate in a real session, checks the recall and teleport
capabilities, and confirms that cancel sends nothing while confirm appoints
the rebirth point. Needs the local stack and a scratch actor.

===========================================================================
*/
import { test } from "node:test";
import path from "node:path";
import { serverGameDataRoot } from "../../../../scripts/build/world/paths.mjs";
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { openProbeAgentSession, readProbeCharacterSpawnFromSession } from "../../../../scripts/lib/probeSession.mjs";
import { resetMissionMovementFixture } from "../../../../scripts/lib/missionMovementFixture.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";
import { installPursuitRecorder } from "./helpers/pursuit-recorder.mjs";

test( "live Dimensional Gate exposes recall and acknowledges appointment", { timeout: 180000 }, async () => {
	const character = assertCharacterAllowed( "asd2", { context: "recall appointment regression" } ),
		out = "temp/artifacts/recall-appointment-live";
	const session = await openProbeAgentSession(),
		original = await readProbeCharacterSpawnFromSession( session, character );
	assert.ok( original );
	const bytes = await readFile( path.join( serverGameDataRoot, "textdata", "teleportbuilding.txt" ) );
	const rows = bytes.toString( bytes[0] === 0xff && bytes[1] === 0xfe ? "utf16le" : "utf8" ).replace( /^\uFEFF/, "" )
		.split( "\n" ).map( r => r.trim().split( "\t" ) );
	const row = rows.find( r => r[1] === "2094" );
	assert.ok( row );
	const start = {
		regionId: Number( row[41] ) & 65535,
		x: Number( row[43] ) + 150,
		y: Number( row[44] ),
		z: Number( row[45] )
	};
	await mkdir( out, { recursive: true } );
	const evidence = { character, original, start, errors: [] };
	let browser, page;
	try {
		console.log( "[recall] prepare scratch placement" );
		await resetMissionMovementFixture( {
			session,
			characterName: character,
			timeoutMs: 30000,
			fixture: { id: "recall-jangan-gate", movementMode: 3, start, startYawRadians: 0 }
		} );
		({ browser, page } = await launchProbeBrowser());
		page.on( "pageerror", e => evidence.errors.push( String( e ) ) );
		await installPursuitRecorder( page, [ 0xb45a, 0xb20d ] );
		await page.route( "**/src/engine/runtime/ui/ui.ts*", async route => {
			const response = await route.fetch(), source = await response.text();
			assert.ok( source.includes( "export function createUi(" ) );
			await route.fulfill( {
				response,
				body: source.replace( "export function createUi(", "function createObservedUi(" ) +
					"\nexport function createUi(...args){const owner=createObservedUi(...args);return {...owner,step(view,now){globalThis.__recallView=view;return owner.step(view,now);}};}",
				contentType: "application/javascript"
			} );
		} );
		console.log( "[recall] authenticated mission boot" );
		await bootPlayableSession( page, character );
		const dead = await page.evaluate( () => {
			const g = __playableRuntime.gameplay();
			return g.vitals.some( v => v.gid === g.localGid && v.hp === 0 );
		} );
		if ( dead ) {
			assert.ok(
				await page.evaluate( () => (__playableRuntime.gameplay().progression?.level ?? Infinity) <= 10 ),
				"dead scratch actor must support present-position rebirth"
			);
			await page.locator( '[data-ui-id="rebirth-alternate"]' ).click();
			await page.waitForFunction(
				() => {
					const g = __playableRuntime.gameplay();
					return g.vitals.some( v => v.gid === g.localGid && v.hp > 0 );
				},
				null,
				{ timeout: 10000 }
			);
		}
		await page.waitForFunction(
			() => globalThis.__recallView?.entities.some( e => e.kind === "teleport" && e.refObjId === 2094 ),
			null,
			{ timeout: 20000 }
		);
		const gate = await page.evaluate( () =>
			__recallView.entities.find( e => e.kind === "teleport" && e.refObjId === 2094 )
		);
		evidence.gate = gate;
		console.log( "[recall] select live gate" );
		await page.evaluate(
			gid => __playableRuntime.session( { kind: "gameplay", command: { kind: "select", gid } } ),
			gate.gid
		);
		await page.waitForFunction( () => __playableRuntime.gameplay()?.npcConversation?.phase === "menu", null, {
			timeout: 20000
		} );
		evidence.capabilities = await page.evaluate( () => __playableRuntime.gameplay().targetCapabilities );
		await page.screenshot( { path: out + "/menu.png" } );
		assert.equal(
			evidence.capabilities & 0xc0,
			0xc0,
			"live service must publish independent recall + teleport capabilities"
		);
		await page.locator( '[data-ui-id="npc-recall-designate"]' ).waitFor( { timeout: 10000 } );
		evidence.menu = await page.locator( '[data-ui-id^="npc-"]' ).evaluateAll( nodes =>
			nodes.map( n => ({ id: n.dataset.uiId, label: n.getAttribute( "aria-label" ) }) )
		);
		await page.locator( '[data-ui-id="npc-recall-designate"]' ).click();
		await page.locator( '[data-ui-id="recall-cancel"]' ).click();
		assert.equal(
			await page.evaluate( () => __pursuit.events.filter( e => e.opcode === 0xb20d ).length ),
			0,
			"cancel cannot appoint"
		);
		await page.locator( '[data-ui-id="npc-recall-designate"]' ).click();
		await page.screenshot( { path: out + "/confirmation.png" } );
		await page.locator( '[data-ui-id="recall-confirm"]' ).click();
		await page.waitForFunction(
			() => __pursuit.events.some( e => e.opcode === 0xb20d && e.payload.length === 1 && e.payload[0] === 1 ),
			null,
			{ timeout: 10000 }
		);
		const notice = await page.evaluate( () =>
			__playableRuntime.gameplay().notices.find( n => n.key === "UIIT_MSG_STATE_REBIRTH_POINT_APPOINT" )
		);
		assert.equal( notice.nativeType, 5 );
		assert.equal( notice.banner, true );
		evidence.notice = notice;
		await page.screenshot( { path: out + "/success.png" } );
		assert.deepEqual( evidence.errors, [] );
		evidence.verdict = "PASS SUCCESS";
	} catch ( error ) {
		evidence.failure = String( error );
		throw error;
	} finally {
		if ( page ) {
			evidence.wire = await page.evaluate( () =>
				globalThis.__pursuit?.events.filter( e => e.opcode === 0xb45a || e.opcode === 0xb20d )
			).catch( () => null );
			await page.screenshot( { path: out + "/final.png" } ).catch( () => {} );
			await page.evaluate( () => globalThis.__playableRuntime?.session( { kind: "logout" } ) ).catch( () => {} );
		}
		await browser?.close();
		await writeFile( out + "/incident.json", JSON.stringify( evidence, null, 2 ) );
		await resetMissionMovementFixture( {
			session,
			characterName: character,
			timeoutMs: 30000,
			fixture: {
				id: "restore-recall-origin",
				movementMode: 3,
				start: original,
				startYawRadians: original.angle / 65535 * Math.PI * 2
			}
		} );
	}
} );
