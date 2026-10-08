/*
===========================================================================

quest-details.test.mjs - independent quest details beside native popup pages

69C730/69F310 select mutually exclusive Party/Quest pages; 69CDF0 owns
QuestInfo separately, and 5C0100 does not close it when hiding the Quest page.
Use real assets, rendering, DOM controls and default keyboard bindings.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const ADMISSION_TIMEOUT_MS = 30000;

test( "quest details survive popup changes and own their close and abandon actions", { timeout: 90000 }, async () => {
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
			const skillData = await (await fetch( "/assets/data/skillUi.json" )).json();
			// Match the shipped Smashing Series setup in ui.test.mjs: rank one
			// learned, rank two (291) available for the real practice dialog.
			const skillCatalog = skillData.skills.filter( row => row.group === 174 && row.level <= 2 ).map( row => ({
				...row,
				name: "Smashing Series",
				nameSymbol: row.name,
				icon: row.icon.replace( /^icon[\\/]/, "" ),
				spCost: 1,
				trainable: true,
				targetRequired: true,
				cooldownMs: 0,
				masteries: [],
				prerequisites: []
			}) );
			const canvas = document.querySelector( "canvas" );
			if ( !canvas ) throw Error( "Missing fixture canvas" );
			const style = document.createElement( "style" );
			style.textContent = "body{overflow:hidden}canvas{display:block;width:100vw;height:100vh}";
			document.head.append( style );
			const renderer = createRenderer( canvas ), assets = createAssets();
			const commands = [], worldClicks = [];
			let semantics;
			const ui = createUi(
				assets,
				command => commands.push( command ),
				scene => renderer.setUi( scene ),
				( id, image ) => renderer.setUiTexture( id, image ),
				location.origin,
				"https://fixture.invalid"
			);
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
				session: { phase: "world", revision: 1, character: "Quest fixture" },
				gameplay: {
					localGid: 7,
					inventory: [],
					inventorySlotCount: 45,
					equipmentSlotCount: 13,
					vitals: [],
					casts: [],
					skillCatalog,
					skills: skillData.skills.filter( row => row.mastery === 257 && row.level === 1 ).map( row =>
						row.id
					),
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
						refId: 3,
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
				/*
				================
				draw
				================
				*/
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
						worldClicks: worldClicks.length,
						abandon: commands.filter( value =>
							value.kind === "gameplay" && value.command.kind === "quest-abandon"
						),
						training: commands.filter( value =>
							value.kind === "gameplay" && value.command.kind === "skill-train"
						),
						questIds: state.gameplay.quests.map( quest => quest.refId )
					};
				},
				/*
				================
				blocked
				================
				*/
				blocked( point ) {
					return ui.blocks( point[0], point[1] );
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

		Advance real publications while assets arrive; no synthetic controls or
		fixed sleeps stand in for the window's admission.
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

		Use browser keyboard events through the platform/bridge and defaults:
		Q is Quests, P is Party, E is Party Matching. Never remap E to Party.
		================
		*/
		async function key( code ) {
			await page.locator( "canvas" ).focus();
			await page.keyboard.press( code );
		}
		try {
			await settle( [ "hotbar:1" ] );
			await key( "KeyQ" );
			await settle( [ "quest:3" ], [ "party-invite" ] );
			await key( "KeyP" );
			await settle( [ "party-invite" ], [ "quest:3", "quest-details-close" ] );

			await key( "KeyQ" );
			await settle( [ "quest:3" ] );
			await page.locator( '[data-ui-id="quest:3"]' ).click();
			await settle( [ "quest-details-close" ] );
			// Place details beneath the centered practice dialog with actual drag
			// events; its controls must not survive on top of modal controls.
			const initialDetails = await page.locator( '[data-ui-id="quest-detail-drag"]' ).boundingBox();
			assert.ok( initialDetails );
			await page.mouse.move( initialDetails.x + 40, initialDetails.y + 10 );
			await page.mouse.down();
			await page.mouse.move( 580, 330, { steps: 4 } );
			await page.mouse.up();
			await settle( [ "quest-details-close" ] );
			await key( "KeyS" );
			await settle( [ "skill-learn:291", "quest-details-close" ] );
			const underPractice = await page.locator( '[data-ui-id="quest-detail-drag"]' ).boundingBox();
			assert.ok( underPractice );
			await page.locator( '[data-ui-id="skill-learn:291"]' ).click();
			await settle( [ "skill-confirm-ok", "skill-confirm-cancel" ], [
				"quest-details-close",
				"quest-detail-drag",
				"skill-learn:291"
			] );
			assert.equal( await page.locator( '[data-ui-id="quest-details-close"]' ).count(), 0 );
			const cancel = await page.locator( '[data-ui-id="skill-confirm-cancel"]' ).boundingBox();
			assert.ok( cancel );
			const cancelPoint = [ cancel.x + cancel.width / 2, cancel.y + cancel.height / 2 ];
			assert.ok( cancelPoint[0] > underPractice.x && cancelPoint[0] < underPractice.x + underPractice.width );
			assert.ok( cancelPoint[1] > underPractice.y && cancelPoint[1] < underPractice.y + underPractice.height );
			const practice = await fixture.evaluate( owner => owner.draw() );
			await page.mouse.click( cancelPoint[0], cancelPoint[1] );
			await settle( [ "skill-learn:291", "quest-details-close" ], [
				"skill-confirm-ok",
				"skill-confirm-cancel"
			] );
			const cancelledPractice = await fixture.evaluate( owner => owner.draw() );
			assert.deepEqual( cancelledPractice.training, [] );
			assert.equal( cancelledPractice.worldClicks, practice.worldClicks );

			await key( "KeyE" );
			await settle( [ "party-match-next", "quest-details-close" ], [ "quest:3" ] );
			await key( "KeyE" );
			await settle( [ "quest-details-close" ], [ "party-match-next" ] );
			await key( "KeyE" );
			await settle( [ "party-match-next", "quest-details-close" ] );
			await key( "KeyP" );
			await settle( [ "party-invite", "quest-details-close" ], [ "party-match-next" ] );

			// Place details over the popup with real pointer capture and drag events.
			const details = await page.locator( '[data-ui-id="quest-detail-drag"]' ).boundingBox();
			const popup = await page.locator( '[data-ui-id="main-popup-drag"]' ).boundingBox();
			assert.ok( details && popup );
			await page.mouse.move( details.x + 40, details.y + 10 );
			await page.mouse.down();
			await page.mouse.move( popup.x + 30, popup.y + 10, { steps: 4 } );
			await page.mouse.up();
			await settle( [ "party-invite", "quest-details-close" ] );
			const moved = await page.locator( '[data-ui-id="quest-detail-drag"]' ).boundingBox();
			assert.ok( moved );
			assert.ok( Math.abs( moved.x - (popup.x - 10) ) <= 1 && Math.abs( moved.y - popup.y ) <= 1 );
			const overlap = [ popup.x + 60, popup.y + 60 ];
			assert.ok( overlap[0] > moved.x && overlap[0] < moved.x + moved.width );
			assert.ok( overlap[1] > moved.y && overlap[1] < moved.y + moved.height );
			assert.equal( await fixture.evaluate( ( owner, point ) => owner.blocked( point ), overlap ), true );
			const before = await fixture.evaluate( owner => owner.draw() );
			await page.mouse.click( overlap[0], overlap[1] );
			await settle( [ "party-invite", "quest-details-close" ] );
			assert.equal( (await fixture.evaluate( owner => owner.draw() )).worldClicks, before.worldClicks );

			await page.locator( '[data-ui-id="quest-details-close"]' ).click();
			await settle( [ "party-invite" ], [ "quest-details-close" ] );
			await key( "KeyQ" );
			await settle( [ "quest:3" ] );
			await page.locator( '[data-ui-id="quest:3"]' ).click();
			await settle( [ "quest-details-close" ] );
			await key( "KeyP" );
			await settle( [ "party-invite", "quest-details-close" ] );
			await key( "Escape" );
			await settle( [ "party-invite" ], [ "quest-details-close" ] );

			await key( "KeyQ" );
			await settle( [ "quest:3" ] );
			await page.locator( '[data-ui-id="quest:3"]' ).click();
			await settle( [ "quest-details-close" ] );
			await key( "KeyP" );
			await settle( [ "party-invite", "quest-details-close" ] );
			await key( "KeyP" );
			await settle( [ "quest-details-close" ], [ "party-invite", "main-popup-drag" ] );
			await key( "KeyP" );
			await settle( [ "party-invite", "quest-details-close" ] );

			await page.locator( '[data-ui-id="quest-abandon"]' ).click();
			await settle( [ "quest-abandon-no", "quest-abandon-yes" ] );
			const modal = await fixture.evaluate( owner => owner.draw() );
			assert.deepEqual( modal.abandon, [] );
			// Even an otherwise uncovered world point is blocked by confirmation.
			const outside = [ 800, 180 ];
			assert.equal( await fixture.evaluate( ( owner, point ) => owner.blocked( point ), outside ), true );
			await page.mouse.click( outside[0], outside[1] );
			assert.equal( (await fixture.evaluate( owner => owner.draw() )).worldClicks, modal.worldClicks );
			await key( "Escape" );
			await settle( [ "party-invite", "quest-details-close" ], [ "quest-abandon-yes" ] );
			assert.deepEqual( (await fixture.evaluate( owner => owner.draw() )).abandon, [] );
			await key( "Escape" );
			await settle( [ "party-invite" ], [ "quest-details-close" ] );
			await key( "Escape" );
			await settle( [], [ "party-invite", "main-popup-drag", "quest-details-close" ] );

			await key( "KeyQ" );
			await settle( [ "quest:3" ] );
			await page.locator( '[data-ui-id="quest:3"]' ).click();
			await settle( [ "quest-details-close" ] );
			await key( "KeyP" );
			await settle( [ "party-invite", "quest-details-close" ] );
			await page.locator( '[data-ui-id="quest-abandon"]' ).click();
			await settle( [ "quest-abandon-no", "quest-abandon-yes" ] );
			await page.locator( '[data-ui-id="quest-abandon-no"]' ).click();
			await settle( [ "party-invite", "quest-details-close" ], [ "quest-abandon-yes" ] );
			assert.deepEqual( (await fixture.evaluate( owner => owner.draw() )).abandon, [] );
			await page.locator( '[data-ui-id="quest-abandon"]' ).click();
			await settle( [ "quest-abandon-yes" ] );
			await page.locator( '[data-ui-id="quest-abandon-yes"]' ).click();
			await settle( [ "party-invite" ], [ "quest-details-close", "quest-abandon-yes" ] );
			const abandoned = await fixture.evaluate( owner => owner.draw() );
			assert.deepEqual( abandoned.abandon, [ {
				kind: "gameplay",
				command: { kind: "quest-abandon", refId: 3 }
			} ] );
			assert.deepEqual(
				abandoned.questIds,
				[ 3 ],
				"The command must not optimistically remove the authoritative quest"
			);
			assert.equal( await fixture.evaluate( ( owner, point ) => owner.blocked( point ), outside ), false );
			await page.mouse.click( outside[0], outside[1] );
			assert.equal( (await fixture.evaluate( owner => owner.draw() )).worldClicks, abandoned.worldClicks + 1 );
		} finally {
			await fixture.evaluate( owner => owner.dispose() );
			await fixture.dispose();
		}
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
