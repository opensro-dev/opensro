/*
===========================================================================

arrow-trail.test.mjs - a projectile and its trail through WebGPU

The published arrow flies with its mirage bow trail attached to its bone:
both render and animate in flight, the trail drains after arrival with
the arrow hidden, and nothing is left once both are gone.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test(
	"published arrow and trail resources render through WebGPU, retain sockets during drain and restore empty pixels",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await holdProbeRuntime( page );
			await page.goto( CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( async () => {
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
				const { createCharacterResources } = await import(
					"/src/engine/runtime/characters/resources/resources.ts"
				);
				const { projectileBasis } = await import( "/src/engine/foundation/animation/moving-stage.ts" );
				const canvas = document.createElement( "canvas" ),
					renderer = createRenderer( canvas ),
					assets = createAssets(),
					resources = createCharacterResources( assets, renderer, location.origin );
				const output = document.createElement( "canvas" );
				output.width = output.height = 256;
				const context = output.getContext( "2d" );
				const manifest = await (await fetch( "/assets/skillfx/manifest.json" )).json(),
					arrow = manifest.models["res/item/china/weapon/cha_arrow_normal.bsr"],
					trail = "/assets/effects/programs.json#skill%2Fchina%2Fmirage_bow_fire.efp";
				const source = { regionId: 257, x: -40, y: 0, z: 0, yaw: 0 },
					end = { ...source, x: 40 },
					body = {
						gid: -1,
						model: arrow.glb,
						pose: source,
						clip: arrow.clips[0] ?? "",
						time: 0,
						loop: true,
						scale: 1,
						effectBasis: projectileBasis( source, end )
					},
					child = {
						gid: -2,
						model: trail,
						pose: source,
						clip: "effect",
						time: 0,
						loop: true,
						scale: 1,
						attachment: { gid: -1, bone: "Bone01", offset: [ 0, 0, 0 ], rootIfMissing: true }
					};
				const hash = async () => {
					const bitmap = await createImageBitmap( canvas );
					context.drawImage( bitmap, 0, 0 );
					bitmap.close();
					const pixels = context.getImageData( 0, 0, 256, 256 ).data;
					return [ ...new Uint8Array( await crypto.subtle.digest( "SHA-256", pixels ) ) ].map( n =>
						n.toString( 16 ).padStart( 2, "0" )
					).join( "" );
				};
				const render = async ( rows, time ) => {
					renderer.setCharacterActors( rows );
					renderer.frame( { width: 256, height: 256 }, time );
					if ( renderer.error() ) throw Error( renderer.error() );
					return hash();
				};
				try {
					const limit = performance.now() + 20000;
					while ( renderer.phase() === "starting" && performance.now() < limit ) {
						await new Promise( requestAnimationFrame );
					}
					renderer.setWorld( { id: "arrow-reference", originRegion: 257, groups: [], warnings: [] } );
					renderer.setWorldCamera( {
						eye: [ 0, 25, -140 ],
						target: [ 0, 0, 0 ],
						originRegion: 257,
						fov: 1,
						near: 1,
						far: 1000
					} );
					while ( performance.now() < limit ) {
						resources.begin( 0 );
						resources.poll();
						const a = resources.ready( arrow.glb ), b = resources.ready( trail );
						if ( resources.error() ) throw Error( resources.error() );
						if ( a && b ) break;
						await new Promise( requestAnimationFrame );
					}
					if ( !resources.ready( arrow.glb ) || !resources.ready( trail ) ) {
						throw Error( "Arrow resource admission deadline" );
					}
					const empty = await render( [], 0 ), arrowOnly = await render( [ body ], 0 ), samples = [];
					for ( let i = 0; i <= 28; i++ ) {
						const time = i / 20;
						await render( [ { ...body, time, pose: { ...source, x: -40 + 80 * time / 1.4 } }, {
							...child,
							time
						} ], time );
						if ( i % 5 === 0 ) samples.push( await hash() );
					}
					const socket = renderer.characterSocket( [ { ...body, pose: end } ], -1, "Bone01", [ 0, 0, 0 ] );
					// The trail is a continuous graph: its clock runs on through the
					// loop, so draining continues it (an earlier clock is a rewind and
					// restarts the effect). Emission ends at the arrival.
					const draining = await render( [ { ...body, pose: end, time: 1.1, drawGeometry: false }, {
						...child,
						time: 1.5,
						loop: false,
						emissionEnd: 1.4
					} ], 1.1 );
					const hidden = await render( [ { ...body, drawGeometry: false } ], 2 ),
						reset = await render( [], 3 );
					return {
						empty,
						arrowOnly,
						samples,
						draining,
						hidden,
						reset,
						socket,
						error: renderer.error(),
						duration: resources.duration( trail, "effect" ),
						stats: renderer.characterStats()
					};
				} finally {
					resources.dispose();
					assets.dispose();
					renderer.dispose();
				}
			} );
			await mkdir( "temp/artifacts/arrow-trail", { recursive: true } );
			await writeFile( "temp/artifacts/arrow-trail/gpu.json", JSON.stringify( result, null, 2 ) + "\n" );
			assert.notEqual( result.arrowOnly, result.empty );
			assert.ok( new Set( result.samples ).size > 2 );
			assert.notEqual( result.draining, result.empty );
			assert.equal( result.hidden, result.empty );
			assert.equal( result.reset, result.empty );
			assert.ok( result.socket );
			assert.equal( result.error, null );
		} finally {
			await browser.close();
		}
	}
);
