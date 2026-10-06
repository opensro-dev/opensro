/*
===========================================================================

ui-control-identities.test.mjs - BUG-064 through the real browser UI bridge

Controlled gameplay snapshots drive the production UI and asset worker.
Actual DOM clicks must dispatch each close/cancel action independently.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const ARTIFACTS = "temp/artifacts/ui-control-identities";

test( "service close and cancel controls survive real DOM publication and clicks", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1600, height: 900 } } );
	const errors = [];
	page.on( "pageerror", error => errors.push( String( error ) ) );
	await mkdir( ARTIFACTS, { recursive: true } );
	await page.context().tracing.start( { screenshots: true, snapshots: true } );
	try {
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const results = await page.evaluate( async () => {
			const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" );
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
			const { createUiBridge } = await import( "/src/engine/runtime/platform/ui/ui.ts" );
			const canvas = document.querySelector( "canvas" );
			if ( !canvas ) throw Error( "Missing fixture canvas" );
			canvas.width = innerWidth;
			canvas.height = innerHeight;
			const results = [];
			for ( const panel of [ "magic-option", "skin", "exchange" ] ) {
				for ( const action of [ "close", "cancel" ] ) {
					const assets = createAssets(), commands = [];
					const ui = createUi(
						assets,
						command => commands.push( command ),
						() => {},
						() => {},
						location.origin,
						"https://fixture.invalid"
					);
					const bridge = createUiBridge( canvas, event => ui.event( event ), () => {} );
					const entity = {
						gid: 1,
						regionId: 1,
						x: 0,
						y: 0,
						z: 0,
						heading: 0,
						kind: "player",
						name: "Fixture",
						mountedOn: 0,
						refObjId: 1907,
						bodyShape: 0x22
					};
					const state = {
						session: { phase: "world", revision: 1, character: "Fixture" },
						gameplay: {
							localGid: 1,
							pose: { ...entity, angle: 0 },
							vitals: [],
							target: 0,
							inventorySlotCount: 45,
							equipmentSlotCount: 13,
							playerModels: [ { refObjId: 1907, sex: 1 }, { refObjId: 1920, sex: 0 } ],
							inventory: [ {
								slot: 13,
								refObjId: 1,
								typeFlags: (3 << 2) | (3 << 5) | (13 << 7) | (9 << 11),
								quantity: 1,
								name: "Skin scroll",
								icon: "item/etc/hp_potion_01.ddj"
							} ]
						},
						entities: [ entity ],
						width: innerWidth,
						height: innerHeight,
						worldReady: true
					};
					try {
						if ( panel === "magic-option" ) {
							Object.assign( state.gameplay, {
								magicOption: {
									visible: true,
									phase: "idle",
									npc: 2,
									item: null,
									error: null,
									parts: []
								}
							} );
						}
						if ( panel === "exchange" ) {
							Object.assign( state.gameplay, {
								exchange: {
									open: true,
									partner: 2,
									own: [],
									theirs: [],
									ownGold: 0,
									theirGold: 0,
									ownLocked: false,
									theirLocked: false,
									approved: false,
									requesting: false
								}
							} );
						}
						let skinOpened = false, semantics;
						const deadline = performance.now() + 15000;
						while ( performance.now() < deadline ) {
							semantics = ui.step( state, performance.now() ) ?? semantics;
							if ( semantics ) bridge.present( semantics );
							if ( panel === "skin" && !skinOpened ) {
								ui.event( { kind: "key", code: "KeyI" } );
								ui.step( state, performance.now() );
								ui.event( { kind: "double-activate", id: "slot:13" } );
								skinOpened = true;
							}
							if ( semantics?.controls.some( control => control.id === `${panel}-cancel` ) ) break;
							await new Promise( resolve => setTimeout( resolve, 25 ) );
						}
						const ids = semantics?.controls.map( control => control.id ) ?? [];
						const control = document.querySelector( `[data-ui-id="${panel}-${action}"]` );
						if ( !(control instanceof HTMLButtonElement) ) {
							throw Error( `Control not admitted: ${panel}-${action}` );
						}
						commands.length = 0;
						control.click();
						results.push( { panel, action, ids, commands, error: null } );
					} catch ( error ) {
						results.push( { panel, action, ids: [], commands, error: String( error ) } );
					} finally {
						bridge.dispose();
						ui.dispose();
						assets.dispose();
					}
				}
			}
			return results;
		} );
		await writeFile( `${ARTIFACTS}/results.json`, JSON.stringify( { errors, results }, null, 2 ) + "\n" );
		assert.deepEqual( errors, [] );
		for ( const result of results ) {
			assert.equal( result.error, null, `${result.panel}-${result.action}` );
			assert.equal( new Set( result.ids ).size, result.ids.length );
			assert.ok( result.ids.includes( `${result.panel}-close` ) );
			assert.ok( result.ids.includes( `${result.panel}-cancel` ) );
			assert.deepEqual(
				result.commands,
				result.panel === "skin" ? [] : [ {
					kind: "gameplay",
					command: { kind: result.panel === "exchange" ? "exchange-cancel" : "magic-option-close" }
				} ]
			);
		}
	} finally {
		await page.context().tracing.stop( { path: `${ARTIFACTS}/trace.zip` } );
		await browser.close();
	}
} );
