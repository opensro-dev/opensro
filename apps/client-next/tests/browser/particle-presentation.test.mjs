/*
===========================================================================

particle-presentation.test.mjs - the GPU particle pass against its reference

In a real browser, the presentation pass (device/particle-shader.ts) must
draw every slot the JavaScript reference draws (tests/helpers/
particle-reference.mjs, built on the CPU functions the pass replaced):
graph and program records, every view mode, linear and step material
frames, looping lifetimes, empty and expired slots, and a later
presentation that uploads only its dirty slots.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "the GPU particle pass draws what the reference draws, for every record kind and view mode", {
	timeout: 90000
}, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const records = await import( "/src/engine/foundation/animation/particle-records.ts" );
			const { snapshotParticleTick, ROTATION_WORK } = await import(
				"/src/engine/foundation/animation/particle-presentation.ts"
			);
			const { cameraAxes } = await import( "/src/engine/foundation/rendering/effect-billboard.ts" );
			const { createParticlePresentation } = await import( "/src/engine/runtime/renderer/device/particles.ts" );
			const { presentSlot } = await import( "/tests/helpers/particle-reference.mjs" );
			const errors = [];
			const adapter = await navigator.gpu.requestAdapter();
			if ( !adapter ) throw Error( "No WebGPU adapter" );
			const device = await adapter.requestDevice();
			device.addEventListener( "uncapturederror", e => errors.push( e.error.message ) );
			const owner = createParticlePresentation( device );
			await owner.ready;
			let seed = 7;
			const random = () => (seed = (seed * 1664525 + 1013904223) >>> 0) / 2 ** 32;
			const spread = scale => (random() - .5) * scale;
			// A scaled rotation about a random axis, as a column-major 4x4.
			const rotation = ( angle, scale = 1 ) => {
				const m = new Float32Array( 16 ), x = spread( 1 ), y = spread( 1 ), z = spread( 1 );
				const length = Math.hypot( x, y, z ), u = x / length, v = y / length, w = z / length;
				const c = Math.cos( angle ), s = Math.sin( angle ), t = 1 - c;
				const r = [
					t * u * u + c,
					t * u * v + s * w,
					t * u * w - s * v,
					t * u * v - s * w,
					t * v * v + c,
					t * v * w + s * u,
					t * u * w + s * v,
					t * v * w - s * u,
					t * w * w + c
				];
				for ( let column = 0; column < 3; column++ ) {
					for ( let row = 0; row < 3; row++ ) m[column * 4 + row] = r[column * 3 + row] * scale;
				}
				m[15] = 1;
				return m;
			};
			// turn = previous then current rotated by a further small step.
			const ticked = ( previous, step ) => {
				const turn = rotation( step ), out = new Float32Array( 16 );
				for ( let c = 0; c < 4; c++ ) {
					for ( let r = 0; r < 4; r++ ) {
						out[c * 4 + r] = previous[r] * turn[c * 4] + previous[4 + r] * turn[c * 4 + 1] +
							previous[8 + r] * turn[c * 4 + 2] + previous[12 + r] * turn[c * 4 + 3];
					}
				}
				return out;
			};
			// A program particle after a tick: a snapshot and a moved, turned pose.
			const programState = () => {
				const state = {
					position: [ spread( 4 ), spread( 4 ), spread( 4 ) ],
					velocity: random() < .2 ? [ 0, 0, 0 ] : [ spread( 3 ), spread( 3 ), spread( 3 ) ],
					scale: [ .5 + random(), .5 + random(), .5 + random() ],
					rotation: rotation( random() * 6 ),
					frame: Math.floor( random() * 8 )
				};
				if ( random() < .8 ) {
					snapshotParticleTick( state );
					state.position = state.position.map( v => v + spread( 1 ) );
					state.scale = state.scale.map( v => v + spread( .2 ) );
					state.rotation = ticked( state.rotation, spread( .8 ) );
					state.frame++;
				}
				return state;
			};
			const views = [ "camera", "y", "v", undefined ];
			// A view-projection's rows: right, up, and (fourth) the camera's forward.
			const view = new Float32Array( rotation( 1.1 ) );
			view[3] = .3;
			view[7] = -.5;
			view[11] = .81;
			view[12] = 3;
			const cases = [];
			for ( const graph of [ true, false ] ) {
				for ( const mode of views ) {
					for ( const sampling of [ "linear", "step", "none" ] ) cases.push( { graph, mode, sampling } );
				}
			}
			let compared = 0, maxError = 0, hidden = 0, drawn = 0;
			const work = new Float64Array( ROTATION_WORK );
			// Every stream stays live to the end: together they outgrow the
			// frame arena's first allocation, and the first ones are drawn
			// again after it grew.
			const live = [];
			for ( const [n, c] of cases.entries() ) {
				const rows = 12, slots = 5, count = rows * slots;
				const frames = c.sampling === "none" ? undefined : {
					fps: 7,
					...(c.sampling === "step" ? { sampling: "step" } : {}),
					colors: Float32Array.from( { length: 12 }, () => random() ),
					windows: Float32Array.from( { length: 12 }, () => random() )
				};
				const particles = {
					rows,
					slots,
					graph: c.graph,
					view: records.particleView( c.mode ),
					lifetime: 1.5,
					loop: n % 2 === 0,
					frames,
					records: new Float32Array( count * records.PARTICLE_RECORD ),
					actors: new Float32Array( rows * records.PARTICLE_ACTOR ),
					axes: new Float32Array( 12 ),
					dirtyStart: 0,
					dirtyEnd: count
				};
				if ( c.mode === "camera" || c.mode === "y" ) {
					const basis = new Float64Array( 9 );
					cameraAxes( view, c.mode, basis );
					for ( let k = 0; k < 9; k++ ) particles.axes[Math.floor( k / 3 ) * 4 + k % 3] = basis[k];
				}
				for ( let row = 0; row < rows; row++ ) {
					const at = row * records.PARTICLE_ACTOR;
					particles.actors[at + records.ACTOR_TIME] = 1 + random() * 2;
					particles.actors[at + records.ACTOR_OPACITY] = random();
					particles.actors[at + records.ACTOR_FRACTION] = random() * 1.2 - .1;
					particles.actors.set( rotation( random() * 6, .5 + random() ), at + records.ACTOR_PALETTE );
					particles.actors[at + records.ACTOR_PALETTE + 12] = spread( 10 );
				}
				const write = slot => {
					const row = Math.floor( slot / slots ), time = particles.actors[row * records.PARTICLE_ACTOR];
					if ( random() < .15 ) return records.hideRecord( particles.records, slot );
					// Most births fall inside the lifetime; some before it or long after.
					const birth = time - (random() < .85 ? random() * 1.4 : random() < .5 ? -.5 : 2 + random());
					const state = programState();
					if ( c.graph ) {
						const matrix = rotation( random() * 6, .5 + random() );
						const element = {
							matrix,
							previousPosition: state.position.map( v => v - spread( 1 ) ),
							state,
							clockBirth: birth * 20
						};
						if ( state.previous ) state.previous.matrix.set( ticked( matrix, -spread( .8 ) ) );
						records.writeGraphRecord( particles.records, slot, element, work );
					} else {
						const matrices = rotation( random() * 6, .5 + random() );
						matrices[12] = spread( 50 );
						matrices[14] = spread( 50 );
						const program = random() < .75 ? state : undefined;
						if ( program ) {
							program.frame = Math.max(
								0,
								Math.floor( (time - birth) % 1.5 * 20 ) - (random() < .5 ? 0 : 1)
							);
						}
						records.writeEmittedRecord( particles.records, slot, matrices, 0, birth, program, work );
					}
				};
				for ( let slot = 0; slot < count; slot++ ) write( slot );
				const instances = device.createBuffer( {
						size: count * 160,
						usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_SRC | GPUBufferUsage.COPY_DST
					} ),
					bones = device.createBuffer( {
						size: count * 64,
						usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_SRC | GPUBufferUsage.COPY_DST
					} );
				const key = {};
				const run = async () => {
					owner.present( key, instances, bones, particles );
					const read = device.createBuffer( {
						size: count * 224,
						usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ
					} );
					const encoder = device.createCommandEncoder();
					owner.encode( encoder );
					encoder.copyBufferToBuffer( instances, 0, read, 0, count * 160 );
					encoder.copyBufferToBuffer( bones, 0, read, count * 160, count * 64 );
					device.queue.submit( [ encoder.finish() ] );
					await read.mapAsync( GPUMapMode.READ );
					const data = new Float32Array( read.getMappedRange().slice( 0 ) );
					read.unmap();
					read.destroy();
					for ( let slot = 0; slot < count; slot++ ) {
						const expected = presentSlot( particles, particles.records, slot );
						const instance = data.subarray( slot * 40, slot * 40 + 40 ),
							palette = data.subarray( count * 40 + slot * 16, count * 40 + slot * 16 + 16 );
						if ( !expected ) {
							hidden++;
							if ( instance.subarray( 0, 16 ).some( v => v !== 0 ) ) {
								throw Error( `case ${n} slot ${slot}: hidden slot drawn` );
							}
							continue;
						}
						drawn++;
						const pairs = [
							[ instance.subarray( 0, 16 ), expected.matrix, "matrix" ],
							[ palette, expected.palette, "palette" ],
							[ instance.subarray( 16, 17 ), [ expected.opacity ], "opacity" ],
							[ instance.subarray( 20, 24 ), expected.color, "color" ],
							[ instance.subarray( 24, 28 ), expected.window, "window" ]
						];
						for ( const [actual, wanted, name] of pairs ) {
							for ( let i = 0; i < wanted.length; i++ ) {
								const error = Math.abs( actual[i] - wanted[i] ) / Math.max( 1, Math.abs( wanted[i] ) );
								if ( !(error < 1e-5) ) {
									throw Error(
										`case ${n} ${JSON.stringify( c )} slot ${slot} ${name}[${i}]: ${actual[i]} vs ${
											wanted[i]
										}`
									);
								}
								maxError = Math.max( maxError, error );
								compared++;
							}
						}
					}
				};
				await run();
				// A later frame: new actor clocks, and two rewritten slots marked dirty.
				for ( let row = 0; row < rows; row++ ) particles.actors[row * records.PARTICLE_ACTOR] += .03;
				particles.dirtyStart = 6;
				particles.dirtyEnd = 8;
				write( 6 );
				write( 7 );
				await run();
				live.push( { key, instances, bones, run } );
			}
			const grown = owner.stats().arenaBytes;
			for ( const stream of live.slice( 0, 3 ) ) await stream.run();
			for ( const stream of live ) {
				owner.release( stream.key );
				stream.instances.destroy();
				stream.bones.destroy();
			}
			const stats = { ...owner.stats(), grown };
			owner.dispose();
			device.destroy();
			return { errors, compared, maxError, hidden, drawn, stats };
		} );
		assert.deepEqual( result.errors, [] );
		assert.ok( result.drawn > 800 && result.hidden > 160, JSON.stringify( result ) );
		assert.equal( result.stats.streams, 0 );
		assert.ok( result.stats.grown > 64 * 256, "the arena grew past its first allocation" );
		console.log(
			`compared ${result.compared} values over ${result.drawn} drawn and ${result.hidden} hidden slots, max relative error ${
				result.maxError.toExponential( 2 )
			}`
		);
	} finally {
		await browser.close();
	}
} );
