/*
===========================================================================

attack-blend.test.mjs - production rig entry and exit blend rendering

Loads the production renderer and action scheduler without rewriting served
source. The hard idle cut is the negative control for natural completion.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "production attack rig blends across natural completion, with abrupt idle as negative control", {
	timeout: 60000
}, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" ),
				{ createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" ),
				{ advanceAction, actionLayers } = await import( "/src/engine/foundation/animation/action-schedule.ts" );
			const assets = createAssets(),
				canvas = document.createElement( "canvas" ),
				renderer = createRenderer( canvas ),
				copy = document.createElement( "canvas" );
			copy.width = copy.height = 256;
			const ctx = copy.getContext( "2d" );
			const path = "/assets/char/china/chinaman_adventurer.glb",
				id = assets.request( new URL( path, location.origin ).href, 16 << 20, "character" );
			let loaded;
			const end = performance.now() + 20000;
			try {
				while ( !(loaded = assets.take( id )) ) {
					if ( performance.now() > end ) throw Error( "Model loading deadline" );
					await new Promise( requestAnimationFrame );
				}
				if ( loaded.kind !== "character" ) throw Error( loaded.error ?? "Wrong asset" );
				renderer.setCharacterModel( path, loaded.model, loaded.images );
				const clip = loaded.model.clips.find( c => c.name === "attack1" );
				if ( !clip ) throw Error( "Missing production attack" );
				const clock = {
					phases: [ null, null, {
						clip: "attack1",
						definition: { durationMs: Math.round( clip.duration * 1000 ), trackEvents: [], soundEvents: [] }
					} ],
					started: 0,
					previous: 0,
					phase: 0,
					entered: false
				};
				const rgb = [ { t: 0, r: .5, g: .5, b: .5 } ];
				renderer.setWorld( {
					id: "attack-blend",
					originRegion: 257,
					groups: [],
					warnings: [],
					environment: { startTimeOfDay: .5, ratePerSecond: 0, tracks: { color0xf0: rgb, color0x124: rgb } }
				} );
				renderer.setWorldCamera( {
					eye: [ 0, 10, -35 ],
					target: [ 0, 10, 0 ],
					originRegion: 257,
					fov: 1,
					near: 1,
					far: 500
				} );
				/*
   ================
   sample
   ================
   */
				async function sample( at, cut = false ) {
					advanceAction( clock, at );
					const layers = cut ? [] : actionLayers( clock, at );
					renderer.setCharacterActors( [ {
						gid: 1,
						model: path,
						pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
						clip: "stand",
						time: 0,
						loop: true,
						scale: 1,
						layers: [ ...layers, { clip: "stand", time: 0, loop: true, weight: 1, lane: "timed" } ]
					} ] );
					for ( let i = 0; i < 6; i++ ) {
						renderer.frame( { width: 256, height: 256 }, 0 );
						if ( renderer.error() ) throw Error( renderer.error() );
						await new Promise( requestAnimationFrame );
					}
					const bitmap = await createImageBitmap( canvas );
					ctx.drawImage( bitmap, 0, 0 );
					bitmap.close();
					return {
						pixels: Array.from( ctx.getImageData( 32, 16, 192, 224 ).data ),
						png: copy.toDataURL(),
						layers
					};
				}
				const endAt = .2 + clock.phases[2].definition.durationMs / 1000;
				const before = await sample( endAt - .005 ),
					after = await sample( endAt + .005 ),
					middle = await sample( endAt + .1 ),
					idle = await sample( endAt + .201 ),
					cut = await sample( endAt + .202, true );
				const distance = ( a, b ) =>
					a.pixels.reduce( ( sum, n, i ) => sum + (i % 4 === 3 ? 0 : Math.abs( n - b.pixels[i] )), 0 );
				return {
					smoothJump: distance( before, after ),
					hardJump: distance( before, cut ),
					settled: distance( idle, cut ),
					images: { before: before.png, after: after.png, middle: middle.png, idle: idle.png },
					layers: { before: before.layers, after: after.layers, middle: middle.layers, idle: idle.layers }
				};
			} finally {
				renderer.dispose();
				assets.dispose();
			}
		} );
		await mkdir( "temp/artifacts/attack-parity/blend", { recursive: true } );
		for ( const [name, png] of Object.entries( result.images ) ) {
			await writeFile(
				"temp/artifacts/attack-parity/blend/" + name + ".png",
				Buffer.from( png.split( "," )[1], "base64" )
			);
		}
		const { images, ...metrics } = result;
		await writeFile( "temp/artifacts/attack-parity/blend/metrics.json", JSON.stringify( metrics, null, 2 ) );
		assert.ok( result.hardJump > 10000, "negative control must visibly change the rig" );
		assert.ok( result.smoothJump < result.hardJump * .4, "natural completion must not snap to idle" );
		assert.equal( result.settled, 0 );
	} finally {
		await browser.close();
	}
} );
