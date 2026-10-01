/*
===========================================================================

abnormal-tooltip.test.mjs - render native abnormal help through the real HUD

The production UI, asset worker and WebGPU renderer consume controlled
status snapshots. Wire ownership is tested separately; this verifies that
power, grade and remaining time reach the visible native tooltip.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const ARTIFACT_DIRECTORY = "temp/artifacts/abnormal-tooltip";
const FRAME_WAIT_MS = 50;
const ADMISSION_TIMEOUT_MS = 30000;

test( "local Burn power and Stun grade render through native status help", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } );
	const errors = [], reports = [];
	page.on( "pageerror", error => errors.push( String( error ) ) );
	try {
		await mkdir( ARTIFACT_DIRECTORY, { recursive: true } );
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const fixture = await page.evaluateHandle( async () => {
			const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" );
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { parseAbnormalSnapshot } = await import( "/src/engine/foundation/gameplay/abnormal-snapshot.ts" );
			const canvas = document.querySelector( "canvas" );
			if ( !canvas ) throw Error( "Fixture canvas missing" );
			canvas.style.width = "100vw";
			canvas.style.height = "100vh";
			const renderer = createRenderer( canvas ), assets = createAssets();
			let scene, semantics;
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
			return {
				/*
				================
				draw
				================
				*/
				draw( bit ) {
					const body = new Uint8Array( 9 ), view = new DataView( body.buffer );
					view.setUint32( 0, 2 ** bit, true );
					view.setUint16( 4, 300, true );
					view.setUint16( 6, 20, true );
					body[8] = bit === 3 ? 38 : 3;
					const state = {
						simulationTimeMs: 2000,
						frontend: { phase: "world" },
						session: { phase: "world", revision: 1, character: "Fixture" },
						gameplay: {
							localGid: 7,
							inventory: [],
							inventorySlotCount: 45,
							equipmentSlotCount: 13,
							vitals: [ { gid: 7, hp: 100, mp: 100, abnormal: 2 ** bit } ],
							abnormalRecords: parseAbnormalSnapshot( body, 1000 ),
							skillCatalog: [],
							casts: []
						},
						entities: [],
						width: innerWidth,
						height: innerHeight,
						worldReady: true
					};
					semantics = ui.step( state, performance.now() ) ?? semantics;
					ui.event( { kind: "hover", id: `abnormal:${bit}` } );
					semantics = ui.step( state, performance.now() ) ?? semantics;
					renderer.frame( { width: innerWidth, height: innerHeight } );
					return {
						pending: ui.stats().pending,
						error: renderer.error(),
						renderer: renderer.phase(),
						controls: semantics?.controls.map( control => control.id ) ?? [],
						corners: scene?.quads.filter( quad =>
							quad.texture.endsWith( "com_tooltip_corner.png" )
						).length ?? 0
					};
				},
				/*
				================
				dispose
				================
				*/
				dispose() {
					ui.dispose();
					assets.dispose();
					renderer.dispose();
				}
			};
		} );
		try {
			for ( const { name, bit } of [ { name: "burn-power", bit: 3 }, { name: "stun-grade", bit: 14 } ] ) {
				const deadline = Date.now() + ADMISSION_TIMEOUT_MS;
				let report;
				do {
					report = await fixture.evaluate( ( owner, selected ) => owner.draw( selected ), bit );
					if ( report.error ) throw Error( report.error );
					if ( report.pending === 0 && report.renderer === "running" && report.corners === 4 ) break;
					await page.waitForTimeout( FRAME_WAIT_MS );
				} while ( Date.now() < deadline );
				assert.equal( report.pending, 0, name );
				assert.equal( report.renderer, "running", name );
				assert.equal( report.corners, 4, name );
				assert.ok( report.controls.includes( `abnormal:${bit}` ) );
				await page.screenshot( { path: `${ARTIFACT_DIRECTORY}/${name}.png` } );
				reports.push( { name, ...report } );
			}
		} finally {
			await fixture.evaluate( owner => owner.dispose() );
			await fixture.dispose();
		}
		assert.deepEqual( errors, [] );
		await writeFile( `${ARTIFACT_DIRECTORY}/report.json`, JSON.stringify( { reports, errors }, null, 2 ) + "\n" );
	} finally {
		await browser.close();
	}
} );
