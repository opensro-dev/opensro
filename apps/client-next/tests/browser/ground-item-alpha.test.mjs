/*
===========================================================================

ground-item-alpha.test.mjs - authored item and corpse alpha in the real renderer

Load the published character contract through its worker. Compare the native
alpha threshold with a deliberately low threshold, then verify actor fading
against the same scene background.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test(
	"published gold models discard dark fringe pixels at the native 128 alpha reference",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await holdProbeRuntime( page );
			await page.goto( CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( async () => {
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
				const { loadCharacter } = await import( "/tests/browser/helpers/character-assets.mjs" );
				const assets = createAssets(),
					canvas = document.createElement( "canvas" ),
					renderer = createRenderer( canvas ),
					out = document.createElement( "canvas" );
				out.width = out.height = 256;
				const ctx = out.getContext( "2d" );
				const samples = [];
				try {
					const start = performance.now();
					while ( renderer.phase() === "starting" && performance.now() - start < 15000 ) {
						await new Promise( requestAnimationFrame );
					}
					if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? renderer.phase() );
					renderer.setWorld( { id: "gold-alpha", originRegion: 257, groups: [], warnings: [] } );
					renderer.setWorldCamera( {
						eye: [ 0, 35, -40 ],
						target: [ 0, 0, 0 ],
						originRegion: 257,
						fov: 1,
						near: 1,
						far: 500
					} );
					/*
					================
					pixels

					Render the requested actors against an unchanged background for pixel comparison.
					================
					*/
					async function pixels( rows ) {
						renderer.setCharacterActors( rows );
						renderer.frame( { width: 256, height: 256 }, 0 );
						if ( renderer.error() ) throw Error( renderer.error() );
						const bitmap = await createImageBitmap( canvas );
						ctx.drawImage( bitmap, 0, 0 );
						bitmap.close();
						return { pixels: [ ...ctx.getImageData( 0, 0, 256, 256 ).data ], png: out.toDataURL() };
					}
					const background = await pixels( [] );
					for ( const size of [ "small", "normal", "large" ] ) {
						const { model, images } = await loadCharacter(
							assets,
							"/assets/itemdrop/item/etc/drop_ch_money_" + size + ".glb"
						);
						const cutoffs = model.primitives.map( p => p.geometry.material.alphaCutoff );
						const stale = {
							...model,
							primitives: model.primitives.map( p => ({
								...p,
								geometry: { ...p.geometry, material: { ...p.geometry.material, alphaCutoff: .004 } }
							}) )
						};
						renderer.setCharacterModel( size, model, images );
						renderer.setCharacterModel(
							size + "-stale",
							stale,
							await Promise.all(
								images.map( i => "kind" in i ? structuredClone( i ) : createImageBitmap( i ) )
							)
						);
						const actor = {
							gid: 1,
							groundItem: true,
							model: size,
							clip: "",
							time: 0,
							loop: false,
							scale: 1,
							pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 }
						};
						const fixed = await pixels( [ actor ] ),
							old = await pixels( [ { ...actor, model: size + "-stale" } ] );
						let removed = 0, retained = 0;
						for ( let i = 0; i < fixed.pixels.length; i += 4 ) {
							const same = ( a, b ) => a.slice( i, i + 3 ).every( ( v, c ) => v === b[i + c] );
							if (
								!same( old.pixels, background.pixels ) && same( fixed.pixels, background.pixels )
							) removed++;
							if ( !same( fixed.pixels, background.pixels ) ) retained++;
						}
						samples.push( { size, cutoffs, removed, retained, fixed: fixed.png, stale: old.png } );
					}
					return samples;
				} finally {
					renderer.dispose();
					assets.dispose();
				}
			} );
			await mkdir( "temp/artifacts/ground-items", { recursive: true } );
			for ( const row of result ) {
				for ( const kind of [ "fixed", "stale" ] ) {
					await writeFile(
						"temp/artifacts/ground-items/" + row.size + "-" + kind + ".png",
						Buffer.from( row[kind].split( "," )[1], "base64" )
					);
				}
				assert.ok( row.cutoffs.every( v => v === 128 / 255 ) );
				assert.ok( row.removed > 0, row.size + " must remove fringe pixels" );
				assert.ok( row.retained > 0, row.size + " must retain the gold body" );
				delete row.fixed;
				delete row.stale;
			}
			await writeFile( "temp/artifacts/ground-items/gpu.json", JSON.stringify( result, null, 2 ) );
		} finally {
			await browser.close();
		}
	}
);

