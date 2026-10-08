/*
===========================================================================

hidpi-hud.test.mjs - Retina inventory rendering and real pointer admission

The production UI, asset worker, platform and renderer share a logical
viewport while the world canvas retains the full physical resolution.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const ARTIFACT_DIRECTORY = "temp/artifacts/hidpi-hud";
const ADMISSION_TIMEOUT_MS = 30000;
const FRAME_WAIT_MS = 50;

test( "Retina HUD keeps native logical size and blocks inventory clicks", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser( {
		viewport: { width: 1512, height: 982 },
		deviceScaleFactor: 2
	} );
	const errors = [];
	page.on( "pageerror", error => errors.push( String( error ) ) );
	try {
		await mkdir( ARTIFACT_DIRECTORY, { recursive: true } );
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const fixture = await page.evaluateHandle( async () => {
			const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" );
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
			const canvas = document.querySelector( "canvas" );
			if ( !canvas ) throw Error( "Missing fixture canvas" );
			const style = document.createElement( "style" );
			style.textContent = "body{overflow:hidden}canvas{display:block;width:100vw;height:100vh}";
			document.head.append( style );
			const renderer = createRenderer( canvas ), assets = createAssets();
			let scene, semantics;
			const worldClicks = [];
			const ui = createUi(
				assets,
				() => {},
				value => {
					scene = value;
					renderer.setUi( value );
				},
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
				session: { phase: "world", revision: 1, character: "Retina fixture" },
				gameplay: {
					localGid: 7,
					inventory: [],
					inventorySlotCount: 45,
					equipmentSlotCount: 13,
					vitals: [],
					casts: [],
					skillCatalog: []
				},
				entities: [],
				worldReady: true
			};
			ui.step( { ...state, ...platform.readUiViewport() }, performance.now() );
			ui.event( { kind: "activate", id: "open-window:Inventory" } );
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
					const slot = semantics?.controls.find( control => control.id === "slot:13" );
					return {
						pending: ui.stats().pending,
						error: renderer.error(),
						phase: renderer.phase(),
						slot: slot?.rect,
						logical: scene && [ scene.width, scene.height ],
						physical: [ canvas.width, canvas.height ],
						clicks: worldClicks.length
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
		try {
			const deadline = Date.now() + ADMISSION_TIMEOUT_MS;
			let report;
			do {
				report = await fixture.evaluate( owner => owner.draw() );
				if ( report.error ) throw Error( report.error );
				if ( report.pending === 0 && report.phase === "running" && report.slot ) break;
				await page.waitForTimeout( FRAME_WAIT_MS );
			} while ( Date.now() < deadline );
			assert.equal( report.pending, 0 );
			assert.equal( report.phase, "running" );
			assert.deepEqual( report.logical, [ 1512, 982 ] );
			assert.deepEqual( report.physical, [ 3024, 1964 ] );
			assert.ok( report.slot );
			const slot = await page.locator( '[data-ui-id="slot:13"]' ).boundingBox();
			assert.ok( slot );
			for ( const [index, value] of [ slot.x, slot.y, slot.width, slot.height ].entries() ) {
				assert.ok( Math.abs( value - report.slot[index] ) < .1 );
			}
			const inside = [ slot.x + slot.width / 2, slot.y + slot.height / 2 ];
			assert.equal( await fixture.evaluate( ( owner, point ) => owner.blocked( point ), inside ), true );
			await page.mouse.click( inside[0], inside[1] );
			assert.equal( (await fixture.evaluate( owner => owner.draw() )).clicks, 0 );
			const outside = [ 756, 350 ];
			assert.equal( await fixture.evaluate( ( owner, point ) => owner.blocked( point ), outside ), false );
			await page.mouse.click( outside[0], outside[1] );
			assert.equal( (await fixture.evaluate( owner => owner.draw() )).clicks, 1 );
			await page.screenshot( { path: `${ARTIFACT_DIRECTORY}/retina-inventory.png` } );
			await writeFile( `${ARTIFACT_DIRECTORY}/report.json`, JSON.stringify( report, null, 2 ) );
			// BR-261008-2000-59AA reports the canvas backing size, not CSS pixels.
			await page.setViewportSize( { width: 2560, height: 992 } );
			await page.evaluate( async () => {
				await new Promise( requestAnimationFrame );
				await new Promise( requestAnimationFrame );
			} );
			const ultrawide = await fixture.evaluate( owner => owner.draw() );
			assert.deepEqual( ultrawide.logical, [ 2560, 992 ] );
			assert.deepEqual( ultrawide.physical, [ 5120, 1984 ] );
			const wideSlot = await page.locator( '[data-ui-id="slot:13"]' ).boundingBox();
			assert.ok( wideSlot );
			assert.equal( wideSlot.width, slot.width, "Ultrawide must not shrink the inventory controls" );
			assert.equal( wideSlot.height, slot.height );
			await page.screenshot( { path: `${ARTIFACT_DIRECTORY}/ultrawide-inventory.png` } );
		} finally {
			await fixture.evaluate( owner => owner.dispose() );
			await fixture.dispose();
		}
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
