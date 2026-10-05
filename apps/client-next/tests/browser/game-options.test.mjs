/*
===========================================================================

game-options.test.mjs - native options and auto-potion through production UI

Uses the repository browser harness and renderer, with a deterministic world
snapshot so controls, cancellation and saved settings can be exercised.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test(
	"native options and potion controls publish, persist and cancel through production owners",
	{ timeout: 90000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await page.setViewportSize( { width: 1200, height: 900 } );
			await page.goto( CLIENT_NEXT_BASE_URL );
			await page.waitForFunction( () =>
				document.querySelector( "output" )?.textContent?.includes( "runtime: running" )
			);
			await page.evaluate( async () => {
				localStorage.removeItem( "sro:v1150:game-options:1" );
				const entry = Array.from( document.scripts ).find( s =>
					s.src && new URL( s.src ).pathname === "/src/bootstrap.ts"
				);
				const { runtime } = await import( entry.src );
				runtime.dispose();
				const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" ),
					{ createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" ),
					{ createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
				const jobs = new Map(),
					requests = [],
					commands = [],
					renderer = createRenderer( document.querySelector( "canvas" ) );
				let serial = 0, scene = null;
				const assets = {
					available: () => Math.max( 0, 32 - jobs.size ),
					request( url, limit, kind ) {
						const id = ++serial;
						jobs.set( id, null );
						requests.push( url );
						fetch( url ).then( async r => {
							if ( !r.ok ) throw Error( r.status + " " + url );
							const result = kind === "png" ?
								{ kind: "image", image: await createImageBitmap( await r.blob() ) } :
								{ kind: "bytes", buffer: await r.arrayBuffer() };
							if ( jobs.has( id ) ) jobs.set( id, result );
							else if ( result.kind === "image" ) result.image.close();
						} ).catch( e => {
							if ( jobs.has( id ) ) jobs.set( id, { kind: "error", error: String( e ) } );
						} );
						return id;
					},
					take( id ) {
						const result = jobs.get( id );
						if ( result ) jobs.delete( id );
						return result;
					},
					cancel( id ) {
						const result = jobs.get( id );
						if ( result?.kind === "image" ) result.image.close();
						jobs.delete( id );
					}
				};
				let platform;
				const ui = createUi(
					assets,
					c => commands.push( c ),
					s => {
						scene = s;
						renderer.setUi( s );
					},
					( id, image ) => renderer.setUiTexture( id, image ),
					location.origin,
					"http://fixture.invalid",
					() => {},
					() => {},
					() => 1,
					() => 0,
					value => platform.saveGameOptions( value )
				);
				platform = createPlatform(
					document.querySelector( "canvas" ),
					document.querySelector( "output" ),
					() => {},
					() => {},
					() => {},
					ui.event,
					ui.blocks
				);
				const state = {
					session: {
						phase: "world",
						revision: 1,
						character: "Fixture",
						characters: [ { id: 1, name: "Fixture", level: 1, maxHp: 200, maxMp: 200 } ]
					},
					gameplay: {
						localGid: 1,
						inventory: [],
						inventorySlotCount: 45,
						equipmentSlotCount: 13,
						vitals: [ { gid: 1, hp: 200, mp: 200 } ],
						casts: []
					},
					entities: [],
					width: 1200,
					height: 900,
					worldReady: true
				};
				window.flagFixture = {
					ui,
					renderer,
					platform,
					state,
					requests,
					commands,
					get scene() {
						return scene;
					},
					get pending() {
						return jobs.size;
					},
					draw() {
						const semantic = ui.step( { ...state }, performance.now() );
						if ( semantic ) platform.presentUi( semantic );
						renderer.frame( { width: state.width, height: state.height } );
					}
				};
				flagFixture.draw();
				ui.event( { kind: "activate", id: "open-window:Option" } );
			} );

			const draw = async id =>
				page.waitForFunction(
					id => {
						flagFixture.draw();
						return flagFixture.pending === 0 && document.querySelector( '[data-ui-id="' + id + '"]' );
					},
					id,
					{ timeout: 15000 }
				);
			const click = async id => {
				await draw( id );
				await page.locator( '[data-ui-id="' + id + '"]' ).click();
				await page.evaluate( () => flagFixture.draw() );
			};
			await click( "option-tab:4" );
			await draw( "option-toggle:ownName" );
			await mkdir( "temp/artifacts/login-flags", { recursive: true } );
			await page.screenshot( { path: "temp/artifacts/login-flags/options.png" } );
			assert.deepEqual( await page.evaluate( () => flagFixture.ui.stats().failed ), [] );
			await click( "option-toggle:ownName" );
			await click( "option-ok" );
			assert.equal(
				await page.evaluate( () => JSON.parse( localStorage.getItem( "sro:v1150:game-options:1" ) ).ownName ),
				false
			);
			assert.deepEqual(
				await page.evaluate( () => {
					const p = JSON.parse( localStorage.getItem( "sro:v1150:game-options:1" ) );
					return [
						"ownStatus",
						"cosStatus",
						"partyStatus",
						"monsterStatus",
						"hpWarning",
						"mpWarning",
						"warningSound"
					].map( k => p[k] );
				} ),
				Array( 7 ).fill( false ),
				"opening and applying unrelated options preserves native startup-disabled warnings and bars"
			);
			await page.evaluate( () => {
				flagFixture.ui.event( { kind: "activate", id: "open-window:Option" } );
				flagFixture.draw();
			} );
			await click( "option-tab:4" );
			await click( "option-toggle:ownName" );
			await click( "option-cancel" );
			await page.evaluate( () => {
				flagFixture.ui.event( { kind: "activate", id: "open-window:Option" } );
				flagFixture.draw();
			} );
			await click( "option-tab:4" );
			await draw( "option-toggle:ownName" );
			assert.equal(
				await page.locator( '[data-ui-id="option-toggle:ownName"]' ).getAttribute( "aria-pressed" ),
				"false"
			);
			const visited = new Set();
			for ( const group of [ 0, 1 ] ) {
				for ( let i = 0; i < 5; i++ ) {
					await draw( "option-scroll:" + group + ":1" );
					for (
						const id of await page.locator( '[data-ui-id^="option-toggle:"]' ).evaluateAll( es =>
							es.map( e => e.dataset.uiId )
						)
					) visited.add( id );
					const button = page.locator( '[data-ui-id="option-scroll:' + group + ':1"]' );
					if ( await button.isDisabled() ) break;
					await button.click();
					await page.evaluate( () => flagFixture.draw() );
				}
			}
			assert.equal( visited.size, 23, "every local checkbox must be reachable: " + [ ...visited ].join( "," ) );
			await click( "option-cancel" );
			await page.evaluate( () => {
				flagFixture.ui.event( { kind: "activate", id: "open-window:Auto Potion" } );
				flagFixture.draw();
			} );
			await draw( "potion-enable:hp" );
			await click( "potion-time-up" );
			await click( "potion-time-down" );
			const spin = await page.evaluate( () => {
				const button = document.querySelector( '[data-ui-id="potion-time-up"]' ).getBoundingClientRect(),
					quads = flagFixture.scene.quads;
				return {
					fill: quads.some( q =>
						q.texture === "" && q.rect[0] === button.x - 38 && q.rect[1] === button.y - 1 &&
						q.rect[2] === 38 && q.rect[3] === 25 && q.color.join() === "0,0,0,1"
					),
					arrows: quads.filter( q =>
						/ub_(up|down)_arrow/.test( q.texture ) && q.rect[0] >= button.x && q.rect[0] < button.right &&
						q.rect[1] >= button.y && q.rect[1] < button.y + 25
					).map( q => q.texture )
				};
			} );
			assert.equal( spin.fill, true, "generated native spin child has an opaque field" );
			assert.equal( spin.arrows.length, 2 );
			// Exercise the bounded native stepping through production input routing.
			await page.evaluate( () => {
				for ( let i = 0; i < 30; i++ ) flagFixture.ui.event( { kind: "activate", id: "potion-time-down" } );
				flagFixture.ui.event( { kind: "activate", id: "potion-save" } );
				flagFixture.draw();
			} );
			assert.equal(
				await page.evaluate( () => flagFixture.commands.at( -1 ).command.settings.timing ),
				0x85,
				"lower limit preserves enable bit"
			);
			await page.evaluate( () => {
				flagFixture.ui.event( { kind: "activate", id: "open-window:Auto Potion" } );
				flagFixture.draw();
			} );
			await draw( "potion-time-up" );
			await page.evaluate( () => {
				for ( let i = 0; i < 30; i++ ) flagFixture.ui.event( { kind: "activate", id: "potion-time-up" } );
				flagFixture.draw();
			} );
			await draw( "potion-percent:hp" );
			assert.equal( await page.locator( '[data-ui-id="potion-percent:hp"]' ).isDisabled(), true );
			await click( "potion-enable:hp" );
			await page.locator( '[data-ui-id="potion-percent:hp"]' ).fill( "73" );
			await page.evaluate( () => flagFixture.draw() );
			await click( "potion-enable:hp" );
			assert.equal( await page.locator( '[data-ui-id="potion-percent:hp"]' ).isDisabled(), true );
			await click( "potion-enable:hp" );
			assert.equal(
				await page.locator( '[data-ui-id="potion-percent:hp"]' ).inputValue(),
				"50",
				"re-enable restores saved percentage"
			);
			await click( "potion-combo:hp:page" );
			await click( "potion-choice:hp:page:3" );
			await click( "potion-combo:hp:key" );
			await draw( "potion-choice:hp:key:1" );
			assert.equal( await page.locator( '[data-ui-id^="potion-choice:hp:key:"]' ).count(), 4 );
			for ( let i = 0; i < 6; i++ ) await click( "potion-combo-down" );
			await click( "potion-choice:hp:key:10" );
			await page.locator( '[data-ui-id="potion-percent:hp"]' ).fill( "75" );
			await page.evaluate( () => flagFixture.draw() );
			await draw( "potion-save" );
			await page.screenshot( { path: "temp/artifacts/login-flags/auto-potion.png" } );
			assert.deepEqual( await page.evaluate( () => flagFixture.ui.stats().failed ), [] );
			await click( "potion-save" );
			const command = await page.evaluate( () => flagFixture.commands.at( -1 ) );
			assert.equal( command.command.kind, "auto-potion-save" );
			assert.equal( command.command.settings.hp, 0xcb4a );
			assert.equal(
				command.command.settings.timing,
				0xdf,
				"upper limit is 9.5 seconds and preserves enable bit"
			);
			const display = await page.evaluate( async () => {
				const calls = [];
				let fullscreen = null, reject = false;
				Object.defineProperty( document, "fullscreenElement", { configurable: true, get: () => fullscreen } );
				document.documentElement.requestFullscreen = () => {
					calls.push( "enter" );
					if ( reject ) return Promise.reject( Error( "denied" ) );
					fullscreen = document.documentElement;
					document.dispatchEvent( new Event( "fullscreenchange" ) );
					return Promise.resolve();
				};
				document.exitFullscreen = () => {
					calls.push( "exit" );
					fullscreen = null;
					document.dispatchEvent( new Event( "fullscreenchange" ) );
					return Promise.resolve();
				};
				const save = value =>
					flagFixture.platform.saveGameOptions( {
						...JSON.parse( localStorage.getItem( "sro:v1150:game-options:1" ) ),
						windowMode: value
					} );
				save( true );
				save( false );
				await new Promise( resolve => queueMicrotask( resolve ) );
				await new Promise( resolve => queueMicrotask( resolve ) );
				save( true );
				await new Promise( resolve => queueMicrotask( resolve ) );
				await new Promise( resolve => queueMicrotask( resolve ) );
				reject = true;
				save( false );
				await new Promise( resolve => queueMicrotask( resolve ) );
				await new Promise( resolve => queueMicrotask( resolve ) );
				return {
					calls,
					windowMode: JSON.parse( localStorage.getItem( "sro:v1150:game-options:1" ) ).windowMode
				};
			} );
			assert.deepEqual( display, { calls: [ "enter", "exit", "enter" ], windowMode: true } );
			await writeFile(
				"temp/artifacts/login-flags/browser.json",
				JSON.stringify( { checkboxes: [ ...visited ], command }, null, 2 )
			);
			await page.evaluate( () => {
				flagFixture.ui.dispose();
				flagFixture.platform.dispose();
				flagFixture.renderer.dispose();
			} );
		} finally {
			await browser.close();
		}
	}
);
