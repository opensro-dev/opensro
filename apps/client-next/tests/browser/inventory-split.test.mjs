/*
===========================================================================

inventory-split.test.mjs - the native stack split dialog in a real page

Opens the split dialog through the browser platform and reads its
captions back from the glyphs the published text runs paint.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "native split dialog receives real Shift-click, displays authored geometry and sends only confirmation", {
	timeout: 45000
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
					quickSlots: [ { slot: 41, kind: 0x4a, payload: 4000 }, { slot: 42, kind: 0x4a, payload: 4001 } ],
					inventorySlotCount: 45,
					equipmentSlotCount: 13,
					inventoryPending: false,
					inventory: [ {
						slot: 14,
						refObjId: 3630,
						typeFlags: 0x8ec,
						quantity: 10,
						name: "HP recovery herb",
						icon: "item/etc/hp_potion_01.ddj",
						magic: []
					} ],
					vitals: [],
					casts: []
				},
				entities: [],
				width: 1200,
				height: 900,
				worldReady: true
			};
			globalThis.inventoryFixture = {
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
			ui.event( { kind: "activate", id: "open-window:Inventory" } );
		} );
		await page.waitForFunction( () => {
			inventoryFixture.draw();
			return inventoryFixture.semantics?.controls.some( c => c.id === "slot:14" );
		} );
		await page.waitForFunction( () => {
			inventoryFixture.draw();
			const cover = document.getElementById( "startup-loading" );
			return !cover || getComputedStyle( cover ).opacity === "0";
		} );
		await page.locator( '[data-ui-id="slot:14"]' ).click( { modifiers: [ "Shift" ] } );
		await page.waitForFunction( () => {
			inventoryFixture.draw();
			return inventoryFixture.scene.quads.some( q => q.texture.endsWith( "msgbox_iteminfo_3.png" ) );
		} );
		await page.locator( '[data-ui-id="split-amount"]' ).fill( "3" );
		await page.evaluate( () => inventoryFixture.draw() );
		await mkdir( "temp/artifacts/inventory-split", { recursive: true } );
		await page.screenshot( { path: "temp/artifacts/inventory-split/divide-count.png" } );
		const captions = await page.evaluate( async () => {
			const atlas = await (await fetch( "/assets/fonts/native-ui-font-atlas.json" )).json(),
				font = atlas.fonts["0"];
			const { expandTextRuns } = await import( "/src/engine/foundation/rendering/text-run.ts" );
			const quantity = inventoryFixture.scene.quads.find( q => q.texture.endsWith( "msgbox_quantity.png" ) ).rect;
			const x = quantity[0] - 223, y = quantity[1] - 102;
			return [ 34, 153 ].map( offset => {
				const left = x + offset, right = left + 64;
				// Published text is runs; read the glyphs they paint.
				const glyphs = expandTextRuns( inventoryFixture.scene.quads ).filter( q =>
					q.texture === atlas.image && q.clip[0] === left && q.clip[2] === 64 && q.rect[1] >= y + 106 &&
					q.rect[1] < y + 125
				);
				return {
					text: glyphs.map( q =>
						String.fromCodePoint(
							Number(
								Object.entries( font.glyphs ).find( ( [, g] ) =>
									g.x / atlas.atlasWidth === q.uv[0] && g.y / atlas.atlasHeight === q.uv[1]
								)[0]
							)
						)
					).join( "" ),
					contained: glyphs.length > 0 &&
						glyphs.every( q => q.rect[0] >= left && q.rect[0] + q.rect[2] <= right )
				};
			} );
		} );
		assert.deepEqual( captions.map( c => c.text ), [ "Retained ...", "Distribute..." ] );
		assert.ok( captions.every( c => c.contained ), "localized captions cannot overlap either quantity control" );
		const before = await page.evaluate( () => ({
			commands: inventoryFixture.commands,
			quantity: inventoryFixture.scene.quads.find( q => q.texture.endsWith( "msgbox_quantity.png" ) )?.rect,
			controls: inventoryFixture.semantics.controls.map( c => c.id )
		}) );
		assert.deepEqual( before.commands, [] );
		assert.deepEqual( before.quantity.slice( 2 ), [ 42, 24 ] );
		assert.deepEqual( before.controls, [ "split-amount", "split-confirm", "split-cancel" ] );
		await page.locator( '[data-ui-id="split-confirm"]' ).click();
		await page.evaluate( () => inventoryFixture.draw() );
		assert.deepEqual( await page.evaluate( () => inventoryFixture.commands.at( -1 ) ), {
			kind: "gameplay",
			command: { kind: "inventory-move", source: 14, destination: 13, quantity: 3 }
		} );
		await page.keyboard.press( "KeyI" );
		await page.evaluate( () => inventoryFixture.draw() );
		await page.waitForFunction( () => {
			inventoryFixture.draw();
			return document.querySelector( '[data-ui-id="hotbar:41"]' ) &&
				document.querySelector( '[data-ui-id="hotbar:42"]' );
		} );
		const source = await page.locator( '[data-ui-id="hotbar:41"]' ).boundingBox(),
			destination = await page.locator( '[data-ui-id="hotbar:42"]' ).boundingBox();
		assert.ok( source && destination );
		await page.evaluate( () => inventoryFixture.commands.length = 0 );
		await page.mouse.move( source.x + 16, source.y + 16 );
		await page.mouse.down();
		await page.mouse.move( destination.x + 16, destination.y + 16, { steps: 6 } );
		await page.evaluate( () => inventoryFixture.draw() );
		await page.mouse.up();
		await page.evaluate( () => inventoryFixture.draw() );
		assert.deepEqual( await page.evaluate( () => inventoryFixture.commands ), [
			{ kind: "gameplay", command: { kind: "quickslot-set", binding: { slot: 41, kind: 0x4a, payload: 4001 } } },
			{ kind: "gameplay", command: { kind: "quickslot-set", binding: { slot: 42, kind: 0x4a, payload: 4000 } } }
		], "real pointer drag swaps both extended bindings and never activates either action" );
		await page.evaluate( () => inventoryFixture.dispose() );
	} finally {
		await browser.close();
	}
} );
