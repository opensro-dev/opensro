/*
===========================================================================

login-flow.test.mjs - the title login against the live development stack

Drives the real login window in a browser: native presentation through the
authenticated roster, invalid-credential retries, the full-roster refusal,
and edits that keep focus while the server list is still in flight.

===========================================================================
*/
import { test } from "node:test";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { resolveProbeCredentials } from "../../../../scripts/lib/probeSession.mjs";
import { checkNativeButton } from "./helpers/native-button-oracle.mjs";
test( "login retains native presentation through authenticated roster arrival", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } ),
		events = [],
		errors = [];
	let roster = [];
	const control = id => page.locator( `[data-ui-id="${id}"]` );
	await mkdir( "temp/artifacts/login-regression", { recursive: true } );
	page.on( "pageerror", e => errors.push( e.message ) );
	page.on( "framenavigated", frame => {
		if ( frame === page.mainFrame() ) events.push( { navigation: frame.url() } );
	} );
	page.on( "console", message => {
		if ( message.text().includes( "[vite]" ) ) events.push( { vite: message.text() } );
	} );
	page.on( "response", r => {
		if ( /\/title\/|\/character\/list|server_rollover|uibutton/.test( r.url() ) ) {
			events.push( { url: r.url(), status: r.status() } );
		}
	} );
	page.on( "response", async r => {
		if ( r.url().endsWith( "/character/list" ) && r.ok() ) roster = (await r.json()).characters ?? [];
	} );
	try {
		await page.addInitScript( () => {
			window.__uiSoundStarts = 0;
			const start = AudioBufferSourceNode.prototype.start;
			AudioBufferSourceNode.prototype.start = function( ...args ) {
				if ( this.buffer && this.buffer.duration < 2 ) window.__uiSoundStarts++;
				return start.apply( this, args );
			};
		} );
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await control( "frontend:reveal" ).click( { timeout: 30000 } );
		await page.waitForFunction( () =>
			getComputedStyle( document.documentElement ).cursor.includes( "sro_client_cursor_0x95.cur" )
		);
		await control( "login" ).waitFor( { timeout: 15000 } );
		await page.waitForFunction( () =>
			document.querySelector( "output" )?.textContent.includes( "Frontend: login\n" )
		);
		await page.waitForFunction( () => !document.querySelector( '[data-ui-id="login"]' )?.disabled );
		const { loginId, loginPassword } = resolveProbeCredentials();
		await control( "account" ).fill( loginId );
		await control( "password" ).fill( loginPassword );
		assert.equal( await control( "account" ).getAttribute( "autocomplete" ), "off" );
		assert.equal( await control( "password" ).getAttribute( "autocomplete" ), "off" );
		assert.equal( await page.locator( "img[data-retail-cursor]" ).count(), 0 );
		await control( "password" ).press( "Enter" );
		await control( "frontend:create" ).waitFor( { timeout: 30000 } );
		await page.waitForFunction(
			count =>
				new RegExp( "Characters: " + count + " actors" ).test(
					document.querySelector( "output" )?.textContent
				),
			Math.min( 4, roster.length ),
			{ timeout: 30000 }
		);
		await page.waitForFunction( () => /Frontend: dock\n/.test( document.querySelector( "output" )?.textContent ) );
		const status = await page.locator( "output" ).textContent(),
			labels = await page.locator( "[data-gpu-ui] button" ).allTextContents();
		await page.screenshot( { path: "temp/artifacts/login-regression/dock.png" } );
		await writeFile(
			"temp/artifacts/login-regression/result.json",
			JSON.stringify( { events, errors, status, labels, roster }, null, 2 )
		);
		assert.deepEqual( errors, [] );
		assert.match( status, /Frontend: dock\n/ );
		assert.ok( roster.length > 0 );
		assert.match( status, new RegExp( "Characters: " + Math.min( 4, roster.length ) + " actors" ) );
		assert.ok(
			await page.evaluate( () => window.__uiSoundStarts ) > 0,
			"Native UI buffers must actually start playback"
		);
		assert.equal( await page.locator( "#startup-loading" ).getAttribute( "data-active" ), "false" );
		// The development roster's middle model is scratch character asd2. Select
		// through the rendered mesh hit test; do not send an enter-world request.
		assert.equal( roster[1]?.name, "asd2" );
		await page.mouse.move( 505, 430 );
		await page.waitForTimeout( 600 );
		await page.screenshot( { path: "temp/artifacts/login-regression/hover-name.png" } );
		await page.mouse.click( 505, 430 );
		await page.waitForTimeout( 800 );
		await writeFile(
			"temp/artifacts/login-regression/pick.json",
			JSON.stringify( { status: await page.locator( "output" ).textContent(), errors }, null, 2 )
		);
		await control( "enter" ).waitFor( { timeout: 5000 } );
		await page.waitForFunction( () => !document.querySelector( '[data-ui-id="enter"]' )?.disabled );
		await page.screenshot( { path: "temp/artifacts/login-regression/selected.png" } );
		await control( "dock:back" ).click();
		await control( "frontend:create" ).waitFor();
	} catch ( error ) {
		await writeFile(
			"temp/artifacts/login-regression/failure.json",
			JSON.stringify(
				{
					events,
					errors,
					status: await page.locator( "output" ).textContent(),
					controls: await page.locator( "[data-ui-id]" ).evaluateAll( nodes =>
						nodes.map( n => ({ id: n.dataset.uiId, disabled: n.disabled }) )
					),
					message: await page.locator( '[data-gpu-ui] [role="status"]' ).textContent()
				},
				null,
				2
			)
		);
		throw error;
	} finally {
		await browser.close();
	}
} );
test( "invalid credentials retain the native title status and allow an Enter retry", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } ),
		control = id => page.locator( `[data-ui-id="${id}"]` ),
		invalidId = "UiParity" + Date.now().toString( 36 );
	let attempts = 0;
	try {
		await page.addInitScript( () => {
			window.__uiStarts = [];
			const start = AudioBufferSourceNode.prototype.start;
			AudioBufferSourceNode.prototype.start = function( ...args ) {
				if ( this.buffer ) window.__uiStarts.push( this.buffer.duration );
				return start.apply( this, args );
			};
		} );
		await page.route( "**/title/login", async route => {
			attempts++;
			const response = await route.fetch(), body = await response.json();
			assert.equal( body.nativeTitleStatus, 2 );
			assert.equal( body.nativeTitleArgument, 5 * 65536 + attempts );
			await page.waitForFunction( () => document.querySelector( '[data-ui-id="login"]' )?.disabled );
			await page.mouse.move( 1, 200 );
			const pixels = [
				await checkNativeButton( page, "login", "GDR_BTN_OK" ),
				await checkNativeButton( page, "logout", "GDR_BTN_CANCEL" )
			];
			await mkdir( "temp/artifacts/login-regression", { recursive: true } );
			await writeFile(
				`temp/artifacts/login-regression/pending-${attempts}.json`,
				JSON.stringify( pixels, null, 2 )
			);
			return route.fulfill( { response } );
		} );
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await control( "frontend:reveal" ).click( { timeout: 30000 } );
		await control( "account" ).waitFor();
		await page.waitForFunction( () =>
			document.querySelector( "output" )?.textContent.includes( "Frontend: login\n" )
		);
		await control( "account" ).fill( invalidId );
		await control( "password" ).fill( "not-a-real-password" );
		await control( "password" ).press( "Enter" );
		await page.waitForFunction( () =>
			document.querySelector( '[data-gpu-ui] [role="status"]' )?.textContent ===
				"Password entry has failed 1 out of 5 times."
		);
		assert.equal( attempts, 1 );
		await page.waitForFunction( () =>
			window.__uiStarts.filter( n => Math.abs( n - 13208 / 22050 ) < .0001 ).length >= 2
		);
		assert.equal(
			await control( "password" ).inputValue(),
			"not-a-real-password",
			"a rejected login keeps the typed password for correction"
		);
		await control( "password" ).press( "NumpadEnter" );
		await page.waitForFunction( () =>
			document.querySelector( '[data-gpu-ui] [role="status"]' )?.textContent ===
				"Password entry has failed 2 out of 5 times."
		);
		assert.equal( attempts, 2 );
		await page.waitForFunction( () =>
			window.__uiStarts.filter( n => Math.abs( n - 13208 / 22050 ) < .0001 ).length >= 4
		);
		assert.equal( await control( "account" ).inputValue(), invalidId );
		assert.equal( await control( "password" ).inputValue(), "not-a-real-password" );
		await page.screenshot( { path: "temp/artifacts/login-regression/invalid-credentials.png" } );
	} finally {
		await browser.close();
	}
} );

