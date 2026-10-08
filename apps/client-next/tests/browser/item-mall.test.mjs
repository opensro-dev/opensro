/*
===========================================================================

item-mall.test.mjs - native mall assets, geometry and modal purchase controls

Uses the production UI, asset worker and renderer in an isolated document.
It proves rendering and semantic admission; server transactions have separate
wire and durable-authority tests.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "Item Mall uses native category and merchandise controls", { timeout: 180000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } );
	const directory = "temp/artifacts/item-mall";
	const errors = [], reports = [];
	page.on( "pageerror", error => errors.push( String( error ) ) );
	try {
		await mkdir( directory, { recursive: true } );
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const fixture = await page.evaluateHandle( async () => {
			const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" );
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createMallPreview } = await import( "/src/engine/runtime/characters/mall-preview.ts" );
			const { createCharacterResources } = await import(
				"/src/engine/runtime/characters/resources/resources.ts"
			);
			const roster = await (await fetch( "/assets/char/roster.json" )).json();
			const resource = roster.models.find( row => row.codename === "CHAR_CH_MAN_ADVENTURER" );
			if ( !resource ) throw Error( "Missing native mannequin fixture body" );
			const garment = Object.entries( roster.dress.equipment ).find( ( [, row] ) =>
				row.avatarSlot === 1 && row.bodies.CH_M && !roster.dress.avatarAuxiliary[Number( row.refObjId )]
			);
			if ( !garment ) throw Error( "Missing native mannequin garment" );
			const garmentId = Number( garment[0] );
			const presentation = await (await fetch( "/assets/data/missionPresentation.json" )).json();
			const garmentItem = presentation.itemsByRefObjId[garmentId];

			const canvas = document.querySelector( "canvas" );
			if ( !canvas ) throw new Error( "Missing browser fixture canvas" );
			canvas.style.width = "100vw";
			canvas.style.height = "100vh";
			canvas.style.display = "block";
			const renderer = createRenderer( canvas ), assets = createAssets(), commands = [];
			const mannequin = createMallPreview(),
				characterResources = createCharacterResources( assets, renderer, location.origin );
			let mannequinActors = [];

			let scene, semantics;
			const ui = createUi(
				assets,
				command => commands.push( command ),
				value => {
					scene = value;
					renderer.setUi( value );
				},
				( id, image ) => renderer.setUiTexture( id, image ),
				location.origin,
				"https://fixture.invalid"
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
						// refshoptab/refmappingshopwithtab: Consumables and Pets have
						// the shipped maximum of five tabs. Cover the full Consumables row.
						tabs: [
							...[ "SPECIAL", "SCROLL", "POTION", "COMMUNITY", "ETC" ].map( ( label, tab ) => ({
								shop: 2,
								tab,
								category: "MALL_CONSUME",
								label: "UIIT_STT_SILKMALL_" + label
							}) ),
							{
								shop: 1,
								tab: 1,
								category: "MALL_AVATAR",
								label: "UIIT_STT_SILKMALL_DRESS"
							}
						],
						offers: Array.from(
							{ length: 8 },
							( _, slot ) => ({
								group: 852,
								shop: 2,
								tab: 0,
								slot,
								packageId: slot + 1,
								name: "SN_ITEM_ETC_HP_POTION_01",
								description: "",
								icon: "item/etc/hp_potion_01.ddj",
								silk: 30,
								giftSilk: 0,
								currencyMask: 22,
								allowsPoints: true,
								purchaseLimit: 5,
								itemIds: [ 3630 ]
							})
						)
					}
				},
				entities: [ entity ],
				width: innerWidth,
				height: innerHeight,
				worldReady: true
			};
			state.gameplay.itemMall.offers.push( {
				...state.gameplay.itemMall.offers[0],
				shop: 1,
				tab: 1,
				slot: 0,
				packageId: 100,
				name: "SN_" + garmentItem.codename,
				icon: garmentItem.iconDdjPath.replace( /^icon\//, "" ),
				itemIds: [ garmentId ]
			} );
			return {
				/*
    ================
    draw
    ================
    */
				draw() {
					state.width = innerWidth;
					state.height = innerHeight;
					const seconds = performance.now() / 1000;
					characterResources.begin( seconds );
					characterResources.poll();
					mannequin.request( ui.mallPreview() );
					mannequinActors = [ ...mannequin.step(
						{
							resource,
							dress: roster.dress,
							equipment: [],
							avatars: [],
							seconds
						},
						characterResources,
						renderer
					) ];
					const portraits = mannequinActors.length ?
						[ ...mannequinActors, { ...mannequinActors[0], gid: 1 } ] :
						[];
					renderer.setCharacterActors( [], portraits );
					characterResources.retainWanted( portraits.map( actor => actor.model ) );
					ui.mallPreviewState( mannequin.state() );
					if ( characterResources.error() ) throw Error( characterResources.error() );
					semantics = ui.step( state, performance.now() ) ?? semantics;
					renderer.frame( { width: innerWidth, height: innerHeight } );
					return {
						pending: ui.stats().pending,
						uiError: ui.stats().error,
						invalidQuads: scene?.quads.filter( q =>
							!q.rect.every( Number.isFinite ) || q.rect[2] < 0 || q.rect[3] < 0
						).length ?? 0,
						mannequin: mannequinActors[0]?.model,
						garment: garment[1].bodies.CH_M.glb,
						renderer: renderer.phase(),
						error: renderer.error(),
						textures: scene?.quads.map( q =>
							q.texture
						),
						controls: semantics?.controls.map( c => ({
							id: c.id,
							rect: c.rect,
							disabled: c.disabled,
							value: c.value
						}) ),
						commands
					};
				},
				/*
    ================
    activate
    ================
    */
				activate( id ) {
					ui.event( { kind: "activate", id } );
				},
				/*
    ================
    dispose
    ================
    */
				dispose() {
					ui.dispose();
					characterResources.dispose();
					assets.dispose();
					renderer.dispose();
				}
			};
		} );
		try {
			/*
			================
			compactFrame
			Wait for real assets and published controls before inspecting the GPU frame.
			================
			*/
			async function compactFrame( requiredId, enabled = false ) {
				const deadline = Date.now() + 25000;
				let row;
				do {
					row = await fixture.evaluate( f => f.draw() );
					assert.ok( !row.error, row.error ?? "Renderer error" );
					assert.ok( !row.uiError, row.uiError ?? "UI error" );
					assert.equal( row.invalidQuads, 0 );
					if (
						!row.pending && row.renderer === "running" &&
						row.controls?.some( c => c.id === requiredId && (!enabled || !c.disabled) )
					) {
						return row;
					}
					await page.waitForTimeout( 50 );
				} while ( Date.now() < deadline );
				assert.fail( `Compact Mall did not admit ${requiredId}: ${JSON.stringify( row )}` );
			}
			for (
				const [width, height, visibleProducts] of [ [ 320, 568, 2 ], [ 375, 375, 1 ], [ 375, 667, 3 ], [
					667,
					375,
					2
				] ]
			) {
				await page.setViewportSize( { width, height } );
				await fixture.evaluate( f => f.draw() );
				await fixture.evaluate( f => f.activate( "item-mall" ) );
				await compactFrame( "item-mall-category:1" );
				await fixture.evaluate( f => f.activate( "item-mall-category:1" ) );
				const catalogue = await compactFrame( "item-mall-buy:0" );
				assert.equal( catalogue.controls.filter( c => c.id.startsWith( "item-mall-tab:" ) ).length, 5 );
				assert.equal(
					catalogue.controls.filter( c => c.id.startsWith( "item-mall-buy:" ) ).length,
					visibleProducts
				);
				await page.screenshot( { path: `${directory}/compact-${width}x${height}-catalogue.png` } );
				assert.ok( catalogue.controls.some( c => c.id === "item-mall-page:1" && !c.disabled ) );
				await fixture.evaluate( f => f.activate( "item-mall-page:1" ) );
				const second = await compactFrame( "item-mall-page:0" );
				for ( const row of [ catalogue, second ] ) {
					for ( const control of row.controls ) {
						const [x, y, w, h] = control.rect;
						assert.ok(
							w > 0 && h > 0 && x >= 0 && y >= 0 && x + w <= width && y + h <= height,
							`${control.id} fits ${width}x${height}`
						);
						if ( /^item-mall-(buy|wear|reserve):/.test( control.id ) ) {
							assert.ok( y + h <= height - 48, `${control.id} must not overlap pagination` );
						}
					}
				}
				assert.deepEqual(
					second.controls.filter( c => c.id.startsWith( "item-mall-offer:" ) ).map( c => c.id ),
					Array.from(
						{ length: visibleProducts },
						( _, index ) => `item-mall-offer:${visibleProducts + index + 1}`
					)
				);
				assert.equal(
					second.controls.filter( c => c.id.startsWith( "item-mall-buy:" ) ).length,
					visibleProducts
				);
				await fixture.evaluate( ( f, id ) => f.activate( id ), `item-mall-buy:${visibleProducts - 1}` );
				const confirmation = await compactFrame( "item-mall-purchase" );
				assert.equal( confirmation.controls.find( c => c.id === "item-mall-purchase" )?.disabled, false );
				for ( const id of [ "item-mall-purchase", "item-mall-cancel", "item-mall-close" ] ) {
					const control = confirmation.controls.find( c => c.id === id );
					assert.ok( control, id );
					const [x, y, w, h] = control.rect;
					assert.ok( x >= 0 && y >= 0 && x + w <= width && y + h <= height, `${id} fits ${width}x${height}` );
				}
				assert.ok( confirmation.controls.find( c => c.id === "item-mall-close" ).rect[3] >= 40 );
				await page.screenshot( { path: `${directory}/compact-${width}x${height}-confirmation.png` } );
				await fixture.evaluate( f => f.activate( "item-mall-points" ) );
				const compactPoints = await compactFrame( "item-mall-point-value" );
				assert.ok( compactPoints.controls.some( c => c.id === "item-mall-points-close" ) );
				await fixture.evaluate( f => f.activate( "item-mall-points-close" ) );
				await compactFrame( "item-mall-purchase" );
				await fixture.evaluate( f => f.activate( "item-mall-cancel" ) );
				const cancelled = await compactFrame( "item-mall-buy:0" );
				assert.ok( !cancelled.controls.some( c => c.id === "item-mall-purchase" ) );
				await fixture.evaluate( f => f.activate( "item-mall-reserve:0" ) );
				const compactReservation = await compactFrame( "item-mall-question-confirm" );
				await fixture.evaluate( f => f.activate( "item-mall-question-cancel" ) );
				const reservationCancelled = await compactFrame( "item-mall-buy:0" );
				for (
					const row of [
						catalogue,
						second,
						confirmation,
						compactPoints,
						cancelled,
						compactReservation,
						reservationCancelled
					]
				) {
					assert.equal(
						row.commands.filter( c => c.kind === "gameplay" && c.command?.kind === "mall-buy" ).length,
						0
					);
				}
				reports.push( { name: `compact-${width}x${height}`, catalogue, second, confirmation, cancelled } );
				await fixture.evaluate( f => f.activate( "item-mall-home" ) );
				await fixture.evaluate( f => f.draw() );
				await fixture.evaluate( f => f.activate( "item-mall-close" ) );
				await fixture.evaluate( f => f.draw() );
			}
			// A transient short viewport need not expose the whole Mall, but must
			// never submit negative geometry or retain a failed UI frame.
			await page.setViewportSize( { width: 667, height: 200 } );
			await fixture.evaluate( f => f.draw() );
			await fixture.evaluate( f => f.activate( "item-mall" ) );
			await compactFrame( "item-mall-category:1" );
			await fixture.evaluate( f => f.activate( "item-mall-category:1" ) );
			await compactFrame( "item-mall-buy:0" );
			await fixture.evaluate( f => f.activate( "item-mall-view:preview" ) );
			await compactFrame( "item-mall-root:4" );
			await fixture.evaluate( f => f.activate( "item-mall-home" ) );
			await fixture.evaluate( f => f.draw() );
			await fixture.evaluate( f => f.activate( "item-mall-close" ) );
			await fixture.evaluate( f => f.draw() );
			await page.setViewportSize( { width: 1024, height: 768 } );
			for (
				const [name, action] of [ [ "home", "item-mall" ], [ "catalogue", "item-mall-category:1" ], [
					"confirmation",
					"item-mall-buy:0"
				] ]
			) {
				if ( name === "home" ) await fixture.evaluate( f => f.draw() );
				await fixture.evaluate( ( f, id ) => f.activate( id ), action );
				let row;
				const deadline = Date.now() + 25000;
				do {
					row = await fixture.evaluate( f => f.draw() );
					if ( row.error ) throw Error( row.error );
					if (
						row.pending === 0 && row.renderer === "running" && !!row.mannequin &&
						row.controls?.some( c =>
							c.id === (name === "confirmation" ? "item-mall-purchase" : "item-mall-close")
						)
					) break;
					await page.waitForTimeout( 50 );
				} while ( Date.now() < deadline );
				assert.equal( row.pending, 0, name );
				assert.equal( row.renderer, "running", name );
				assert.ok( row.controls.every( c => c.id.startsWith( "item-mall" ) ), name + " modal leaked controls" );
				if ( name === "catalogue" ) {
					assert.equal( row.controls.filter( c => c.id.startsWith( "item-mall-buy:" ) ).length, 6 );
					assert.equal( row.controls.filter( c => c.id.startsWith( "item-mall-page:" ) ).length, 2 );
					const tab = row.controls.find( c => c.id === "item-mall-tab:0" );
					assert.deepEqual( tab.rect, [ 250, 127, 60, 24 ] );
				}
				if ( name === "confirmation" ) {
					assert.equal( row.controls.find( c => c.id === "item-mall-purchase" )?.disabled, false );
				}
				await page.screenshot( { path: `${directory}/${name}.png` } );
				reports.push( { name, ...row } );
			}
			await fixture.evaluate( f => f.activate( "item-mall-points" ) );
			const points = await fixture.evaluate( f => f.draw() );
			assert.deepEqual( points.controls.map( c => c.id ), [ "item-mall-point-value", "item-mall-points-apply" ] );
			assert.equal( points.controls[0].value, "0" );
			await page.screenshot( { path: `${directory}/points.png` } );
			reports.push( { name: "points", ...points } );
			await fixture.evaluate( f => f.activate( "item-mall-points-apply" ) );
			await fixture.evaluate( f => f.draw() );
			await fixture.evaluate( f => f.activate( "item-mall-cancel" ) );
			await fixture.evaluate( f => f.draw() );
			await fixture.evaluate( f => f.activate( "item-mall-category:2" ) );
			const unworn = await compactFrame( "item-mall-wear:0", true );
			assert.equal( unworn.controls.find( c => c.id === "item-mall-wear:0" )?.disabled, false );
			await fixture.evaluate( f => f.activate( "item-mall-wear:0" ) );
			let worn;
			const previewDeadline = Date.now() + 45000;
			do {
				worn = await fixture.evaluate( f => f.draw() );
				if ( worn.mannequin?.includes( worn.garment ) && !worn.pending ) break;
				await page.waitForTimeout( 50 );
			} while ( Date.now() < previewDeadline );
			assert.ok( worn.mannequin?.includes( worn.garment ), "try-on must assemble the actual published garment" );
			assert.notEqual( worn.mannequin, unworn.mannequin );
			await page.screenshot( { path: `${directory}/mannequin.png` } );
			await fixture.evaluate( f => f.activate( "item-mall-root:4" ) );
			const wornBuy = await fixture.evaluate( f => f.draw() );
			assert.equal( wornBuy.controls.find( c => c.id === "item-mall-question-confirm" )?.disabled, false );
			await page.screenshot( { path: `${directory}/worn-purchase.png` } );
			await fixture.evaluate( f => f.activate( "item-mall-question-cancel" ) );
			await fixture.evaluate( f => f.draw() );
			await fixture.evaluate( f => f.activate( "item-mall-root:5" ) );
			const restored = await fixture.evaluate( f => f.draw() );
			assert.equal( restored.mannequin, unworn.mannequin );
			reports.push( { name: "mannequin", ...worn }, { name: "worn-purchase", ...wornBuy } );
			await fixture.evaluate( f => f.activate( "item-mall-category:1" ) );
			await fixture.evaluate( f => f.draw() );
			await fixture.evaluate( f => f.activate( "item-mall-page:1" ) );
			const secondPage = await fixture.evaluate( f => f.draw() );
			assert.equal( secondPage.controls.filter( c => c.id.startsWith( "item-mall-buy:" ) ).length, 2 );
			await fixture.evaluate( f => f.activate( "item-mall-reserve:0" ) );
			const reservation = await fixture.evaluate( f => f.draw() );
			assert.deepEqual( reservation.controls.map( c => c.id ), [
				"item-mall-question-confirm",
				"item-mall-question-cancel"
			] );
			assert.deepEqual( reservation.controls[0].rect, [ 434, 449, 76, 22 ] );
			await page.screenshot( { path: `${directory}/reservation.png` } );
			await fixture.evaluate( f => f.activate( "item-mall-question-confirm" ) );
			await fixture.evaluate( f => f.draw() );
			await fixture.evaluate( f => f.activate( "item-mall-category:7" ) );
			const reserved = await fixture.evaluate( f => f.draw() );
			assert.equal( reserved.controls.filter( c => c.id.startsWith( "item-mall-buy:" ) ).length, 1 );
			assert.equal( reserved.controls.find( c => c.id === "item-mall-buy-all" )?.disabled, false );
			await fixture.evaluate( f => f.activate( "item-mall-buy-all" ) );
			const batch = await fixture.evaluate( f => f.draw() );
			assert.equal( batch.controls.find( c => c.id === "item-mall-question-confirm" )?.disabled, false );
			await page.screenshot( { path: `${directory}/reserved-purchase.png` } );
			await fixture.evaluate( f => f.activate( "item-mall-points" ) );
			const batchPoints = await fixture.evaluate( f => f.draw() );
			assert.ok( batchPoints.controls.some( c => c.id === "item-mall-point-value" ) );
			await fixture.evaluate( f => f.activate( "item-mall-points-apply" ) );
			await fixture.evaluate( f => f.draw() );
			await fixture.evaluate( f => f.activate( "item-mall-question-confirm" ) );
			const sending = await fixture.evaluate( f => f.draw() );
			assert.equal(
				sending.commands.filter( command =>
					command.kind === "gameplay" && command.command?.kind === "mall-buy"
				).length,
				1
			);
			await fixture.evaluate( f => f.draw() );
			reports.push( { name: "reservation", ...reservation }, { name: "reserved-purchase", ...batch } );
		} finally {
			await fixture.evaluate( f => f.dispose() );
			await fixture.dispose();
		}
		assert.deepEqual( errors, [] );
		await writeFile( `${directory}/report.json`, JSON.stringify( { reports, errors }, null, 2 ) + "\n" );
	} finally {
		await browser.close();
	}
} );
