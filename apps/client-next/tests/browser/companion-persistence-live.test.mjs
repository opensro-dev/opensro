/*
===========================================================================

companion-persistence-live.test.mjs - authenticated item-owned pet lifecycle

Run only against an isolated local authority with a level-five scratch GM.
GM grants create unopened items. Browser input, normal gameplay commands and
real server replies own every subsequent pet transition. Sources and network
responses are never replaced. The retained scratch state permits a second
run after a complete server restart.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";
import { resolveProbeCredentials, resolveProbeDivisionId } from "../../../../scripts/lib/probeSession.mjs";

const CONTROL_TIMEOUT_MS = 15_000;
const SCENE_TIMEOUT_MS = 180_000;
const PET_ITEMS = [ "ITEM_COS_P_FLUTE", "ITEM_COS_P_RABBIT_SCROLL" ];

/*
================
bindRuntime
================
*/
async function bindRuntime( page ) {
	await page.evaluate( async () => {
		globalThis.__companionRuntime = (await import( "/src/bootstrap.ts" )).runtime;
	} );
}

/*
================
waitWorld
================
*/
async function waitWorld( page ) {
	await page.waitForFunction(
		() => {
			const state = globalThis.__companionRuntime.sessionState();
			if ( state?.phase === "failed" ) throw Error( state.error );
			return state?.phase === "world" &&
				document.querySelector( "output" )?.textContent.includes( "Frontend: world\n" );
		},
		null,
		{ timeout: SCENE_TIMEOUT_MS }
	);
	await page.locator( "#startup-loading" ).waitFor( { state: "hidden", timeout: SCENE_TIMEOUT_MS } );
}

/*
================
command
================
*/
async function command( page, value ) {
	await page.evaluate( value => __companionRuntime.session( { kind: "gameplay", command: value } ), value );
}

/*
================
snapshot
================
*/
async function snapshot( page ) {
	return page.evaluate( () => {
		const runtime = __companionRuntime, game = runtime.gameplay();
		return {
			items: game.inventory,
			pets: game.cosRecords ?? [],
			entities: runtime.entities().filter( row => row.kind === "cos" ),
			actors: runtime.characterActors().filter( row => game.cosRecords?.some( pet => pet.gid === row.gid ) ).map(
				row => ({ gid: row.gid, model: row.model, pose: row.pose, clip: row.clip })
			),
			error: game.error
		};
	} );
}

/*
================
waitCompanionActors

An inventory success alone does not prove that the native spawn reached the
scene and renderer. Require both independent identities in all three planes.
================
*/
async function waitCompanionActors( page ) {
	await page.waitForFunction(
		() => {
			const runtime = __companionRuntime, pets = runtime.gameplay().cosRecords ?? [];
			return pets.length === 2 &&
				pets.every( pet =>
					runtime.entities().some( row => row.gid === pet.gid && row.kind === "cos" ) &&
					runtime.characterActors().some( actor =>
						actor.gid === pet.gid && actor.model &&
						Number.isFinite( actor.pose.x ) && Number.isFinite( actor.pose.z )
					)
				);
		},
		null,
		{ timeout: SCENE_TIMEOUT_MS }
	);
}

/*
================
grantUnopenedSummoner

The GM command and ordinary pickup travel through authenticated transports.
No fixture installs a companion record or fabricates its server response.
================
*/
async function grantUnopenedSummoner( page, codename ) {
	const before = await page.evaluate( () => __companionRuntime.entities().map( row => row.gid ) );
	await command( page, { kind: "gm-command", line: `/MAKEITEM ${codename} 1` } );
	await page.waitForFunction(
		before =>
			__companionRuntime.entities().some( row => row.kind === "ground-item" && !before.includes( row.gid ) ),
		before
	);
	const item = await page.evaluate(
		before =>
			__companionRuntime.entities().find( row => row.kind === "ground-item" && !before.includes( row.gid ) ),
		before
	);
	await command( page, { kind: "pickup", gid: item.gid } );
	await page.waitForFunction(
		ref => __companionRuntime.gameplay().inventory.some( row => row.refObjId === ref ),
		item.refObjId
	);
}

