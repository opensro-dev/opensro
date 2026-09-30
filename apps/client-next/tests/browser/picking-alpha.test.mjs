/*
===========================================================================

picking-alpha.test.mjs - entity picking and alpha

Native entity picks use their own bounds without a scenery veto, while
rendering keeps alpha and depth.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test( "native entity picks use their own bounds without a scenery veto; rendering retains alpha and depth", {
	timeout: 45000
}, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
			const geometry = ( z, color ) => ({
				positions: Float32Array.of( -2, -2, z, 0, 2, z, 2, -2, z ),
				normals: new Float32Array( 9 ),
				uvs: Float32Array.of( .25, .5, .25, .5, .25, .5 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				instances: identity(),
				transform: identity(),
				material: { color, alphaCutoff: .5, blend: false, doubleSided: false, unlit: true }
			});
			const bitmap = alpha =>
				createImageBitmap(
					new ImageData( Uint8ClampedArray.of( 255, 255, 255, alpha, 255, 255, 255, 255 ), 2, 1 )
				);
			const model = g => ({
				nodes: [],
				clips: [],
				images: [ { width: 2, height: 1 } ],
				primitives: [ {
					name: "p",
					node: 0,
					joints: [],
					inverseBind: new Float32Array(),
					image: 0,
					geometry: g
				} ]
			});
			const actor = ( gid, model, z ) => ({
				gid,
				model,
				pose: { regionId: 257, x: 0, y: 0, z, yaw: 0 },
				clip: "",
				time: 0,
				loop: false,
				scale: 1
			});
			const out = document.createElement( "canvas" );
			out.width = out.height = 64;
			const ctx = out.getContext( "2d" );
			try {
				renderer.setCharacterModel( "hole", model( geometry( 0, [ 1, 0, 0, 1 ] ) ), [ await bitmap( 0 ) ] );
				renderer.setCharacterModel( "solid", model( geometry( 0, [ 0, 1, 0, 1 ] ) ), [ await bitmap( 255 ) ] );
				renderer.setCharacterActors( [ actor( 2, "hole", 5 ), actor( 3, "solid", 8 ) ] );
				renderer.setWorldCamera( {
					originRegion: 257,
					eye: [ 0, 0, 0 ],
					target: [ 0, 0, 1 ],
					fov: 1,
					near: 1,
					far: 100
				} );
				renderer.setWorld( { id: "empty", originRegion: 257, groups: [], warnings: [] } );
				const deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" && performance.now() < deadline ) {
					await new Promise( requestAnimationFrame );
				}
				async function sample() {
					for ( let i = 0; i < 3; i++ ) {
						renderer.frame( { width: 64, height: 64 }, i / 60 );
						await new Promise( requestAnimationFrame );
					}
					renderer.frame( { width: 64, height: 64 }, .1 );
					const image = await createImageBitmap( canvas );
					ctx.drawImage( image, 0, 0 );
					image.close();
					return {
						gid: renderer.pickEntity( .5, .5, 0 ),
						pixel: [ ...ctx.getImageData( 32, 32, 1, 1 ).data ]
					};
				}
				const characterHole = await sample(),
					near = {
						world: renderer.pickEntity( .65, .35, 0 ),
						dock: renderer.pickFrontendCharacter( .65, .35, [ 2, 3 ] )
					},
					world = [];
				for ( const alpha of [ 0, 255, 0 ] ) {
					const g = geometry( 3, [ 1, 0, 0, 1 ] ), path = `alpha:${alpha}`;
					renderer.setWorldTexture( path, await bitmap( alpha ) );
					renderer.setWorld( {
						id: path,
						originRegion: 257,
						warnings: [],
						groups: [ {
							id: path,
							center: [ 0, 0, 3 ],
							radius: 3,
							geometry: g,
							material: { ...g.material, texture: path }
						} ]
					} );
					world.push( await sample() );
				}
				const labels = [];
				for ( const z of [ 8, 2 ] ) {
					renderer.setWorldTexture( "solid-rock", await bitmap( 255 ) );
					const g = geometry( 3, [ 1, 0, 0, 1 ] );
					renderer.setWorld( {
						id: "rock",
						originRegion: 257,
						warnings: [],
						groups: [ {
							id: "rock",
							center: [ 0, 0, 3 ],
							radius: 3,
							geometry: g,
							material: { ...g.material, texture: "solid-rock" }
						} ]
					} );
					renderer.setUi( {
						revision: z,
						width: 64,
						height: 64,
						quads: [ {
							texture: "",
							rect: [ -4, -4, 8, 8 ],
							uv: [ 0, 0, 1, 1 ],
							clip: [ 0, 0, 64, 64 ],
							color: [ 0, 0, 1, 1 ],
							worldAnchor: { regionId: 257, x: 0, y: 0, z }
						} ]
					} );
					labels.push( (await sample()).pixel );
				}
				renderer.setUi( {
					revision: 20,
					width: 64,
					height: 64,
					quads: [ {
						texture: "",
						rect: [ 28, 28, 8, 8 ],
						uv: [ 0, 0, 1, 1 ],
						clip: [ 0, 0, 64, 64 ],
						color: [ 1, 1, 1, 1 ]
					} ]
				} );
				const overlay = (await sample()).pixel;
				renderer.setCharacterActors( [] );
				renderer.setTeleportGates( [ {
					gid: 7,
					kind: "teleport",
					regionId: 257,
					x: 0,
					y: 0,
					z: 5,
					heading: 0,
					teleport: { radius: 1, height: 2 }
				} ] );
				const gate = (await sample()).gid;
				renderer.setTeleportGates( [] );
				const retiredGate = (await sample()).gid;
				return { gate, retiredGate, characterHole, near, world, labels, overlay, error: renderer.error() };
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		assert.equal( result.error, null );
		assert.equal( result.gate, 7 );
		assert.equal( result.retiredGate, null );
		assert.deepEqual( result.labels, [ [ 255, 0, 0, 255 ], [ 0, 0, 255, 255 ] ] );
		assert.deepEqual( result.overlay, [ 255, 255, 255, 255 ] );
		assert.deepEqual( result.near, { world: 3, dock: 2 } );
		assert.deepEqual( result.characterHole, { gid: 2, pixel: [ 0, 255, 0, 255 ] } );
		assert.deepEqual( result.world, [
			{ gid: 2, pixel: [ 0, 255, 0, 255 ] },
			{ gid: 2, pixel: [ 255, 0, 0, 255 ] },
			{ gid: 2, pixel: [ 0, 255, 0, 255 ] }
		] );
	} finally {
		await browser.close();
	}
} );
