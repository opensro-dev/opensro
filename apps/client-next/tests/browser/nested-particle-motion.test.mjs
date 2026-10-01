/*
===========================================================================

nested-particle-motion.test.mjs - nested particle placement

A nested particle's raster must equal an independently positioned quad,
including a delayed first draw.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";
test( "nested particle raster equals an independently positioned quad, including a delayed first draw", {
	timeout: 60000
}, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createEffectPrograms } = await import(
				"/src/engine/runtime/assets/worker/effects/program/program.ts"
			);
			const canvas = document.createElement( "canvas" ),
				renderer = createRenderer( canvas ),
				output = document.createElement( "canvas" );
			output.width = output.height = 128;
			const context = output.getContext( "2d" );
			const white = document.createElement( "canvas" );
			white.width = white.height = 1;
			white.getContext( "2d" ).fillStyle = "white";
			white.getContext( "2d" ).fillRect( 0, 0, 1, 1 );
			const emit = start => ({
				name: "StaticEmit",
				parameter: { value: { min: start, max: 1, burstRate: 1, minParticles: 1, spawnRate: 1 } }
			});
			const leaf = {
				name: "quad",
				children: [],
				globalData: { totalFrames: 20 },
				preProgram: [],
				postEmitterProgram: [],
				emitterProgram: [ emit( 0 ) ],
				renderProgram: [ { name: "SetGraphScale", parameter: { value: [ [ 10, 10, 10 ] ] } } ],
				viewCommand: { name: "ViewNone" },
				renderCommand: { name: "RenderPlate" },
				lifeCommand: { name: "NormalTimeExtinct" },
				resource: {
					srcBlend: 5,
					dstBlend: 6,
					backFaceType: 1,
					// Native default stage 0: MODULATE(TEXTURE, DIFFUSE) for colour and alpha.
					srcTextureArg1: 2,
					srcTextureArg2: 0,
					srcTextureOp: 4,
					dstTextureArg1: 2,
					dstTextureArg2: 0,
					dstTextureOp: 4,
					meshes: [ { path: "", textures: [ "white.ddj" ] } ]
				}
			};
			const parent = {
				...leaf,
				name: "invisible parent",
				renderCommand: { name: "RenderNone" },
				renderProgram: [ {
					name: "SetVelocity",
					flags: 0,
					byte1: 0,
					start: 0,
					end: 1,
					step: 0,
					parameter: { value: [ 5, 0, 0 ] }
				} ],
				children: [ { ...leaf, emitterProgram: [ emit( 2 ) ] } ]
			};
			const catalog = {
				framesPerSecond: 20,
				effects: {
					"nested.efp": { scale: 1, root: parent },
					"reference.efp": { scale: 1, root: leaf },
					"stone.efp": {
						scale: 1,
						root: {
							...leaf,
							resource: { ...leaf.resource, srcBlend: 3 },
							renderProgram: [ ...leaf.renderProgram, {
								name: "SetGraphDiffuse",
								parameter: { value: [ [ 128, 64, 32, 128 ] ] }
							} ]
						}
					}
				},
				meshes: {},
				textures: { "white.png": "white" }
			};
			const bytes = new TextEncoder().encode( JSON.stringify( catalog ) ), decoder = createEffectPrograms();
			try {
				const start = performance.now();
				while ( renderer.phase() === "starting" && performance.now() - start < 15000 ) {
					await new Promise( requestAnimationFrame );
				}
				if ( renderer.phase() !== "running" ) throw Error( renderer.error() );
				renderer.setWorld( { id: "particle oracle", originRegion: 257, groups: [], warnings: [] } );
				renderer.setWorldCamera( {
					eye: [ 0, 0, -100 ],
					target: [ 0, 0, 0 ],
					originRegion: 257,
					fov: 1,
					near: 1,
					far: 1000
				} );
				for ( const name of [ "nested.efp", "reference.efp", "stone.efp" ] ) {
					const { model } = decoder.decode( bytes, name );
					renderer.setCharacterModel( name, {
						...model,
						images: [ { width: white.width, height: white.height } ]
					}, [ await createImageBitmap( white ) ] );
				}
				const actor = ( model, time, x ) => ({
					gid: 1,
					model,
					clip: "effect",
					time,
					loop: false,
					scale: 1,
					pose: { regionId: 257, x, y: 0, z: 0, yaw: 0 }
				});
				async function snapshot( rows ) {
					renderer.setCharacterActors( rows );
					renderer.frame( { width: 128, height: 128 }, 0 );
					if ( renderer.error() ) throw Error( renderer.error() );
					const image = await createImageBitmap( canvas );
					context.drawImage( image, 0, 0 );
					image.close();
					return Array.from( context.getImageData( 0, 0, 128, 128 ).data );
				}
				const background = await snapshot( [] ),
					birth = await snapshot( [ actor( "nested.efp", .1, 0 ) ] ),
					later = await snapshot( [ actor( "nested.efp", .2, 0 ) ] );
				await snapshot( [] );
				const delayed = await snapshot( [ actor( "nested.efp", .2, 0 ) ] );
				const birthReference = await snapshot( [ actor( "reference.efp", 0, 15 ) ] );
				await snapshot( [] );
				const laterReference = await snapshot( [ actor( "reference.efp", 0, 25 ) ] );
				await snapshot( [] );
				const stone = await snapshot( [ actor( "stone.efp", 0, 0 ) ] ), center = (64 * 128 + 64) * 4;
				const stoneErrors = [ 128, 64, 32 ].map( ( source, i ) =>
					Math.abs( stone[center + i] - (source * source / 255 + background[center + i] * (1 - 128 / 255)) )
				);
				return {
					stoneErrors,
					birthDifferences: birth.filter( ( v, i ) => v !== birthReference[i] ).length,
					laterDifferences: later.filter( ( v, i ) => v !== laterReference[i] ).length,
					delayedDifferences: later.filter( ( v, i ) => v !== delayed[i] ).length,
					visible: birth.filter( ( v, i ) => v !== background[i] ).length
				};
			} finally {
				renderer.dispose();
			}
		} );
		assert.ok( result.stoneErrors.every( error => error <= 1 ), JSON.stringify( result.stoneErrors ) );
		assert.ok( result.visible > 0 );
		assert.equal( result.birthDifferences, 0 );
		assert.equal( result.laterDifferences, 0 );
		assert.equal( result.delayedDifferences, 0 );
	} finally {
		await browser.close();
	}
} );
