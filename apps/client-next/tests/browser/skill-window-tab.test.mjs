import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

/*
================
skill window reopens on the tab the player left

Forum thread 1563257126787485728: "when we open skill page we need to
make sure we lock previous skill was open". Native keeps the current
tab inside the skill window object itself: only a tab click ever
changes it, and the show path displays whatever tab the window already
holds, so closing and reopening the window keeps the player's tab. The
port keeps the same contract (the tab resets only at session
boundaries), and this probe pins it through the real UI module so the
behaviour cannot silently regress: open on the first tab, switch to
the second, close, reopen, and the second tab's masteries are still
the ones drawn, with the second tab button still selected.
================
*/
test(
	"the skill window reopens on the tab the player left",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		const errors = [];
		page.on( "pageerror", e => errors.push( e.message ) );
		try {
			await holdProbeRuntime( page );
			await page.setViewportSize( { width: 1600, height: 900 } );
			await page.goto( CLIENT_NEXT_BASE_URL );
			await page.waitForFunction( () =>
				document.querySelector( "output" )?.textContent?.includes( "runtime: running" )
			);
			await page.evaluate( async () => {
				const boot = Array.from( document.scripts ).find(
					s => s.src && new URL( s.src ).pathname === "/src/bootstrap.ts"
				);
				const { runtime } = await import( boot.src );
				runtime.dispose();
				const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" ),
					{ createAssets } = await import( "/src/engine/runtime/assets/assets.ts" ),
					{ createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" ),
					{ createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
				const canvas = document.querySelector( "canvas" ),
					renderer = createRenderer( canvas ),
					assets = createAssets(),
					commands = [];
				let scene = null, semantics = null;
				const skill = {
					id: 3,
					group: 174,
					level: 1,
					name: "SKILL_CH_SWORD_SMASH_A",
					nameSymbol: "SN_SKILL_CH_SWORD_SMASH_A",
					icon: "skill/china/sword_smash_a.ddj",
					spCost: 1,
					reqStr: 0,
					reqInt: 0,
					trainable: true,
					targetRequired: true,
					cooldownMs: 5000,
					cooldownGroup: 0,
					masteries: [ { ID: 257, Level: 1 }, { ID: 0, Level: 0 } ],
					prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
				};
				const state = {
					session: { phase: "world", revision: 1, character: "Fixture" },
					gameplay: {
						localGid: 1,
						skills: [ 3 ],
						skillCatalog: [ skill ],
						quickSlots: [],
						inventory: [],
						vitals: [],
						casts: [],
						target: 8,
						progression: {
							level: 10,
							skillPoints: 0,
							masteries: [ { id: 257, level: 1 }, { id: 273, level: 1 } ]
						}
					},
					entities: [
						{
							gid: 1,
							refObjId: 1907,
							name: "Fixture",
							kind: "player",
							regionId: 1,
							x: 0,
							y: 0,
							z: 0,
							heading: 0
						},
						{ gid: 8, name: "Target", kind: "monster", regionId: 1, x: 2, y: 0, z: 0, heading: 0 }
					],
					width: 1600,
					height: 900,
					worldReady: true,
					simulationTimeMs: 1000
				};
				const ui = createUi(
					assets,
					c => commands.push( c ),
					s => {
						scene = s;
						renderer.setUi( s );
					},
					renderer.setUiTexture,
					location.origin,
					"http://fixture.invalid",
					...Array( 10 ).fill( undefined ),
					() => {}
				);
				const platform = createPlatform(
					canvas,
					document.querySelector( "output" ),
					() => {},
					() => {},
					() => {},
					ui.event,
					ui.blocks
				);
				const draw = () => {
					const result = ui.step( { ...state }, performance.now() );
					if ( result ) {
						semantics = result;
						platform.presentUi( result );
					}
					renderer.frame( { width: 1600, height: 900 } );
				};
				const settle = async () => {
					const end = performance.now() + 20000;
					let stable = 0;
					while ( performance.now() < end ) {
						draw();
						if ( ui.stats().pending === 0 && !ui.stats().failed.length && ui.stats().windowReady ) {
							if ( ++stable >= 4 ) return;
						} else stable = 0;
						await new Promise( requestAnimationFrame );
					}
					throw Error( "skill window probe did not settle: " + JSON.stringify( ui.stats() ) );
				};
				window.skillTabFixture = {
					ui,
					platform,
					renderer,
					assets,
					state,
					draw,
					settle,
					semantics: () => semantics
				};
				await settle();
			} );

			const openAndRead = async () =>
				page.evaluate( async () => {
					const f = window.skillTabFixture;
					f.ui.event( { kind: "activate", id: "open-window:Skills" } );
					await f.settle();
					const controls = f.semantics().controls;
					return {
						tabs: controls.filter( c => c.id.startsWith( "skill-tab:" ) ).map( c => c.id ),
						selectedTab: controls.find( c => c.id.startsWith( "skill-tab:" ) && c.selected )?.id ?? null,
						masteries: controls.filter( c => c.id.startsWith( "skill-mastery:" ) ).map( c => c.id )
					};
				} );

			const first = await openAndRead();
			assert.deepEqual(
				first.tabs,
				[ "skill-tab:0", "skill-tab:1" ],
				"the fixture character must see both skill tabs: " + JSON.stringify( first )
			);
			assert.equal( first.selectedTab, "skill-tab:0", "the first open starts on the first tab" );
			assert.deepEqual( first.masteries, [ "skill-mastery:257" ], "tab 0 draws the weapon mastery" );

			await page.evaluate( async () => {
				const f = window.skillTabFixture;
				f.ui.event( { kind: "activate", id: "skill-tab:1" } );
				await f.settle();
			} );
			const switched = await page.evaluate( () => {
				const controls = window.skillTabFixture.semantics().controls;
				return {
					selectedTab: controls.find( c => c.id.startsWith( "skill-tab:" ) && c.selected )?.id ?? null,
					masteries: controls.filter( c => c.id.startsWith( "skill-mastery:" ) ).map( c => c.id )
				};
			} );
			assert.equal( switched.selectedTab, "skill-tab:1", "the click selects the second tab" );
			assert.deepEqual( switched.masteries, [ "skill-mastery:273" ], "tab 1 draws the force mastery" );

			await page.evaluate( async () => {
				const f = window.skillTabFixture;
				f.ui.event( { kind: "key", code: "Escape" } );
				f.draw();
				f.ui.event( { kind: "activate", id: "open-window:Character" } );
				await f.settle();
			} );
			const reopened = await openAndRead();
			assert.equal(
				reopened.selectedTab,
				"skill-tab:1",
				"reopening the skill window must keep the tab the player left"
			);
			assert.deepEqual(
				reopened.masteries,
				[ "skill-mastery:273" ],
				"the reopened window still draws the second tab's masteries"
			);
			assert.deepEqual( errors, [] );
		} finally {
			await page.evaluate( () => {
				const f = window.skillTabFixture;
				if ( f ) {
					f.ui.dispose();
					f.platform.dispose();
					f.assets.dispose();
					f.renderer.dispose();
				}
			} ).catch( () => {} );
			await browser.close();
		}
	}
);
