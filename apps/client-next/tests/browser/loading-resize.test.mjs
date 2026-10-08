/*
===========================================================================
loading-resize.test.mjs - real loading artwork through GPU viewport changes

Preserve native art proportions and compact background containment across
destination branches, including travel that intentionally omits the scene.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"loading artwork retains its shape through GPU resize across shared loading branches",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		const rows = [];
		try {
			await mkdir( "temp/artifacts/loading-resize", { recursive: true } );
			await page.route( CLIENT_NEXT_BASE_URL + "/", route =>
				route.fulfill( {
					contentType: "text/html",
					body:
						"<!doctype html><style>html,body{margin:0;background:black}canvas{display:block;width:100vw;height:100vh}</style><canvas></canvas>"
				} ) );
			await page.goto( CLIENT_NEXT_BASE_URL + "/" );
			await page.evaluate( async () => {
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const { travelLoadingQuads, loadingScreenQuads } = await import(
					"/src/engine/foundation/ui/mission-loading.ts"
				);
				const canvas = document.querySelector( "canvas" ),
					renderer = createRenderer( canvas ),
					textures = new Map();
				let revision = 0;
				const started = performance.now();
				while ( renderer.phase() === "starting" && performance.now() - started < 10000 ) {
					await new Promise( requestAnimationFrame );
				}
				window.loadingResize = {
					dispose: () => renderer.dispose(),
					async draw( mode ) {
						const width = innerWidth, height = innerHeight;
						const quads = mode === "europe-create" ?
							loadingScreenQuads(
								width,
								height,
								"/assets/images/Media_extracted/interface/loading/loading_charactercustom_europe.png",
								.75
							) :
							travelLoadingQuads( width, height, { mode, region: 0x61a8, revision: 1 }, 1, .75 );
						for ( const q of quads ) {
							if ( q.texture && !textures.has( q.texture ) ) {
								const response = await fetch( q.texture );
								if ( !response.ok ) throw Error( "Missing loading artwork: " + q.texture );
								const image = await createImageBitmap( await response.blob() );
								textures.set( q.texture, [ image.width, image.height ] );
								renderer.setUiTexture( q.texture, image );
							}
						}
						renderer.setUi( { revision: ++revision, width, height, quads } );
						renderer.frame( { width, height }, 0 );
						if ( renderer.phase() !== "running" ) {
							throw Error( renderer.error() );
						}
						const label = quads.find( q => q.texture.endsWith( "/nowloading.png" ) ),
							source = textures.get( label.texture );
						const bitmap = await createImageBitmap( canvas ), copy = document.createElement( "canvas" );
						copy.width = width;
						copy.height = height;
						const ctx = copy.getContext( "2d" );
						ctx.drawImage( bitmap, 0, 0 );
						bitmap.close();
						const pixels = ctx.getImageData(
							label.rect[0],
							label.rect[1],
							Math.ceil( label.rect[2] ),
							Math.ceil( label.rect[3] )
						).data;
						let gold = 0;
						for ( let i = 0; i < pixels.length; i += 4 ) {
							if ( pixels[i] > 70 && pixels[i + 1] > 40 && pixels[i] > pixels[i + 2] * 1.4 ) gold++;
						}
						return {
							width,
							height,
							mode,
							rect: label.rect,
							source,
							gold,
							background: mode === 6 ? null : quads[1].rect
						};
					}
				};
			} );
			for (
				const [width, height] of [
					[ 360, 858 ],
					[ 375, 667 ],
					[ 667, 375 ],
					[ 582, 723 ],
					[ 1920, 1080 ],
					[ 3440, 1440 ],
					[ 1024, 768 ],
					[ 582, 723 ]
				]
			) {
				await page.setViewportSize( { width, height } );
				for ( const mode of [ 0, 1, 2, 3, 4, 5, 6, "europe-create" ] ) {
					const row = await page.evaluate( mode => window.loadingResize.draw( mode ), mode );
					rows.push( row );
					assert.ok( Math.abs( row.rect[2] / row.rect[3] - row.source[0] / row.source[1] ) < 1e-10 );
					assert.ok( row.gold > 10, JSON.stringify( row ) );
					if ( (width < 800 || height < 600) && row.background ) {
						const [x, y, w, h] = row.background;
						assert.ok( x >= 0 && y >= 0 && x + w <= width + 1e-9 && y + h <= height + 1e-9 );
						assert.ok( Math.abs( w / h - 4 / 3 ) < 1e-10 );
					}
					if ( mode === 0 ) {
						await page.screenshot( {
							path: `temp/artifacts/loading-resize/${width}x${height}.png`
						} );
					}
				}
			}
			await writeFile( "temp/artifacts/loading-resize/results.json", JSON.stringify( rows, null, 2 ) );
			await page.evaluate( () => window.loadingResize.dispose() );
		} finally {
			await browser.close();
		}
	}
);
