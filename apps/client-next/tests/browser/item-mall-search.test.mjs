/*
===========================================================================

item-mall-search.test.mjs - the mall search field's editing keys

Drives the later-category Item Mall through the browser platform: opens the
mall, picks a category, types in the real DOM search field and presses
Backspace and Delete. #4153 reports Backspace never deletes there while
Delete does; the focused field must keep its native editing keys.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "Item Mall search field deletes with Backspace and Delete", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", e => errors.push( String( e ) ) );
	try {
		await page.setViewportSize( { width: 1200, height: 900 } );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await page.waitForFunction( () =>
			document.querySelector( "output" )?.textContent?.includes( "runtime: running" )
		);
		await page.evaluate( async () => {
			localStorage.setItem(
				"sro:v1150:experimental-options:3",
				JSON.stringify( { mallCategories: true } )
			);
			const { runtime } = await import( "/src/bootstrap.ts" );
			runtime.dispose();
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" ),
				{ createUi } = await import( "/src/engine/runtime/ui/ui.ts" ),
				{ createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" ),
				{ createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
			const assets = createAssets(),
				renderer = createRenderer( document.querySelector( "canvas" ) ),
				commands = [];
			let semantics, scene;
			const ui = createUi( {
				assets,
				commands: c => commands.push( c ),
				publish: s => {
					scene = s;
					renderer.setUi( s );
				},
				texture: ( id, image ) => renderer.setUiTexture( id, image ),
				base: location.origin,
				apiBase: "http://fixture.invalid"
			} );
			const platform = createPlatform(
				document.querySelector( "canvas" ),
				document.querySelector( "output" ),
				() => {},
				() => {},
				() => {},
				ui.event,
				ui.blocks
			);
			const entity = {
				gid: 1,
				regionId: 1,
				x: 0,
				y: 0,
				z: 0,
				heading: 0,
				kind: "player",
				name: "Player",
				mountedOn: 0
			};
			const state = {
				session: { phase: "world", revision: 1, character: "Player" },
				gameplay: {
					localGid: 1,
					pose: { ...entity, angle: 0 },
					vitals: [],
					inventory: [],
					inventorySlotCount: 45,
					equipmentSlotCount: 13,
					target: 0,
					inventoryPending: false,
					itemMall: {
						silk: 100,
						giftSilk: 0,
						points: 10,
						pending: false,
						revision: 1,
						tabs: [ {
							shop: 2,
							tab: 0,
							category: "MALL_CONSUME",
							label: "UIIT_STT_SILKMALL_SPECIAL"
						} ],
						offers: [ {
							group: 852,
							shop: 2,
							tab: 0,
							slot: 0,
							packageId: 1,
							name: "SN_ITEM_ETC_HP_POTION_01",
							description: "",
							icon: "item/etc/hp_potion_01.ddj",
							silk: 30,
							giftSilk: 0,
							currencyMask: 22,
							allowsPoints: true,
							purchaseLimit: 5,
							itemIds: [ 3630 ]
						} ]
					}
				},
				entities: [ entity ],
				width: 1200,
				height: 900,
				worldReady: true
			};
			globalThis.mallSearchFixture = {
				state,
				commands,
				event: ui.event,
				stats: ui.stats,
				get scene() {
					return scene;
				},
				get semantics() {
					return semantics;
				},
				draw() {
					const next = ui.step( state, performance.now() );
					if ( next ) {
						semantics = next;
						platform.presentUi( next );
					}
					renderer.frame( { width: 1200, height: 900 } );
				},
				dispose() {
					ui.dispose();
					platform.dispose();
					assets.dispose();
					renderer.dispose();
				}
			};
			mallSearchFixture.draw();
		} );
		await mkdir( "temp/artifacts/item-mall-search", { recursive: true } );
		await page.evaluate( () => {
			mallSearchFixture.event( { kind: "key", code: 121 } );
			mallSearchFixture.draw();
			mallSearchFixture.event( { kind: "activate", id: "item-mall-main:0" } );
			mallSearchFixture.draw();
		} );
		await page.waitForFunction( () => {
			mallSearchFixture.draw();
			return mallSearchFixture.semantics?.controls.some( c => c.id === "item-mall-search" );
		} );
		const search = page.locator( '[data-ui-id="item-mall-search"]' );
		await search.click();
		await page.keyboard.type( "SN_POTION" );
		await page.evaluate( () => mallSearchFixture.draw() );
		assert.equal( await search.inputValue(), "SN_POTION", "typed search reaches the field" );
		assert.equal(
			await page.evaluate( () =>
				mallSearchFixture.semantics.controls.find( c => c.id === "item-mall-search" )?.value
			),
			"SN_POTION",
			"typed search reaches the mall state"
		);
		await page.screenshot( { path: "temp/artifacts/item-mall-search/typed.png" } );
		await page.keyboard.press( "Backspace" );
		await page.evaluate( () => mallSearchFixture.draw() );
		assert.equal( await search.inputValue(), "SN_POTIO", "Backspace deletes the last character" );
		await page.keyboard.press( "Backspace" );
		await page.keyboard.press( "Backspace" );
		await page.evaluate( () => mallSearchFixture.draw() );
		assert.equal( await search.inputValue(), "SN_POT", "Backspace keeps deleting" );
		assert.equal(
			await page.evaluate( () =>
				mallSearchFixture.semantics.controls.find( c => c.id === "item-mall-search" )?.value
			),
			"SN_POT",
			"Backspace edits the mall state"
		);
		await page.keyboard.press( "ArrowLeft" );
		await page.keyboard.press( "Delete" );
		await page.evaluate( () => mallSearchFixture.draw() );
		assert.equal( await search.inputValue(), "SN_PT", "Delete removes the character after the caret" );
		await page.keyboard.press( "Control+A" );
		await page.keyboard.press( "Backspace" );
		await page.evaluate( () => mallSearchFixture.draw() );
		assert.equal( await search.inputValue(), "", "Backspace clears a full selection" );
		// Inventory work pending elsewhere in the world must not lock this
		// field: its edits are local state, and a locked editor eats its
		// editing keys without any sign (#4153).
		await page.keyboard.type( "AB" );
		await page.evaluate( () => {
			mallSearchFixture.state.gameplay.inventoryPending = true;
			mallSearchFixture.draw();
		} );
		await page.keyboard.press( "Backspace" );
		await page.evaluate( () => mallSearchFixture.draw() );
		assert.equal( await search.inputValue(), "A", "Backspace deletes while inventory work is pending" );
		await page.evaluate( () => {
			mallSearchFixture.state.gameplay.inventoryPending = false;
			mallSearchFixture.draw();
		} );
		assert.deepEqual( errors, [], "no page errors" );
		await page.evaluate( () => mallSearchFixture.dispose() );
	} finally {
		await browser.close();
	}
} );
