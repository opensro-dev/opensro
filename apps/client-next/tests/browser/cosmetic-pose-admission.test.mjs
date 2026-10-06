/*
===========================================================================

cosmetic-pose-admission.test.mjs - overload cannot expose GPU bind poses

An authored joint moves a red quad away from rest. Read actual rendered
pixels so CPU socket correctness cannot conceal an uninitialized palette.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "cold GPU rigs and clip entry stay animated with no cosmetic budget", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const samples = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
			const model = {
				nodes: [ {
					name: "root",
					parent: -1,
					translation: [ 0, 0, 0 ],
					rotation: [ 0, 0, 0, 1 ],
					scale: [ 1, 1, 1 ]
				} ],
				images: [],
				clips: [ -5, 5 ].map( ( x, i ) => ({
					name: i ? "run" : "stand",
					duration: 1,
					channels: [ {
						node: 0,
						path: "translation",
						interpolation: "LINEAR",
						times: Float32Array.of( 0, 1 ),
						values: Float32Array.of( x, 0, 0, x, 0, 0 )
					} ]
				}) ),
				primitives: [ {
					name: "body",
					node: 0,
					joints: [ 0 ],
					inverseBind: identity,
					image: -1,
					geometry: {
						positions: Float32Array.of( -2, -2, 0, 2, -2, 0, 2, 2, 0, -2, 2, 0 ),
						indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
						joints: new Uint32Array( 16 ),
						weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
						transform: identity,
						material: { color: [ 1, 0, 0, 1 ], doubleSided: true, unlit: true }
					}
				} ]
			};
			const copy = document.createElement( "canvas" );
			copy.width = copy.height = 64;
			const context = copy.getContext( "2d" );
			/*
			================
			sample
			================
			*/
			async function sample( clip, seconds ) {
				if ( !context ) throw Error( "Pixel readback context unavailable" );
				renderer.setCharacterActors( [ {
					gid: 1,
					model: "rig",
					pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
					clip,
					time: seconds,
					loop: true,
					scale: 1,
					animationLod: { fraction: .8, crowded: true, optional: true }
				} ] );
				await renderer.frame( { width: 64, height: 64 }, seconds );
				if ( renderer.error() ) throw Error( renderer.error() );
				const bitmap = await createImageBitmap( canvas );
				context.drawImage( bitmap, 0, 0 );
				bitmap.close();
				const pixels = context.getImageData( 0, 0, 64, 64 ).data;
				let sum = 0, count = 0;
				for ( let i = 0; i < pixels.length; i += 4 ) {
					if ( pixels[i] > 200 && pixels[i + 1] < 30 && pixels[i + 2] < 30 ) {
						sum += (i / 4) % 64;
						count++;
					}
				}
				return { center: sum / count, count };
			}
			try {
				const deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" ) {
					if ( performance.now() > deadline ) throw Error( "GPU admission timed out" );
					await new Promise( requestAnimationFrame );
				}
				renderer.setWorld( { id: "rig", originRegion: 257, warnings: [], groups: [] } );
				renderer.setWorldCamera( {
					originRegion: 257,
					eye: [ 0, 0, 30 ],
					target: [ 0, 0, 0 ],
					fov: 1,
					near: 1,
					far: 100
				} );
				renderer.setCharacterModel( "rig", model, [] );
				renderer.setFrameWork( { level: () => 2, remaining: () => 0, spend() {} } );
				return [ await sample( "stand", 0 ), await sample( "run", .016 ), await sample( "stand", 1.016 ) ];
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		assert.ok(
			samples.every( sample => sample.count > 0 && Math.abs( sample.center - 31.5 ) > 5 ),
			"every frame must show the authored joint, never the central rest pose"
		);
		assert.ok( Math.abs( samples[0].center - samples[1].center ) > 10, "clip entry reaches the GPU immediately" );
		assert.equal( samples[2].center, samples[0].center, "stall recovery retains animation admission" );
	} finally {
		await browser.close();
	}
} );
