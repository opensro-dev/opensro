/*
===========================================================================

caravan-star-nameplate.test.mjs - the 5-star mark in a real browser

Port-only (#2958): in a real browser the roster frame decodes, the
overhead layout puts the five-star mark over a rostered transport with
the bag window's art and tint, the art itself loads through the asset
owner, and the production UI paints the transport's name board. The
five star quads themselves are pinned through the same production UI
in tests/runtime/caravan-star-nameplate.test.mjs; an ad-hoc probe page
never drains the HUD's warm-path queue, so quad residency is not
asserted here.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "a rostered 5-star transport carries its mark in the browser", { timeout: 120000 }, async () => {
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
		const proof = await page.evaluate( async () => {
			const { runtime } = await import(
				Array.from( document.scripts ).find( s => s.src && new URL( s.src ).pathname === "/src/bootstrap.ts" )
					.src
			);
			runtime.dispose();
			const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" );
			const { createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
			const { createCaravanStars, OP_CARAVAN_STARS } = await import(
				"/src/engine/runtime/simulation/worker/session/world/gameplay/caravan-stars/caravan-stars.ts"
			);
			const { overheadLayout } = await import( "/src/engine/foundation/ui/overhead-layout.ts" );
			const { defaultGameOptions } = await import( "/src/engine/foundation/gameplay/game-options.ts" );
			const { emptySocial } = await import( "/src/engine/foundation/gameplay/social.ts" );

			// The roster frame decodes to the transport's gid.
			const stars = createCaravanStars();
			const payload = new Uint8Array( 2 + 4 );
			const view = new DataView( payload.buffer );
			view.setUint16( 0, 1, true );
			view.setUint32( 2, 9, true );
			if ( !stars.receive( { opcode: OP_CARAVAN_STARS, payload } ) ) throw Error( "roster frame refused" );

			// The layout marks the rostered transport, and only it.
			const local = {
				gid: 1,
				refObjId: 1907,
				kind: "local-player",
				name: "Fixture",
				heading: 0,
				regionId: 0x271b,
				x: 900,
				y: 0,
				z: 900
			};
			const transport = {
				...local,
				gid: 9,
				refObjId: 3914,
				kind: "cos",
				name: "Camel",
				ownerGid: 1,
				ownerName: "Fixture",
				x: 903
			};
			const game = { localGid: 1, social: emptySocial( "Fixture" ), vitals: [], caravanStars: stars.gids() };
			const options = defaultGameOptions();
			const mark = overheadLayout( transport, local, game, options ).caravanMark;
			const unmarked = overheadLayout( transport, local, { ...game, caravanStars: [] }, options ).caravanMark;

			// The mark's art loads through the asset owner as an image.
			const assets = createAssets();
			const id = assets.request( new URL( mark.path, location.origin ).href, 4 * 1024 * 1024, "png" );
			let art = null;
			for ( let i = 0; i < 600 && !art; i++ ) {
				art = assets.take( id );
				if ( !art ) await new Promise( requestAnimationFrame );
			}

			// The production UI paints the transport's name board.
			const canvas = document.querySelector( "canvas" ), renderer = createRenderer( canvas ), scenes = [];
			const ui = createUi( {
				assets,
				commands: () => {},
				publish: s => {
					scenes.push( s );
					renderer.setUi( s );
				},
				texture: ( texId, image ) => renderer.setUiTexture( texId, image ),
				base: location.origin,
				apiBase: "http://fixture.invalid",
				clickSound: () => {},
				sound: () => {},
				audioPreference: () => {},
				saveBindings: () => {},
				saveVideo: () => {}
			} );
			const platform = createPlatform(
				canvas,
				document.querySelector( "output" ),
				() => {},
				() => {},
				() => {},
				ui.event,
				ui.blocks
			);
			const state = {
				frontend: null,
				session: { phase: "world", revision: 3, character: "Fixture" },
				gameplay: {
					revision: 1,
					localGid: 1,
					pose: { regionId: 0x271b, x: 900, y: 0, z: 900, angle: 0 },
					vitals: [ { gid: 1, hp: 100, mp: 50 } ],
					inventory: [],
					inventorySlotCount: 45,
					equipmentSlotCount: 13,
					inventoryPending: false,
					target: 0,
					casts: [],
					caravanStars: stars.gids()
				},
				entities: [ local, transport ],
				width: 1600,
				height: 900,
				worldReady: true
			};
			let board = 0;
			for ( let i = 0; i < 600 && board === 0; i++ ) {
				const semantic = ui.step( { ...state }, performance.now() );
				if ( semantic ) platform.presentUi( semantic );
				renderer.frame( { width: 1600, height: 900 } );
				board = (scenes.at( -1 )?.quads ?? []).filter( q => q.characterAnchor === 9 ).length;
				if ( !board ) await new Promise( requestAnimationFrame );
			}
			return {
				gids: stars.gids(),
				mark,
				unmarked,
				art: art && { kind: art.kind, width: art.image?.width, height: art.image?.height },
				board
			};
		} );
		assert.deepEqual( proof.gids, [ 9 ] );
		assert.ok( proof.mark, "the rostered transport is marked" );
		assert.match( proof.mark.path, /com_star\.png$/ );
		assert.deepEqual( proof.mark.tint, [ 250 / 255, 72 / 255, 72 / 255, 1 ] );
		assert.equal( proof.unmarked, null, "an empty roster marks nothing" );
		assert.deepEqual( proof.art, { kind: "image", width: 12, height: 12 }, "the star art loads in the browser" );
		assert.ok( proof.board > 0, "the transport's name board paints in the browser" );
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
