/*
===========================================================================

native-service-windows.test.mjs - shop, quantity and storage windows in a real page

Drives the native service windows through the browser platform: asset
admission, typed quantities (clamped to their limits as 521A85 does),
modifier clicks and the commands they send.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { assertNativeStretchRaster } from "./stretch-raster.mjs";

test( "native service windows admit assets and preserve typed transactions", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", e => errors.push( String( e ) ) );
	await page.context().tracing.start( { screenshots: true, snapshots: true, sources: true } );
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
			const ui = createUi(
				assets,
				c => commands.push( c ),
				s => {
					scene = s;
					renderer.setUi(
						s ?
							{
								...s,
								quads: [ {
									rect: [ 0, 0, 1200, 900 ],
									clip: [ 0, 0, 1200, 900 ],
									texture: "",
									uv: [ 0, 0, 1, 1 ],
									color: [ .08, .4, .15, 1 ]
								}, ...s.quads ]
							} :
							null
					);
				},
				( id, image ) => renderer.setUiTexture( id, image ),
				location.origin,
				"http://fixture.invalid"
			);
			const item = {
				slot: 14,
				refObjId: 3630,
				typeFlags: 0x8ec,
				quantity: 10,
				name: "HP recovery herb",
				icon: "item/etc/hp_potion_01.ddj",
				magic: []
			};
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
					target: 2,
					inventorySlotCount: 45,
					equipmentSlotCount: 13,
					inventoryPending: false,
					inventory: [ item ],
					progression: { level: 30, gold: "12345", skillPoints: 200, masteries: [] },
					skills: [],
					vitals: [],
					casts: [],
					shop: {
						npc: 2,
						name: "Merchant",
						offers: [ {
							tab: 0,
							slot: 0,
							refObjId: 3630,
							name: item.name,
							icon: item.icon,
							price: "10",
							maxStack: 50,
							items: [ item ]
						} ],
						buyback: [ {
							id: 5,
							index: 0,
							refObjId: 3630,
							name: item.name,
							icon: item.icon,
							quantity: 2,
							price: "20",
							plus: 0
						} ]
					},
					cosRecords: [ {
						gid: 3,
						refObjId: 4,
						band: 4,
						hp: 100,
						mp: 10,
						status: 28,
						dead: false,
						name: "Pet",
						commandMode: 199,
						inventory: [ { ...item, slot: 0 } ]
					} ],
					social: {
						self: 1,
						localName: "Fixture",
						leader: 0,
						options: 0,
						members: [],
						invitation: null,
						error: null,
						guild: {
							id: 1,
							name: "Guild",
							level: 3,
							gp: 123,
							subject: "Guild notice",
							contents: "Contents",
							members: [ {
								id: 1,
								name: "Fixture",
								grade: 0,
								level: 30,
								donated: 123,
								permissions: 255,
								grant: "Master",
								model: 1907,
								role: 0,
								offline: 0
							} ]
						}
					}
				},
				entities: [],
				width: 1200,
				height: 900,
				worldReady: true
			};
			globalThis.serviceFixture = {
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
			serviceFixture.draw();
			ui.event( { kind: "activate", id: "open-window:Shop" } );
		} );
		await mkdir( "temp/artifacts/native-service-windows", { recursive: true } );
		for ( const panel of [ "Shop", "Guild", "Alchemy", "COS inventory" ] ) {
			await page.evaluate(
				panel => serviceFixture.event( { kind: "activate", id: "open-window:" + panel } ),
				panel
			);
			await page.waitForFunction( () => {
				serviceFixture.draw();
				return serviceFixture.stats().panel && serviceFixture.semantics?.controls.length > 0 &&
					!serviceFixture.stats().windowMissing.length && !serviceFixture.stats().error;
			} );
			await page.waitForFunction( () => {
				serviceFixture.draw();
				const cover = document.getElementById( "startup-loading" );
				return !cover || getComputedStyle( cover ).opacity === "0";
			} );
			await page.waitForTimeout( 300 );
			await page.evaluate( () => serviceFixture.draw() );
			const png = await page.screenshot( {
				path: "temp/artifacts/native-service-windows/" + panel.replaceAll( " ", "-" ) + ".png"
			} );
			if ( panel === "Guild" ) {
				const holes = await page.evaluate( async png => {
					const image = await createImageBitmap(
							new Blob( [ Uint8Array.from( atob( png ), c => c.charCodeAt( 0 ) ) ], {
								type: "image/png"
							} )
						),
						canvas = new OffscreenCanvas( image.width, image.height ),
						ctx = canvas.getContext( "2d" );
					ctx.drawImage( image, 0, 0 );
					image.close();
					const { data } = ctx.getImageData( 0, 0, canvas.width, canvas.height ),
						sample = (200 * canvas.width + 200) * 4;
					let holes = 0;
					for ( let y = 502; y < 610; y++ ) {
						for ( let x = 400; x < 697; x++ ) {
							const p = (y * canvas.width + x) * 4;
							if (
								data[p] === data[sample] && data[p + 1] === data[sample + 1] &&
								data[p + 2] === data[sample + 2]
							) holes++;
						}
					}
					return holes;
				}, png.toString( "base64" ) );
				assert.equal( holes, 0, "empty guild rows must paint their native background" );
			}
			const controls = await page.evaluate( () => serviceFixture.semantics.controls );
			if ( panel === "Shop" ) assert.ok( controls.some( c => c.id === "shop-offer:0" ) );
			if ( panel === "Guild" ) assert.ok( controls.some( c => c.id === "social-member:1" ) );
			if ( panel === "Alchemy" ) {
				assert.equal( controls.filter( c => c.id.startsWith( "alchemy-empty:" ) ).length, 5 );
			}
			if ( panel === "COS inventory" ) {
				assert.equal( controls.filter( c => c.id.startsWith( "cos-slot:" ) ).length, 28 );
			}
		}
		await page.evaluate( () => {
			serviceFixture.event( { kind: "activate", id: "cos-tab:2" } );
			serviceFixture.draw();
		} );
		await page.waitForFunction( () => {
			serviceFixture.draw();
			// A held-over window publishes its controls disabled; wait for the admitted one.
			return !serviceFixture.stats().windowMissing.length &&
				serviceFixture.semantics.controls.some( c => c.id === "cos-save" && !c.disabled );
		} );
		await page.screenshot( { path: "temp/artifacts/native-service-windows/COS-settings.png" } );
		await page.evaluate( () => {
			serviceFixture.event( { kind: "activate", id: "cos-setting:1" } );
			serviceFixture.draw();
		} );
		// The toggled setting changes the window's art; save once it is re-admitted.
		await page.waitForFunction( () => {
			serviceFixture.draw();
			return serviceFixture.semantics.controls.some( c => c.id === "cos-save" && !c.disabled );
		} );
		await page.evaluate( () => {
			serviceFixture.event( { kind: "activate", id: "cos-save" } );
			serviceFixture.draw();
		} );
		assert.ok(
			(await page.evaluate( () => serviceFixture.commands )).some( c =>
				c.command?.kind === "cos-behavior" && c.command.mode === 198
			)
		);
		await page.evaluate( () => {
			serviceFixture.event( { kind: "activate", id: "cos-tab:1" } );
			serviceFixture.draw();
			const target = serviceFixture.semantics.controls.find( c => c.id === "cos-slot:0" ).rect;
			serviceFixture.event( { kind: "drag", id: "slot:14", dx: 1, dy: 1 } );
			serviceFixture.event( { kind: "drag-end", id: "slot:14", x: target[0] + 16, y: target[1] + 16 } );
			serviceFixture.draw();
		} );
		assert.ok(
			(await page.evaluate( () => serviceFixture.commands )).some( c =>
				c.command?.kind === "cos-transfer" && c.command.toCos && c.command.source === 14 &&
				c.command.destination === 0
			)
		);
		await page.evaluate( () => {
			serviceFixture.event( { kind: "activate", id: "open-window:Shop" } );
			serviceFixture.draw();
			serviceFixture.event( { kind: "activate", id: "shop-offer:0" } );
			serviceFixture.draw();
		} );
		await page.waitForFunction( () => {
			serviceFixture.draw();
			return serviceFixture.semantics.controls.some( c => c.id === "shop-trade" );
		} );
		const quantity = page.locator( '[data-ui-id="shop-quantity"]' );
		const before = await quantity.evaluate( el => ({
			focused: document.activeElement === el,
			value: el.value,
			start: el.selectionStart,
			end: el.selectionEnd
		}) );
		const confirmationPng = await page.screenshot( {
			path: "temp/artifacts/native-service-windows/confirmation-open.png"
		} );
		const origin = await page.evaluate( () => {
			const r = serviceFixture.semantics.controls.find( c => c.id === "shop-dialog-drag" ).rect;
			return [ r[0] - 10, r[1] ];
		} );
		const foreground = await page.evaluate( () =>
			serviceFixture.scene.quads.filter( q => q.texture.startsWith( "/assets/fonts/" ) ).map( q => q.rect )
		);
		const borderPixels = await assertNativeStretchRaster( page, confirmationPng, [
			{ id: "ifitemmallconfirmbuy/Create/GDR_ITEMMALL_CONFIRM_BUY_BLACKSQUARE_1", origin },
			{ id: "ifitemmallconfirmbuy/Create/GDR_ITEMMALL_CONFIRM_BUY_BLACKSQUARE_6", origin }
		], foreground );
		await writeFile(
			"temp/artifacts/native-service-windows/border-raster.json",
			JSON.stringify( borderPixels, null, 2 )
		);
		// Reproduce the reported seven-digit purchase, without changing the
		// inventory money control's distinct grouping/color contract.
		await page.evaluate( () => {
			const f = serviceFixture, g = f.state.gameplay;
			f.event( { kind: "activate", id: "shop-cancel" } );
			g.shop.offers[0] = { ...g.shop.offers[0], name: "Virgo Wind'sVane", price: "1915000" };
			g.progression.gold = "999999999";
			f.draw();
			f.event( { kind: "activate", id: "shop-offer:0" } );
			f.draw();
		} );
		await page.screenshot( {
			path: "temp/artifacts/native-service-windows/confirmation-seven-digits.png",
			clip: { x: origin[0], y: origin[1], width: 327, height: 177 }
		} );
		const priceText = await page.evaluate( async ( [mx, my] ) => {
			const atlas = await (await fetch( "/assets/fonts/native-ui-font-atlas.json" )).json(),
				glyphs = atlas.fonts["0"].glyphs;
			const decode = quads =>
				quads.map( q => {
					const entry = Object.entries( glyphs ).find( ( [, g] ) =>
						q.uv[0] === g.x / atlas.atlasWidth && q.uv[1] === g.y / atlas.atlasHeight
					);
					return entry ? String.fromCodePoint( Number( entry[0] ) ) : "?";
				} ).join( "" );
			const { expandTextRuns } = await import( "/src/engine/foundation/rendering/text-run.ts" );
			// Published text is runs; read the glyphs they paint.
			const quads = expandTextRuns( serviceFixture.scene.quads ).filter( q => q.texture === atlas.image );
			return {
				amount: decode(
					quads.filter( q =>
						q.rect[0] >= mx + 112 && q.rect[0] + q.rect[2] <= mx + 214 && q.rect[1] >= my + 102 &&
						q.rect[1] < my + 118
					)
				),
				dialog: decode(
					quads.filter( q =>
						q.rect[0] >= mx && q.rect[0] < mx + 327 && q.rect[1] >= my && q.rect[1] < my + 177
					)
				)
			};
		}, origin );
		assert.equal( priceText.amount, "1915000", "native currency row uses %d, not grouped inventory money" );
		assert.ok(
			priceText.dialog.includes( "Quantity" ) && priceText.dialog.includes( "Gold" ),
			"native captions may extend into unoccupied space without being shortened"
		);
		await page.evaluate( () => {
			const f = serviceFixture, g = f.state.gameplay;
			f.event( { kind: "activate", id: "shop-cancel" } );
			g.shop.offers[0] = { ...g.shop.offers[0], name: "HP recovery herb", price: "10" };
			g.progression.gold = "12345";
			f.draw();
			f.event( { kind: "activate", id: "shop-offer:0" } );
			f.draw();
		} );
		await page.keyboard.type( "50" );
		await page.evaluate( () => serviceFixture.draw() );
		const typed = await quantity.inputValue();
		await writeFile(
			"temp/artifacts/native-service-windows/confirmation-input.json",
			JSON.stringify( { before, typed }, null, 2 )
		);
		assert.deepEqual( before, { focused: true, value: "1", start: 0, end: 1 } );
		assert.equal( typed, "50", "typing immediately replaces the selected default quantity" );
		await page.screenshot( { path: "temp/artifacts/native-service-windows/confirmation-50.png" } );
		await page.keyboard.press( "Enter" );
		await page.evaluate( () => serviceFixture.draw() );
		assert.ok(
			(await page.evaluate( () => serviceFixture.commands )).some( c =>
				c.command?.kind === "shop-buy" && c.command.quantity === 50
			),
			"Enter from the actual input submits the purchase"
		);
		await page.evaluate( () => {
			serviceFixture.event( { kind: "activate", id: "shop-offer:0" } );
			serviceFixture.draw();
		} );
		assert.deepEqual( await quantity.evaluate( el => [ el.value, el.selectionStart, el.selectionEnd ] ), [
			"1",
			0,
			1
		], "reopening resets and selects the default" );
		await page.keyboard.press( "Backspace" );
		await page.evaluate( () => serviceFixture.draw() );
		assert.equal( await quantity.inputValue(), "" );
		assert.equal( await page.locator( '[data-ui-id="shop-trade"]' ).isDisabled(), true, "empty input cannot buy" );
		await page.keyboard.type( "51" );
		await page.evaluate( () => serviceFixture.draw() );
		// 521A85..521AD1: an oversized draft is replaced by the offer limit, which is a valid purchase.
		assert.equal( await quantity.inputValue(), "50", "over-stack input clamps to the stack limit" );
		assert.equal(
			await page.locator( '[data-ui-id="shop-trade"]' ).isDisabled(),
			false,
			"the clamped amount can buy"
		);
		await page.keyboard.press( "Control+A" );
		await page.keyboard.type( "abc" );
		await page.evaluate( () => serviceFixture.draw() );
		assert.equal( await quantity.inputValue(), "", "letters are rejected" );
		await page.keyboard.press( "Escape" );
		await page.evaluate( () => serviceFixture.draw() );
		assert.equal( await quantity.count(), 0, "Escape closes without a transaction" );
		await page.evaluate( () => {
			serviceFixture.event( { kind: "activate", id: "shop-offer:0" } );
			serviceFixture.draw();
		} );
		const box = await quantity.boundingBox();
		await page.mouse.click( box.x + 2, box.y + 2 );
		await page.evaluate( () => serviceFixture.draw() );
		assert.equal(
			await quantity.evaluate( el => document.activeElement === el ),
			true,
			"the edit border is clickable, not just its text inset"
		);
		const header = await page.locator( '[data-ui-id="shop-dialog-drag"]' ).boundingBox();
		await page.mouse.move( header.x + 80, header.y + 15 );
		await page.mouse.down();
		await page.mouse.move( header.x + 120, header.y + 45 );
		await page.mouse.up();
		await page.evaluate( () => serviceFixture.draw() );
		const moved = await quantity.boundingBox();
		assert.equal( moved.x - box.x, 40 );
		assert.equal( moved.y - box.y, 30 );
		await page.evaluate( () => {
			serviceFixture.event( { kind: "activate", id: "shop-trade" } );
			serviceFixture.draw();
		} );
		assert.ok(
			(await page.evaluate( () => serviceFixture.commands )).some( c =>
				c.command?.kind === "shop-buy" && c.command.tab === 0 && c.command.slot === 0 &&
				c.command.quantity === 1
			)
		);
		// Quantity mode follows reference type, independently of count and limit.
		await page.evaluate( () => {
			const f = serviceFixture, g = f.state.gameplay;
			g.shop.offers[0] = {
				...g.shop.offers[0],
				maxStack: 1,
				purchaseLimit: 5,
				items: [ { ...g.inventory[0], typeFlags: 0x12c } ],
				name: "Libra Gold Crown"
			};
			f.event( { kind: "activate", id: "shop-offer:0" } );
			f.draw();
		} );
		assert.deepEqual(
			await quantity.evaluate(
				el => [ document.activeElement === el, el.value, el.selectionStart, el.selectionEnd ]
			),
			[ true, "1", 0, 1 ],
			"native package purchase permits multiple pieces of equipment"
		);
		await page.keyboard.type( "5" );
		await page.evaluate( () => serviceFixture.draw() );
		assert.equal( await page.locator( '[data-ui-id="shop-trade"]' ).isDisabled(), false );
		await page.keyboard.press( "Control+A" );
		await page.keyboard.type( "6" );
		await page.evaluate( () => serviceFixture.draw() );
		// The purchase limit clamps the draft like the stack limit (521A85).
		assert.equal( await quantity.inputValue(), "5" );
		assert.equal( await page.locator( '[data-ui-id="shop-trade"]' ).isDisabled(), false );
		await page.screenshot( { path: "temp/artifacts/native-service-windows/confirmation-equipment.png" } );
		await page.keyboard.press( "Escape" );
		await page.evaluate( () => {
			const f = serviceFixture, g = f.state.gameplay;
			f.draw();
			g.shop.offers[0] = {
				...g.shop.offers[0],
				purchaseLimit: 1,
				items: [ g.inventory[0] ],
				name: "One-unit consumable"
			};
			f.event( { kind: "activate", id: "shop-offer:0" } );
			f.draw();
		} );
		assert.deepEqual(
			await quantity.evaluate(
				el => [ document.activeElement === el, el.disabled, el.selectionStart, el.selectionEnd ]
			),
			[ true, false, 0, 1 ],
			"a stackable item with limit one still owns an editable focused field"
		);
		await page.keyboard.press( "Escape" );
		await page.evaluate( () => {
			const f = serviceFixture, g = f.state.gameplay;
			f.draw();
			g.inventory = [ { ...g.inventory[0], quantity: 1 } ];
			f.event( { kind: "double-activate", id: "slot:14" } );
			f.draw();
		} );
		assert.deepEqual( await quantity.evaluate( el => [ document.activeElement === el, el.disabled, el.value ] ), [
			true,
			false,
			"1"
		], "selling a one-item stack still focuses its quantity" );
		await page.keyboard.press( "Escape" );
		await page.evaluate( () => {
			const f = serviceFixture;
			f.draw();
			f.event( { kind: "activate", id: "shop-buyback:0" } );
			f.draw();
		} );
		assert.equal( await quantity.count(), 0, "buyback restores the whole recorded stack, without an editor" );
		await page.keyboard.press( "Escape" );
		await page.evaluate( () => serviceFixture.draw() );
		// Real platform events must carry all modifier bits. CTRL wins combinations.
		await page.evaluate( () => {
			const f = serviceFixture, g = f.state.gameplay;
			g.inventory = [ { ...g.inventory[0], quantity: 10 } ];
			g.shop.offers[0] = { ...g.shop.offers[0], maxStack: 50, purchaseLimit: 50 };
			f.draw();
		} );
		for (
			const modifiers of [ [ "Control" ], [ "Control", "Shift" ], [ "Control", "Alt" ], [
				"Control",
				"Shift",
				"Alt"
			] ]
		) {
			const before = await page.evaluate( () => serviceFixture.commands.length );
			await page.locator( '[data-ui-id="shop-offer:0"]' ).click( { modifiers } );
			await page.evaluate( () => serviceFixture.draw() );
			assert.deepEqual(
				await page.evaluate(
					n => serviceFixture.commands.slice( n ).filter( c => c.command?.kind === "shop-buy" ).map( c =>
						c.command.quantity
					),
					before
				),
				[ 50 ]
			);
			assert.equal( await quantity.count(), 0 );
			const sellBefore = await page.evaluate( () => serviceFixture.commands.length );
			// The fixture draws only on request; the runtime draws every frame. Keep
			// drawing until the window re-admits after the buy (endWindow holds the
			// previous window, controls disabled, until its new images are resident).
			await page.waitForFunction( () => {
				serviceFixture.draw();
				return serviceFixture.semantics.controls.find( c => c.id === "slot:14" )?.disabled === false;
			} );
			await page.locator( '[data-ui-id="slot:14"]' ).click( { modifiers } );
			await page.evaluate( () => serviceFixture.draw() );
			assert.deepEqual(
				await page.evaluate(
					n => serviceFixture.commands.slice( n ).filter( c => c.command?.kind === "shop-sell" ).map( c =>
						c.command.quantity
					),
					sellBefore
				),
				[ 10 ]
			);
			assert.equal( await quantity.count(), 0 );
		}
		for ( const modifiers of [ [ "Shift" ], [ "Shift", "Alt" ] ] ) {
			// The fixture draws only on request; the runtime draws every frame. Keep
			// drawing until the window re-admits after the buy (endWindow holds the
			// previous window, controls disabled, until its new images are resident).
			await page.waitForFunction( () => {
				serviceFixture.draw();
				return serviceFixture.semantics.controls.find( c => c.id === "slot:14" )?.disabled === false;
			} );
			await page.locator( '[data-ui-id="slot:14"]' ).click( { modifiers } );
			await page.evaluate( () => serviceFixture.draw() );
			assert.equal(
				await page.locator( '[data-ui-id="split-amount"]' ).count(),
				1,
				"SHIFT splits while shop is visible"
			);
			await page.keyboard.press( "Escape" );
			await page.evaluate( () => serviceFixture.draw() );
		}
		for ( const modifiers of [ [ "Shift" ], [ "Alt" ], [ "Shift", "Alt" ] ] ) {
			await page.locator( '[data-ui-id="shop-offer:0"]' ).click( { modifiers } );
			await page.evaluate( () => serviceFixture.draw() );
			assert.deepEqual( await quantity.evaluate( el => [ document.activeElement === el, el.value ] ), [
				true,
				"1"
			], "without CTRL, package purchase still opens its quantity dialog" );
			await page.keyboard.press( "Escape" );
			await page.evaluate( () => serviceFixture.draw() );
		}
		const altBefore = await page.evaluate( () => serviceFixture.commands.length );
		await page.locator( '[data-ui-id="slot:14"]' ).click( { modifiers: [ "Alt" ] } );
		await page.evaluate( () => serviceFixture.draw() );
		assert.equal(
			await page.evaluate(
				n => serviceFixture.commands.slice( n ).filter( c => c.command?.kind === "shop-sell" ).length,
				altBefore
			),
			0,
			"ALT alone is not quick sell"
		);
		await page.evaluate( () => {
			const f = serviceFixture, g = f.state.gameplay;
			g.shop.saleQuotes = [ {
				slot: 14,
				refObjId: g.inventory[0].refObjId,
				quantity: 10,
				price: "5",
				noBuyback: true
			} ];
			f.draw();
		} );
		const warningBefore = await page.evaluate( () => serviceFixture.commands.length );
		await page.locator( '[data-ui-id="slot:14"]' ).click( { modifiers: [ "Control" ] } );
		await page.evaluate( () => serviceFixture.draw() );
		assert.equal( await page.locator( '[data-ui-id="shop-warning-confirm"]' ).count(), 1 );
		assert.equal(
			await page.evaluate(
				n => serviceFixture.commands.slice( n ).filter( c => c.command?.kind === "shop-sell" ).length,
				warningBefore
			),
			0
		);
		await page.locator( '[data-ui-id="shop-warning-confirm"]' ).click();
		await page.evaluate( () => serviceFixture.draw() );
		assert.equal(
			await page.evaluate(
				n => serviceFixture.commands.slice( n ).filter( c => c.command?.kind === "shop-sell" ).length,
				warningBefore
			),
			1
		);
		for ( const patch of [ { typeFlags: 0x8ed }, { summon: { state: 2, rentals: [] } } ] ) {
			const before = await page.evaluate( patch => {
				const f = serviceFixture;
				f.state.gameplay.inventory = [ {
					...f.state.gameplay.inventory[0],
					typeFlags: 0x8ec,
					summon: undefined,
					...patch
				} ];
				f.draw();
				return f.commands.length;
			}, patch );
			await page.locator( '[data-ui-id="slot:14"]' ).click( { modifiers: [ "Control" ] } );
			await page.evaluate( () => serviceFixture.draw() );
			assert.equal(
				await page.evaluate(
					n => serviceFixture.commands.slice( n ).filter( c => c.command?.kind === "shop-sell" ).length,
					before
				),
				0,
				"cash/active summon quick sell is refused"
			);
			assert.equal( await page.locator( '[data-ui-id="shop-warning-confirm"]' ).count(), 0 );
		}
		const admission = await page.evaluate( () => {
			const f = serviceFixture, g = f.state.gameplay;
			f.event( { kind: "activate", id: "open-window:" } );
			f.draw();
			const accepted = g.shop;
			g.shopCompletionRevision = 0;
			f.event( { kind: "activate", id: "shop-open" } );
			f.draw();
			const pending = f.stats().panel;
			g.shop = { ...accepted };
			f.draw();
			const stale = f.stats().panel;
			g.shop = { npc: 2, name: "", offers: [], error: "Select a nearby merchant" };
			g.shopCompletionRevision = 1;
			f.draw();
			const refused = f.stats().panel;
			f.event( { kind: "activate", id: "shop-open" } );
			g.shop = accepted;
			g.shopCompletionRevision = 2;
			f.draw();
			const granted = f.stats().panel;
			f.event( { kind: "activate", id: "open-window:" } );
			f.draw();
			f.event( { kind: "activate", id: "shop-open" } );
			f.event( { kind: "activate", id: "open-window:Inventory" } );
			g.shopCompletionRevision = 3;
			g.shop = { ...accepted };
			f.draw();
			return { pending, stale, refused, granted, cancelled: f.stats().panel };
		} );
		assert.deepEqual( admission, { pending: "", stale: "", refused: "", granted: "Shop", cancelled: "Inventory" } );
		await page.evaluate( () => {
			const f = serviceFixture;
			f.event( { kind: "activate", id: "open-window:" } );
			f.state.entities = [ {
				gid: 2,
				refObjId: 7526,
				kind: "npc",
				name: "Guide Lipria",
				regionId: 27471,
				x: 1349.25,
				y: 82.7,
				z: 412.72
			} ];
			f.state.gameplay = {
				...f.state.gameplay,
				target: 2,
				targetCapabilities: 2,
				shop: undefined,
				npcConversation: {
					phase: "ready",
					gid: 2,
					dialogueRevision: 1,
					dialogue: {
						kind: 4,
						prompt: "SN_NPC_EU_ADVICE_QS",
						options: [ { choice: 5, symbol: "SN_QNO_EU_TUTORIAL_1" } ]
					}
				}
			};
			f.draw();
		} );
		await page.waitForFunction( () => {
			serviceFixture.draw();
			return serviceFixture.semantics?.controls.some( c => c.id === "npc-choice:5" ) &&
				!serviceFixture.stats().windowMissing.length;
		} );
		assert.equal(
			await page.locator( '[data-ui-id="shop-open"]' ).count(),
			0,
			"a non-merchant NPC has no invented target Shop button"
		);
		const scroll = await page.evaluate( () =>
			Object.fromEntries(
				serviceFixture.semantics.controls.filter( c => c.id.startsWith( "npc-scroll-" ) ).map(
					c => [ c.id, c.rect ]
				)
			)
		);
		assert.equal( scroll["npc-scroll-down"][1] - scroll["npc-scroll-up"][1], 352 );
		assert.equal( scroll["npc-scroll-thumb"][1] - scroll["npc-scroll-up"][1], 16 );
		await page.screenshot( { path: "temp/artifacts/native-service-windows/Lipria-conversation.png" } );
		assert.deepEqual( errors, [] );
	} finally {
		await page.context().tracing.stop( { path: "temp/artifacts/native-service-windows/trace.zip" } );
		await page.evaluate( () => globalThis.serviceFixture?.dispose() ).catch( () => {} );
		await browser.close();
	}
} );
