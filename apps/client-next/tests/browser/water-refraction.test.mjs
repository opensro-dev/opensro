/*
===========================================================================

water-refraction.test.mjs - the refracted water stage through the renderer

Port-only, not native. A lit seabed below a wide blended water plane and
a shore sloping through the waterline, drawn by the production renderer
with the native water reflection on (video slot 4). The proof is the
presented pixels: the row off restores the native frame byte for byte
(off, on, off), and the row on shows the seabed's hue through the water
and whitens the waterline along the sloped shore. The pass order (the
water before the transparent tail) is proven in
tests/runtime/frame-water-order.test.mjs.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const WIDTH = 192;
const HEIGHT = 120;
// Pixels the row must change: the water covers most of the frame, so a
// working stage changes far more than this.
const MIN_CHANGED_PIXELS = 400;
// A pixel whose channel sum reaches this reads as foam-white.
const FOAM_SUM = 600;
// The water pixel sampled for the seabed's hue: lower middle of the frame.
const WATER_PIXEL = { x: WIDTH / 2, y: Math.round( HEIGHT * 0.65 ) };

/*
================
pixel
================
*/
function pixel( rgba, x, y ) {
	const at = (y * WIDTH + x) * 4;
	return rgba.slice( at, at + 3 );
}

/*
================
foamPixels
================
*/
function foamPixels( rgba ) {
	let count = 0;
	for ( let i = 0; i < rgba.length; i += 4 ) if ( rgba[i] + rgba[i + 1] + rgba[i + 2] >= FOAM_SUM ) count++;
	return count;
}

test( "the refracted water row bends, foams and restores native bytes", {
	timeout: 120000
}, async () => {
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", error => errors.push( error.message ) );
	try {
		await page.route(
			CLIENT_NEXT_BASE_URL + "/",
			route => route.fulfill( { contentType: "text/html", body: "<!doctype html><html><body></body></html>" } )
		);
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async ( { width, height } ) => {
			const gpuErrors = [];
			const requestDevice = GPUAdapter.prototype.requestDevice;
			GPUAdapter.prototype.requestDevice = async function( descriptor ) {
				const device = await requestDevice.call( this, descriptor );
				device.addEventListener( "uncapturederror", event => gpuErrors.push( String( event.error?.message ) ) );
				return device;
			};
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
			renderer.setWorldCamera( { eye: [ 30, 16, -46 ], target: [ 0, 0, 6 ], fov: 1.1, near: 1, far: 500 } );
			// A quad from four corners (x, y, z), two triangles.
			const quad = ( id, corners, material ) => ({
				id,
				center: corners.reduce( ( sum, c ) => sum.map( ( v, i ) => v + c[i] / 4 ), [ 0, 0, 0 ] ),
				radius: 200,
				material,
				geometry: {
					world: true,
					positions: Float32Array.from( corners.flat() ),
					normals: Float32Array.of( 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ),
					uvs: Float32Array.of( 0, 0, 1, 0, 1, 1, 0, 1 ),
					indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
					instances: identity,
					transform: identity
				}
			});
			const flat = (
				y,
				size
			) => [ [ -size, y, -size ], [ size, y, -size ], [ size, y, size ], [ -size, y, size ] ];
			const lit = colour => ({
				color: colour,
				unlit: false,
				objectLight: 1,
				alphaCutoff: 0,
				blend: false,
				doubleSided: true
			});
			renderer.setWorld( {
				id: "water-refraction",
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
					quad( "seabed", flat( -5, 90 ), lit( [ .9, .85, .3, 1 ] ) ),
					quad( "water", flat( 0, 80 ), {
						color: [ .55, .7, .85, .55 ],
						unlit: false,
						objectLight: 1,
						alphaCutoff: 0,
						blend: true,
						water: true,
						doubleSided: true
					} ),
					// The shore slopes from under the water up through the
					// waterline: its depth meets the water's along that line.
					quad(
						"shore",
						[ [ -60, -4, 20 ], [ 60, -4, 20 ], [ 60, 6, 40 ], [ -60, 6, 40 ] ],
						lit( [ .55, .5, .42, 1 ] )
					)
				]
			} );
			const capture = async () => {
				for ( let frame = 0; frame < 6; frame++ ) await renderer.frame( { width, height }, .16 );
				if ( renderer.error() ) throw Error( renderer.error() );
				// Let the compositor present the last frame before the canvas
				// is read: a bitmap taken in the submit's task reads the prior
				// present.
				await new Promise( requestAnimationFrame );
				const image = await createImageBitmap( canvas );
				context.drawImage( image, 0, 0 );
				image.close();
				return [ ...context.getImageData( 0, 0, width, height ).data ];
			};
			const shots = [];
			for ( const on of [ false, true, false ] ) {
				// The native water reflection (video slot 4) stays on in every
				// mode: the row changes only how the water draws.
				renderer.videoOptions( changeVideo( defaultVideoOptions(), 4, 1 ) );
				renderer.experimentalVideo( { ...options, waterRefraction: on } );
				shots.push( await capture() );
			}
			renderer.dispose();
			return { shots, gpuErrors };
		}, {
			width: WIDTH,
			height: HEIGHT
		} );
		const [before, during, after] = result.shots;
		assert.deepEqual( after, before, "The row off restores the exact native frame" );
		let changed = 0;
		for ( let i = 0; i < before.length; i += 4 ) {
			if ( before[i] !== during[i] || before[i + 1] !== during[i + 1] || before[i + 2] !== during[i + 2] ) {
				changed++;
			}
		}
		assert.ok( changed > MIN_CHANGED_PIXELS, `The on frame differs (${changed} pixels)` );
		const [, g, b] = pixel( during, WATER_PIXEL.x, WATER_PIXEL.y );
		assert.ok(
			g > b,
			`The water shows the seabed's yellow through it (${pixel( during, WATER_PIXEL.x, WATER_PIXEL.y )})`
		);
		assert.ok(
			foamPixels( during ) > foamPixels( before ),
			`The waterline foams on (${foamPixels( during )} white pixels, ${foamPixels( before )} off)`
		);
		assert.deepEqual( result.gpuErrors, [], "No uncaptured GPU errors in any mode" );
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
