/*
===========================================================================

player-camera-fade.test.mjs - the camera fade of the local player

The WebGPU camera fade restores pixels afterwards and leaves a peer intact.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test( "WebGPU player camera fade restores pixels and preserves a peer", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", e => errors.push( e.message ) );
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createModelDecoder } = await import( "/src/engine/runtime/assets/worker/model/model.ts" );
			// Equipment models are content-named: take the first published one that
			// authors an opaque, fade-only material without environment reflection
			// (this scene owns no environment texture).
			const decoder = createModelDecoder();
			const index = await (await fetch( "/assets/packs/manifest.json" )).json();
			let opaqueMaterial;
			for ( const { path } of index.assets.filter( e => e.path.startsWith( "/assets/char/equipment/" ) ) ) {
				const bytes = new Uint8Array( await (await fetch( path )).arrayBuffer() );
				opaqueMaterial = decoder.character( decoder.decode( bytes ) ).primitives.find( p =>
					p.geometry.material.fadeAlphaOnly && !p.geometry.material.environmentReflection
				)?.geometry.material;
				if ( opaqueMaterial ) break;
			}
			if ( !opaqueMaterial ) throw Error( "no published equipment model authors a fade-only material" );
			const { advanceCharacterFade } = await import( "/src/engine/foundation/animation/character-fade.ts" );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const output = document.createElement( "canvas" );
			output.width = output.height = 128;
			const ctx = output.getContext( "2d" );
			const I = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
			const model = {
				nodes: [ {
					name: "root",
					parent: -1,
					translation: [ 0, 0, 0 ],
					rotation: [ 0, 0, 0, 1 ],
					scale: [ 1, 1, 1 ]
				} ],
				images: [ { width: 1, height: 1 } ],
				clips: [],
				primitives: [ {
					name: "body",
					node: 0,
					joints: [ 0 ],
					inverseBind: I(),
					image: 0,
					geometry: {
						positions: Float32Array.of( -10, -10, 0, 10, -10, 0, 0, 10, 0 ),
						normals: Float32Array.of( 0, 0, -1, 0, 0, -1, 0, 0, -1 ),
						uvs: new Float32Array( 6 ),
						indices: Uint32Array.of( 0, 1, 2 ),
						joints: new Uint32Array( 12 ),
						weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
						transform: I(),
						material: {
							...opaqueMaterial,
							color: [ 1, 0, 0, 1 ],
							alphaCutoff: 0,
							blend: false,
							doubleSided: true,
							unlit: true
						}
					}
				} ]
			};
			const actor = ( gid, x, opacity ) => ({
				gid,
				model: "body",
				pose: { regionId: 257, x, y: 0, z: 0, yaw: 0 },
				clip: "",
				time: 0,
				loop: false,
				scale: 1,
				opacity
			});
			const fade = { mode: false, current: 255, start: 255, progress: 1 };
			try {
				renderer.setCharacterModel( "body", model, [
					await createImageBitmap( new ImageData( Uint8ClampedArray.of( 255, 255, 255, 0 ), 1, 1 ) )
				] );
				renderer.setWorld( { id: "fade", originRegion: 257, groups: [], warnings: [] } );
				renderer.setWorldCamera( {
					eye: [ 0, 0, -80 ],
					target: [ 0, 0, 0 ],
					originRegion: 257,
					fov: 1,
					near: 1,
					far: 500
				} );
				async function sample( actors ) {
					renderer.setCharacterActors( actors );
					for ( let i = 0; i < 120; i++ ) {
						renderer.frame( { width: 128, height: 128 }, 0 );
						if ( renderer.error() ) throw new Error( renderer.error() );
						if ( renderer.phase() === "running" ) break;
						await new Promise( requestAnimationFrame );
					}
					await new Promise( requestAnimationFrame );
					renderer.frame( { width: 128, height: 128 }, 0 );
					const bitmap = await createImageBitmap( canvas );
					ctx.drawImage( bitmap, 0, 0 );
					bitmap.close();
					return { pixels: [ ...ctx.getImageData( 0, 0, 128, 128 ).data ], png: output.toDataURL() };
				}
				const peer = actor( 2, 18, 1 ),
					baseline = await sample( [ peer ] ),
					opaque = await sample( [ actor( 1, -18, 1 ), peer ] ),
					half = await sample( [ actor( 1, -18, advanceCharacterFade( fade, true, .25 ) ), peer ] ),
					hidden = await sample( [ actor( 1, -18, advanceCharacterFade( fade, true, .25 ) ), peer ] ),
					restored = await sample( [ actor( 1, -18, advanceCharacterFade( fade, false, .5 ) ), peer ] );
				const diff = ( a, b ) => a.pixels.reduce( ( n, v, i ) => n + Number( v !== b.pixels[i] ), 0 );
				const peerChanged = opaque.pixels.reduce(
					( n, v, i ) => n + Number( Math.floor( i / 4 ) % 128 >= 64 && half.pixels[i] !== v ),
					0
				);
				// Draw two faded instances front-to-back. Native fade retains ZWRITE: the
				// second, farther instance must not blend through the first one.
				const front = actor( 3, 0, 127 / 255 ),
					back = { ...actor( 4, 0, 127 / 255 ), pose: { ...front.pose, z: 2 } };
				const frontOnly = await sample( [ front ] ), overlap = await sample( [ front, back ] );
				return {
					visible: diff( opaque, baseline ),
					half: diff( half, opaque ),
					hidden: diff( hidden, baseline ),
					restored: diff( restored, opaque ),
					peerChanged,
					depthChanged: diff( frontOnly, overlap ),
					images: {
						baseline: baseline.png,
						opaque: opaque.png,
						half: half.png,
						hidden: hidden.png,
						restored: restored.png,
						front: frontOnly.png,
						overlap: overlap.png
					}
				};
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		const dir = "temp/artifacts/bugs/player-camera-fade";
		await mkdir( dir, { recursive: true } );
		for ( const [name, png] of Object.entries( result.images ) ) {
			await writeFile( dir + "/" + name + ".png", Buffer.from( png.split( "," )[1], "base64" ) );
		}
		const { images, ...report } = result;
		await writeFile( dir + "/result.json", JSON.stringify( { ...report, errors }, null, 2 ) );
		assert.deepEqual( errors, [] );
		assert.ok( result.visible > 100 );
		assert.ok( result.half > 100 );
		assert.equal( result.hidden, 0 );
		assert.equal( result.restored, 0 );
		assert.equal( result.peerChanged, 0 );
		assert.equal( result.depthChanged, 0 );
	} finally {
		await browser.close();
	}
} );
