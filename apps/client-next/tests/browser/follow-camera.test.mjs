/*
===========================================================================

follow-camera.test.mjs - collision pixels and the renderer's copied world view

The diagnostic view must describe the camera that rendered the frame, including
collision shortening, viewport changes and region origin. Reading or changing a
returned snapshot must never change picking or a later frame.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test( "follow collision puts an obstructing wall behind the camera on the GPU", { timeout: 45000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const initialView = renderer.worldView();
			/*
			================
			identity
			================
			*/
			const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
			/*
			================
			quad
			================
			*/
			const quad = ( id, z, color ) => ({
				id,
				center: [ 0, 15, z ],
				radius: 150,
				material: { color, alphaCutoff: 0, blend: false, doubleSided: true, unlit: true },
				geometry: {
					positions: new Float32Array( [ -100, -100, z, 100, -100, z, 100, 100, z, -100, 100, z ] ),
					normals: new Float32Array( 12 ),
					uvs: new Float32Array( 8 ),
					indices: new Uint32Array( [ 0, 1, 2, 0, 2, 3 ] ),
					instances: identity(),
					transform: identity()
				}
			});
			const output = document.createElement( "canvas" );
			output.width = output.height = 64;
			const ctx = output.getContext( "2d" );
			if ( !ctx ) throw Error( "Missing 2D context" );
			try {
				const deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" && performance.now() < deadline ) {
					await new Promise( requestAnimationFrame );
				}
				renderer.setWorld( {
					id: "camera-wall",
					originRegion: 0,
					warnings: [],
					groups: [ quad( "wall", 30, [ 1, 0, 0, 1 ] ), quad( "target", 0, [ 0, 1, 0, 1 ] ) ]
				} );
				/*
				================
				sample
				================
				*/
				async function sample( camera, width = 64 ) {
					renderer.setWorldCamera( camera );
					for ( let i = 0; i < 3; i++ ) {
						await renderer.frame( { width, height: 64 }, i / 60 );
						await new Promise( requestAnimationFrame );
					}
					await renderer.frame( { width, height: 64 }, .1 );
					const view = renderer.worldView();
					if ( !view ) throw Error( renderer.error() ?? "Missing prepared world view" );
					const matrix = [ ...view.matrix ];
					view.matrix.fill( 0 );
					const nextView = renderer.worldView();
					const isolated = nextView && matrix.every( ( value, index ) => value === nextView.matrix[index] );
					output.width = width;
					const image = await createImageBitmap( canvas );
					ctx.drawImage( image, 0, 0 );
					image.close();
					return {
						pixels: [ ...ctx.getImageData( width / 2, 32, 1, 1 ).data ],
						matrix,
						originRegion: view.originRegion,
						width: view.width,
						height: view.height,
						isolated,
						wallW: matrix[7] * 15 + matrix[11] * 30 + matrix[15]
					};
				}
				const base = { eye: [ 0, 15, 80 ], target: [ 0, 15, 0 ], fov: 1, near: 1, far: 3500 };
				const blocked = await sample( base ),
					corrected = await sample( {
						...base,
						target: [ 0, 0, 0 ],
						follow: { yaw: 0, pitch: 0, distance: 80 }
					} );
				const decoration = quad( "decoration", 30, [ 1, 0, 0, 1 ] );
				decoration.collision = [];
				renderer.setWorld( {
					id: "camera-decoration",
					originRegion: 257,
					warnings: [],
					groups: [ decoration, quad( "target", 0, [ 0, 1, 0, 1 ] ) ]
				} );
				const decorative = await sample( {
					...base,
					target: [ 0, 0, 0 ],
					follow: { yaw: 0, pitch: 0, distance: 80 }
				} );
				const resized = await sample( base, 128 );
				renderer.dispose();
				return {
					initialView,
					blocked,
					corrected,
					decorative,
					resized,
					disposedView: renderer.worldView(),
					error: renderer.error()
				};
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		assert.equal( result.error, null );
		assert.equal( result.initialView, null );
		assert.equal( result.disposedView, null );
		assert.deepEqual( result.blocked.pixels, [ 255, 0, 0, 255 ] );
		assert.deepEqual( result.corrected.pixels, [ 0, 255, 0, 255 ] );
		assert.ok( result.blocked.wallW > 0 );
		assert.ok(
			result.corrected.wallW < 0,
			"copied projection puts the obstructing wall behind the corrected camera"
		);
		for ( const sample of [ result.blocked, result.corrected, result.decorative, result.resized ] ) {
			assert.equal( sample.isolated, true, "diagnostics cannot mutate the renderer's projection" );
			assert.equal( sample.height, 64 );
		}
		assert.equal( result.blocked.originRegion, 0 );
		assert.equal( result.decorative.originRegion, 257 );
		assert.equal( result.resized.width, 128 );
		assert.equal( result.resized.matrix[0], result.blocked.matrix[0] / 2, "view reflects the new aspect ratio" );
		assert.deepEqual(
			result.decorative.pixels,
			[ 255, 0, 0, 255 ],
			"non-colliding decoration remains drawn without shortening the camera"
		);
	} finally {
		await browser.close();
	}
} );
