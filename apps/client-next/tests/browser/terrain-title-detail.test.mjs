/*
===========================================================================

terrain-title-detail.test.mjs - native terrain coverage through the GPU

Compare production terrain selection with authored LOD0 using the same
renderer-owned texture demand and native texture upload path.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
import {
	readPublishedAssetJsonSync as read,
	readPublishedAssetBytesSync as bytes
} from "../../../../scripts/lib/publishedAsset.mjs";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
const CAPTURE_SECONDS = 0.16;
test(
	"retail title full-detail terrain prevents ocean showing through the dock surface",
	{ timeout: 120000 },
	async () => {
		const publicRoot = CLIENT_PUBLIC_ROOT + "/";
		const bundle = read( "/assets/world/constantinople/region-694e.json", publicRoot );
		const resources = bundle.objects.resources;
		const refs = resources.bsr.filter( r => r.objectId === 1630 );
		bundle.objects.placements = bundle.objects.placements.filter( p => p.objectId === 1630 );
		resources.bsr = refs;
		resources.meshes = resources.meshes.filter( m => refs.some( r => r.meshPaths.includes( m.sourcePath ) ) );
		resources.materialSets = resources.materialSets.filter( m =>
			refs.some( r => r.materialPaths.includes( m.sourcePath ) )
		);
		for ( const sector of bundle.terrain.sectors ?? [] ) {
			sector.blocks = sector.blocks.filter( b => {
				const x = (sector.sectorX - bundle.source.sectorX) * 6 + b.blockX,
					z = (sector.sectorY - bundle.source.sectorY) * 6 + b.blockZ;
				return x >= 12 && x <= 19 && z >= -1 && z <= 5;
			} );
		}
		const camera = read( "/assets/title/constantinople/manifest.json", publicRoot ).camera;
		const { browser, page } = await launchProbeBrowser();
		try {
			await page.route( "**/assets/**", route => {
				const path = new URL( route.request().url() ).pathname;
				if ( !path.startsWith( "/assets/" ) ) return route.continue();
				try {
					return route.fulfill( { body: Buffer.from( bytes( path, publicRoot ) ) } );
				} catch ( e ) {
					return route.abort();
				}
			} );
			await page.goto( CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( async ( { bundle, camera, captureSeconds } ) => {
				const { decodeDxt1 } = await import( "/src/engine/foundation/assets/dds.ts" );
				const { decodeNativeTexture } = await import(
					"/src/engine/foundation/assets/native-texture.ts"
				);
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const { createWorldDecoder } = await import( "/src/engine/runtime/assets/worker/world/world.ts" );
				const { sampleFrontendCamera, frontendCameraView } = await import(
					"/src/engine/foundation/rendering/frontend-camera.ts"
				);
				const { prepareWorldScene, worldSceneTransfers } = await import(
					"/src/engine/foundation/rendering/world-scene.ts"
				);
				const { createWorldLease } = await import( "/src/engine/runtime/assets/world-lease.ts" );
				const canvas = document.createElement( "canvas" ),
					r = createRenderer(
						canvas,
						(await import( "/src/engine/runtime/random/random.ts" )).createPresentationRandom( 1 )
					),
					out = document.createElement( "canvas" );
				out.width = 1823;
				out.height = 845;
				const ctx = out.getContext( "2d" );
				const scene = { ...createWorldDecoder().decode( bundle, true ), terrainDetail: "full" }, images = [];
				const textures = new Map();
				try {
					r.setWorld( scene );
					for ( const path of r.neededWorldTextures() ) {
						try {
							const blob = await (await fetch( path )).blob();
							if ( path.endsWith( ".dds" ) ) {
								const d = decodeDxt1( new Uint8Array( await blob.arrayBuffer() ) );
								textures.set(
									path,
									await createImageBitmap( new ImageData( d.pixels, d.width, d.height ) )
								);
							} else if ( path.endsWith( ".texture" ) ) {
								// Keep authored levels through the production native upload path.
								textures.set( path, decodeNativeTexture( new Uint8Array( await blob.arrayBuffer() ) ) );
							} else textures.set( path, await createImageBitmap( blob ) );
						} catch ( e ) {
							throw Error( path + ":" + e.message );
						}
					}
					const deadline = performance.now() + 15000;
					while ( r.phase() === "starting" && performance.now() < deadline ) {
						await new Promise( requestAnimationFrame );
					}
					if ( r.phase() !== "running" ) throw Error( r.error() ?? "GPU startup deadline" );
					// The reference submits only authored LOD0 indices, independently of production selection.
					for ( const t of [ .7, .9 ] ) {
						for ( const mode of [ "normal", "adopted", "distance", "lod0" ] ) {
							let groups = scene.groups;
							if ( mode === "lod0" ) {
								groups = groups.map( g =>
									g.ranges ?
										{
											...g,
											ranges: undefined,
											geometry: {
												...g.geometry,
												indices: new Uint32Array(
													g.ranges.filter( x => x.lod === 0 ).flatMap( x =>
														Array.from(
															g.geometry.indices.slice(
																x.indexStart,
																x.indexStart + x.indexCount
															)
														)
													)
												)
											}
										} :
										g
								);
							}
							const input = {
								...scene,
								id: `${t}:${mode}`,
								terrainDetail: mode === "distance" ? "distance" : "full",
								groups
							};
							if ( mode === "adopted" ) {
								const prepared = prepareWorldScene( structuredClone( input ) );
								r.adoptWorld(
									createWorldLease(
										structuredClone( prepared, { transfer: worldSceneTransfers( prepared.scene ) } )
									)
								);
							} else r.setWorld( input );
							r.setWorldCamera( frontendCameraView( sampleFrontendCamera( camera, t ) ) );
							for ( const [p, b] of textures ) {
								r.setWorldTexture(
									p,
									"kind" in b ? structuredClone( b ) : await createImageBitmap( b )
								);
							}
							let k = 0;
							do {
								// Residency work can take a different number of frames per mode.
								// Keep water and environment time identical throughout preparation.
								await r.frame( { width: 1823, height: 845 }, captureSeconds );
								k++;
								await new Promise( requestAnimationFrame );
								if ( r.phase() === "failed" || k > 1000 ) {
									throw Error( JSON.stringify( {
										error: r.error(),
										stats: r.worldStats(),
										missing: r.neededWorldTextures()
									} ) );
								}
							} while ( k < 10 || r.worldStats().pendingGroups || r.worldStats().pendingTextures );
							const bitmap = await createImageBitmap( canvas );
							ctx.drawImage( bitmap, 0, 0 );
							bitmap.close();
							images.push( {
								t,
								mode,
								pixels: Array.from( ctx.getImageData( 1100, 500, 230, 280 ).data )
							} );
						}
					}
					return images;
				} finally {
					r.dispose();
					for ( const b of textures.values() ) if ( !("kind" in b) ) b.close();
				}
			}, { bundle, camera, captureSeconds: CAPTURE_SECONDS } );
			for ( const t of [ .7, .9 ] ) {
				const rows = result.filter( r => r.t === t ),
					normal = rows.find( r => r.mode === "normal" ).pixels,
					reference = rows.find( r => r.mode === "lod0" ).pixels;
				assert.deepEqual( normal, reference, `Full-detail reference at ${t}` );
				assert.deepEqual(
					rows.find( r => r.mode === "adopted" ).pixels,
					reference,
					`Transferred full-detail reference at ${t}`
				);
				if ( t === .7 ) {
					const bad = rows.find( r => r.mode === "distance" ).pixels;
					let changed = 0;
					for ( let i = 0; i < normal.length; i += 4 ) {
						if (
							normal[i] !== bad[i] || normal[i + 1] !== bad[i + 1] || normal[i + 2] !== bad[i + 2]
						) changed++;
					}
					assert.ok(
						changed > 100,
						`Early dock fixture must detect water showing through coarse terrain: ${changed}`
					);
				}
			}
		} finally {
			await browser.close();
		}
	}
);
