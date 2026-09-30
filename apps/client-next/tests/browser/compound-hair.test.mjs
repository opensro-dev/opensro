/*
===========================================================================

compound-hair.test.mjs - published berserk hair reaches the head on the GPU

Uses the production decoder, assembly and renderer with authored resources.
Pixel positions catch a sideways attachment that actor-count assertions miss.
The ordinary socket is a negative control, never a rewritten served module.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { readPublishedAssetBytesSync, readPublishedAssetJsonSync } from "../../../../scripts/lib/publishedAsset.mjs";

const WIDTH = 800;
const HEIGHT = 600;
const MAX_HEAD_OFFSET_PX = 25;

test(
	"published male and female berserk hair covers the head instead of attaching sideways",
	{ timeout: 120000 },
	async () => {
		const root = path.resolve( "../../.generated/client-public" );
		const roster = readPublishedAssetJsonSync( "/assets/char/roster.json", root );
		const directory = "temp/artifacts/compound-hair";
		await mkdir( directory, { recursive: true } );
		const { browser, page } = await launchProbeBrowser();
		const errors = [];
		page.on( "pageerror", error => errors.push( String( error ) ) );
		const results = [];
		try {
			// A same-origin data document permits production module imports without booting a session.
			await page.goto( CLIENT_NEXT_BASE_URL + "/assets/char/roster.json" );
			await page.setContent( '<html><body style="margin:0"><canvas></canvas></body></html>' );
			for (
				const [prefix, codename] of [ [ "CH_M", "CHAR_CH_MAN_ADVENTURER" ], [
					"CH_W",
					"CHAR_CH_WOMAN_ADVENTURER"
				] ]
			) {
				const body = roster.models.find( row => row.codename === codename );
				assert.ok( body );
				const hair = roster.dress.hwan[prefix];
				const data = [ body.glb, hair.glb ].map(
					resource => [ ...readPublishedAssetBytesSync( resource, root ) ]
				);
				for ( const compound of [ false, true ] ) {
					const result = await page.evaluate( async ( { data, hair, covers, compound, width, height } ) => {
						const decoderPath = "/src/engine/runtime/assets/worker/model/model.ts";
						const posePath = "/src/engine/foundation/animation/animation-pose.ts";
						const rendererPath = "/src/engine/runtime/renderer/renderer.ts";
						const texturePath = "/src/engine/foundation/assets/native-texture.ts";
						const { createModelDecoder } = await import( decoderPath );
						const { createCharacterPose } = await import( posePath );
						const { createRenderer } = await import( rendererPath );
						const { decodeNativeTexture, NATIVE_TEXTURE_MIME } = await import( texturePath );
						const decoder = createModelDecoder();
						const models = data.map( bytes =>
							decoder.character( decoder.decode( Uint8Array.from( bytes ) ) )
						);
						const canvas = document.querySelector( "canvas" );
						const renderer = createRenderer( canvas );
						try {
							for ( const [index, model] of models.entries() ) {
								const images = await Promise.all(
									model.images.map( image =>
										image.mime === NATIVE_TEXTURE_MIME ?
											decodeNativeTexture( image.bytes ) :
											createImageBitmap( new Blob( [ image.bytes ], { type: image.mime } ), {
												premultiplyAlpha: "none",
												colorSpaceConversion: "none"
											} )
									)
								);
								model.images = images.map( image => ({ width: image.width, height: image.height }) );
								renderer.setCharacterModel( String( index ), model, images );
							}
							renderer.setCharacterAssembly( "body", "0", [ { model: "1", parts: [], covers } ] );
							const pose = createCharacterPose( models[0] );
							pose.evaluate( "stand", 0 );
							const head = pose.socket( hair.bone );
							const actor = {
								gid: 1,
								model: "body",
								pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
								clip: "stand",
								time: 0,
								loop: true,
								scale: 1
							};
							renderer.setCharacterActors( [ actor, {
								...actor,
								gid: 2,
								model: "1",
								shadowAttachment: true,
								attachment: {
									gid: 1,
									bone: hair.bone,
									offset: [ 0, 0, 0 ],
									basis: compound ? "compound" : undefined
								}
							} ] );
							renderer.setWorld( { id: "hair", originRegion: 257, groups: [], warnings: [] } );
							renderer.setWorldCamera( {
								eye: [ 0, head[13] + 2, -35 ],
								target: [ 0, head[13], 0 ],
								originRegion: 257,
								fov: .7,
								near: .1,
								far: 500
							} );
							const deadline = performance.now() + 15000;
							do {
								renderer.frame( { width, height }, 0 );
								if ( renderer.error() ) throw Error( renderer.error() );
								await new Promise( requestAnimationFrame );
							} while ( !renderer.characterStats().draws && performance.now() < deadline );
							renderer.frame( { width, height }, 0 );
							const copy = document.createElement( "canvas" );
							copy.width = width;
							copy.height = height;
							const context = copy.getContext( "2d" );
							if ( !context || !canvas ) throw Error( "Missing capture surface" );
							context.drawImage( canvas, 0, 0 );
							const pixels = context.getImageData( 0, 0, width, height ).data;
							let count = 0, sumX = 0, sumY = 0;
							// The authored red hair is isolated from skin and the lower-body clothing.
							for ( let y = 150; y < 340; y++ ) {
								for ( let x = 250; x < 550; x++ ) {
									const at = (y * width + x) * 4;
									if (
										pixels[at] > 100 && pixels[at] > pixels[at + 1] * 2 &&
										pixels[at] > pixels[at + 2] * 2
									) {
										count++;
										sumX += x;
										sumY += y;
									}
								}
							}
							return {
								count,
								x: sumX / count,
								y: sumY / count,
								stats: renderer.characterStats(),
								png: copy.toDataURL()
							};
						} finally {
							renderer.dispose();
						}
					}, {
						data,
						hair,
						covers: hair.covers.HWAN_HAIR.map( key => body.cover[key] ),
						compound,
						width: WIDTH,
						height: HEIGHT
					} );
					const name = prefix + (compound ? "-compound" : "-ordinary");
					await writeFile(
						directory + "/" + name + ".png",
						Buffer.from( result.png.split( "," )[1], "base64" )
					);
					const { png, ...metrics } = result;
					results.push( { name, ...metrics } );
					assert.equal( result.stats.visibleActors, 2 );
					assert.ok( result.count > 100, "Hair must produce visible pixels" );
					assert.equal(
						Math.abs( result.x - WIDTH / 2 ) < MAX_HEAD_OFFSET_PX,
						compound,
						name + " horizontal alignment"
					);
					if ( compound ) assert.ok( result.y < HEIGHT / 2, name + " must cover the upper head" );
				}
			}
			assert.deepEqual( errors, [] );
		} finally {
			await writeFile( directory + "/report.json", JSON.stringify( { errors, results }, null, 2 ) + "\n" );
			await browser.close();
		}
	}
);
