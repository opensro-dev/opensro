/*
===========================================================================
environment-gpu.mjs - deterministic retail environment captures

The same production fixture runs against both reviewed trees. Texture demand
comes from the renderer and authored native levels reach the native uploader.
===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetJsonSync, readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";

/*
================
prepareEnvironmentFixture
================
*/
export async function prepareEnvironmentFixture( page, origin ) {
	const root = CLIENT_PUBLIC_ROOT + "/";
	const bundle = readPublishedAssetJsonSync( "/assets/world/constantinople/region-694e.json", root );
	const resources = bundle.objects.resources;
	const refs = resources.bsr.filter( row => row.objectId === 1630 );
	bundle.objects.placements = bundle.objects.placements.filter( row => row.objectId === 1630 );
	resources.bsr = refs;
	resources.meshes = resources.meshes.filter( mesh => refs.some( row => row.meshPaths.includes( mesh.sourcePath ) ) );
	resources.materialSets = resources.materialSets.filter( set =>
		refs.some( row => row.materialPaths.includes( set.sourcePath ) )
	);
	for ( const sector of bundle.terrain.sectors ?? [] ) {
		sector.blocks = sector.blocks.filter( block => {
			const x = (sector.sectorX - bundle.source.sectorX) * 6 + block.blockX;
			const z = (sector.sectorY - bundle.source.sectorY) * 6 + block.blockZ;
			return x >= 12 && x <= 19 && z >= -1 && z <= 5;
		} );
	}
	const camera = readPublishedAssetJsonSync( "/assets/title/constantinople/manifest.json", root ).camera;
	await page.route( origin + "/", route =>
		route.fulfill( {
			contentType: "text/html",
			body: "<!doctype html><html><body></body></html>"
		} ) );
	await page.route( origin + "/assets/**", route => {
		try {
			return route.fulfill( {
				body: Buffer.from( readPublishedAssetBytesSync( new URL( route.request().url() ).pathname, root ) )
			} );
		} catch {
			return route.abort();
		}
	} );
	await page.goto( origin );
	return { bundle, camera };
}

/*
================
captureEnvironment

Serialized by Playwright; no outer runtime state or source rewriting.
================
*/
export async function captureEnvironment( { bundle, camera, features = false } ) {
	const { decodeDxt1 } = await import( "/src/engine/foundation/assets/dds.ts" );
	const { decodeNativeTexture } = await import( "/src/engine/foundation/assets/native-texture.ts" );
	const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
	const { createWorldDecoder } = await import( "/src/engine/runtime/assets/worker/world/world.ts" );
	const { sampleFrontendCamera, frontendCameraView } = await import(
		"/src/engine/foundation/rendering/frontend-camera.ts"
	);
	const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
	const { defaultVideoOptions, changeVideo } = await import( "/src/engine/foundation/rendering/video-options.ts" );
	const width = 912, height = 424, seconds = .16;
	const canvas = document.createElement( "canvas" );
	const output = document.createElement( "canvas" );
	output.width = width;
	output.height = height;
	const context = output.getContext( "2d", { willReadFrequently: true } );
	if ( !context ) throw Error( "No capture context" );
	const renderer = createRenderer( canvas, createPresentationRandom( 1 ) );
	const textures = new Map();
	const rows = [];
	const options = {
		postProcessing: false,
		anisotropicFiltering: false,
		heightFog: false,
		dynamicSun: false,
		terrainRelief: false,
		texturedHorizon: false,
		floatBloom: false
	};
	try {
		const startupDeadline = performance.now() + 15000;
		while ( renderer.phase() === "starting" && performance.now() < startupDeadline ) {
			await new Promise( requestAnimationFrame );
		}
		if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "GPU startup deadline" );
		const decoder = createWorldDecoder();
		const native = { ...decoder.decode( bundle, true ), terrainDetail: "full" };
		const relief = features ?
			{ ...decoder.decode( bundle, true, { terrainNormals: true } ), terrainDetail: "full" } :
			native;
		renderer.videoOptions( changeVideo( defaultVideoOptions(), 11, 1 ) );
		const cases = features ?
			[ "off", "dynamicSun", "off", "terrainRelief", "off", "texturedHorizon", "off", "floatBloom", "off" ] :
			[ "off", "off" ];
		for ( const mode of cases ) {
			renderer.experimentalVideo( { ...options, ...(mode === "off" ? {} : { [mode]: true }) } );
			renderer.setWorld( { ...(mode === "terrainRelief" ? relief : native), id: `environment:${rows.length}` } );
			renderer.setWorldCamera( frontendCameraView( sampleFrontendCamera( camera, .7 ) ) );
			for ( const path of renderer.neededWorldTextures() ) {
				if ( !textures.has( path ) ) {
					const response = await fetch( path );
					if ( !response.ok ) throw Error( `Texture HTTP ${response.status}: ${path}` );
					const blob = await response.blob();
					if ( path.endsWith( ".texture" ) ) {
						textures.set( path, decodeNativeTexture( new Uint8Array( await blob.arrayBuffer() ) ) );
					} else if ( path.endsWith( ".dds" ) ) {
						const image = decodeDxt1( new Uint8Array( await blob.arrayBuffer() ) );
						textures.set(
							path,
							await createImageBitmap( new ImageData( image.pixels, image.width, image.height ) )
						);
					} else textures.set( path, await createImageBitmap( blob ) );
				}
				const texture = textures.get( path );
				renderer.setWorldTexture(
					path,
					"kind" in texture ? structuredClone( texture ) : await createImageBitmap( texture )
				);
			}
			const deadline = performance.now() + 30000;
			let frames = 0;
			do {
				await renderer.frame( { width, height }, seconds );
				frames++;
				if ( renderer.error() || performance.now() > deadline ) {
					throw Error( JSON.stringify( {
						mode,
						error: renderer.error(),
						stats: renderer.worldStats(),
						missing: renderer.neededWorldTextures()
					} ) );
				}
				await new Promise( requestAnimationFrame );
			} while ( frames < 12 || renderer.worldStats().pendingGroups || renderer.worldStats().pendingTextures );
			const image = await createImageBitmap( canvas );
			context.drawImage( image, 0, 0 );
			image.close();
			rows.push( { mode, rgba: [ ...context.getImageData( 0, 0, width, height ).data ] } );
		}
		return { width, height, rows };
	} finally {
		renderer.dispose();
		for ( const texture of textures.values() ) if ( !("kind" in texture) ) texture.close();
	}
}