test(
	"authored Mangyang corpse and gold retain visible GPU pixels through instance fading",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await holdProbeRuntime( page );
			await page.goto( CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( async () => {
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" ),
					{ createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
				const { loadCharacter } = await import( "/tests/browser/helpers/character-assets.mjs" );
				const canvas = document.createElement( "canvas" ),
					renderer = createRenderer( canvas ),
					assets = createAssets(),
					output = document.createElement( "canvas" );
				output.width = output.height = 256;
				const ctx = output.getContext( "2d" );
				try {
					const deadline = performance.now() + 15000;
					while ( renderer.phase() === "starting" && performance.now() < deadline ) {
						await new Promise( requestAnimationFrame );
					}
					renderer.setWorld( { id: "corpse-fade", originRegion: 257, groups: [], warnings: [] } );
					renderer.setWorldCamera( {
						eye: [ 70, 80, -100 ],
						target: [ 0, 5, 0 ],
						originRegion: 257,
						fov: 1,
						near: 1,
						far: 500
					} );
					/*
					================
					pixels

					Read back the current actor fade without changing the source texture.
					================
					*/
					async function pixels( actors ) {
						renderer.setCharacterActors( actors );
						renderer.frame( { width: 256, height: 256 }, 0 );
						if ( renderer.error() ) throw Error( renderer.error() );
						const bitmap = await createImageBitmap( canvas );
						ctx.drawImage( bitmap, 0, 0 );
						bitmap.close();
						return ctx.getImageData( 0, 0, 256, 256 ).data;
					}
					const background = await pixels( [] ), cases = [];
					for (
						const path of [
							"/assets/npc/mob/china/mangnyang.glb",
							"/assets/itemdrop/item/etc/drop_ch_money_small.glb"
						]
					) {
						const { model, images } = await loadCharacter( assets, path );
						renderer.setCharacterModel( path, model, images );
						const actor = {
								gid: -1073741824,
								model: path,
								clip: path.includes( "/npc/" ) ?
									"deathLoop" :
									model.clips.some( c => c.name === "stand" ) ?
									"stand" :
									"",
								time: 0,
								loop: true,
								scale: 1,
								pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 }
							},
							samples = [];
						for ( const opacity of [ 1, .75, .5, .25, .05, 0 ] ) {
							const data = await pixels( [ { ...actor, opacity } ] );
							let difference = 0, visible = 0;
							for ( let i = 0; i < data.length; i += 4 ) {
								let d = 0;
								for ( let c = 0; c < 3; c++ ) d += Math.abs( data[i + c] - background[i + c] );
								difference += d;
								if ( d > 3 ) visible++;
							}
							samples.push( { opacity, difference, visible } );
						}
						cases.push( { path, samples } );
					}
					return cases;
				} finally {
					renderer.dispose();
					assets.dispose();
				}
			} );
			await writeFile( "temp/artifacts/ground-items/corpse-fade.json", JSON.stringify( result, null, 2 ) );
			for ( const row of result ) {
				for ( const sample of row.samples.slice( 0, -1 ) ) {
					assert.ok(
						sample.visible > 0,
						JSON.stringify( { path: row.path, sample } )
					);
				}
				assert.equal( row.samples.at( -1 ).difference, 0 );
				assert.ok( row.samples[3].difference < row.samples[1].difference );
			}
		} finally {
			await browser.close();
		}
	}
);
