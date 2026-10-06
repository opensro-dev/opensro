/*
===========================================================================

gpu-animation.test.mjs - the GPU pose owner against the CPU evaluator

In a real browser, the compute pass must produce the CPU palettes for every
clip, clock and binding, including an assembled model that shares its
body clips and has a node past every animated one.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { writeFile, mkdir } from "node:fs/promises";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "shipping GPU pose owner matches CPU palettes across clocks, bindings, release and recreation", {
	timeout: 90000
}, async () => {
	const bytes = readPublishedAssetBytesSync(
		"/assets/npc/mob/china/mangnyang.glb",
		CLIENT_PUBLIC_ROOT
	);
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async bytes => {
			const { createModelDecoder } = await import( "/src/engine/runtime/assets/worker/model/model.ts" );
			const { createCharacterPose } = await import( "/src/engine/foundation/animation/animation-pose.ts" );
			const { createGpuAnimationResources } = await import( "/src/engine/runtime/renderer/device/animation.ts" );
			const decoder = createModelDecoder(),
				m = decoder.character( decoder.decode( Uint8Array.from( bytes ) ) ),
				errors = [],
				runs = [];
			const step = {
				...m.clips[0],
				name: "step-oracle",
				channels: m.clips[0].channels.map( c => ({ ...c, interpolation: "STEP" }) )
			};
			m.clips.push( step );
			// An assembled model on the same clips: it shares the clip buffer, and its extra
			// node lies past every animated node, so the shader must read its rest pose.
			const extra = m.nodes.length,
				assembled = {
					...m,
					nodes: [ ...m.nodes, {
						name: "attachment",
						parent: 0,
						translation: [ 1, 2, 3 ],
						rotation: [ 0, 0, .6, .8 ],
						scale: [ 1, 1, 1 ]
					} ],
					primitives: m.primitives.map( p => ({ ...p, joints: [ ...p.joints.slice( 0, -1 ), extra ] }) )
				};
			for ( let recovery = 0; recovery < 2; recovery++ ) {
				const device = await (await navigator.gpu.requestAdapter()).requestDevice();
				device.addEventListener( "uncapturederror", e => errors.push( e.error.message ) );
				const owner = createGpuAnimationResources( device );
				await owner.ready;
				try {
					const cases = m.clips.flatMap( clip =>
						[
							0,
							.333333,
							.999999,
							1,
							86400.333333,
							...clip.channels.flatMap( c =>
								c.times.length > 1 ? [ Math.max( 0, c.times[1] - Number.EPSILON ) ] : []
							).slice( 0, 1 )
						].flatMap( time => [ false, true ].map( loop => ({ clip, time, loop }) ) )
					);
					let maxError = 0, compared = 0;
					for (
						const [model, primitive] of [
							...m.primitives.map( p => [ m, p ] ),
							...assembled.primitives.map( p => [ assembled, p ] )
						]
					) {
						const size = cases.length * primitive.joints.length * 64,
							source = new Float32Array( size / 4 ),
							output = device.createBuffer( {
								size,
								usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_SRC
							} ),
							read = device.createBuffer( {
								size,
								usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ
							} );
						const cpu = new Float32Array( source.length ),
							samples = cases.map( ( c, i ) => {
								const pose = createCharacterPose( model );
								pose.evaluate( c.clip.name, c.time, c.loop );
								pose.palette( primitive, cpu, i * primitive.joints.length * 16 );
								return pose.gpuSample();
							} );
						if ( !owner.prepare( source, output, model, primitive, samples ) ) {
							throw Error( "Published animation unexpectedly refused" );
						}
						if ( model === assembled && owner.stats().clipSets !== 1 ) {
							throw Error( "Assembled model did not share the clip set" );
						}
						const encoder = device.createCommandEncoder();
						owner.encode( encoder );
						encoder.copyBufferToBuffer( output, 0, read, 0, size );
						device.queue.submit( [ encoder.finish() ] );
						await read.mapAsync( GPUMapMode.READ );
						const actual = new Float32Array( read.getMappedRange() );
						for ( let i = 0; i < cpu.length; i++ ) {
							const error = Math.abs( cpu[i] - actual[i] );
							if ( !Number.isFinite( error ) ) throw Error( "Nonfinite GPU pose" );
							maxError = Math.max( maxError, error );
							compared++;
						}
						read.unmap();
						// A retired output must not remain in the next command encoder.
						owner.prepare( source, output, model, primitive, samples );
						owner.release( source );
						const empty = device.createCommandEncoder();
						owner.encode( empty );
						device.queue.submit( [ empty.finish() ] );
						output.destroy();
						read.destroy();
					}
					await device.queue.onSubmittedWorkDone();
					runs.push( { maxError, compared, stats: owner.stats() } );
				} finally {
					owner.dispose();
					device.destroy();
				}
			}
			return { runs, errors };
		}, [ ...bytes ] );
		await mkdir( "temp/artifacts/gpu-animation", { recursive: true } );
		await writeFile( "temp/artifacts/gpu-animation/owner-parity.json", JSON.stringify( result, null, 2 ) );
		assert.deepEqual( result.errors, [] );
		for ( const run of result.runs ) {
			assert.ok( run.maxError < .0001, JSON.stringify( run ) );
			assert.equal( run.stats.streams, 0 );
			assert.equal( run.stats.models, 0 );
			assert.equal( run.stats.staticBytes, 0 );
			assert.ok( run.stats.poses > 0 );
		}
	} finally {
		await browser.close();
	}
} );

test(
	"published Manyang CPU and GPU rendering agrees through seeks and layer fallback",
	{ timeout: 90000 },
	async () => {
		const bytes = readPublishedAssetBytesSync(
			"/assets/npc/mob/china/mangnyang.glb",
			CLIENT_PUBLIC_ROOT
		);
		const { browser, page } = await launchProbeBrowser();
		try {
			await holdProbeRuntime( page );
			await page.goto( CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( async bytes => {
				const { createModelDecoder } = await import( "/src/engine/runtime/assets/worker/model/model.ts" );
				const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
				const decoder = createModelDecoder(),
					model = decoder.character( decoder.decode( Uint8Array.from( bytes ) ) );
				// Identical unlit material isolates geometry, animation and skinning from lighting.
				for ( const p of model.primitives ) {
					p.image = -1;
					p.geometry.material = {
						...p.geometry.material,
						color: [ 1, .4, .2, 1 ],
						unlit: true,
						doubleSided: true,
						blend: false,
						alphaCutoff: 0
					};
				}
				model.images = [];
				const canvases = [ document.createElement( "canvas" ), document.createElement( "canvas" ) ],
					renderers = canvases.map( ( c, i ) =>
						createRenderer( c, undefined, undefined, { gpuAnimation: i === 1 } )
					);
				const out = document.createElement( "canvas" );
				out.width = 256;
				out.height = 256;
				const ctx = out.getContext( "2d" ), rows = [];
				try {
					const start = performance.now();
					while ( renderers.some( r => r.phase() === "starting" ) ) {
						if ( performance.now() - start > 15000 ) throw Error( "GPU startup timeout" );
						await new Promise( requestAnimationFrame );
					}
					for ( const r of renderers ) {
						r.setCharacterModel( "m", model, [] );
						r.setCharacterPreview( {
							eye: [ 0, 15, -60 ],
							target: [ 0, 7, 0 ],
							near: 1,
							far: 1000,
							fov: .8
						} );
					}
					const clips = model.clips.slice( 0, 8 );
					for ( let frame = 0; frame < 18; frame++ ) {
						const clip = clips[frame % clips.length],
							time = frame === 6 ? 86400.333333 : frame * .137,
							layered = frame === 7 || frame === 8;
						const actor = {
							gid: 1,
							model: "m",
							clip: clip.name,
							time,
							loop: frame % 2 === 0,
							scale: 1,
							pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: 0 },
							...(layered ?
								{
									layers: [ {
										clip: clip.name,
										time: time % 1,
										loop: true,
										weight: .6,
										lane: "timed"
									}, { clip: clips[0].name, time: .3, loop: true, weight: .4, lane: "timed" } ]
								} :
								{})
						};
						const pixels = [];
						for ( let i = 0; i < 2; i++ ) {
							renderers[i].setCharacterActors( [ actor ] );
							renderers[i].frame( { width: 256, height: 256 }, frame / 60 );
							const image = await createImageBitmap( canvases[i] );
							ctx.drawImage( image, 0, 0 );
							image.close();
							pixels.push( ctx.getImageData( 0, 0, 256, 256 ).data );
						}
						let different = 0, maxError = 0, visible = 0;
						for ( let p = 0; p < pixels[0].length; p += 4 ) {
							if ( pixels[0][p] > 180 ) visible++;
							let mismatch = false;
							for ( let c = 0; c < 4; c++ ) {
								const error = Math.abs( pixels[0][p + c] - pixels[1][p + c] );
								maxError = Math.max( maxError, error );
								if ( error ) mismatch = true;
							}
							if ( mismatch ) different++;
						}
						rows.push( { frame, different, maxError, visible } );
					}
					return {
						rows,
						errors: renderers.map( r => r.error() ),
						stats: renderers.map( r => r.characterStats() )
					};
				} finally {
					renderers.forEach( r => r.dispose() );
				}
			}, [ ...bytes ] );
			await mkdir( "temp/artifacts/gpu-animation", { recursive: true } );
			await writeFile( "temp/artifacts/gpu-animation/render-parity.json", JSON.stringify( result, null, 2 ) );
			assert.deepEqual( result.errors, [ null, null ] );
			assert.ok( result.stats[1].gpuAnimation.poses > 0 );
			assert.ok(
				result.stats[1].liveOwnedCpuEvaluations < result.stats[0].liveOwnedCpuEvaluations / 2,
				"GPU route must avoid CPU pose work"
			);
			for ( const row of result.rows ) {
				assert.ok( row.visible > 100, JSON.stringify( row ) );
				assert.ok( row.different <= 2, JSON.stringify( row ) );
			}
		} finally {
			await browser.close();
		}
	}
);
