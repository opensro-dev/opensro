/*
===========================================================================

world-fade-composition.test.mjs - distance fades over finished terrain

An object fading in by distance is drawn over the terrain behind it at
0, half and full alpha, with and without a lightmap pass, and whatever
order its asset names sort in: the fade composes over the finished
ground, never over a partial one.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "distance fades composite over finished terrain, independent of asset names", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", e => errors.push( e.message ) );
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
			const quad = ( id, z, color ) => ({
				id,
				center: [ 0, 0, z ],
				radius: 300,
				material: { color, alphaCutoff: 0, blend: false, doubleSided: true, unlit: true },
				geometry: {
					world: true,
					positions: new Float32Array( [ -200, -200, z, 200, -200, z, 200, 200, z, -200, 200, z ] ),
					normals: new Float32Array( 12 ).fill( 1 ),
					uvs: new Float32Array( [ 0, 0, 1, 0, 1, 1, 0, 1 ] ),
					indices: new Uint32Array( [ 0, 1, 2, 0, 2, 3 ] ),
					instances: identity(),
					transform: identity()
				}
			});
			const environment = {
				startTimeOfDay: .5,
				ratePerSecond: 0,
				tracks: {
					color0x1c0: [ { t: 0, r: 0, g: 0, b: 0 } ],
					zenith: [ { t: 0, r: 0, g: 0, b: 1 } ],
					horizon: [ { t: 0, r: 0, g: 0, b: 1 } ],
					scalar0x2e8: [ { t: 0, value: 1 } ],
					scalar0x314: [ { t: 0, value: 1 } ]
				}
			};
			const output = document.createElement( "canvas" );
			output.width = output.height = 64;
			const ctx = output.getContext( "2d" );
			const sample = async seconds => {
				renderer.frame( { width: 64, height: 64 }, seconds );
				await new Promise( requestAnimationFrame );
				renderer.frame( { width: 64, height: 64 }, seconds );
				if ( renderer.phase() !== "running" ) throw new Error( renderer.error() );
				const image = await createImageBitmap( canvas );
				ctx.drawImage( image, 0, 0 );
				image.close();
				return [ ...ctx.getImageData( 32, 32, 1, 1 ).data ];
			};
			try {
				renderer.setWorldCamera( {
					eye: [ 0, 0, 400 ],
					target: [ 0, 0, 0 ],
					fov: Math.PI / 3,
					near: 1,
					far: 2000
				} );
				const deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" && performance.now() < deadline ) {
					await new Promise( requestAnimationFrame );
				}
				const cases = [];
				for ( const reversed of [ false, true ] ) {
					for ( const lightmap of [ false, true ] ) {
						const ground = quad( reversed ? "a-ground" : "z-ground", -1, [ 0, 1, 0, 1 ] );
						ground.material.terrain = true;
						const object = quad( reversed ? "z-object" : "a-object", 0, [ 1, 0, 0, 1 ] );
						object.material.objectFade = true;
						object.instanceRadius = 300;
						// Each case is a new object: a placement seen in the previous
						// scene of the same world keeps its fade (a region crossing must
						// not fade the buildings it retains in again).
						object.visibility = [ {
							id: `placement:${reversed}:${lightmap}`,
							radius: 0,
							range: 480,
							cellRadius: 7,
							cells: [ [ 0, 0 ] ]
						} ];
						const groups = [ object, ground ];
						if ( lightmap ) {
							const light = quad( "lightmap", -1, [ 1, 1, 1, 1 ] );
							light.material = { ...light.material, lightmap: true, blend: true, texture: "light" };
							groups.push( light );
						}
						renderer.setWorld( {
							id: `composition:${reversed}:${lightmap}`,
							originRegion: 0,
							warnings: [],
							environment,
							groups
						} );
						if ( lightmap ) {
							renderer.setWorldTexture(
								"light",
								await createImageBitmap(
									new ImageData( Uint8ClampedArray.of( 128, 128, 128, 255 ), 1, 1 )
								)
							);
						}
						const hidden = await sample( 0 ), half = await sample( .25 ), full = await sample( .5 );
						cases.push( { reversed, lightmap, hidden, half, full } );
					}
				}
				return { cases, error: renderer.error() };
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		await mkdir( "temp/artifacts/bugs", { recursive: true } );
		await writeFile( "temp/artifacts/bugs/fade-composition.json", JSON.stringify( { result, errors }, null, 2 ) );
		for ( const row of result.cases ) {
			const ground = row.lightmap ? 128 : 255;
			for (
				const [name, expected] of [ [ "hidden", [ 0, ground, 0, 255 ] ], [ "half", [
					128,
					Math.round( ground * 127 / 255 ),
					0,
					255
				] ], [ "full", [ 255, 0, 0, 255 ] ] ]
			) {
				assert.ok(
					row[name].every( ( v, i ) => Math.abs( v - expected[i] ) <= 2 ),
					JSON.stringify( { row, name, expected } )
				);
			}
		}
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
