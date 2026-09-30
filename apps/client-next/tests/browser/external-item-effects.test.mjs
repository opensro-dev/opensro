/*
===========================================================================

external-item-effects.test.mjs - authored item effects on the GPU path

Decodes the fireworks, stone and temptation programs and renders them
through the production renderer in a real browser.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";
test(
	"authored fireworks, stone and temptation render through the production GPU path",
	{ timeout: 120000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await holdProbeRuntime( page );
			await page.goto( CLIENT_NEXT_BASE_URL );
			const samples = await page.evaluate( async () => {
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const { createEffectPrograms } = await import(
					"/src/engine/runtime/assets/worker/effects/program/program.ts"
				);
				const canvas = document.createElement( "canvas" ),
					renderer = createRenderer( canvas ),
					decoder = createEffectPrograms(),
					out = document.createElement( "canvas" );
				out.width = out.height = 256;
				const ctx = out.getContext( "2d" );
				const response = await fetch( "/assets/effects/programs.json" );
				if ( !response.ok ) throw Error( "Effect catalog HTTP " + response.status );
				const bytes = new Uint8Array( await response.arrayBuffer() );
				const paths = Object.keys( JSON.parse( new TextDecoder().decode( bytes ) ).effects ).filter( p =>
					p.startsWith( "system/" ) && p.includes( "firework" ) ||
					[
						"battle/status_bad_stone_on.efp",
						"battle/status_bad_temptation.efp",
						"system/item_returnscroll.efp",
						"system/item_returnscroll_use.efp"
					].includes( p )
				);
				const results = [];
				try {
					const start = performance.now();
					while ( renderer.phase() === "starting" && performance.now() - start < 15000 ) {
						await new Promise( requestAnimationFrame );
					}
					if ( renderer.phase() !== "running" ) throw Error( renderer.error() );
					renderer.setWorld( { id: "fireworks", originRegion: 257, groups: [], warnings: [] } );
					renderer.setWorldCamera( {
						eye: [ 0, 140, -350 ],
						target: [ 0, 100, 0 ],
						originRegion: 257,
						fov: 1,
						near: 1,
						far: 2000
					} );
					async function snapshot( rows ) {
						renderer.setCharacterActors( rows );
						renderer.frame( { width: 256, height: 256 }, 0 );
						if ( renderer.error() ) throw Error( renderer.error() );
						const bitmap = await createImageBitmap( canvas );
						ctx.drawImage( bitmap, 0, 0 );
						bitmap.close();
						return { pixels: ctx.getImageData( 0, 0, 256, 256 ).data, png: out.toDataURL() };
					}
					const background = (await snapshot( [] )).pixels;
					for ( const path of paths ) {
						renderer.setWorldCamera(
							path.startsWith( "battle/" ) ?
								{
									eye: [ 0, 10, -30 ],
									target: [ 0, 8, 0 ],
									originRegion: 257,
									fov: 1,
									near: 1,
									far: 2000
								} :
								{
									eye: [ 0, 140, -350 ],
									target: [ 0, 100, 0 ],
									originRegion: 257,
									fov: 1,
									near: 1,
									far: 2000
								}
						);
						const { model, imagePaths } = decoder.decode( bytes, path );
						const images = await Promise.all( imagePaths.map( async p => {
							const r = await fetch( p );
							if ( !r.ok ) throw Error( "Texture HTTP " + r.status + " " + p );
							return createImageBitmap( await r.blob(), {
								premultiplyAlpha: "none",
								colorSpaceConversion: "none"
							} );
						} ) );
						renderer.setCharacterModel( path, {
							...model,
							images: images.map( b => ({ width: b.width, height: b.height }) )
						}, images );
						let best = { changed: 0, png: "" };
						for ( let frame = 0; frame < 100; frame++ ) {
							const shot = await snapshot( [ {
								gid: 1,
								model: path,
								clip: "effect",
								time: frame / 20,
								loop: false,
								scale: 1,
								pose: { regionId: 257, x: 0, y: 8, z: 0, yaw: 0 }
							} ] );
							let changed = 0;
							for ( let i = 0; i < shot.pixels.length; i += 4 ) {
								if (
									shot.pixels[i] !== background[i] || shot.pixels[i + 1] !== background[i + 1] ||
									shot.pixels[i + 2] !== background[i + 2]
								) changed++;
							}
							if ( changed > best.changed ) best = { changed, png: shot.png };
						}
						results.push( { path, ...best } );
						await snapshot( [] );
						renderer.retainCharacterModels( [] );
					}
					return results;
				} finally {
					renderer.dispose();
				}
			} );
			await mkdir( "temp/artifacts/ground-items/fireworks", { recursive: true } );
			for ( const path of [ "system/item_returnscroll.efp", "system/item_returnscroll_use.efp" ] ) {
				assert.ok(
					samples.some( s => s.path === path ),
					path
				);
			}
			for ( const sample of samples ) {
				assert.ok( sample.changed > 0, sample.path );
				await writeFile(
					"temp/artifacts/ground-items/fireworks/" + sample.path.split( "/" ).at( -1 ) + ".png",
					Buffer.from( sample.png.split( "," )[1], "base64" )
				);
				delete sample.png;
			}
			await writeFile( "temp/artifacts/ground-items/fireworks/gpu.json", JSON.stringify( samples, null, 2 ) );
		} finally {
			await browser.close();
		}
	}
);
