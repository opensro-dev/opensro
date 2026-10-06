import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetJsonSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { root } from "../../tools/project.mjs";
import path from "node:path";
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
test( "retail object fade reaches GPU pixels and finishes without camera movement", { timeout: 30000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( "http://127.0.0.1:5180/" );
		const pixels = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ),
				instances = identity();
			instances[14] = 450;
			const camera = { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], fov: Math.PI / 3, near: 1, far: 1000 };
			const read = async time => {
				renderer.frame( { width: 64, height: 64 }, time );
				if ( renderer.phase() !== "running" ) throw Error( renderer.error() );
				const image = await createImageBitmap( canvas ), out = document.createElement( "canvas" );
				out.width = 64;
				out.height = 64;
				const ctx = out.getContext( "2d" );
				ctx.drawImage( image, 0, 0 );
				image.close();
				return [ ...ctx.getImageData( 32, 32, 1, 1 ).data ];
			};
			try {
				renderer.setWorld( {
					id: "fade",
					originRegion: 0,
					warnings: [],
					groups: [ {
						id: "object",
						center: [ 0, 0, 450 ],
						radius: 300,
						instanceRadius: 300,
						visibility: [ { id: "placement", radius: 0, range: 480, cells: [ [ 0, 1 ] ], cellRadius: 7 } ],
						material: {
							color: [ 1, 0, 0, 1 ],
							unlit: true,
							objectFade: true,
							alphaCutoff: 0,
							blend: false,
							doubleSided: true
						},
						geometry: {
							world: true,
							positions: new Float32Array( [ -200, -200, 0, 200, -200, 0, 200, 200, 0, -200, 200, 0 ] ),
							normals: new Float32Array( 12 ),
							uvs: new Float32Array( 8 ),
							indices: new Uint32Array( [ 0, 1, 2, 0, 2, 3 ] ),
							transform: identity(),
							instances
						}
					} ]
				} );
				renderer.setWorldCamera( camera );
				const started = performance.now();
				while ( renderer.phase() === "starting" && performance.now() - started < 10000 ) {
					await new Promise( resolve => requestAnimationFrame( resolve ) );
				}
				const hidden = await read( 0 ), half = await read( .25 ), full = await read( .5 );
				renderer.setWorldCamera( { ...camera, eye: [ 0, 0, -100 ] } );
				await read( .75 );
				const fading = await read( 1 ), gone = await read( 1.25 );
				return { hidden, half, full, fading, gone };
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		assert.ok( pixels.hidden[0] < 15, JSON.stringify( pixels ) );
		assert.ok( pixels.half[0] > 125 && pixels.half[0] < 140, JSON.stringify( pixels ) );
		assert.deepEqual( pixels.full, [ 255, 0, 0, 255 ] );
		assert.ok( pixels.fading[0] > 120 && pixels.fading[0] < 140, JSON.stringify( pixels ) );
		assert.deepEqual( pixels.gone, pixels.hidden );
	} finally {
		await browser.close();
	}
} );
test( "native object lighting preserves ambient, vertex interpolation and NOLIGHT", { timeout: 30000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( "http://127.0.0.1:5180/" );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const identity = new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
			const read = async ( material, normals ) => {
				renderer.setGeometry( {
					positions: new Float32Array( [ -1, -1, .5, 1, -1, .5, 0, 1, .5 ] ),
					normals: new Float32Array( normals ),
					indices: new Uint32Array( [ 0, 1, 2 ] ),
					transform: identity,
					material: {
						alphaCutoff: 0,
						blend: false,
						doubleSided: true,
						stageFactor: 1,
						unlit: false,
						...material
					}
				} );
				renderer.frame( { width: 64, height: 64 } );
				if ( renderer.phase() !== "running" ) throw new Error( renderer.error() ?? "Renderer failed" );
				const image = await createImageBitmap( canvas ), output = document.createElement( "canvas" );
				output.width = 64;
				output.height = 64;
				const context = output.getContext( "2d" );
				context.drawImage( image, 0, 0 );
				image.close();
				return [ ...context.getImageData( 32, 32, 1, 1 ).data ];
			};
			try {
				const started = performance.now();
				while ( renderer.phase() === "starting" && performance.now() - started < 10000 ) {
					await new Promise( resolve => requestAnimationFrame( resolve ) );
				}
				const dark = [ 0, 0, -1, 0, 0, -1, 0, 0, -1 ];
				return {
					ambient: await read( { color: [ .2, .4, .8, 1 ], objectLight: .6 }, dark ),
					vertex: await read( { color: [ 1, 1, 1, 1 ], objectLight: .1 }, [ -1, 0, 0, 1, 0, 0, 0, 0, -1 ] ),
					noLight: await read( { color: [ .2, .4, .8, 1 ], objectLight: .6, unlit: true }, dark )
				};
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		// Native constants: ambient=1*.6; c11=.6; only one vertex receives .6/sqrt(2).
		assert.ok( result.ambient.slice( 0, 3 ).every( v => Math.abs( v - 153 ) <= 1 ), JSON.stringify( result ) );
		assert.ok( result.vertex.slice( 0, 3 ).every( v => v >= 51 && v <= 56 ), JSON.stringify( result ) );
		assert.deepEqual( result.noLight, [ 255, 255, 255, 255 ] );
	} finally {
		await browser.close();
	}
} );
test( "indexed geometry reaches the GPU and produces the expected center pixel", { timeout: 30000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", error => errors.push( error.message ) );
	try {
		await page.goto( "http://127.0.0.1:5180/" );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const canvas = document.createElement( "canvas" );
			canvas.width = 64;
			canvas.height = 64;
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			try {
				const positions = new Float32Array( [ -1, -1, 0.5, 1, -1, 0.5, 0, 1, 0.5 ] );
				const material = { color: [ 1, 0, 0, 1 ], alphaCutoff: 0, blend: false, doubleSided: true };
				const mesh = {
					positions,
					material,
					indices: new Uint32Array( [ 0, 1, 2 ] ),
					transform: new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] )
				};
				renderer.setGeometry( mesh );
				material.color[0] = 0;
				try {
					renderer.setGeometry( { ...mesh, uvs: new Float32Array( 1 ) } );
					throw new Error( "Invalid geometry accepted" );
				} catch ( error ) {
					if ( !String( error ).includes( "attributes" ) ) throw error;
				}
				positions.fill( 1000 ); // Proves renderer ownership is independent of caller mutation.
				const started = performance.now();
				while ( renderer.phase() === "starting" && performance.now() - started < 10000 ) {
					await new Promise( resolve => requestAnimationFrame( resolve ) );
				}
				renderer.frame( { width: 64, height: 64 } );
				await new Promise( resolve => requestAnimationFrame( resolve ) );
				if ( renderer.phase() !== "running" ) {
					throw new Error( renderer.error() ?? "Geometry renderer not running" );
				}
				renderer.frame( { width: 64, height: 64 } );
				const snapshot = await createImageBitmap( canvas ), readback = document.createElement( "canvas" );
				readback.width = 64;
				readback.height = 64;
				const context = readback.getContext( "2d" );
				context.drawImage( snapshot, 0, 0 );
				snapshot.close();
				const before = [ ...context.getImageData( 32, 32, 1, 1 ).data ];
				renderer.setGeometryTransform( new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 3, 0, 0, 1 ] ) );
				renderer.frame( { width: 64, height: 64 } );
				const moved = await createImageBitmap( canvas );
				context.drawImage( moved, 0, 0 );
				moved.close();
				return { before, after: [ ...context.getImageData( 32, 32, 1, 1 ).data ] };
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		assert.deepEqual( result.before, [ 255, 0, 0, 255 ] );
		assert.ok( Math.abs( result.after[0] - 9 ) <= 2 );
		assert.ok( Math.abs( result.after[1] - 18 ) <= 2 );
		assert.ok( Math.abs( result.after[2] - 23 ) <= 2 );
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );

