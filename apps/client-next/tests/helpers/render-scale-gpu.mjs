/*
===========================================================================

render-scale-gpu.mjs - deterministic scene, occlusion and sharp UI witnesses

Real renderer captures are shared by the browser test and native-off review.
One triangle edge reveals scaling; UI markers reveal missing or blurred labels.

===========================================================================
*/

/*
================
captureRenderScale
================
*/
export async function captureRenderScale( { nativeOnly = false, recover = false } = {} ) {
	const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
	const { experimentalOptions, experimentalVideo } = await import(
		"/src/engine/foundation/ui/experimental-options.ts"
	);
	const { defaultVideoOptions, changeVideo } = await import( "/src/engine/foundation/rendering/video-options.ts" );
	const canvas = document.createElement( "canvas" ), copy = document.createElement( "canvas" );
	const ctx = copy.getContext( "2d", { willReadFrequently: true } );
	if ( !ctx ) throw Error( "No capture context" );
	const devices = [], requestDevice = GPUAdapter.prototype.requestDevice;
	if ( recover ) {
		GPUAdapter.prototype.requestDevice = async function( descriptor ) {
			const device = await requestDevice.call( this, descriptor );
			devices.push( device );
			return device;
		};
	}
	const renderer = createRenderer( canvas );
	const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	const rows = [];
	try {
		if ( recover ) {
			renderer.experimentalVideo(
				experimentalVideo( experimentalOptions( { renderScale: 50, hdrToneMap: true } ) )
			);
		}
		const deadline = performance.now() + 15000;
		while ( renderer.phase() === "starting" && performance.now() < deadline ) {
			await new Promise( requestAnimationFrame );
		}
		if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "GPU startup deadline" );
		renderer.setWorldCamera( { eye: [ 0, 0, -10 ], target: [ 0, 0, 0 ], fov: 1, near: 1, far: 100 } );
		renderer.setWorld( {
			id: "render-scale-witness",
			originRegion: 257,
			warnings: [],
			groups: [ {
				id: "triangle",
				center: [ 0, 0, 0 ],
				radius: 100,
				material: {
					color: [ .8, .2, .1, 1 ],
					alphaCutoff: 0,
					blend: false,
					unlit: true,
					doubleSided: true,
					fogDisabled: true
				},
				geometry: {
					world: true,
					positions: Float32Array.of( -30, -30, 0, 30, -30, 0, 0, 15, 0 ),
					normals: Float32Array.of( 0, 0, -1, 0, 0, -1, 0, 0, -1 ),
					uvs: Float32Array.of( 0, 0, 1, 0, .5, 1 ),
					indices: Uint32Array.of( 0, 1, 2 ),
					instances: identity,
					transform: identity
				}
			} ]
		} );
		/*
		================
		capture
		================
		*/
		async function capture( mode, options, bloom = false, width = 129, height = 97 ) {
			if ( !ctx ) throw Error( "No capture context" );
			if ( options !== undefined ) {
				renderer.experimentalVideo( experimentalVideo( experimentalOptions( options ) ) );
				renderer.videoOptions( changeVideo( defaultVideoOptions(), 11, bloom ? 1 : 0 ) );
			}
			const clip = /** @type {import('../../src/engine/contracts/ui').UiRect} */ ([ 0, 0, width, height ]);
			/** @type {(rect: import('../../src/engine/contracts/ui').UiRect, color: import('../../src/engine/contracts/ui').UiQuad['color'], extra?: Partial<import('../../src/engine/contracts/ui').UiQuad>) => import('../../src/engine/contracts/ui').UiQuad} */
			const quad = ( rect, color, extra = {} ) => ({
				rect,
				color,
				texture: "",
				uv: [ 0, 0, 1, 1 ],
				clip,
				...extra
			});
			renderer.setUi( {
				revision: rows.length + 1,
				width,
				height,
				damageText: true,
				quads: [
					quad( [ 10, 10, 7, 9 ], [ 1, 0, 1, 1 ], { depth: .1 } ),
					quad( [ 60, 44, 7, 9 ], [ 0, 1, 1, 1 ], { depth: .9999 } ),
					...Array.from(
						{ length: 9 },
						( _, i ) => quad( [ 100 + i, 10, 1, 9 ], i % 2 ? [ 0, 1, 0, 1 ] : [ 1, 1, 1, 1 ] )
					)
				]
			} );
			for ( let frame = 0; frame < 8; frame++ ) await renderer.frame( { width, height }, .16 );
			if ( renderer.error() ) throw Error( mode + ": " + renderer.error() );
			copy.width = width;
			copy.height = height;
			const image = await createImageBitmap( canvas );
			ctx.drawImage( image, 0, 0 );
			image.close();
			rows.push( {
				mode,
				width: canvas.width,
				height: canvas.height,
				rgba: [ ...ctx.getImageData( 0, 0, width, height ).data ]
			} );
		}
		await capture( "native", {} );
		if ( nativeOnly ) return rows;
		for ( const renderScale of [ 75, 50 ] ) {
			await capture( "plain-" + renderScale, { renderScale } );
			await capture( "bloom-" + renderScale, { renderScale }, true );
			await capture( "hdr-" + renderScale, { renderScale, hdrToneMap: true } );
			await capture( "combined-" + renderScale, {
				renderScale,
				hdrToneMap: true,
				floatBloom: true,
				sunShadow: true,
				perPixelLighting: true,
				sunRays: true
			}, true );
		}
		await capture( "resize", { renderScale: 50 }, false, 173, 111 );
		if ( recover ) {
			await capture( "before-recovery", { renderScale: 50, hdrToneMap: true } );
			const old = devices.at( -1 );
			old.destroy();
			await old.lost;
			const deadline = performance.now() + 15000;
			do {
				await renderer.frame( { width: 129, height: 97 }, .16 );
				await new Promise( requestAnimationFrame );
			} while ( (devices.at( -1 ) === old || renderer.phase() === "starting") && performance.now() < deadline );
			if ( devices.at( -1 ) === old || renderer.phase() !== "running" ) throw Error( "Device did not recover" );
			await capture( "recovered", undefined );
		}
		await capture( "native-again", {} );
		return rows;
	} finally {
		renderer.dispose();
		if ( recover ) GPUAdapter.prototype.requestDevice = requestDevice;
	}
}
