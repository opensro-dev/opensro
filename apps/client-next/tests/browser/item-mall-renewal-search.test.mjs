/*
===========================================================================

item-mall-renewal-search.test.mjs - the renewal mall search field's
editing keys

Drives the renewal Item Mall (the default window) through the browser
platform: opens the mall, types in the real DOM search field, presses
Enter to apply the search, then Backspace and Delete. #4153's classic
rail test (item-mall-search.test.mjs) covers the later-category field;
the renewal field edits a draft its Search button or Enter applies, and
the draft must keep shrinking under Backspace afterwards.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

// Installed on the page's global by the setup evaluate below; declared
// so the browser-side closures type-check (in the page they resolve the
// global the setup installed).
/** @type {any} */
let mallRenewalSearchFixture;

test( "renewal Item Mall search field deletes with Backspace and Delete after Enter", { timeout: 120000 }, async () => {
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
						silk: 700,
						giftSilk: 20,
						points: 5,
						pending: false,
						revision: 1,
						tabs: [ {
							shop: 5,
							tab: 0,
							category: "MALL_AVATAR",
							label: "UIIT_STT_SILKMALL_BOOTH"
						}, {
							shop: 5,
							tab: 1,
							category: "MALL_AVATAR",
							label: "UIIT_STT_SILKMALL_DRESS"
						} ],
						offers: [ {
							group: 852,
							shop: 5,
							tab: 1,
							slot: 0,
							packageId: 30000,
							name: "Dress 0",
							description: "",
							icon: "item/dress.ddj",
							silk: 130,
							giftSilk: 0,
							currencyMask: 2,
							allowsPoints: false,
							purchaseLimit: 1,
							itemIds: [ 500 ]
						} ]
					}
				},
				entities: [ entity ],
				width: 1200,
				height: 900,
				worldReady: true
			};
			globalThis.mallRenewalSearchFixture = {
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
			mallRenewalSearchFixture.draw();
		} );
		await mkdir( "temp/artifacts/item-mall-renewal-search", { recursive: true } );
		await page.evaluate( () => {
			mallRenewalSearchFixture.event( { kind: "key", code: 121 } );
			mallRenewalSearchFixture.draw();
		} );
		await page.waitForFunction( () => {
			mallRenewalSearchFixture.draw();
			return mallRenewalSearchFixture.semantics?.controls.some( c => c.id === "item-mall-search" );
		} );
		const search = page.locator( '[data-ui-id="item-mall-search"]' );
		await search.click();
		await page.keyboard.type( "DRESS" );
		await page.evaluate( () => mallRenewalSearchFixture.draw() );
		assert.equal( await search.inputValue(), "DRESS", "typed search reaches the field" );
		assert.equal(
			await page.evaluate( () =>
				mallRenewalSearchFixture.semantics.controls.find( c => c.id === "item-mall-search" )?.value
			),
			"DRESS",
			"typed search reaches the mall draft"
		);
		await page.screenshot( { path: "temp/artifacts/item-mall-renewal-search/typed.png" } );
		await page.keyboard.press( "Enter" );
		await page.evaluate( () => mallRenewalSearchFixture.draw() );
		assert.equal( await search.inputValue(), "DRESS", "Enter applies the search and keeps the draft" );
		await page.keyboard.press( "Backspace" );
		await page.evaluate( () => mallRenewalSearchFixture.draw() );
		assert.equal( await search.inputValue(), "DRES", "Backspace deletes the last character after Enter" );
		await page.keyboard.press( "Backspace" );
		await page.keyboard.press( "Backspace" );
		await page.evaluate( () => mallRenewalSearchFixture.draw() );
		assert.equal( await search.inputValue(), "DR", "Backspace keeps deleting" );
		assert.equal(
			await page.evaluate( () =>
				mallRenewalSearchFixture.semantics.controls.find( c => c.id === "item-mall-search" )?.value
			),
			"DR",
			"Backspace edits the mall draft"
		);
		await page.keyboard.press( "ArrowLeft" );
		await page.keyboard.press( "Delete" );
		await page.evaluate( () => mallRenewalSearchFixture.draw() );
		assert.equal( await search.inputValue(), "D", "Delete removes the character after the caret" );
		await page.keyboard.press( "Control+A" );
		await page.keyboard.press( "Backspace" );
		await page.evaluate( () => mallRenewalSearchFixture.draw() );
		assert.equal( await search.inputValue(), "", "Backspace clears a full selection" );
		// The player clicks the drawn glyphs at the field's left, where the
		// draft is drawn, not the box's centre: that click must put the
		// caret inside the text. It used to land at 0 - the editor's text
		// was centred while the drawing is left-aligned - where Backspace
		// has nothing before it and only Delete eats forward (#4153).
		await page.keyboard.type( "DRESS" );
		await page.evaluate( () => mallRenewalSearchFixture.draw() );
		await search.click( { position: { x: 10, y: 10 } } );
		await page.keyboard.press( "Backspace" );
		await page.evaluate( () => mallRenewalSearchFixture.draw() );
		assert.equal(
			(await search.inputValue()).length,
			4,
			"Backspace deletes after a click on the drawn text"
		);
		// Enter applies the draft from the focused field, as the Search
		// button does: the open category's tabs stand while the draft is
		// unapplied and give way once the search takes.
		await page.evaluate( () => {
			mallRenewalSearchFixture.event( { kind: "activate", id: "item-mall-category:0" } );
			mallRenewalSearchFixture.draw();
		} );
		assert.equal(
			await page.evaluate( () =>
				mallRenewalSearchFixture.semantics.controls.some( c => c.id.startsWith( "item-mall-tab:" ) )
			),
			true,
			"the open category shows its tabs before the search"
		);
		await page.keyboard.press( "Control+A" );
		await page.keyboard.type( "ZZZZ" );
		await page.evaluate( () => mallRenewalSearchFixture.draw() );
		assert.equal(
			await page.evaluate( () =>
				mallRenewalSearchFixture.semantics.controls.some( c => c.id.startsWith( "item-mall-tab:" ) )
			),
			true,
			"an unapplied draft leaves the category tabs standing"
		);
		await page.keyboard.press( "Enter" );
		await page.evaluate( () => mallRenewalSearchFixture.draw() );
		assert.equal( await search.inputValue(), "ZZZZ", "Enter keeps the draft it applied" );
		assert.equal(
			await page.evaluate( () =>
				mallRenewalSearchFixture.semantics.controls.some( c => c.id.startsWith( "item-mall-tab:" ) )
			),
			false,
			"Enter applies the search from the focused field"
		);
		assert.deepEqual( errors, [], "no page errors" );
		await page.evaluate( () => mallRenewalSearchFixture.dispose() );
	} finally {
		await browser.close();
	}
} );
