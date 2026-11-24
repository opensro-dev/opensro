import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { bootPlayableSession, waitPlayableWorld, bindPlayableRuntime } from "./helpers/playable-session.mjs";
import { waitShownCharacters } from "./helpers/shown-characters.mjs";

test( "retail Restart counts down into the dock, re-enters, and Exit signs out", { timeout: 240000 }, async () => {
	const directory = "apps/client-next/temp/artifacts/restart-native";
	await mkdir( directory, { recursive: true } );
	const { browser, page } = await launchProbeBrowser();
	const events = [], errors = [];
	page.on( "pageerror", e => errors.push( e.message ) );
	page.on( "websocket", socket => {
		for ( const [event, direction] of [ [ "framesent", "out" ], [ "framereceived", "in" ] ] ) {
			socket.on( event, frame => {
				if ( typeof frame.payload === "string" ) {
					return;
				}
				const p = Buffer.from( frame.payload );
				if ( p.length < 2 ) return;
				const opcode = p.readUInt16LE();
				if ( [ 0x70b7, 0xb0b7, 0x315a, 5 ].includes( opcode ) ) {
					events.push( { at: Date.now(), direction, opcode, payload: [ ...p.subarray( 2 ) ] } );
				}
			} );
		}
	} );
	const dock = async () => {
		await page.waitForFunction(
			() =>
				__playableRuntime.sessionState()?.phase === "character-select" &&
				/Frontend: dock\n/.test( document.querySelector( "output" )?.textContent ),
			null,
			{ timeout: 45000 }
		);
		await waitShownCharacters( page, 1, 45000 );
	};
	async function depart( kind, type ) {
		const start = events.length;
		await page.keyboard.press( "Escape" );
		await page.locator( `[data-ui-id="system-${kind}"]` ).waitFor();
		await page.screenshot( { path: `${directory}/${kind}-menu.png` } );
		await page.locator( `[data-ui-id="system-${kind}"]` ).click();
		await page.waitForFunction(
			() => __playableRuntime.gameplay()?.notices?.some( n => n.key === "UIIT_MSG_LOGOUT_REMAIN_TIME" ),
			null,
			{ timeout: 10000 }
		);
		assert.equal( await page.evaluate( () => __playableRuntime.sessionState()?.phase ), "world" );
		assert.equal( await page.locator( '[data-ui-id="system-restart"]' ).count(), 0 );
		await page.screenshot( { path: `${directory}/${kind}-countdown.png` } );
		if ( type === 2 ) await dock();
		else {await page.waitForFunction( () => __playableRuntime.sessionState()?.phase === "signed-out", null, {
				timeout: 15000
			} );}
		const sequence = events.slice( start ),
			request = sequence.find( e => e.opcode === 0x70b7 ),
			reply = sequence.find( e => e.opcode === 0xb0b7 ),
			complete = sequence.find( e => e.opcode === 0x315a );
		assert.deepEqual( request?.payload, [ type ] );
		assert.deepEqual( reply?.payload, [ 1, 5, type ] );
		assert.deepEqual( complete?.payload, [] );
		assert.ok( complete.at - reply.at >= 4500, "the server must own the countdown" );
		assert.equal( sequence.filter( e => e.opcode === 0x70b7 ).length, 1 );
	}
	try {
		await bootPlayableSession( page, "asd2" );
		await depart( "restart", 2 );
		await page.screenshot( { path: directory + "/dock.png" } );
		assert.ok(
			!(await page.context().cookies()).some( c => c.name.endsWith( "SessionCharacter" ) ),
			"restart must retire automatic world restoration"
		);
		// Refresh must restore the dock, not silently re-enter the old character.
		await page.reload();
		await bindPlayableRuntime( page );
		await dock();
		await page.mouse.click( 505, 430 );
		await page.locator( '[data-ui-id="enter"]' ).waitFor();
		await page.waitForFunction( () => document.querySelector( '[data-ui-id="enter"]' )?.disabled === false );
		await page.locator( '[data-ui-id="enter"]' ).click();
		await waitPlayableWorld( page, "asd2" );
		await depart( "exit", 1 );
		assert.ok(
			!(await page.context().cookies()).some( c =>
				c.name === "SROLoopbackSession" || c.name === "__Host-SROSession"
			)
		);
		assert.deepEqual( errors, [] );
	} finally {
		const status = await page.locator( "output" ).textContent().catch( () => null );
		await writeFile( directory + "/incident.json", JSON.stringify( { events, errors, status }, null, 2 ) );
		await browser.close();
	}
} );
