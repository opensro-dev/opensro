/*
===========================================================================

scenery-particles.test.mjs - published scenery effects on the GPU

Checks animation and native fog exclusion. Inverse-source-colour smoke must
not become bright quads when the surrounding world enters its fog band.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test(
	"published torch, waterfall splash and smoke programs produce animated GPU pixels",
	{ timeout: 120000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await page.goto( CLIENT_NEXT_BASE_URL );
			const results = await page.evaluate( async () => {
				const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const { assetRequestBudget } = await import( "/src/engine/foundation/assets/asset-budget.ts" );
				const { createSceneryEmission } = await import(
					"/src/engine/foundation/animation/scenery-emission.ts"
				);
				const assets = createAssets(), canvas = document.createElement( "canvas" );
				document.body.append( canvas );
				const renderer = createRenderer( canvas ), owner = createSceneryEmission();
				const capture = document.createElement( "canvas" );
				capture.width = capture.height = 192;
				const ctx = capture.getContext( "2d" ), results = [];
				/*
				================
				sample
				================
				*/
				const sample = async ( t, actors ) => {
					renderer.setCharacterActors( actors );
					renderer.frame( { width: 192, height: 192 }, t );
					await new Promise( requestAnimationFrame );
					renderer.frame( { width: 192, height: 192 }, t );
					if ( renderer.error() ) throw Error( renderer.error() );
					const image = await createImageBitmap( canvas );
					ctx.drawImage( image, 0, 0 );
					image.close();
					return { pixels: ctx.getImageData( 0, 0, 192, 192 ).data, png: capture.toDataURL() };
				};
				try {
					let deadline = performance.now() + 20000;
					while ( renderer.phase() === "starting" ) {
						if ( performance.now() > deadline ) throw Error( "GPU initialization timeout" );
						await new Promise( requestAnimationFrame );
					}
					renderer.setWorld( {
						id: "scenery-test",
						originRegion: 257,
						warnings: [],
						groups: [],
						environment: {
							startTimeOfDay: .5,
							ratePerSecond: 0,
							tracks: {
								color0x2b4: [ { t: 0, r: .25, g: .36, b: .64 } ],
								scalar0x2e8: [ { t: 0, value: .2 } ],
								scalar0x314: [ { t: 0, value: .4 } ]
							}
						}
					} );
					renderer.setWorldCamera( {
						originRegion: 257,
						eye: [ 0, 15, -100 ],
						target: [ 0, 15, 0 ],
						fov: 1,
						near: .1,
						far: 1000
					} );
					const empty = await sample( 0, [] );
					for (
						const path of [
							"map/frame2.efp",
							"map/pajang.efp",
							"map/pokpopapyun.efp",
							"map/minga_smoke_b.efp",
							"map/minga_smoke_s.efp"
						]
					) {
						const model = "/assets/effects/programs.json#" + encodeURIComponent( path ),
							id = assets.request(
								new URL( model, location.origin ).href,
								assetRequestBudget( "effect" ),
								"effect"
							);
						deadline = performance.now() + 30000;
						let result;
						while ( !(result = assets.take( id )) ) {
							if ( performance.now() > deadline ) throw Error( "Scenery decode timeout " + path );
							await new Promise( requestAnimationFrame );
						}
						if ( result.kind !== "character" ) throw Error( path + ": " + result.error );
						renderer.setCharacterModel( model, result.model, result.images );
						owner.reset();
						const scene = {
							night: true,
							emitters: [ {
								id: path,
								placement: path,
								model,
								pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
								basis: [ 1, 0, 0, 0, 1, 0, 0, 0, 1 ],
								nightOnly: false,
								renderPriority: 0
							} ]
						};
						let early, late;
						for ( let i = 0; i <= 40; i++ ) {
							const frame = await sample( i / 20, owner.step( scene, i / 20, () => true, 1 ) );
							if ( i === 20 ) early = frame;
							late = frame;
						}
						renderer.setWorld( { id: "reset-environment", originRegion: 257, warnings: [], groups: [] } );
						// Reset environment smoothing without retiring the particle streams.
						await sample( 2, owner.step( scene, 2, () => true, 1 ) );
						renderer.setWorld( {
							id: "fogged-smoke",
							originRegion: 257,
							warnings: [],
							groups: [],
							environment: {
								startTimeOfDay: .5,
								ratePerSecond: 0,
								tracks: {
									color0x2b4: [ { t: 0, r: .25, g: .36, b: .64 } ],
									scalar0x2e8: [ { t: 0, value: .01 } ],
									scalar0x314: [ { t: 0, value: .02 } ]
								}
							}
						} );
						const fogged = await sample( 2, owner.step( scene, 2, () => true, 1 ) );
						/*
						================
						diff
						================
						*/
						const diff = ( a, b ) => a.pixels.reduce( ( n, v, i ) => n + Number( v !== b.pixels[i] ), 0 );
						results.push( {
							path,
							changed: diff( late, empty ),
							animated: diff( early, late ),
							fogChanged: diff( late, fogged ),
							png: fogged.png
						} );
						renderer.setCharacterActors( [] );
						renderer.setWorld( { id: "reset-environment", originRegion: 257, warnings: [], groups: [] } );
						await sample( 0, [] );
						renderer.setWorld( {
							id: "scenery-test",
							originRegion: 257,
							warnings: [],
							groups: [],
							environment: {
								startTimeOfDay: .5,
								ratePerSecond: 0,
								tracks: {
									color0x2b4: [ { t: 0, r: .25, g: .36, b: .64 } ],
									scalar0x2e8: [ { t: 0, value: .2 } ],
									scalar0x314: [ { t: 0, value: .4 } ]
								}
							}
						} );
					}
					return results;
				} finally {
					renderer.dispose();
					assets.dispose();
					canvas.remove();
				}
			} );
			await mkdir( "temp/artifacts/bugs/scenery-emitters", { recursive: true } );
			for ( const row of results ) {
				await writeFile(
					"temp/artifacts/bugs/scenery-emitters/" + row.path.split( "/" ).at( -1 ) + ".png",
					Buffer.from( row.png.split( "," )[1], "base64" )
				);
				delete row.png;
			}
			await writeFile( "temp/artifacts/bugs/scenery-emitters/result.json", JSON.stringify( results, null, 2 ) );
			for ( const row of results ) {
				assert.ok( row.changed > 100, JSON.stringify( row ) );
				assert.ok( row.animated > 10, JSON.stringify( row ) );
				assert.equal( row.fogChanged, 0, JSON.stringify( row ) );
			}
		} finally {
			await browser.close();
		}
	}
);