/*
================
captureFlatRelief

Zero-light channels cannot divide by an artificial epsilon and blacken flat
NOLIGHT ground. This fixture observes the rendered result, not shader text.
================
*/
export async function captureFlatRelief() {
	const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
	const { defaultVideoOptions, changeVideo } = await import( "/src/engine/foundation/rendering/video-options.ts" );
	const canvas = document.createElement( "canvas" );
	const output = document.createElement( "canvas" );
	output.width = output.height = 64;
	const context = output.getContext( "2d", { willReadFrequently: true } );
	if ( !context ) throw Error( "No capture context" );
	const renderer = createRenderer( canvas );
	const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	try {
		const deadline = performance.now() + 15000;
		while ( renderer.phase() === "starting" && performance.now() < deadline ) {
			await new Promise( requestAnimationFrame );
		}
		if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "GPU startup deadline" );
		renderer.videoOptions( changeVideo( defaultVideoOptions(), 11, 0 ) );
		renderer.setWorldCamera( { eye: [ 0, 100, -50 ], target: [ 0, 0, 0 ], fov: 1, near: 1, far: 1000 } );
		renderer.setWorld( {
			id: "flat-relief",
			originRegion: 257,
			warnings: [],
			environment: {
				startTimeOfDay: .5,
				ratePerSecond: 0,
				tracks: {
					color0xf0: [ { t: 0, r: 0, g: 0, b: 0 } ],
					color0x124: [ { t: 0, r: 0, g: 0, b: 0 } ]
				}
			},
			groups: [ {
				id: "ground",
				center: [ 0, 0, 0 ],
				radius: 200,
				material: {
					color: [ .7, .5, .3, 1 ],
					unlit: true,
					terrain: true,
					alphaCutoff: 0,
					blend: false,
					doubleSided: true
				},
				geometry: {
					world: true,
					positions: Float32Array.of( -100, 0, -100, 100, 0, -100, 100, 0, 100, -100, 0, 100 ),
					normals: Float32Array.of( 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ),
					uvs: Float32Array.of( 0, 0, 1, 0, 1, 1, 0, 1 ),
					indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
					instances: identity,
					transform: identity
				}
			} ]
		} );
		const result = [];
		for ( const terrainRelief of [ false, true ] ) {
			renderer.experimentalVideo( {
				postProcessing: false,
				anisotropicFiltering: false,
				heightFog: false,
				dynamicSun: false,
				terrainRelief,
				texturedHorizon: false,
				floatBloom: false
			} );
			for ( let frame = 0; frame < 4; frame++ ) await renderer.frame( { width: 64, height: 64 }, .16 );
			if ( renderer.error() ) throw Error( renderer.error() );
			const image = await createImageBitmap( canvas );
			context.drawImage( image, 0, 0 );
			image.close();
			result.push( [ ...context.getImageData( 16, 16, 32, 32 ).data ] );
		}
		return result;
	} finally {
		renderer.dispose();
	}
}
