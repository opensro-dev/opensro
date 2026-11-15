/*
===========================================================================

water-refraction-cells.test.mjs - deep water hides the seabed's cell edges

Port-only, not native. The owner saw square patches on the sea with the
water refraction row on (2026-11-15): the seabed under open water changes
per 320 m cell (a textured cell, the untextured fog cell past the detailed
band, no cell at all), and the refraction bent a fixed share of it into
the surface, so every cell edge showed through. The scene here is deep
water over two seabed cells of opposite colour. The proof is presented
pixels: the water carries the native vertex alpha for its depth, and the
edge across it with the row on is no stronger than the native frame's
(row off): a fixed share drew it at 98.7 over the native 0.
water-refraction.test.mjs keeps the seabed visible through shallow
water. SRO_WATER_SHOTS names a folder for the before/after captures.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const WIDTH = 192;
const HEIGHT = 120;
// The seabed lies this far under the water, in metres: open sea.
const SEABED_DEPTH = 20;
// The on frame's edge may exceed the native one by this much at most, in
// mean channel difference: the bend shifts texels, it never adds seabed.
const EDGE_SLACK = 2;
// The rows sampled across the edge (the cells meet at x = 0, mid-frame).
const SAMPLE_ROWS = [ 70, 85, 100 ];
// Pixels either side of the edge column that are averaged.
const SIDE_SPAN = 20;

/*
================
edgeStrength

The mean channel difference between the water left and right of the
cell edge over SAMPLE_ROWS.
================
*/
function edgeStrength( rgba ) {
	let total = 0, samples = 0;
	const middle = WIDTH / 2;
	for ( const y of SAMPLE_ROWS ) {
		for ( let d = 4; d < 4 + SIDE_SPAN; d++ ) {
			const left = (y * WIDTH + middle - d) * 4, right = (y * WIDTH + middle + d) * 4;
			for ( let c = 0; c < 3; c++ ) total += Math.abs( rgba[left + c] - rgba[right + c] );
			samples++;
		}
	}
	return total / samples;
}

test( "refracted water shows the seabed's cell edge no more than native", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", error => errors.push( error.message ) );
	try {
		await page.route(
			CLIENT_NEXT_BASE_URL + "/",
			route => route.fulfill( { contentType: "text/html", body: "<!doctype html><html><body></body></html>" } )
		);
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async ( { width, height, depth } ) => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
			const { defaultVideoOptions, changeVideo } = await import(
				"/src/engine/foundation/rendering/video-options.ts"
			);
			const canvas = document.createElement( "canvas" );
			const output = document.createElement( "canvas" );
			output.width = width;
			output.height = height;
			const context = output.getContext( "2d", { willReadFrequently: true } );
			if ( !context ) throw Error( "No capture context" );
			const renderer = createRenderer( canvas, createPresentationRandom( 1 ) );
			const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
			const options = {
				renderScale: 100,
				phoneRenderCap: false,
				postProcessing: false,
				anisotropicFiltering: false,
				heightFog: false,
				dynamicSun: false,
				floatBloom: false,
				hdrToneMap: false,
				sunShadow: false,
				perPixelLighting: false,
				ambientOcclusion: false,
				skyDetail: false,
				waterRefraction: false,
				smoothCamera: false,
				enchantSparks: false,
				sharedActorRoom: false,
				vHidesCompanions: false,
				dungeonCharacterPvs: false
			};
			const deadline = performance.now() + 15000;
			while ( renderer.phase() === "starting" && performance.now() < deadline ) {
				await new Promise( requestAnimationFrame );
			}
			if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "GPU startup deadline" );
			// Looking along +z at the water, the two cells split left and right.
			renderer.setWorldCamera( { eye: [ 0, 25, -60 ], target: [ 0, 0, 20 ], fov: 1.1, near: 1, far: 500 } );
			const quad = ( id, corners, material, alpha = 1 ) => ({
				id,
				center: corners.reduce( ( sum, c ) => sum.map( ( v, i ) => v + c[i] / 4 ), [ 0, 0, 0 ] ),
				radius: 200,
				material,
				geometry: {
					world: true,
					positions: Float32Array.from( corners.flat() ),
					normals: Float32Array.of( 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ),
					uvs: Float32Array.of( 0, 0, 1, 0, 1, 1, 0, 1 ),
					colors: Float32Array.from( { length: 16 }, ( _, i ) => i % 4 === 3 ? alpha : 1 ),
					indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
					instances: identity,
					transform: identity
				}
			});
			const lit = colour => ({
				color: colour,
				unlit: false,
				objectLight: 1,
				alphaCutoff: 0,
				blend: false,
				doubleSided: true
			});
			const y = -depth;
			renderer.setWorld( {
				id: "water-refraction-cells",
				originRegion: 257,
				warnings: [],
				environment: {
					startTimeOfDay: .5,
					ratePerSecond: 0,
					tracks: {
						color0xf0: [ { t: 0, r: 1, g: 1, b: 1 } ],
						color0x124: [ { t: 0, r: .55, g: .55, b: .55 } ]
					}
				},
				groups: [
					// Two seabed cells meeting at x = 0: a dark one and a bright one.
					quad(
						"cell-dark",
						[ [ -200, y, -200 ], [ 0, y, -200 ], [ 0, y, 200 ], [ -200, y, 200 ] ],
						lit( [
							.05,
							.15,
							.1,
							1
						] )
					),
					quad(
						"cell-bright",
						[ [ 0, y, -200 ], [ 200, y, -200 ], [ 200, y, 200 ], [ 0, y, 200 ] ],
						lit( [
							.95,
							.9,
							.5,
							1
						] )
					),
					quad( "water", [ [ -150, 0, -150 ], [ 150, 0, -150 ], [ 150, 0, 150 ], [ -150, 0, 150 ] ], {
						color: [ .3, .55, .55, 0xb4 / 255 ],
						unlit: false,
						objectLight: 1,
						alphaCutoff: 0,
						blend: true,
						water: true,
						doubleSided: true
					}, 10 / 15 )
				]
			} );
			const capture = async () => {
				for ( let frame = 0; frame < 6; frame++ ) await renderer.frame( { width, height }, .16 );
				if ( renderer.error() ) throw Error( renderer.error() );
				await new Promise( requestAnimationFrame );
				const image = await createImageBitmap( canvas );
				context.drawImage( image, 0, 0 );
				image.close();
				return { rgba: [ ...context.getImageData( 0, 0, width, height ).data ], png: output.toDataURL() };
			};
			const shots = [];
			for ( const on of [ false, true ] ) {
				renderer.videoOptions( changeVideo( defaultVideoOptions(), 4, 1 ) );
				renderer.experimentalVideo( { ...options, waterRefraction: on } );
				shots.push( await capture() );
			}
			renderer.dispose();
			return shots;
		}, { width: WIDTH, height: HEIGHT, depth: SEABED_DEPTH } );
		const [native, refracted] = result;
		const folder = process.env.SRO_WATER_SHOTS;
		if ( folder ) {
			mkdirSync( folder, { recursive: true } );
			for ( const [name, shot] of Object.entries( { native, refracted } ) ) {
				writeFileSync( path.join( folder, name + ".png" ), Buffer.from( shot.png.split( "," )[1], "base64" ) );
			}
		}
		const off = edgeStrength( native.rgba ), on = edgeStrength( refracted.rgba );
		console.log( `cell edge: native ${off.toFixed( 1 )}, refracted ${on.toFixed( 1 )}` );
		assert.ok(
			on <= off + EDGE_SLACK,
			`The refracted edge (${on.toFixed( 1 )}) stays within native (${off.toFixed( 1 )})`
		);
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