test( "full roster keeps Create active and displays the native formatted refusal", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } ),
		control = id => page.locator( `[data-ui-id="${id}"]` );
	try {
		await page.route( "**/character/list", async route => {
			const response = await route.fetch(), body = await response.json();
			assert.ok( body.characters.length );
			const row = body.characters[0];
			return route.fulfill( {
				response,
				json: {
					...body,
					characters: Array.from(
						{ length: 4 },
						( _, i ) => ({ ...row, id: 900000 + i, name: `Parity${i}`, deletePending: false })
					)
				}
			} );
		} );
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await control( "frontend:reveal" ).click( { timeout: 30000 } );
		await control( "account" ).waitFor();
		await page.waitForFunction( () =>
			document.querySelector( "output" )?.textContent.includes( "Frontend: login\n" )
		);
		const { loginId, loginPassword } = resolveProbeCredentials();
		await control( "account" ).fill( loginId );
		await control( "password" ).fill( loginPassword );
		await control( "password" ).press( "Enter" );
		await control( "frontend:create" ).waitFor( { timeout: 30000 } );
		await page.waitForFunction( () =>
			document.querySelector( "output" )?.textContent.includes( "Frontend: dock\n" )
		);
		assert.equal( await control( "frontend:create" ).isDisabled(), false );
		await control( "frontend:create" ).click();
		await page.waitForFunction( () =>
			document.querySelector( '[data-gpu-ui] [role="status"]' )?.textContent ===
				"Maximum of 4 characters can be created."
		);
		assert.match( await page.locator( "output" ).textContent(), /Frontend: dock\n/ );
		assert.equal( await control( "frontend:create" ).isDisabled(), false );
		await page.mouse.click( 1, 200 );
		await page.waitForFunction( () => document.activeElement?.getAttribute( "data-ui-id" ) !== "frontend:create" );
		await page.waitForTimeout( 100 );
		await page.screenshot( { path: "temp/artifacts/login-regression/full-roster-before-oracle.png" } );
		const pixels = await checkNativeButton( page, "frontend:create", "GDR_BTN_CREATE", "pscharacterselect_europe" );
		await mkdir( "temp/artifacts/login-regression", { recursive: true } );
		await page.screenshot( { path: "temp/artifacts/login-regression/full-roster.png" } );
		await writeFile(
			"temp/artifacts/login-regression/full-roster.json",
			JSON.stringify(
				{
					pixels,
					message: await page.locator( '[data-gpu-ui] [role="status"]' ).textContent(),
					createEnabled: !await control( "frontend:create" ).isDisabled(),
					fixture: "Response-only cloned roster; no character mutation or world entry"
				},
				null,
				2
			)
		);
	} finally {
		await browser.close();
	}
} );

