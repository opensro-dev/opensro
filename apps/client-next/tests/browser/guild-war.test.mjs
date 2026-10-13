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
const { panelCloseId } = await import( "../../src/engine/foundation/ui/panels.ts" );
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
							members: [],
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
			await mkdir( "temp/artifacts/guild-war", { recursive: true } );
			await draw( "guild-tab:0" );
			await page.screenshot( { path: "temp/artifacts/guild-war/guild-info.png" } );
			await click( "guild-tab:1" );
			await draw( "union-sort:name" );
			await page.screenshot( { path: "temp/artifacts/guild-war/union-empty.png" } );
			await page.evaluate( () => {
				warFixture.state.gameplay.social.allianceMaster = 7;
				warFixture.state.gameplay.social.alliances = [
					{ id: 7, name: "Red", master: "Fixture", level: 5, model: 1907, flags: 12 },
					{ id: 8, name: "Blue", master: "Partner", level: 4, model: 14875, flags: 20 }
				];
				warFixture.draw();
			} );
			await click( "social-member:8" );
			await draw( "union-sort:name" );
			await page.screenshot( { path: "temp/artifacts/guild-war/union-selected.png" } );
			await click( "war-relation:1" );
			await page.evaluate( () => {
				warFixture.state.gameplay.social.wars = [];
				warFixture.draw();
			} );
			await draw( "war-declare" );
			await page.screenshot( { path: "temp/artifacts/guild-war/hostility-empty.png" } );
			await click( "guild-tab:2" );
			await draw( "war-sort" );
			await page.screenshot( { path: "temp/artifacts/guild-war/combat-empty.png" } );
			await page.evaluate( () => {
				warFixture.state.gameplay.social.wars = [ {
					id: 77,
					name: "Blue",
					enemyId: 8,
					type: 2,
					word38: 800,
					word3c: 900,
					localScore: 100,
					enemyScore: 25
				} ];
				warFixture.draw();
			} );

			await click( "guild-tab:1" );
			await click( "war-relation:1" );
			await draw( "war-select:77" );
			await click( "war-select:77" );
			await draw( "war-surrender" );
			await page.screenshot( { path: "temp/artifacts/guild-war/hostility-selected.png" } );
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
			await page.evaluate( () => {
				const guild = warFixture.state.gameplay.social.guild;
				guild.subject = "Guild notice";
				guild.contents = "Meet at the guild warehouse.\nBring your equipment.";
				guild.members[0].permissions = 31;
				guild.members.push( ...Array.from( { length: 10 }, ( _, i ) => ({
					...guild.members[0],
					id: 20 + i,
					name: "Member" + i,
					grade: 1,
					level: 40 + i,
					permissions: 31,
					model: 1907
				}) ) );
				warFixture.state.gameplay.progression.skillPoints = 1000;
				warFixture.ui.event( { kind: "activate", id: "open-window:Guild" } );
				warFixture.draw();
			} );
			await click( "guild-tab:0" );
			await page.evaluate( () => {
				warFixture.state.gameplay.social.guild.contents = Array.from(
					{ length: 14 },
					( _, i ) => "Notice line " + (i + 1)
				).join( "\n" );
				warFixture.draw();
			} );
			await click( "guild-notice-read" );
			await draw( "guild-notice-close" );
			assert.equal( await page.locator( '[data-ui-id^="social-member:"]' ).count(), 0 );
			assert.equal( await page.locator( '[data-ui-id="guild-dialog:authority"]' ).count(), 0 );
			await page.screenshot( { path: "temp/artifacts/guild-war/notice-reader.png" } );
			await page.locator( '[data-ui-id="guild-notice-body"]' ).hover();
			await page.mouse.wheel( 0, 100 );
			await page.evaluate( () => warFixture.draw() );
			const readerLabel = () => page.locator( '[data-ui-id="guild-notice-body"]' ).getAttribute( "aria-label" );
			assert.match( await readerLabel() ?? "", /^Notice line 2\n/ );
			for ( let i = 0; i < 20; i++ ) await click( "guild-notice-scroll-down" );
			assert.match( await readerLabel() ?? "", /Notice line 14$/ );
			await page.screenshot( { path: "temp/artifacts/guild-war/notice-reader-scrolled.png" } );
			await click( "guild-notice-close" );
			await draw( "social-member:20" );
			await click( "guild-notice-read" );
			await draw( "guild-notice-close" );
			assert.match( await readerLabel() ?? "", /^Notice line 1\n/ );
			await click( "guild-tab:1" );
			await click( "guild-tab:0" );
			await draw( "social-member:20" );
			assert.equal( await page.locator( '[data-ui-id="guild-notice-close"]' ).count(), 0 );
			await page.evaluate( () => {
				warFixture.state.gameplay.social.guild.contents = "Meet at the guild warehouse.\nBring your equipment.";
				warFixture.draw();
			} );
			await draw( "social-member:20" );
			await page.locator( '[data-ui-id="social-member:20"]' ).hover();
			await page.mouse.wheel( 0, 100 );
			await draw( "social-member:29" );
			await page.screenshot( { path: "temp/artifacts/guild-war/members-scrolled.png" } );
			await page.mouse.wheel( 0, -100 );
			await draw( "social-member:20" );
			await click( "social-member:20" );
			await draw( "guild-dialog:authority" );
			await page.screenshot( { path: "temp/artifacts/guild-war/member-selected.png" } );
			await click( "guild-dialog:authority" );
			await draw( "guild-grant:20:1" );
			await page.screenshot( { path: "temp/artifacts/guild-war/authority.png" } );
			await page.locator( '[data-ui-id="guild-grant:20:1"]' ).hover();
			await page.mouse.wheel( 0, 100 );
			await draw( "guild-grant:25:1" );
			await page.mouse.wheel( 0, -100 );
			await draw( "guild-grant:20:1" );
			await click( "guild-grant:20:1" );
			await click( "guild-grant-cancel" );
			await click( "guild-dialog:authority" );
			await draw( "guild-grant:20:1" );
			assert.equal(
				await page.locator( '[data-ui-id="guild-grant:20:1"]' ).getAttribute( "aria-pressed" ),
				"true"
			);
			await click( "guild-grant-cancel" );
			for (
				const [name, ready] of [ [ "notice", "social-subject" ], [ "donate", "social-amount" ], [
					"title",
					"social-name"
				], [ "role", "guild-role-choice:0" ] ]
			) {
				await click( "guild-dialog:" + name );
				await draw( ready );
				if ( name === "notice" ) {
					const edit = page.locator( 'textarea[data-ui-id="social-contents"]' );
					assert.equal( await edit.inputValue(), "Meet at the guild warehouse.\nBring your equipment." );
					await edit.fill( "First line" );
					const before = await page.evaluate( () => warFixture.commands.length );
					await edit.press( "End" );
					await edit.press( "Enter" );
					await edit.pressSequentially( "Second line" );
					await page.evaluate( () => warFixture.draw() );
					assert.equal( await edit.inputValue(), "First line\nSecond line" );
					assert.equal( await page.evaluate( () => warFixture.commands.length ), before );
					await click( "guild-notice" );
					assert.deepEqual( await page.evaluate( () => warFixture.commands.at( -1 ).command ), {
						kind: "guild-notice",
						subject: "Guild notice",
						contents: "First line\nSecond line"
					} );
				}
				await page.screenshot( { path: "temp/artifacts/guild-war/" + name + ".png" } );
				await click( "guild-dialog-close" );
			}
			await page.evaluate( () => {
				warFixture.state.gameplay.social.self = 20;
				warFixture.state.gameplay.social.localName = "Member0";
				warFixture.draw();
			} );
			await click( "guild-dialog:authority" );
			await draw( "guild-grant-ok" );
			assert.equal( await page.locator( '[data-ui-id="guild-grant-ok"]' ).isDisabled(), true );
			await page.screenshot( { path: "temp/artifacts/guild-war/authority-readonly.png" } );
			await click( "guild-grant-cancel" );
			await click( panelCloseId( "Guild" ) );
			await page.evaluate( () => {
				warFixture.ui.event( { kind: "activate", id: "open-window:Guild" } );
				warFixture.draw();
			} );
			await draw( "guild-tab:0" );
			await click( panelCloseId( "Guild" ) );
			await page.evaluate( () => {
				const game = warFixture.state.gameplay;
				game.social.self = 10;
				game.social.localName = "Fixture";
				game.social.guild.level = 2;
				game.target = 99;
				game.targetCapabilities = 0x4000;
				game.npcConversation = { phase: "menu", gid: 99, dialogueRevision: 1 };
				warFixture.draw();
			} );
			await draw( "npc-guild:level-up" );
			for ( const action of [ "level-up", "dissolve", "release", "master-leave" ] ) {
				await click( "npc-guild:" + action );
				await draw( "guild-manager-no" );
				const before = await page.evaluate( () => warFixture.commands.length );
				await page.screenshot( { path: "temp/artifacts/guild-war/manager-" + action + ".png" } );
				await click( "guild-manager-no" );
				assert.equal( await page.evaluate( () => warFixture.commands.length ), before );
			}
			await click( "npc-guild:level-up" );
			await draw( "guild-manager-yes" );
			await click( "guild-manager-yes" );
			assert.deepEqual( await page.evaluate( () => warFixture.commands.at( -1 ).command ), {
				kind: "guild-level-up",
				gid: 99
			} );
			await click( "npc-guild:master-leave" );
			await draw( "guild-transfer-member:20" );
			assert.equal( await page.locator( '[data-ui-id="guild-transfer-member:10"]' ).count(), 0 );
			assert.equal( await page.locator( '[data-ui-id="guild-manager-yes"]' ).isDisabled(), true );
			const transferBefore = await page.evaluate( () => warFixture.commands.length );
			await click( "guild-transfer-member:20" );
			await page.evaluate( () => {
				warFixture.state.gameplay.fortress = {
					worldId: 0,
					worlds: [],
					fortresses: [],
					registered: [],
					listId: 0,
					wars: [ { id: 1, name: "Red", flags: 1 } ]
				};
				warFixture.draw();
			} );
			await click( "guild-manager-yes" );
			await draw( "guild-transfer-member:20" );
			assert.equal( await page.evaluate( () => warFixture.commands.length ), transferBefore );
			await page.screenshot( { path: "temp/artifacts/guild-war/manager-transfer-fortress-refusal.png" } );
			await page.evaluate( () => {
				warFixture.state.gameplay.fortress.wars[0].flags = 0;
				warFixture.draw();
			} );
			await click( "guild-manager-yes" );
			await draw( "guild-manager-no" );
			assert.equal( await page.evaluate( () => warFixture.commands.length ), transferBefore );
			await page.screenshot( { path: "temp/artifacts/guild-war/manager-transfer-confirm.png" } );
			await click( "guild-manager-no" );
			await click( "npc-guild:master-leave" );
			await draw( "guild-transfer-member:20" );
			assert.equal( await page.locator( '[data-ui-id="guild-manager-yes"]' ).isDisabled(), true );
			await page.locator( '[data-ui-id="guild-transfer-member:20"]' ).hover();
			await page.mouse.wheel( 0, 100 );
			await draw( "guild-transfer-member:27" );
			await click( "guild-transfer-member:27" );
			await page.screenshot( { path: "temp/artifacts/guild-war/manager-transfer-selected.png" } );
			await click( "guild-manager-yes" );
			await click( "guild-manager-yes" );
			assert.deepEqual( await page.evaluate( () => warFixture.commands.at( -1 ).command ), {
				kind: "guild-master-leave",
				gid: 99,
				id: 27
			} );
			await page.evaluate( () => {
				warFixture.state.gameplay.social.guild.votes = [ { id: 88, kind: 0, remainingMs: 119999 } ];
				warFixture.draw();
			} );
			await click( "npc-guild:vote" );
			await draw( "guild-election-remaining" );
			assert.equal( await page.locator( '[data-ui-id="guild-manager-yes"]' ).isDisabled(), true );
			assert.match(
				await page.locator( '[data-ui-id="guild-election-remaining"]' ).getAttribute( "aria-label" ) ?? "",
				/^1\D/
			);
			const electionBefore = await page.evaluate( () => warFixture.commands.length );
			await page.keyboard.press( "Enter" );
			await draw( "guild-election-remaining" );
			assert.equal( await page.evaluate( () => warFixture.commands.length ), electionBefore );
			await page.screenshot( { path: "temp/artifacts/guild-war/manager-election.png" } );
			await page.evaluate( () => {
				warFixture.state.gameplay.social.guild.votes[0].remainingMs = 59999;
				warFixture.draw();
			} );
			assert.match(
				await page.locator( '[data-ui-id="guild-election-remaining"]' ).getAttribute( "aria-label" ) ?? "",
				/^0\D/
			);
			await click( "guild-manager-no" );
			await click( "npc-guild:vote" );
			await draw( "guild-election-remaining" );
			await page.evaluate( () => {
				warFixture.state.gameplay.social.guild.votes = [];
				warFixture.draw();
			} );
			assert.equal( await page.locator( '[data-ui-id="guild-election-remaining"]' ).count(), 0 );
			assert.equal( await page.evaluate( () => warFixture.commands.length ), electionBefore );
			await page.evaluate( () => {
				warFixture.state.gameplay.social.guild = null;
				warFixture.draw();
			} );
			await draw( "npc-guild:create" );
			await click( "npc-guild:create" );
			await draw( "guild-manager-text" );
			await page.locator( '[data-ui-id="guild-manager-text"]' ).fill( "GuildReview" );
			await page.evaluate( () => warFixture.draw() );
			await page.screenshot( { path: "temp/artifacts/guild-war/manager-create.png" } );
			await click( "guild-manager-no" );
			await click( "npc-guild:create" );
			await draw( "guild-manager-text" );
			assert.equal( await page.locator( '[data-ui-id="guild-manager-text"]' ).inputValue(), "" );
			await click( "guild-manager-no" );
			assert.deepEqual( await page.evaluate( () => warFixture.ui.stats().windowMissing ), [] );
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