test( "published city mesh draws through the indexed path with depth enabled", { timeout: 30000 }, async () => {
	const region = readPublishedAssetJsonSync( "/assets/world/china/region-62a8.json", CLIENT_PUBLIC_ROOT );
	const mesh = region.objects.resources.meshes[0], { min, max } = mesh.bounds;
	const scale = 1.6 / Math.max( max[0] - min[0], max[1] - min[1] ), depth = 0.8 / (max[2] - min[2]);
	const transform = [
		scale,
		0,
		0,
		0,
		0,
		scale,
		0,
		0,
		0,
		0,
		depth,
		0,
		-scale * (min[0] + max[0]) / 2,
		-scale * (min[1] + max[1]) / 2,
		0.1 - depth * min[2],
		1
	];
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( "http://127.0.0.1:5180/" );
		const count = await page.evaluate( async data => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" ),
				canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			try {
				renderer.setGeometry( {
					positions: new Float32Array( data.positions ),
					indices: new Uint32Array( data.indices ),
					transform: new Float32Array( data.transform )
				} );
				const started = performance.now();
				while ( renderer.phase() === "starting" && performance.now() - started < 10000 ) {
					await new Promise( resolve => requestAnimationFrame( resolve ) );
				}
				renderer.frame( { width: 128, height: 128 } );
				if ( renderer.phase() !== "running" ) throw new Error( renderer.error() ?? "Renderer failed" );
				const snapshot = await createImageBitmap( canvas ), readback = document.createElement( "canvas" );
				readback.width = 128;
				readback.height = 128;
				const context = readback.getContext( "2d" );
				context.drawImage( snapshot, 0, 0 );
				snapshot.close();
				const pixels = context.getImageData( 0, 0, 128, 128 ).data;
				let count = 0;
				for ( let i = 0; i < pixels.length; i += 4 ) if ( pixels[i] > 150 ) count++;
				return count;
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		}, { positions: mesh.positions, indices: mesh.indices, transform } );
		assert.ok( count > 100, "published geometry must cover visible pixels" );
		assert.ok( count < 128 * 128, "clear background remains visible" );
	} finally {
		await browser.close();
	}
} );