/*
================
runCompanionLifecycle
================
*/
async function runCompanionLifecycle() {
	const character = assertCharacterAllowed( process.env.SRO_PROBE_CHARACTER ?? "asd2", {
		context: "companion persistence"
	} );
	assert.ok(
		[ "127.0.0.1", "localhost" ].includes( new URL( CLIENT_NEXT_BASE_URL ).hostname ),
		"isolated loopback only"
	);
	const directory = process.env.SRO_COMPANION_EVIDENCE ?? "temp/artifacts/companion-persistence";
	await mkdir( directory, { recursive: true } );
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } );
	page.setDefaultTimeout( CONTROL_TIMEOUT_MS );
	/** @type {{ stages: { name: string; state: any; }[]; errors: string[]; verdict?: string; failure?: string; last?: any; }} */
	const report = { stages: [], errors: [] };
	page.on( "pageerror", error => report.errors.push( error.message ) );
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		await bindRuntime( page );
		await page.waitForFunction( () => __companionRuntime.sessionState()?.phase === "signed-out" );
		const credentials = resolveProbeCredentials(), serverId = resolveProbeDivisionId();
		await page.evaluate( ( { credentials, serverId } ) =>
			__companionRuntime.session( {
				kind: "login",
				apiBase: location.origin + "/api",
				id: credentials.loginId,
				password: credentials.loginPassword,
				serverId
			} ), { credentials, serverId } );
		await page.waitForFunction( () => {
			const state = __companionRuntime.sessionState();
			return state?.phase === "character-select" && Array.isArray( state.characters );
		} );
		const roster = await page.evaluate( () => __companionRuntime.sessionState().characters );
		assert.equal( roster.length, 1, "dedicated scratch roster" );
		assert.equal( roster[0].name, character );
		assert.ok( roster[0].level >= 5, "scratch character must satisfy native summon level" );
		await page.locator( '[data-ui-id="frontend:create"]' ).waitFor( { timeout: SCENE_TIMEOUT_MS } );
		await page.mouse.click( 505, 430 );
		await page.locator( '[data-ui-id="enter"]' ).click( { timeout: SCENE_TIMEOUT_MS } );
		await waitWorld( page );
		assert.equal( await page.evaluate( () => __companionRuntime.gameplay().eligibility.gm ), true );
		await page.context().tracing.start( { screenshots: true, snapshots: true } );
		report.stages.push( { name: "entry", state: await snapshot( page ) } );
		let summoners = (await snapshot( page )).items.filter( row => row.summon );
		if ( !summoners.length ) {
			for ( const codename of PET_ITEMS ) await grantUnopenedSummoner( page, codename );
			summoners = (await snapshot( page )).items.filter( row => row.summon );
		}
		assert.equal( summoners.length, 2, "attack and pickup summoners" );
		await page.keyboard.press( "i" );
		for ( const item of summoners ) {
			if ( item.summon.state === 2 ) continue;
			await page.locator( `[data-ui-id="slot:${item.slot}"]` ).dblclick();
			await page.waitForFunction(
				slot =>
					__companionRuntime.gameplay().inventory.some( row => row.slot === slot && row.summon?.state === 2 ),
				item.slot
			);
		}
		await page.waitForFunction( () => __companionRuntime.gameplay().cosRecords?.length === 2 );
		await waitCompanionActors( page );
		const active = await snapshot( page );
		assert.equal( active.pets.find( pet => pet.band === 3 ).commandMode, 1 );
		assert.equal( active.pets.find( pet => pet.band === 3 ).status, 0 );
		assert.equal( active.pets.find( pet => pet.band === 4 ).commandMode, 7 );
		assert.equal( active.pets.find( pet => pet.band === 4 ).status, 28 );
		const pickup = active.pets.find( pet => pet.band === 4 );
		let bagItem = pickup.inventory.find( row => row.slot === 0 );
		if ( !bagItem ) {
			const source = active.items.find( row => row.refObjId === 4 && row.quantity === 50 );
			assert.ok( source, "scratch HP herbs for a real pet-bag transaction" );
			await command( page, {
				kind: "cos-transfer",
				gid: pickup.gid,
				toCos: true,
				source: source.slot,
				destination: 0
			} );
			await page.waitForFunction(
				gid =>
					__companionRuntime.gameplay().cosRecords.find( pet => pet.gid === gid )?.inventory.some( row =>
						row.slot === 0
					),
				pickup.gid
			);
			bagItem = (await snapshot( page )).pets.find( pet => pet.band === 4 ).inventory.find( row =>
				row.slot === 0
			);
		}
		assert.equal( bagItem.refObjId, 4 );
		assert.equal( bagItem.quantity, 50 );
		report.stages.push( { name: "pet-bag-deposit", state: await snapshot( page ) } );
		report.stages.push( { name: "both-summoned", state: active } );
		await page.screenshot( { path: `${directory}/both-summoned.png` } );
		const attack = active.pets.find( pet => pet.band === 3 );
		await page.locator( `[data-ui-id="slot:${attack.inventorySlot}"]` ).dblclick();
		await page.waitForFunction(
			gid => !__companionRuntime.gameplay().cosRecords?.some( row => row.gid === gid ),
			attack.gid
		);
		const dismissed = await snapshot( page );
		assert.equal( dismissed.pets.length, 1, "dismissal preserves the sister family" );
		assert.equal( dismissed.items.find( row => row.slot === attack.inventorySlot ).summon.state, 3 );
		report.stages.push( { name: "attack-dismissed", state: dismissed } );
		await page.locator( `[data-ui-id="slot:${attack.inventorySlot}"]` ).dblclick();
		await page.waitForFunction( () => __companionRuntime.gameplay().cosRecords?.length === 2 );
		await command( page, { kind: "cos-cancel", gid: pickup.gid } );
		await page.waitForFunction(
			gid => !__companionRuntime.gameplay().cosRecords.some( pet => pet.gid === gid ),
			pickup.gid
		);
		assert.equal( (await snapshot( page )).pets.length, 1, "pickup cancellation preserves the attack pet" );
		await page.locator( `[data-ui-id="slot:${pickup.inventorySlot}"]` ).dblclick();
		await page.waitForFunction( () => __companionRuntime.gameplay().cosRecords?.length === 2 );
		const retained = await snapshot( page );
		assert.equal( retained.pets.find( row => row.band === 3 ).refObjId, attack.refObjId );
		assert.deepEqual(
			retained.pets.find( row => row.band === 4 ).inventory.find( row => row.slot === 0 ),
			bagItem
		);
		await page.reload( { waitUntil: "commit" } );
		await bindRuntime( page );
		await waitWorld( page );
		await page.waitForFunction( () => __companionRuntime.gameplay().cosRecords?.length === 2 );
		await waitCompanionActors( page );
		const resumed = await snapshot( page );
		assert.deepEqual(
			resumed.pets.map( row => [ row.band, row.refObjId, row.inventorySlot, row.commandMode, row.status ] ).sort(
				( a, b ) => a[0] - b[0]
			),
			retained.pets.map( row => [ row.band, row.refObjId, row.inventorySlot, row.commandMode, row.status ] ).sort(
				( a, b ) => a[0] - b[0]
			)
		);
		report.stages.push( { name: "session-resumed", state: resumed } );
		assert.deepEqual( resumed.pets.find( row => row.band === 4 ).inventory.find( row => row.slot === 0 ), bagItem );
		const destination = Array.from( { length: 32 }, ( _, index ) => index + 13 ).find(
			slot => !resumed.items.some( row => row.slot === slot )
		);
		assert.notEqual( destination, undefined, "scratch inventory needs a free withdrawal slot" );
		await command( page, { kind: "cos-transfer", gid: pickup.gid, toCos: false, source: 0, destination } );
		await page.waitForFunction(
			slot => __companionRuntime.gameplay().inventory.some( row => row.slot === slot ),
			destination
		);
		const withdrawn = await snapshot( page );
		assert.equal( withdrawn.items.find( row => row.slot === destination ).quantity, bagItem.quantity );
		assert.equal( withdrawn.pets.find( row => row.band === 4 ).inventory.length, 0 );
		report.stages.push( { name: "pet-bag-withdrawal", state: withdrawn } );
		await command( page, {
			kind: "cos-transfer",
			gid: pickup.gid,
			toCos: true,
			source: destination,
			destination: 0
		} );
		await page.waitForFunction(
			gid => __companionRuntime.gameplay().cosRecords.find( pet => pet.gid === gid )?.inventory.length === 1,
			pickup.gid
		);
		await page.screenshot( { path: `${directory}/resumed.png` } );
		assert.deepEqual( report.errors, [] );
		report.verdict = "PASS";
	} catch ( error ) {
		report.verdict = "FAIL";
		report.failure = error instanceof Error ? error.stack ?? error.message : String( error );
		report.last = await snapshot( page ).catch( () => null );
		await page.screenshot( { path: `${directory}/failure.png` } ).catch( () => {} );
		throw error;
	} finally {
		await page.context().tracing.stop( { path: `${directory}/trace.zip` } ).catch( () => {} );
		await writeFile( `${directory}/report.json`, JSON.stringify( report, null, 2 ) );
		await browser.close();
	}
}

test( "authenticated companion summoning, family cancellation and persistence", {
	skip: process.env.SRO_COMPANION_LIVE !== "1",
	timeout: 480_000
}, runCompanionLifecycle );