// On a real network the title's server-list request takes a round trip. The
// account and password edits were disabled for its duration, and the browser
// takes focus away from a disabled element for good: a click into the account
// box during the request left it unfocused and swallowed the typing. Hold the
// request open and type through it.
test( "the login edits keep focus and typing while the server list is in flight", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } );
	const control = ( id ) => page.locator( `[data-ui-id="${id}"]` );
	let releaseServers;
	const serversHeld = new Promise( ( resolve ) => releaseServers = resolve );
	let serverRequests = 0;
	try {
		await page.route( "**/title/servers", async ( route ) => {
			serverRequests++;
			await serversHeld;
			return route.continue();
		} );
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await control( "frontend:reveal" ).click( { timeout: 30000 } );
		await control( "account" ).waitFor();
		await page.waitForFunction( () =>
			document.querySelector( "output" )?.textContent.includes( "Frontend: login\n" )
		);
		assert.ok( serverRequests > 0, "the server list is requested on reaching the login window" );

		await control( "account" ).click();
		await page.keyboard.type( "typed" );
		// Let several frames re-project the still-pending title UI.
		await page.waitForTimeout( 300 );
		assert.equal( await page.evaluate( () => document.activeElement?.getAttribute( "data-ui-id" ) ), "account" );
		assert.equal( await control( "account" ).inputValue(), "typed" );
		assert.equal( await control( "account" ).isDisabled(), false );

		releaseServers();
		await page.waitForTimeout( 300 );
		assert.equal( await page.evaluate( () => document.activeElement?.getAttribute( "data-ui-id" ) ), "account" );
		await page.keyboard.type( "!" );
		assert.equal( await control( "account" ).inputValue(), "typed!" );
	} finally {
		releaseServers();
		await browser.close();
	}
} );