test( "retained direct instancing handles capacity growth, zero visibility and reuse", { timeout: 30000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( "http://127.0.0.1:5180/" );
		const result = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" ),
				canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const matrix = x => [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, 0, 0, 1 ];
			const pixels = async () => {
				renderer.frame( { width: 64, height: 64 } );
				const image = await createImageBitmap( canvas ), output = document.createElement( "canvas" );
				output.width = 64;
				output.height = 64;
				const context = output.getContext( "2d" );
				context.drawImage( image, 0, 0 );
				image.close();
				return [ context.getImageData( 16, 32, 1, 1 ).data[0], context.getImageData( 48, 32, 1, 1 ).data[0] ];
			};
			try {
				renderer.setGeometry( {
					positions: new Float32Array( [ -.25, -.5, .5, .25, -.5, .5, 0, .5, .5 ] ),
					indices: new Uint32Array( [ 0, 1, 2 ] ),
					transform: new Float32Array( matrix( 0 ) )
				} );
				const start = performance.now();
				while ( renderer.phase() === "starting" && performance.now() - start < 10000 ) {
					await new Promise( resolve => requestAnimationFrame( resolve ) );
				}
				await pixels();
				renderer.setGeometryInstances( new Float32Array( [ ...matrix( -.5 ), ...matrix( .5 ) ] ) );
				const both = await pixels();
				renderer.setGeometryInstances( new Float32Array() );
				const hidden = await pixels();
				renderer.setGeometryInstances( new Float32Array( matrix( .5 ) ) );
				const restored = await pixels();
				if ( renderer.phase() !== "running" ) {
					throw new Error( renderer.error() ?? "Instanced renderer failed" );
				}
				return { both, hidden, restored };
			} finally {
				renderer.dispose();
				canvas.remove();
			}
		} );
		assert.ok( result.both.every( value => value > 150 ) );
		assert.ok( result.hidden.every( value => value < 30 ) );
		assert.ok( result.restored[0] < 30 && result.restored[1] > 150 );
	} finally {
		await browser.close();
	}
} );
