/*
===========================================================================

repair-hammer.test.mjs - item targeting overrides real inventory pointer input

Uses the shipped UI and browser platform with published assets. Captures
commands to distinguish repair clicks from pickup, sale and equipment use.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "repair hammer consumes pickup, modifier clicks and right-button equipment use", {
	timeout: 90000
}, async () => {
	const { browser, page } = await launchProbeBrowser();
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
			let scene, semantics;
			const ui = createUi(
				assets,
				c => commands.push( c ),
				s => {
					scene = s;
					renderer.setUi( s );
				},
				( id, image ) => renderer.setUiTexture( id, image ),
				location.origin,
				"http://fixture.invalid"
			);
			const platform = createPlatform(
				document.querySelector( "canvas" ),
				document.querySelector( "output" ),
				() => {},
				() => {},
				() => {},
				ui.event,
				ui.blocks
			);
			const state = {
				session: { phase: "world", revision: 1, character: "Fixture" },
				gameplay: {
					localGid: 1,
					target: 42,
					shop: { npc: 42, name: "Smith", offers: [], buyback: [] },
					quickSlots: [ { slot: 41, kind: 0x4a, payload: 4000 }, { slot: 42, kind: 0x4a, payload: 4001 } ],
					inventorySlotCount: 45,
					equipmentSlotCount: 13,
					inventoryPending: false,
					inventory: [ 6, 14 ].map( slot => ({
						slot,
						refObjId: 4161,
						typeFlags: 13100,
						quantity: 1,
						name: "Bronz Bow",
						icon: "item/china/weapon/bow_02.ddj",
						magic: [],
						durability: 40,
						variance: "31",
						tooltip: {
							fields: {
								varianceIntMin1c0: 40,
								varianceIntMax1c4: 100,
								canRepair: 1,
								repairCostB4: 200,
								reviveCostB8: 50
							}
						}
					}) ),
					vitals: [],
					casts: []
				},
				entities: [],
				width: 1200,
				height: 900,
				worldReady: true
			};
			globalThis.inventoryFixture = {
				ui,
				state,
				commands,
				get scene() {
					return scene;
				},
				get semantics() {
					return semantics;
				},
				/*
				================
				draw
				================
				*/
				draw() {
					const next = ui.step( state, performance.now() );
					if ( next ) {
						semantics = next;
						platform.presentUi( next );
					}
					renderer.frame( { width: 1200, height: 900 } );
				},
				/*
				================
				dispose
				================
				*/
				dispose() {
					ui.dispose();
					platform.dispose();
					assets.dispose();
					renderer.dispose();
				}
			};
			inventoryFixture.draw();
			ui.event( { kind: "activate", id: "open-window:Shop" } );
		} );
		await page.waitForFunction( () => {
			inventoryFixture.draw();
			return inventoryFixture.semantics?.controls.some( c => c.id === "slot:14" ) &&
				inventoryFixture.semantics.controls.some( c => c.id === "shop-repair:GDR_STORE_BTN_REPAIR" );
		} );
		await page.waitForFunction( () => {
			inventoryFixture.draw();
			const cover = document.getElementById( "startup-loading" );
			return !cover || getComputedStyle( cover ).opacity === "0";
		} );

		/*
		================
		draw

		Keep tooltip admission separate from this pointer-routing fixture.
		================
		*/
		const draw = async () => {
			await page.mouse.move( 0, 0 );
			await page.evaluate( () => inventoryFixture.draw() );
		};
		const hammer = page.locator( '[data-ui-id="shop-repair:GDR_STORE_BTN_REPAIR"]' );
		await hammer.click();
		await draw();
		assert.equal( await page.evaluate( () => inventoryFixture.ui.cursor() ), 0x96 );
		const slots = await page.evaluate( () =>
			inventoryFixture.semantics.controls.filter( c => [ "slot:6", "slot:14" ].includes( c.id ) )
		);
		assert.equal( slots.length, 2 );
		assert.ok( slots.every( c => !c.carry && !c.draggable ), "hammer must suppress browser carry and drag" );
		/** @type {Array<Array<"Control" | "Shift" | "Alt">>} */
		const modifierCases = [ [], [ "Control" ], [ "Shift" ], [ "Alt" ] ];
		for ( const slot of [ 6, 14 ] ) {
			for ( const modifiers of modifierCases ) {
				await page.evaluate( () => inventoryFixture.commands.length = 0 );
				await page.locator( `[data-ui-id="slot:${slot}"]` ).click( { modifiers } );
				await draw();
				assert.deepEqual( await page.evaluate( () => inventoryFixture.commands ), [
					{ kind: "gameplay", command: { kind: "shop-repair", mode: 1, slot } }
				] );
				assert.equal( await page.evaluate( () => inventoryFixture.ui.cursor() ), 0x96 );
			}
		}
		await page.evaluate( () => inventoryFixture.commands.length = 0 );
		await page.locator( '[data-ui-id="slot:6"]' ).click( { button: "right" } );
		await draw();
		assert.equal( await page.evaluate( () => inventoryFixture.ui.cursor() ), null );
		assert.deepEqual( await page.evaluate( () => inventoryFixture.commands ), [] );
		await hammer.click();
		await draw();
		await page.locator( '[data-ui-id="slot:14"]' ).dblclick();
		await draw();
		const double = await page.evaluate( () => inventoryFixture.commands );
		assert.equal( double.length, 2 );
		assert.ok( double.every( c => c.command.kind === "shop-repair" ), "double-click never sells" );
		await page.evaluate( () => inventoryFixture.commands.length = 0 );
		const from = await page.locator( '[data-ui-id="slot:6"]' ).boundingBox();
		const to = await page.locator( '[data-ui-id="slot:14"]' ).boundingBox();
		assert.ok( from && to );
		await page.mouse.move( from.x + 16, from.y + 16 );
		await page.mouse.down();
		await page.mouse.move( to.x + 16, to.y + 16, { steps: 5 } );
		await page.mouse.up();
		await draw();
		assert.deepEqual( await page.evaluate( () => inventoryFixture.commands ), [] );
		await page.keyboard.press( "Escape" );
		await draw();
		assert.equal( await page.evaluate( () => inventoryFixture.ui.cursor() ), null );
		await hammer.click();
		await draw();
		await page.locator( '[data-ui-id="shop-repair:GDR_STORE_BTN_REPAIRALL"]' ).click();
		await draw();
		assert.equal( await page.evaluate( () => inventoryFixture.ui.cursor() ), null );
		await page.locator( '[data-ui-id="repair-all-cancel"]' ).click();
		await draw();
		await page.locator( '[data-ui-id="shop-repair:GDR_STORE_BTN_REPAIRALL"]' ).click();
		await draw();
		await page.evaluate( () => inventoryFixture.commands.length = 0 );
		await page.locator( '[data-ui-id="repair-all-confirm"]' ).click();
		await draw();
		assert.deepEqual( await page.evaluate( () => inventoryFixture.commands ), [
			{ kind: "gameplay", command: { kind: "shop-repair", mode: 2, slot: 0 } }
		] );
		await hammer.click();
		await draw();
		await page.evaluate( () => {
			inventoryFixture.state.gameplay.shop = undefined;
			inventoryFixture.draw();
		} );
		assert.equal( await page.evaluate( () => inventoryFixture.ui.cursor() ), null );
		assert.ok(
			await page.evaluate( () =>
				inventoryFixture.semantics.controls
					.filter( c => [ "slot:6", "slot:14" ].includes( c.id ) ).every( c => c.carry && c.draggable )
			)
		);
		await mkdir( "temp/artifacts/repair-hammer", { recursive: true } );
		await page.screenshot( { path: "temp/artifacts/repair-hammer/restored-input.png" } );
		await page.evaluate( () => {
			inventoryFixture.state.gameplay.inventory.push( {
				...inventoryFixture.state.gameplay.inventory[1],
				slot: 13,
				refObjId: 8985,
				typeFlags: 0x66ec,
				name: "Clock of Reincarnation"
			} );
			inventoryFixture.ui.event( { kind: "activate", id: "open-window:Inventory" } );
			inventoryFixture.draw();
		} );
		await page.waitForFunction( () => {
			inventoryFixture.draw();
			return inventoryFixture.semantics?.controls.some( control => control.id === "slot:13" );
		} );
		for ( const slot of [ 6, 14 ] ) {
			await page.evaluate( () => inventoryFixture.commands.length = 0 );
			await page.locator( '[data-ui-id="slot:13"]' ).click( { button: "right" } );
			await draw();
			assert.equal( await page.evaluate( () => inventoryFixture.ui.cursor() ), 0xa6 );
			const target = await page.locator( `[data-ui-id="slot:${slot}"]` ).boundingBox();
			assert.ok( target );
			await page.mouse.move( target.x + 12, target.y + 12 );
			await page.mouse.down();
			await page.mouse.move( target.x + 14, target.y + 14 );
			await page.mouse.up();
			await draw();
			assert.equal( await page.evaluate( () => inventoryFixture.ui.cursor() ), null );
			assert.equal( await page.locator( '[data-ui-id="cos-renew-confirm"]' ).count(), 1 );
			assert.deepEqual( await page.evaluate( () => inventoryFixture.commands ), [] );
			await page.locator( '[data-ui-id="cos-renew-confirm"]' ).click();
			await draw();
			assert.deepEqual( await page.evaluate( () => inventoryFixture.commands ), [ {
				kind: "gameplay",
				command: { kind: "item-use", slot: 13, summonerSlot: slot }
			} ] );
		}
		await page.evaluate( () => inventoryFixture.dispose() );
	} finally {
		await browser.close();
	}
} );
