/*
===========================================================================
survival-firepit.test.mjs - GPU pixels prove a faint circle, not a bright quad.

Uses an isolated browser and the real character renderer; no game session.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test(
	"Fire Pit preserves ground pixels outside its circular boundary and most of its interior",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await holdProbeRuntime( page );
			await page.goto( CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( async () => {
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const { createProceduralEffect } = await import(
					"/src/engine/runtime/characters/resources/procedural-effects.ts"
				);
				const { survivalEffectActors } = await import(
					"/src/engine/runtime/characters/effects/survival-effects.ts"
				);
				const { identity } = await import( "/src/engine/foundation/rendering/world-math.ts" );
				const canvas = document.createElement( "canvas" ), copy = document.createElement( "canvas" );
				document.body.append( canvas );
				copy.width = copy.height = 384;
				const ctx = copy.getContext( "2d", { willReadFrequently: true } );
				const renderer = createRenderer( canvas );
				try {
					const material = {
						color: [ .18, .24, .14, 1 ],
						alphaCutoff: 0,
						blend: false,
						unlit: true,
						doubleSided: true,
						fogDisabled: true
					};
					renderer.setWorld( {
						id: "firepit-ground",
						originRegion: 257,
						warnings: [],
						groups: [ {
							id: "ground",
							center: [ 0, -1, 0 ],
							radius: 1500,
							material,
							geometry: {
								positions: Float32Array.of(
									-1000,
									-1,
									-1000,
									1000,
									-1,
									-1000,
									-1000,
									-1,
									1000,
									1000,
									-1,
									1000
								),
								normals: Float32Array.of( 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ),
								uvs: new Float32Array( 8 ),
								indices: Uint32Array.of( 0, 1, 2, 2, 1, 3 ),
								instances: identity(),
								transform: identity(),
								world: true,
								material
							}
						} ]
					} );
					renderer.setWorldCamera( {
						eye: [ 0, 450, 0 ],
						target: [ 0, 0, 0 ],
						up: [ 0, 0, 1 ],
						originRegion: 257,
						fov: Math.PI / 2,
						near: 1,
						far: 3000
					} );
					const actors = survivalEffectActors( {
						cue: 10,
						pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
						gids: Array.from( { length: 49 }, ( _, i ) => i + 1 )
					}, 8 );
					for ( const path of new Set( actors.map( a => a.model ) ) ) {
						const built = createProceduralEffect( path );
						if ( !built ) throw Error( "Missing Fire Pit resource" );
						renderer.setCharacterModel( path, built.model, built.images );
					}
					/*
				================
				sample
				================
				*/
					async function sample( rows ) {
						if ( !ctx ) throw Error( "No capture context" );
						renderer.setCharacterActors( rows );
						for ( let i = 0; i < 90; i++ ) {
							renderer.frame( { width: 384, height: 384 }, 0 );
							if ( renderer.error() ) throw Error( renderer.error() );
							await new Promise( requestAnimationFrame );
						}
						const bitmap = await createImageBitmap( canvas );
						ctx.drawImage( bitmap, 0, 0 );
						bitmap.close();
						return ctx.getImageData( 0, 0, 384, 384 ).data;
					}
					const baseline = await sample( [] ), fire = await sample( actors );
					let changed = 0, cornerChanges = 0;
					for ( let y = 0; y < 384; y++ ) {
						for ( let x = 0; x < 384; x++ ) {
							const i = (y * 384 + x) * 4;
							const difference = Math.max(
								...[ 0, 1, 2 ].map( c => Math.abs( fire[i + c] - baseline[i + c] ) )
							);
							if ( difference > 8 ) {
								changed++;
								if ( Math.abs( x - 192 ) > 102 && Math.abs( y - 192 ) > 102 ) cornerChanges++;
							}
						}
					}
					const png = copy.toDataURL();
					renderer.setWorldCamera( {
						eye: [ 0, 350, -500 ],
						target: [ 0, 0, 0 ],
						up: [ 0, 1, 0 ],
						originRegion: 257,
						fov: 1,
						near: 1,
						far: 3000
					} );
					await sample( actors );
					return { changed, cornerChanges, png, oblique: copy.toDataURL(), error: renderer.error() };
				} finally {
					renderer.dispose();
					canvas.remove();
				}
			} );
			await mkdir( "temp/artifacts/bugs/survival-firepit", { recursive: true } );
			await writeFile(
				"temp/artifacts/bugs/survival-firepit/current.png",
				Buffer.from( result.png.split( "," )[1], "base64" )
			);
			await writeFile(
				"temp/artifacts/bugs/survival-firepit/oblique.png",
				Buffer.from( result.oblique.split( "," )[1], "base64" )
			);
			console.log( JSON.stringify( { changed: result.changed, cornerChanges: result.cornerChanges } ) );
			assert.equal( result.error, null );
			assert.equal( result.cornerChanges, 0, "outside-circle square corners must remain unchanged" );
			assert.ok( result.changed > 40, "fire must remain visible" );
			assert.ok( result.changed < 384 * 384 * .12, "most of the ground must remain unobscured" );
		} finally {
			await browser.close();
		}
	}
);
