/*
===========================================================================

object-cull.test.mjs - the GPU object cull against the CPU visibility rule

The world used to keep an object draw when visibleFrustumSphere passed its
sphere against the view or the water mirror. The GPU cull
(object-cull.ts) must keep every such draw (a draw the CPU kept and the
GPU dropped would vanish on screen) and may only add draws whose sphere
lies within CULL_EPSILON of a plane (clipped by the rasterizer), over
random views, mirrors and spheres, through slot reuse and growth.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "the GPU object cull keeps every draw the CPU rule kept", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createObjectCull } = await import( "/src/engine/runtime/renderer/device/object-cull.ts" );
			const { DeviceDraw } = await import( "/src/engine/runtime/renderer/device/device-draw.ts" );
			const { viewProjection, prepareViewFrustum, visibleFrustumSphere } = await import(
				"/src/engine/foundation/rendering/world-math.ts"
			);
			const adapter = await navigator.gpu.requestAdapter(), gpu = await adapter.requestDevice();
			const cull = createObjectCull( gpu, resource => resource.destroy?.() );
			await cull.ready;
			let seed = 7;
			const random = () => (seed = (Math.imul( seed, 1103515245 ) + 12345) >>> 0) / 4294967296;
			const draws = [], spheres = new Map();
			const adopt = () => {
				const draw = new DeviceDraw( { pipeline: {}, vertices: {}, indices: {}, count: 3 }, {}, 4, 4 );
				const sphere = [ random() * 4000 - 2000, random() * 400 - 200, random() * 4000 - 2000, random() * 60 ];
				cull.adopt( draw, sphere );
				draws.push( draw );
				spheres.set( draw, sphere );
			};
			// 300 first (the first buffer is 256 slots: one growth), then forget
			// and re-adopt to reuse slots.
			for ( let i = 0; i < 300; i++ ) adopt();
			for ( let i = 0; i < 40; i++ ) {
				const [draw] = draws.splice( Math.floor( random() * draws.length ), 1 );
				cull.forget( draw );
				spheres.delete( draw );
			}
			for ( let i = 0; i < 60; i++ ) adopt();
			let missing = 0, extraOutsideMargin = 0, kept = 0, cpuKept = 0, views = 0;
			for ( let v = 0; v < 24; v++ ) {
				const camera = {
					eye: [ random() * 2000 - 1000, 50 + random() * 300, random() * 2000 - 1000 ],
					target: [ random() * 2000 - 1000, 0, random() * 2000 - 1000 ],
					near: 1,
					far: 1500 + random() * 2000,
					fov: Math.PI / 3
				};
				const main = prepareViewFrustum( viewProjection( camera, 16 / 10 ) );
				const mirrored = v % 3 === 0 ?
					prepareViewFrustum(
						viewProjection( { ...camera, eye: [ camera.eye[0], -camera.eye[1], camera.eye[2] ] }, 16 / 10 )
					) :
					null;
				cull.view( main, mirrored );
				const encoder = gpu.createCommandEncoder();
				cull.encode( encoder );
				const { args, slotOf } = cull.buffers();
				const read = gpu.createBuffer( {
					size: args.size,
					usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST
				} );
				encoder.copyBufferToBuffer( args, 0, read, 0, args.size );
				gpu.queue.submit( [ encoder.finish() ] );
				await read.mapAsync( GPUMapMode.READ );
				const words = new Uint32Array( read.getMappedRange().slice( 0 ) );
				read.destroy();
				views++;
				for ( const draw of draws ) {
					const [x, y, z, r] = spheres.get( draw );
					const cpu = visibleFrustumSphere( main, x, y, z, r ) ||
						!!mirrored && visibleFrustumSphere( mirrored, x, y, z, r );
					const gpuKept = words[slotOf( draw ) * 5 + 1] === 4;
					if ( cpu ) cpuKept++;
					if ( gpuKept ) kept++;
					if ( cpu && !gpuKept ) missing++;
					if ( !cpu && gpuKept ) {
						// Only a sphere within the margin of the planes may be added.
						const grown = visibleFrustumSphere( main, x, y, z, r + 1.01 ) ||
							!!mirrored && visibleFrustumSphere( mirrored, x, y, z, r + 1.01 );
						if ( !grown ) extraOutsideMargin++;
					}
				}
			}
			cull.dispose();
			return { missing, extraOutsideMargin, kept, cpuKept, views, draws: draws.length };
		} );
		assert.equal( result.views, 24 );
		assert.ok( result.cpuKept > 200, JSON.stringify( result ) );
		assert.equal( result.missing, 0, JSON.stringify( result ) );
		assert.equal( result.extraOutsideMargin, 0, JSON.stringify( result ) );
	} finally {
		await browser.close();
	}
} );
