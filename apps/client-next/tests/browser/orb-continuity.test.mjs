/*
===========================================================================

orb-continuity.test.mjs - kill reward orbs across root cycles

Reward orbs stay visible through native root cycles until they arrive.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";
test( "kill reward orbs remain visible through native root cycles until arrival", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createEffectPrograms } = await import(
				"/src/engine/runtime/assets/worker/effects/program/program.ts"
			);
			const { createOrbs } = await import( "/src/engine/runtime/characters/orbs/orbs.ts" );
			const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
			const canvas = document.createElement( "canvas" ),
				renderer = createRenderer( canvas ),
				out = document.createElement( "canvas" );
			out.width = out.height = 128;
			const ctx = out.getContext( "2d" ), decoder = createEffectPrograms();
			const bytes = new Uint8Array( await (await fetch( "/assets/effects/programs.json" )).arrayBuffer() ),
				durations = new Map(),
				results = [];
			const names = [ "hwan_g", "hwan_y", "hwan_v", "hwn_blue_indraft", "hwn_red_indraft", "hwn_violet_indraft" ];
			const id = name => "/assets/effects/programs.json#" + encodeURIComponent( "battle/" + name + ".efp" );
			try {
				const start = performance.now();
				while ( renderer.phase() === "starting" && performance.now() - start < 15000 ) {
					await new Promise( requestAnimationFrame );
				}
				if ( renderer.phase() !== "running" ) throw Error( renderer.error() );
				renderer.setWorld( { id: "orb-continuity", originRegion: 0, groups: [], warnings: [] } );
				for ( const name of names ) {
					const { model, imagePaths } = decoder.decode( bytes, "battle/" + name + ".efp" ),
						images = await Promise.all( imagePaths.map( async path => {
							const response = await fetch( path );
							if ( !response.ok ) throw Error( "Texture " + response.status );
							return createImageBitmap( await response.blob(), {
								premultiplyAlpha: "none",
								colorSpaceConversion: "none"
							} );
						} ) );
					renderer.setCharacterModel( id( name ), {
						...model,
						images: images.map( b => ({ width: b.width, height: b.height }) )
					}, images );
					durations.set( id( name ), model.clips[0].duration );
				}
				async function render( rows, position ) {
					renderer.setWorldCamera( {
						eye: [ position.x, position.y + 5, position.z - 25 ],
						target: [ position.x, position.y, position.z ],
						originRegion: 0,
						fov: 1,
						near: .1,
						far: 100
					} );
					renderer.setCharacterActors( rows );
					renderer.frame( { width: 128, height: 128 }, 0 );
					if ( renderer.error() ) throw Error( renderer.error() );
					const image = await createImageBitmap( canvas );
					ctx.drawImage( image, 0, 0 );
					image.close();
					return ctx.getImageData( 0, 0, 128, 128 ).data;
				}
				const entities = [ { gid: 1, kind: "monster", regionId: 0, x: 1500, y: 0, z: 0 }, {
					gid: 2,
					kind: "local-player",
					regionId: 0,
					x: 0,
					y: 0,
					z: 0
				} ];
				const background = await render( [], { x: 0, y: 0, z: 0 } );
				for ( let color = 0; color < 3; color++ ) {
					const orbs = createOrbs( () => {}, createPresentationRandom( 1 ) );
					orbs.receive( [ { kind: "orb-feedback", source: 1, target: 2, color, count: 1 } ], entities );
					const samples = [];
					let arrival = false, first = "", last = "";
					for ( let frame = 0; frame < 240; frame++ ) {
						const rows = orbs.step(
							entities,
							frame / 20,
							new Set( [ 1 ] ),
							() => null,
							() => true,
							path => durations.get( path )
						);
						if ( !rows.length ) {
							await render( [], { x: 0, y: 0, z: 0 } );
							continue;
						}
						const actor = rows[0], pixels = await render( rows, actor.pose );
						if ( !actor.loop ) {
							arrival = true;
							continue;
						}
						if ( frame < 10 ) continue;
						let changed = 0;
						for ( let n = 0; n < pixels.length; n += 4 ) {
							if (
								pixels[n] !== background[n] || pixels[n + 1] !== background[n + 1] ||
								pixels[n + 2] !== background[n + 2]
							) changed++;
						}
						samples.push( { frame, time: actor.time, changed } );
						if ( !first ) first = out.toDataURL();
						last = out.toDataURL();
					}
					results.push( { color, samples, arrival, first, last } );
					orbs.reset();
					await render( [], { x: 0, y: 0, z: 0 } );
				}
				await render( [], { x: 15, y: 0, z: 0 } );
				let previousPixels, changedFrames = 0, samples = 0;
				for ( let frame = 0; frame < 120; frame++ ) {
					const time = frame / 60,
						actor = {
							gid: 77,
							model: id( "hwan_v" ),
							clip: "effect",
							time,
							loop: true,
							scale: 1,
							pose: { regionId: 0, x: time * 15, y: 0, z: 0, yaw: 0 }
						};
					const pixels = await render( [ actor ], { x: 15, y: 0, z: 0 } );
					if ( frame >= 30 && previousPixels ) {
						samples++;
						if ( pixels.some( ( v, i ) => v !== previousPixels[i] ) ) changedFrames++;
					}
					previousPixels = pixels.slice();
				}
				return { colors: results, smoothness: { samples, changedFrames } };
			} finally {
				renderer.dispose();
			}
		} );
		assert.ok(
			result.smoothness.changedFrames >= result.smoothness.samples * .9,
			JSON.stringify( result.smoothness )
		);
		await mkdir( "temp/artifacts/orb-continuity", { recursive: true } );
		for ( const row of result.colors ) {
			for ( const name of [ "first", "last" ] ) {
				await writeFile(
					`temp/artifacts/orb-continuity/${row.color}-${name}.png`,
					Buffer.from( row[name].split( "," )[1], "base64" )
				);
				delete row[name];
			}
		}
		await writeFile( "temp/artifacts/orb-continuity/result.json", JSON.stringify( result, null, 2 ) );
		for ( const row of result.colors ) {
			assert.ok( row.samples.length > 40 );
			assert.ok(
				row.samples.every( sample => sample.changed > 0 ),
				JSON.stringify( { color: row.color, blank: row.samples.filter( s => !s.changed ) } )
			);
			assert.ok( row.arrival, "orb must eventually arrive" );
		}
	} finally {
		await browser.close();
	}
} );
