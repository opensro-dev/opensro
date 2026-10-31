/*
===========================================================================

sun-rays.test.mjs - the experimental sun rays stage through the production renderer

The Sun rays row (device/god-rays.ts, port-only) marches the stored scene
depth toward the sun's screen position and composites the scatter
additively. The studio light pins the sun direction, so one world - open
sky above, a ground plane below and one blocking wall left of the sun -
must show: off stays byte-exact across toggles, on adds light that pools
toward the sun's quadrant, a sun behind the eye adds exactly nothing, a
resized frame keeps the rays, and a wall covering the sun's column kills
every ray - the depth march, not the falloff, decides what lights up.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "sun rays scatter toward the sun and off stays byte-exact", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	page.on( "console", m => {
		if ( m.type() === "error" ) console.log( m.text() );
	} );
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
			const width = 192, height = 120;
			const canvas = document.createElement( "canvas" );
			const output = document.createElement( "canvas" );
			output.width = width;
			output.height = height;
			const context = output.getContext( "2d", { willReadFrequently: true } );
			if ( !context ) throw Error( "No capture context" );
			const renderer = createRenderer( canvas, createPresentationRandom( 1 ) );
			/** @type {import("../../src/engine/foundation/ui/experimental-options.ts").ExperimentalVideo} */
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
				sunRays: false,
				smoothCamera: false,
				enchantSparks: false,
				sharedActorRoom: false,
				vHidesCompanions: false,
				dungeonCharacterPvs: false
			};
			const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
			const lit = shade => ({
				color: shade,
				unlit: false,
				objectLight: 1,
				alphaCutoff: 0,
				blend: false,
				doubleSided: true
			});
			const light = ( x, y, z ) => {
				const length = Math.hypot( x, y, z );
				return [ x / length, y / length, z / length ];
			};
			// The wall stands between the eye and the sky left of the sun; the
			// rays that march across its silhouette are the shafts under test.
			const wall = [], wallNormals = [], wallIndices = [];
			const corners = [
				[ -10, 0, -32 ],
				[ -4, 0, -32 ],
				[ -4, 0, -28 ],
				[ -10, 0, -28 ],
				[ -10, 70, -32 ],
				[ -4, 70, -32 ],
				[ -4, 70, -28 ],
				[ -10, 70, -28 ]
			];
			const faces = [
				[ [ 0, 3, 2, 1 ], [ 0, -1, 0 ] ],
				[ [ 4, 5, 6, 7 ], [ 0, 1, 0 ] ],
				[ [ 0, 1, 5, 4 ], [ 0, 0, -1 ] ],
				[ [ 2, 3, 7, 6 ], [ 0, 0, 1 ] ],
				[ [ 1, 2, 6, 5 ], [ 1, 0, 0 ] ],
				[ [ 0, 4, 7, 3 ], [ -1, 0, 0 ] ]
			];
			for ( const [quad, normal] of faces ) {
				const base = wall.length / 3;
				for ( const corner of quad ) {
					wall.push( ...corners[corner] );
					wallNormals.push( ...normal );
				}
				wallIndices.push( base, base + 1, base + 2, base, base + 2, base + 3 );
			}
			try {
				const deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" && performance.now() < deadline ) {
					await new Promise( requestAnimationFrame );
				}
				if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "GPU startup deadline" );
				renderer.setViewerLight( {
					diffuse: [ 1, 1, 1 ],
					ambient: [ .5, .5, .5 ],
					direction: light( .2, .35, 1 )
				} );
				renderer.setWorldCamera( { eye: [ 0, 10, -60 ], target: [ 0, 14, 0 ], fov: 1, near: 1, far: 500 } );
				const world = {
					id: "sun-rays",
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
						{
							id: "ground",
							center: [ 0, 0, 0 ],
							radius: 300,
							material: lit( [ .6, .55, .5, 1 ] ),
							geometry: {
								world: true,
								positions: Float32Array.of( -150, 0, -150, 150, 0, -150, 150, 0, 150, -150, 0, 150 ),
								normals: Float32Array.of( 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ),
								uvs: Float32Array.of( 0, 0, 1, 0, 1, 1, 0, 1 ),
								indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
								instances: identity,
								transform: identity
							}
						},
						{
							id: "wall",
							center: [ -7, 35, -30 ],
							radius: 36,
							material: lit( [ .45, .45, .5, 1 ] ),
							geometry: {
								world: true,
								positions: new Float32Array( wall ),
								normals: new Float32Array( wallNormals ),
								uvs: new Float32Array( wall.length / 3 * 2 ),
								indices: new Uint32Array( wallIndices ),
								instances: identity,
								transform: identity
							}
						}
					]
				};
				renderer.setWorld( world );
				/*
				================
				capture

				Five frames settle the stage, then one presented frame reads back;
				the presented bitmap lags the submit by a frame, so the rAF comes
				between the submits and the read.
				================
				*/
				const capture = async sunRays => {
					renderer.experimentalVideo( { ...options, sunRays } );
					for ( let frame = 0; frame < 5; frame++ ) await renderer.frame( { width, height }, .16 );
					await new Promise( requestAnimationFrame );
					if ( renderer.error() ) throw Error( renderer.error() );
					const image = await createImageBitmap( canvas );
					context.drawImage( image, 0, 0 );
					image.close();
					return [ ...context.getImageData( 0, 0, width, height ).data ];
				};
				const off = await capture( false ),
					on = await capture( true ),
					offAgain = await capture( false );
				// A sun behind the eye marches nothing: the composite adds zero.
				// The behind light also shades the scene differently, so its zero
				// is proven against the same light with the row off.
				renderer.setViewerLight( {
					diffuse: [ 1, 1, 1 ],
					ambient: [ .5, .5, .5 ],
					direction: light( .2, .35, -1 )
				} );
				const behindOff = await capture( false ),
					behind = await capture( true );
				// A new frame size rebuilds the half-res targets; the rays stay.
				// The sun returns in front of the eye first.
				renderer.setViewerLight( {
					diffuse: [ 1, 1, 1 ],
					ambient: [ .5, .5, .5 ],
					direction: light( .2, .35, 1 )
				} );
				const smallOutput = document.createElement( "canvas" );
				smallOutput.width = 96;
				smallOutput.height = 60;
				const smallContext = smallOutput.getContext( "2d", { willReadFrequently: true } );
				if ( !smallContext ) throw Error( "No small capture context" );
				const smallOff = [], smallOn = [];
				for ( const sunRays of [ false, true ] ) {
					renderer.experimentalVideo( { ...options, sunRays } );
					for ( let frame = 0; frame < 5; frame++ ) await renderer.frame( { width: 96, height: 60 }, .16 );
					await new Promise( requestAnimationFrame );
					if ( renderer.error() ) throw Error( renderer.error() );
					const image = await createImageBitmap( canvas );
					smallContext.drawImage( image, 0, 0 );
					image.close();
					const bytes = smallContext.getImageData( 0, 0, 96, 60 ).data;
					(sunRays ? smallOn : smallOff).push( ...bytes );
				}
				// The occluding world: the wall grows past the sun's screen column,
				// so every march ends on geometry and the rays must vanish - the
				// composite adds zero and the on frame returns the off bytes.
				const occluded = { ...world };
				occluded.groups = world.groups.map( group => {
					if ( group.id !== "wall" ) return group;
					const corners = [
						[ -10, 0, -32 ],
						[ 6, 0, -32 ],
						[ 6, 0, -28 ],
						[ -10, 0, -28 ],
						[ -10, 70, -32 ],
						[ 6, 70, -32 ],
						[ 6, 70, -28 ],
						[ -10, 70, -28 ]
					];
					const positions = [], normals = [], indices = [];
					for ( const [quad, normal] of faces ) {
						const base = positions.length / 3;
						for ( const corner of quad ) {
							positions.push( ...corners[corner] );
							normals.push( ...normal );
						}
						indices.push( base, base + 1, base + 2, base, base + 2, base + 3 );
					}
					return {
						...group,
						center: [ -2, 35, -30 ],
						radius: 38,
						geometry: {
							...group.geometry,
							positions: new Float32Array( positions ),
							normals: new Float32Array( normals ),
							uvs: new Float32Array( positions.length / 3 * 2 ),
							indices: new Uint32Array( indices )
						}
					};
				} );
				renderer.setWorld( occluded );
				const occludedOff = await capture( false ),
					occludedOn = await capture( true );
				return {
					off,
					on,
					offAgain,
					behindOff,
					behind,
					smallOff,
					smallOn,
					occludedOff,
					occludedOn,
					error: null
				};
			} finally {
				renderer.dispose();
			}
		} );
		assert.equal( result.error, null );
		// Off is the native order: toggling the row away restores its bytes.
		assert.deepEqual( result.offAgain, result.off, "off must restore the exact native bytes" );
		assert.deepEqual( result.behind, result.behindOff, "a sun behind the eye adds exactly zero" );
		// The scatter pools toward the sun (upper right of this world).
		const quadrants = { ul: 0, ur: 0, ll: 0, lr: 0 };
		let changed = 0, peak = 0;
		for ( let i = 0; i < result.off.length; i += 4 ) {
			const pixel = i / 4, x = pixel % 192, y = (pixel / 192) | 0;
			const gain = (result.on[i] - result.off[i]) + (result.on[i + 1] - result.off[i + 1]) +
				(result.on[i + 2] - result.off[i + 2]);
			if ( gain > 0 ) changed++;
			if ( gain > peak ) peak = gain;
			const quadrant = (y < 60 ? "u" : "l") + (x < 96 ? "l" : "r");
			quadrants[quadrant] += gain;
		}
		assert.ok( changed > 200, `the rays must light real area (${changed} pixels)` );
		assert.ok( peak >= 40, `open sky near the sun gains the stage's light (${peak})` );
		assert.ok(
			quadrants.ur > quadrants.ul && quadrants.ur > quadrants.lr && quadrants.ur > quadrants.ll,
			JSON.stringify( quadrants )
		);
		// A resized frame rebuilds the half-res targets and keeps the rays.
		let smallGain = 0;
		for ( let i = 0; i < result.smallOff.length; i += 4 ) {
			smallGain += (result.smallOn[i] - result.smallOff[i]) + (result.smallOn[i + 1] - result.smallOff[i + 1]) +
				(result.smallOn[i + 2] - result.smallOff[i + 2]);
		}
		assert.ok( smallGain > 0, "the resized frame keeps the rays" );
		// The wall over the sun's column occludes every path: the march must
		// read that depth and the on frame must return the off bytes exactly.
		assert.deepEqual(
			result.occludedOn,
			result.occludedOff,
			"a wall over the sun kills every ray"
		);
	} finally {
		await browser.close();
	}
} );
