/*
===========================================================================

guild-war.test.mjs - native declarations and surrender through production UI

Uses the repository browser harness and renderer, with a deterministic world
snapshot so native dialog controls and outgoing commands can be exercised.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test(
	"native guild war dialogs and hostile rows send through production UI owners",
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
				if ( !entry ) throw Error( "Missing runtime bootstrap" );
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
							else if ( result.kind === "image" ) result.image?.close();
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
						casts: [],
						progression: { gold: 1000 },
						social: {
							self: 10,
							localName: "Fixture",
							party: null,
							guild: {
								id: 7,
								name: "Red",
								level: 5,
								gp: 10000,
								subject: "",
								contents: "",
								members: [ {
									id: 10,
									name: "Fixture",
									grade: 0,
									level: 1,
									donated: 0,
									permissions: 0,
									grant: "",
									model: 0,
									role: 0,
									offline: 0
								} ]
							},
							wars: [ {
								id: 77,
								name: "Blue",
								enemyId: 8,
								type: 2,
								word38: 800,
								word3c: 900,
								localScore: 100,
								enemyScore: 25
							} ]
						}
					},
					entities: [],
					width: 1200,
					height: 900,
					worldReady: true
				};
				window.warFixture = {
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
				warFixture.draw();
				ui.event( { kind: "activate", id: "open-window:Guild" } );
			} );

			const draw = async id =>
				page.waitForFunction(
					id => {
						warFixture.draw();
						return warFixture.pending === 0 && document.querySelector( '[data-ui-id="' + id + '"]' );
					},
					id,
					{ timeout: 15000 }
				);
			const click = async id => {
				await draw( id );
				await page.locator( '[data-ui-id="' + id + '"]' ).click();
				await page.evaluate( () => warFixture.draw() );
			};

			await click( "guild-tab:1" );
			await click( "war-relation:1" );
			await draw( "war-select:77" );
			await click( "war-select:77" );
			await click( "war-declare" );
			await draw( "war-name" );
			await page.locator( '[data-ui-id="war-name"]' ).fill( "Blue" );
			await page.evaluate( () => warFixture.draw() );
			await click( "war-combo:23" );
			await click( "war-choice:7" );
			await click( "war-money-open" );
			await draw( "war-money" );
			await page.locator( '[data-ui-id="war-money"]' ).fill( "400" );
			await page.evaluate( () => warFixture.draw() );
			await click( "war-money-ok" );
			await mkdir( "temp/artifacts/guild-war", { recursive: true } );
			await draw( "war-confirm" );
			await page.screenshot( { path: "temp/artifacts/guild-war/declaration.png" } );
			await click( "war-confirm" );
			await draw( "war-confirm" );
			await page.screenshot( { path: "temp/artifacts/guild-war/confirmation.png" } );
			await click( "war-confirm" );
			const command = await page.evaluate( () => warFixture.commands.at( -1 ) );
			assert.equal( command.command.kind, "guild-war-declare" );
			assert.deepEqual( command.command.terms, {
				name: "Blue",
				mode: 0,
				period: 1024,
				scoreIndex: 7,
				stake: 400
			} );
			await click( "war-surrender" );
			await draw( "war-confirm" );
			await click( "war-confirm" );
			assert.deepEqual( await page.evaluate( () => warFixture.commands.at( -1 ).command ), {
				kind: "guild-war-surrender",
				id: 77
			} );
			await click( "guild-tab:2" );
			await draw( "war-contribution-sort:61" );
			await click( "war-select:77" );
			await page.screenshot( { path: "temp/artifacts/guild-war/score.png" } );

			await page.evaluate( () => {
				warFixture.state.gameplay.social.invitation = {
					type: 10,
					gid: 123,
					war: { name: "Blue", mode: 0, period: 1024, scoreIndex: 7, stake: 400 }
				};
				warFixture.draw();
			} );
			await draw( "invite-accept" );
			await page.screenshot( { path: "temp/artifacts/guild-war/agreement.png" } );
			await click( "invite-accept" );
			assert.deepEqual( await page.evaluate( () => warFixture.commands.at( -1 ).command ), {
				kind: "social-consent",
				accept: true
			} );
			await click( "invite-refuse" );
			assert.deepEqual( await page.evaluate( () => warFixture.commands.at( -1 ).command ), {
				kind: "social-consent",
				accept: false
			} );
			await page.evaluate( () => {
				warFixture.state.gameplay.social.invitation = null;
				warFixture.state.gameplay.social.warResult = {
					key: "UIIT_MSG_GUILDWAR_SUGGESTIONS_01",
					additionalKey: "UIIT_MSG_GUILDWAR_SUGGESTIONS_02",
					names: [],
					sequence: 1
				};
				warFixture.draw();
			} );
			await draw( "war-result-close" );
			await page.screenshot( { path: "temp/artifacts/guild-war/result-dialog.png" } );
			await click( "war-result-close" );
			await page.evaluate( () => {
				warFixture.ui.event( { kind: "activate", id: "open-window:Inventory" } );
				warFixture.draw();
			} );
			assert.equal( await page.locator( '[data-ui-id="war-result-close"]' ).count(), 0 );
			assert.deepEqual( await page.evaluate( () => warFixture.ui.stats().failed ), [] );
			await writeFile(
				"temp/artifacts/guild-war/result.json",
				JSON.stringify(
					{
						commands: await page.evaluate( () => warFixture.commands ),
						requests: await page.evaluate( () => warFixture.requests )
					},
					null,
					2
				)
			);
		} catch ( error ) {
			await mkdir( "temp/artifacts/guild-war", { recursive: true } );
			await page.screenshot( { path: "temp/artifacts/guild-war/failure.png" } );
			await writeFile(
				"temp/artifacts/guild-war/failure.json",
				JSON.stringify(
					await page.evaluate( () => ({
						stats: warFixture.ui.stats(),
						commands: warFixture.commands,
						controls: [ ...document.querySelectorAll( "[data-ui-id]" ) ].map( e => ({
							id: e.getAttribute( "data-ui-id" ),
							label: e.textContent
						}) ),
						pending: warFixture.pending
					}) ),
					null,
					2
				)
			);
			throw error;
		} finally {
			await browser.close();
		}
	}
);
