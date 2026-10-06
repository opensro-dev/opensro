/*
===========================================================================

water-reflection.test.mjs - planar water capture through the production renderer

===========================================================================
*/
import { test } from "node:test";
import { writeFile, mkdir } from "node:fs/promises";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "water reflection renders above-water geometry and survives off/on transitions", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { defaultVideoOptions } = await import( "/src/engine/foundation/rendering/video-options.ts" );
			const canvas = document.createElement( "canvas" ), renderer = createRenderer( canvas );
			const copy = document.createElement( "canvas" );
			copy.width = copy.height = 128;
			const context = copy.getContext( "2d", { willReadFrequently: true } );
			if ( !context ) throw Error( "Canvas readback unavailable" );
			const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
			const group = ( id, positions, color, water = false ) => ({
				id,
				center: [ 0, water ? 0 : 1, 2 ],
				radius: 20,
				material: {
					color,
					water,
					alphaCutoff: 0,
					blend: water,
					doubleSided: true,
					unlit: true,
					fogDisabled: true
				},
				geometry: {
					positions: new Float32Array( positions ),
					indices: new Uint32Array( [ 0, 1, 2, 0, 2, 3 ] ),
					normals: new Float32Array( 12 ).fill( 1 ),
					uvs: new Float32Array( 8 ),
					colors: new Float32Array( 16 ).fill( 1 ),
					transform: identity(),
					instances: identity(),
					world: true
				}
			});
			renderer.setWorld( {
				id: "water-proof",
				originRegion: 257,
				warnings: [],
				groups: [
					group( "above", [ -1, .5, 3, 1, .5, 3, 1, 2.5, 3, -1, 2.5, 3 ], [ 1, 0, 0, 1 ] ),
					group( "below", [ -1, -2.5, 3, 1, -2.5, 3, 1, -.5, 3, -1, -.5, 3 ], [ 0, 0, 1, 1 ] ),
					group( "water", [ -10, 0, -3, 10, 0, -3, 10, 0, 12, -10, 0, 12 ], [ .1, .1, .1, 1 ], true )
				]
			} );
			renderer.setWorldCamera( {
				eye: [ 0, 3, -5 ],
				target: [ 0, 0, 3 ],
				originRegion: 257,
				near: .1,
				far: 100,
				fov: 1
			} );
			while ( renderer.phase() === "starting" ) await new Promise( requestAnimationFrame );
			if ( renderer.error() ) throw Error( renderer.error() );
			const counts = [];
			try {
				for ( const enabled of [ false, true, false, true ] ) {
					const options = defaultVideoOptions();
					renderer.videoOptions( {
						...options,
						records: options.records.map( row => row.map( ( v, i ) => i === 4 ? Number( enabled ) : v ) )
					} );
					for ( let frame = 0; frame < 12; frame++ ) {
						renderer.frame( { width: 128, height: 128 }, frame / 60 );
						if ( renderer.error() ) throw Error( renderer.error() );
						await new Promise( requestAnimationFrame );
					}
					const bitmap = await createImageBitmap( canvas );
					context.drawImage( bitmap, 0, 0 );
					bitmap.close();
					const pixels = context.getImageData( 0, 64, 128, 64 ).data;
					let red = 0, blue = 0;
					for ( let i = 0; i < pixels.length; i += 4 ) {
						if ( pixels[i] > pixels[i + 1] + 30 && pixels[i] > pixels[i + 2] + 30 ) red++;
					}
					for ( let i = 0; i < pixels.length; i += 4 ) {
						if ( pixels[i + 2] > pixels[i] + 30 && pixels[i + 2] > pixels[i + 1] + 30 ) blue++;
					}
					if ( blue ) throw Error( "Submerged geometry leaked into water reflection" );
					counts.push( red );
				}
				return { counts, image: copy.toDataURL(), error: renderer.error() };
			} finally {
				renderer.dispose();
			}
		} );
		await mkdir( "../../.state/water", { recursive: true } );
		await writeFile( "../../.state/water/reflection.png", Buffer.from( result.image.split( "," )[1], "base64" ) );
		const counts = result.counts;
		assert.ok( counts[1] > counts[0] + 100, JSON.stringify( counts ) );
		assert.equal( counts[2], counts[0] );
		assert.equal( counts[3], counts[1] );
	} finally {
		await browser.close();
	}
} );
