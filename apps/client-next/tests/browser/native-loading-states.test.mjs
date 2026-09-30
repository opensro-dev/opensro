/*
===========================================================================

native-loading-states.test.mjs - render shared transition states with game assets

An isolated document runs the production UI, asset worker and WebGPU renderer.
The fixture controls snapshots; it does not replace live session responses.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "native loading and failure screens cover reload, roster and frontend failures", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } );
	const directory = "temp/artifacts/native-loading-states";
	const errors = [], rows = [];
	page.on( "pageerror", error => errors.push( String( error ) ) );
	try {
		await mkdir( directory, { recursive: true } );
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const fixture = await page.evaluateHandle( async () => {
			const uiPath = "/src/engine/runtime/ui/ui.ts";
			const assetsPath = "/src/engine/runtime/assets/assets.ts";
			const rendererPath = "/src/engine/runtime/renderer/renderer.ts";
			const { createUi } = await import( uiPath );
			const { createAssets } = await import( assetsPath );
			const { createRenderer } = await import( rendererPath );
			const canvas = document.querySelector( "canvas" );
			if ( !canvas ) throw Error( "Fixture canvas missing" );
			canvas.style.width = "100vw";
			canvas.style.height = "100vh";
			canvas.style.display = "block";
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
				ui,
				renderer,
				assets,
				/*
    ================
    draw
    ================
    */
				draw( phase, failed = false ) {
					const state = {
						session: { phase, revision: 1, character: "Fixture", error: "Previous operation failed" },
						gameplay: null,
						entities: [],
						width: innerWidth,
						height: innerHeight,
						worldReady: false,
						loadingProgress: .4,
						...(failed ?
							{
								frontend: {
									phase: "failed",
									generation: 1,
									elapsed: 0,
									alpha: 1,
									logoAlpha: 0,
									error: "Scene resource unavailable"
								}
							} :
							{})
					};
					semantics = ui.step( state, performance.now() ) ?? semantics;
					renderer.frame( { width: innerWidth, height: innerHeight } );
					return {
						pending: ui.stats().pending,
						renderer: renderer.phase(),
						error: renderer.error(),
						textures: scene?.quads.map( q => q.texture ),
						controls: semantics?.controls.map( c => c.id ),
						loading: semantics?.loading,
						loadingError: semantics?.loadingError
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
			const cases = /** @type {const} */ ([
				[ "reload", "world", false ],
				[ "connecting", "connecting", false ],
				[ "roster", "loading-roster", false ],
				[ "frontend-failure", "character-select", true ]
			]);
			for ( const [name, phase, failed] of cases ) {
				let row;
				const deadline = Date.now() + 25000;
				do {
					row = await fixture.evaluate( ( f, input ) => f.draw( input.phase, input.failed ), {
						phase,
						failed
					} );
					if ( row.error ) throw Error( row.error );
					if ( row.pending === 0 && row.renderer === "running" ) break;
					await page.waitForTimeout( 50 );
				} while ( Date.now() < deadline );
				assert.equal( row.pending, 0, name );
				assert.equal( row.renderer, "running", name );
				assert.ok( !row.controls.includes( "logout" ) );
				if ( failed ) {
					assert.equal( row.loading, false, "loaded native message frame replaces the boot overlay" );
					assert.equal( row.loadingError, "Scene resource unavailable" );
					assert.ok( row.textures.some( path => path.includes( "/messagebox/" ) ) );
				} else {
					assert.ok( row.textures.some( path => path.endsWith( "gauge_loading.png" ) ) );
					assert.equal( row.controls.length, 0 );
				}
				await page.screenshot( { path: `${directory}/${name}.png` } );
				rows.push( { name, ...row } );
			}
		} finally {
			await fixture.evaluate( f => f.dispose() );
			await fixture.dispose();
		}
		assert.deepEqual( errors, [] );
		await writeFile( `${directory}/report.json`, JSON.stringify( { rows, errors }, null, 2 ) + "\n" );
	} finally {
		await browser.close();
	}
} );
