import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test(
	"GPU material ambient is independent of diffuse and vertex lighting precedes interpolation",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await holdProbeRuntime( page );
			await page.goto( CLIENT_NEXT_BASE_URL );
			const bytes = readPublishedAssetBytesSync(
				"/assets/char/china/chinaman_adventurer.glb",
				CLIENT_PUBLIC_ROOT
			);
			const result = await page.evaluate( async bytes => {
				const { createModelDecoder } = await import( "/src/engine/runtime/assets/worker/model/model.ts" );
				const decoder = createModelDecoder();
				const actorMaterial =
					decoder.character( decoder.decode( Uint8Array.from( bytes ) ) ).primitives[0].geometry.material;
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const canvas = document.createElement( "canvas" ),
					renderer = createRenderer( canvas ),
					out = document.createElement( "canvas" );
				out.width = out.height = 128;
				const ctx = out.getContext( "2d" );
				const I = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
				const base = {
					color: [ 0, 0, 0, 1 ],
					ambient: [ 1, 0, 0 ],
					objectLight: 1,
					alphaCutoff: 0,
					blend: false,
					doubleSided: true,
					unlit: false,
					fogDisabled: true
				};
				async function sample( material, normals, instance = I(), skinned = false ) {
					const geometry = {
						positions: Float32Array.of( -25, -25, 0, 25, -25, 0, 0, 25, 0 ),
						normals: Float32Array.from( normals ),
						uvs: new Float32Array( 6 ),
						indices: Uint32Array.of( 0, 2, 1 ),
						transform: I(),
						instances: instance,
						material,
						world: true
					};
					if ( skinned ) {
						geometry.joints = new Uint32Array( 12 );
						geometry.weights = Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 );
						geometry.bones = I();
					}
					renderer.setWorld( {
						id: JSON.stringify( [ material, normals, [ ...instance ], skinned ] ),
						originRegion: 257,
						groups: [ { id: "triangle", geometry, material, center: [ 0, 0, 0 ], radius: 40 } ],
						warnings: []
					} );
					renderer.setWorldCamera( {
						eye: [ 0, 0, -80 ],
						target: [ 0, 0, 0 ],
						originRegion: 257,
						fov: 1,
						near: 1,
						far: 500
					} );
					for ( let i = 0; i < 120; i++ ) {
						renderer.frame( { width: 128, height: 128 }, 0 );
						if ( renderer.error() ) throw Error( renderer.error() );
						if ( renderer.worldStats().visibleGroups ) break;
						await new Promise( requestAnimationFrame );
					}
					await new Promise( requestAnimationFrame );
					renderer.frame( { width: 128, height: 128 }, 0 );
					const image = await createImageBitmap( canvas );
					ctx.drawImage( image, 0, 0 );
					image.close();
					return [ ...ctx.getImageData( 64, 64, 1, 1 ).data ];
				}
				try {
					const scaled = I();
					scaled[0] = 2;
					scaled[5] = .5;
					const directional = { ...base, color: [ .2, .2, .2, 1 ], ambient: [ 0, 0, 0 ] };
					const flat = [ 0, 0, -1, 0, 0, -1, 0, 0, -1 ], vary = [ 1, 0, 0, -1, 0, 0, 0, 1, 0 ];
					return {
						skinnedScaled: await sample( directional, [ 1, 0, 0, 1, 0, 0, 1, 0, 0 ], scaled, true ),
						scaled: await sample( directional, [ 1, 0, 0, 1, 0, 0, 1, 0, 0 ], scaled ),
						inverseOracle: await sample( directional, [ .5, 0, 0, .5, 0, 0, .5, 0, 0 ] ),
						actor: await sample( { ...actorMaterial, fogDisabled: true }, flat ),
						nativeStage: await sample( { ...actorMaterial, fogDisabled: true, stageFactor: 2 }, flat ),
						missingStage: await sample( { ...actorMaterial, fogDisabled: true, stageFactor: 1 }, flat ),
						red: await sample( base, flat ),
						green: await sample( { ...base, ambient: [ 0, 1, 0 ] }, flat ),
						dark: await sample( { ...base, ambient: [ 0, 0, 0 ] }, flat ),
						vertex: await sample( { ...base, color: [ 1, 1, 1, 1 ], ambient: [ 0, 0, 0 ] }, vary ),
						pixel: await sample( {
							...base,
							color: [ 1, 1, 1, 1 ],
							ambient: [ 0, 0, 0 ],
							objectLight: undefined
						}, vary )
					};
				} finally {
					renderer.dispose();
				}
			}, [ ...bytes ] );
			await mkdir( "temp/artifacts/character-lighting", { recursive: true } );
			await writeFile( "temp/artifacts/character-lighting/result.json", JSON.stringify( result, null, 2 ) );
			assert.deepEqual( result.skinnedScaled, result.inverseOracle );
			assert.deepEqual(
				result.scaled,
				result.inverseOracle,
				"Object-space native light requires the inverse world transform"
			);
			assert.deepEqual( result.actor, result.nativeStage, "Decoded actor must consume retail MODULATE2X" );
			assert.ok( result.nativeStage[0] > result.missingStage[0] + 10, JSON.stringify( result ) );
			assert.ok( result.red[0] > 20 );
			assert.equal( result.red[1], 0 );
			assert.ok( result.green[1] > 20 );
			assert.equal( result.green[0], 0 );
			assert.deepEqual( result.dark.slice( 0, 3 ), [ 0, 0, 0 ] );
			assert.ok( Math.abs( result.vertex[0] - result.pixel[0] ) > 10, JSON.stringify( result ) );
		} finally {
			await browser.close();
		}
	}
);
