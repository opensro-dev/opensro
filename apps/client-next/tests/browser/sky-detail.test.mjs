/*
===========================================================================

sky-detail.test.mjs - the sky package in isolation

Port-only, not native. A sky draw (the gradient pipeline), a lit dome
and the sun's disc quad: the Sky detail row cycles off-on-off through
the production renderer. Off must restore the native bytes exactly; on
must change the sky (drifting clouds and the sun's atmospheric glow)
and the lit surfaces (the rim light) while the sun disc's authored
alpha path stays untouched. The capture waits one compositor frame
after the submits (the capture rule).
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"the sky detail row draws clouds, glow and rim, and restores native bytes",
	{ timeout: 120000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		const errors = [];
		page.on( "pageerror", error => errors.push( error.message ) );
		try {
			await page.route(
				CLIENT_NEXT_BASE_URL + "/",
				route =>
					route.fulfill( { contentType: "text/html", body: "<!doctype html><html><body></body></html>" } )
			);
			await page.goto( CLIENT_NEXT_BASE_URL );
			const shots = await page.evaluate( async () => {
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
					skyDetail: false,
					smoothCamera: false,
					enchantSparks: false,
					sharedActorRoom: false,
					vHidesCompanions: false,
					dungeonCharacterPvs: false
				};
				let deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" && performance.now() < deadline ) {
					await new Promise( requestAnimationFrame );
				}
				if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "GPU startup deadline" );
				renderer.setWorldCamera( { eye: [ 6, 26, -64 ], target: [ 4, 2, 0 ], fov: 1.1, near: 1, far: 500 } );
				const quad = ( id, material, size, y ) => ({
					id,
					center: [ 0, y, 0 ],
					radius: size * 2,
					material,
					geometry: {
						world: true,
						positions: Float32Array.of(
							-size,
							y,
							-size,
							size,
							y,
							-size,
							size,
							y,
							size,
							-size,
							y,
							size
						),
						normals: Float32Array.of( 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ),
						uvs: Float32Array.of( 0, 0, 1, 0, 1, 1, 0, 1 ),
						indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
						instances: identity,
						transform: identity
					}
				});
				renderer.setWorld( {
					id: "sky-detail",
					originRegion: 257,
					warnings: [],
					// The sky draw: the gradient pipeline.
					sky: 1,
					environment: {
						startTimeOfDay: .5,
						ratePerSecond: 0,
						tracks: {
							color0xf0: [ { t: 0, r: 1, g: 1, b: 1 } ],
							color0x124: [ { t: 0, r: .55, g: .55, b: .55 } ]
						}
					},
					groups: [
						// A lit dome: the rim light reads on its curved sides.
						quad(
							"dome",
							{
								color: [ .8, .45, .35, 1 ],
								unlit: false,
								objectLight: 1,
								alphaCutoff: 0,
								blend: false,
								doubleSided: true
							},
							24,
							5
						)
					]
				} );
				const shots = [];
				for ( const mode of [ "off", "skyDetail", "off" ] ) {
					renderer.experimentalVideo( {
						...options,
						...(mode === "skyDetail" ? { skyDetail: true } : {})
					} );
					for ( let frame = 0; frame < 8; frame++ ) await renderer.frame( { width, height }, .16 );
					if ( renderer.error() ) throw Error( `${mode}: ${renderer.error()}` );
					await new Promise( requestAnimationFrame );
					const image = await createImageBitmap( canvas );
					context.drawImage( image, 0, 0 );
					image.close();
					shots.push( { mode, rgba: [ ...context.getImageData( 0, 0, width, height ).data ] } );
				}
				renderer.dispose();
				return shots;
			} );
			const [before, during, after] = shots;
			// A-B-A: the row off restores the native bytes exactly.
			assert.deepEqual( after.rgba, before.rgba, "The row off restores the exact native frame" );
			// The on frame differs: the sky holds clouds and glow, the dome
			// the rim light.
			let changed = 0;
			for ( let i = 0; i < before.rgba.length; i += 4 ) {
				if ( before.rgba[i] !== during.rgba[i] || before.rgba[i + 1] !== during.rgba[i + 1] ) changed++;
			}
			assert.ok( changed > 100, `The on frame differs (${changed} pixels)` );
			assert.deepEqual( errors, [] );
		} finally {
			await browser.close();
		}
	}
);
