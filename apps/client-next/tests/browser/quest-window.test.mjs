/*
===========================================================================

quest-window.test.mjs - the native quest list window (issue #4245)

The later client's CNIFQuest layout rendered by the res-layout runtime
(its rows are CNIFQuestSlot rows, the tracker CNIFMiniQuestSlot rows):
the list window opens on Q, one quest row plus its objectives, and the
tracker owns its fold; the reward (content) window is the follow-up.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const ADMISSION_TIMEOUT_MS = 30000;

test( "the native quest list window renders its layout and owns its tracker", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser( {
		viewport: { width: 1512, height: 982 },
		deviceScaleFactor: 2
	} );
	const errors = [];
	page.on( "pageerror", error => errors.push( String( error ) ) );
	try {
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const fixture = await page.evaluateHandle( async () => {
			const uiPath = "/src/engine/runtime/ui/ui.ts";
			const assetsPath = "/src/engine/runtime/assets/assets.ts";
			const rendererPath = "/src/engine/runtime/renderer/renderer.ts";
			const platformPath = "/src/engine/runtime/platform/platform.ts";
			const { createUi } = await import( uiPath );
			const { createAssets } = await import( assetsPath );
			const { createRenderer } = await import( rendererPath );
			const { createPlatform } = await import( platformPath );
			const canvas = document.querySelector( "canvas" );
			if ( !canvas ) throw Error( "Missing fixture canvas" );
			const style = document.createElement( "style" );
			style.textContent = "body{overflow:hidden}canvas{display:block;width:100vw;height:100vh}";
			document.head.append( style );
			const renderer = createRenderer( canvas ), assets = createAssets();
			const commands = [], worldClicks = [];
			let semantics;
			const ui = createUi( {
				assets,
				commands: command => commands.push( command ),
				publish: scene => renderer.setUi( scene ),
				texture: ( id, image ) => renderer.setUiTexture( id, image ),
				base: location.origin,
				apiBase: "https://fixture.invalid"
			} );
			const platform = createPlatform(
				canvas,
				document.createElement( "output" ),
				() => {},
				() => {},
				() => {},
				ui.event,
				ui.blocks,
				( x, y ) => worldClicks.push( [ x, y ] )
			);
			const state = {
				frontend: { phase: "world" },
				session: { phase: "world", revision: 1, character: "Quest list fixture" },
				gameplay: {
					localGid: 9,
					inventory: [],
					inventorySlotCount: 45,
					equipmentSlotCount: 13,
					vitals: [],
					casts: [],
					skillCatalog: [],
					skills: [],
					progression: {
						level: 20,
						skillPoints: 100,
						masteries: [ 257, 258, 259, 273, 274, 275, 276 ].map( id => ({ id, level: 10 }) )
					},
					partyMatching: {
						auto: [],
						page: 1,
						pages: 1,
						rows: [],
						own: null,
						request: null,
						pending: null,
						result: null
					},
					quests: [ {
						refId: 27,
						u08: 0x11,
						u09: 0,
						u10: 1,
						flags: 0,
						contents: [ {
							tag: 1,
							kind: 1,
							description: "SN_QNO_CH_SMITH_1",
							objectiveSentinel: false,
							objectiveValues: []
						} ],
						targetIds: []
					} ]
				},
				entities: [],
				worldReady: true
			};
			return {
				draw() {
					semantics = ui.step( { ...state, ...platform.readUiViewport() }, performance.now() ) ?? semantics;
					if ( semantics ) platform.presentUi( semantics );
					renderer.frame( platform.readViewport() );
					return {
						pending: ui.stats().pending,
						windowReady: ui.stats().windowReady,
						error: renderer.error(),
						phase: renderer.phase(),
						ids: semantics?.controls.map( control => control.id ) ?? [],
						worldClicks: worldClicks.length
					};
				},
				/*
	================
				dispose
	================
				*/
				dispose() {
					platform.dispose();
					ui.dispose();
					assets.dispose();
					renderer.dispose();
				}
			};
		} );
		/*
	================
		settle
	================
		*/
		async function settle( present = [], absent = [] ) {
			const ready = await page.waitForFunction(
				( { owner, present, absent } ) => {
					const report = owner.draw();
					if ( report.error ) throw Error( report.error );
					return report.pending === 0 && report.windowReady && report.phase === "running" &&
						present.every( id => report.ids.includes( id ) ) && absent.every( id =>
							!report.ids.includes( id )
						);
				},
				{ owner: fixture, present, absent },
				{ timeout: ADMISSION_TIMEOUT_MS }
			);
			await ready.dispose();
		}
		/*
	================
		key
	================
		*/
		async function key( code ) {
			await page.locator( "canvas" ).focus();
			await page.keyboard.press( code );
		}
		try {
			await settle( [ "hotbar:1" ] );
			await key( "KeyQ" );
			await settle( [ "quest-list-drag" ], [ "party-invite", "quest:27" ] );
			await settle(
				[ "quest-list-track:27", "quest-list-content:27", "quest-list-locate:27" ],
				[ "party-invite" ]
			);
			// Open it and capture the window for the screenshot pair.
			const box = await page.locator( '[data-ui-id="quest-list-drag"]' ).boundingBox();
			assert.ok( box );
			await page.screenshot( { path: "temp/quest-window.png" } );
		} finally {
			await fixture.evaluate( owner => owner.dispose() );
			await browser.close();
		}
		assert.deepEqual( errors, [] );
	} catch ( error ) {
		throw error;
	}
} );
