/*
===========================================================================

world-frustum.test.mjs - world selection against an unculled oracle

Two renderers draw the same scene from 16 camera turns: one with the
port's selection (object placements by group, terrain cells by eye cell),
one with selection disabled. The pixels must match, and a turn must not
change what is chosen or rebuild the draw list.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test( "object placements: the GPU clips what the CPU no longer culls, and turning the camera chooses nothing", {
	timeout: 60000
}, async () => {
	const { browser, page } = await launchProbeBrowser(), errors = [];
	page.on( "pageerror", e => errors.push( e.message ) );
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
			const canvases = [ document.createElement( "canvas" ), document.createElement( "canvas" ) ],
				renderers = canvases.map( c => createRenderer( c ) );
			const output = document.createElement( "canvas" );
			output.width = output.height = 96;
			const ctx = output.getContext( "2d" ), rows = [];
			try {
				const start = performance.now();
				while ( renderers.some( r => r.phase() === "starting" ) ) {
					if ( performance.now() - start > 15000 ) throw Error( "GPU startup timeout" );
					await new Promise( requestAnimationFrame );
				}
				const matrices = [], visibility = [];
				for ( let i = 0; i < 32; i++ ) {
					const angle = i * Math.PI / 16, m = identity();
					m[0] = Math.cos( angle );
					m[2] = Math.sin( angle );
					m[8] = -Math.sin( angle );
					m[10] = Math.cos( angle );
					m[12] = Math.sin( angle ) * 30;
					m[14] = Math.cos( angle ) * 30;
					matrices.push( ...m );
					visibility.push( {
						id: String( i ),
						radius: 0,
						range: 100000,
						cells: [ [ 0, 0 ] ],
						cellRadius: 15
					} );
				}
				const group = {
					id: "objects",
					instanceRadius: 4,
					visibility,
					center: [ 0, 0, 0 ],
					radius: 100000,
					material: {
						color: [ .9, .2, .3, 1 ],
						unlit: true,
						objectFade: true,
						alphaCutoff: 0,
						blend: false,
						doubleSided: true
					},
					geometry: {
						positions: new Float32Array( [ -3, -3, 0, 3, -3, 0, 3, 3, 0, -3, 3, 0 ] ),
						normals: new Float32Array( 12 ),
						uvs: new Float32Array( 8 ),
						indices: new Uint32Array( [ 0, 1, 2, 0, 2, 3 ] ),
						instances: new Float32Array( matrices ),
						transform: identity()
					}
				};
				const scene = { id: "frustum", originRegion: 1, warnings: [], groups: [ group ] };
				renderers[0].setWorld( scene );
				renderers[1].setWorld( { ...scene, groups: [ { ...group, instanceRadius: undefined } ] } );
				for ( let turn = 0; turn < 16; turn++ ) {
					const a = turn * Math.PI / 8,
						camera = {
							eye: [ 0, 0, 0 ],
							target: [ Math.sin( a ), 0, Math.cos( a ) ],
							near: 1,
							far: 100,
							fov: Math.PI / 3
						};
					const pixels = [];
					for ( let i = 0; i < 2; i++ ) {
						renderers[i].setWorldCamera( camera );
						renderers[i].frame( { width: 96, height: 96 }, 1 + turn / 4 );
						const image = await createImageBitmap( canvases[i] );
						ctx.drawImage( image, 0, 0 );
						image.close();
						pixels.push( [ ...ctx.getImageData( 0, 0, 96, 96 ).data ] );
					}
					rows.push( { pixels, stats: renderers.map( r => r.worldStats() ) } );
				}
				return { rows, errors: renderers.map( r => r.error() ) };
			} finally {
				renderers.forEach( r => r.dispose() );
			}
		} );
		assert.deepEqual( errors, [] );
		assert.deepEqual( result.errors, [ null, null ] );
		// Native rejects each placement against the frustum (8AA9C5, A2D410); the
		// port tests the group's sphere and lets the GPU clip placements, so the
		// pixels match an unculled draw and a turn neither re-chooses nor re-uploads.
		const first = result.rows[0].stats[0];
		for ( const [i, row] of result.rows.entries() ) {
			assert.deepEqual( row.pixels[0], row.pixels[1], `camera ${i}` );
			assert.equal( row.stats[0].triangles, first.triangles, "a turn chooses no placements" );
			assert.equal( row.stats[0].bundleRebuilds, first.bundleRebuilds, "a turn rebuilds no draw list" );
			assert.ok( row.pixels[0].some( ( v, index ) => index % 4 === 0 && v > 150 ), "oracle contains objects" );
		}
	} finally {
		await browser.close();
	}
} );

test(
	"terrain cells: the GPU clips off-screen cells, and turning the camera chooses nothing",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await page.goto( CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( async () => {
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
				const canvases = [ document.createElement( "canvas" ), document.createElement( "canvas" ) ],
					renderers = canvases.map( c => createRenderer( c ) );
				const output = document.createElement( "canvas" );
				output.width = output.height = 96;
				const ctx = output.getContext( "2d" ), rows = [];
				try {
					const start = performance.now();
					while ( renderers.some( r => r.phase() === "starting" ) ) {
						if ( performance.now() - start > 15000 ) throw Error( "GPU startup timeout" );
						await new Promise( requestAnimationFrame );
					}
					const positions = [], indices = [], ranges = [];
					for ( let z = -3; z < 3; z++ ) {
						for ( let x = -3; x < 3; x++ ) {
							const base = positions.length / 3, indexStart = indices.length;
							positions.push(
								x * 320,
								0,
								z * 320,
								(x + 1) * 320,
								0,
								z * 320,
								x * 320,
								0,
								(z + 1) * 320,
								(x + 1) * 320,
								0,
								(z + 1) * 320
							);
							indices.push( base, base + 2, base + 1, base + 1, base + 2, base + 3 );
							ranges.push( {
								bounds: [ x * 320, 0, z * 320, (x + 1) * 320, 0, (z + 1) * 320 ],
								cell: [ x, z ],
								lod: 0,
								indexStart,
								indexCount: 6,
								vertexStart: base,
								vertexCount: 4,
								center: [ x * 320 + 160, 0, z * 320 + 160 ],
								radius: Math.hypot( 160, 160 ),
								heights: Array( 289 ).fill( 0 )
							} );
						}
					}
					const group = {
						id: "cells",
						center: [ 0, 0, 0 ],
						radius: 10000,
						ranges,
						material: {
							color: [ .9, .2, .3, 1 ],
							unlit: true,
							alphaCutoff: 0,
							blend: false,
							doubleSided: true
						},
						geometry: {
							positions: new Float32Array( positions ),
							normals: new Float32Array( positions.length ),
							uvs: new Float32Array( positions.length / 3 * 2 ),
							indices: new Uint32Array( indices ),
							instances: identity(),
							transform: identity()
						}
					};
					const scene = {
						id: "terrain-boxes",
						originRegion: 1,
						terrainDetail: "full",
						warnings: [],
						groups: [ group ]
					};
					renderers[0].setWorld( scene );
					renderers[1].setWorld( { ...scene, groups: [ { ...group, ranges: undefined } ] } );
					for ( let turn = 0; turn < 16; turn++ ) {
						const a = turn * Math.PI / 8,
							camera = {
								eye: [ 160, 40, -80 ],
								target: [ 160 + Math.sin( a ) * 500, 0, -80 + Math.cos( a ) * 500 ],
								near: 1,
								far: 3500,
								fov: Math.PI / 3
							},
							pixels = [];
						for ( let i = 0; i < 2; i++ ) {
							renderers[i].setWorldCamera( camera );
							renderers[i].frame( { width: 96, height: 96 }, 1 + turn / 4 );
							const image = await createImageBitmap( canvases[i] );
							ctx.drawImage( image, 0, 0 );
							image.close();
							pixels.push( [ ...ctx.getImageData( 0, 0, 96, 96 ).data ] );
						}
						rows.push( { pixels, stats: renderers.map( r => r.worldStats() ) } );
					}
					return { rows, errors: renderers.map( r => r.error() ) };
				} finally {
					renderers.forEach( r => r.dispose() );
				}
			} );
			assert.deepEqual( result.errors, [ null, null ] );
			// Native culls terrain cells against the frustum each frame (A2D1B0); the
			// port chooses cells by the eye cell alone and lets the GPU clip them.
			const first = result.rows[0].stats[0];
			for ( const [i, row] of result.rows.entries() ) {
				assert.deepEqual( row.pixels[0], row.pixels[1], `terrain camera ${i}` );
				assert.equal( row.stats[0].triangles, first.triangles, "a turn chooses no cells" );
				assert.equal( row.stats[0].bundleRebuilds, first.bundleRebuilds, "a turn rebuilds no draw list" );
				assert.ok( row.pixels[0].some( ( v, index ) => index % 4 === 0 && v > 150 ), "oracle renders terrain" );
			}
		} finally {
			await browser.close();
		}
	}
);
