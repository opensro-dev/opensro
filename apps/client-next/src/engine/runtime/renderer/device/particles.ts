/*
===========================================================================

particles.ts - GPU emitted particle presentation: streams and encoding

Each emitted primitive's draw owns one stream: its particle records and
material frames, and a block of the shared frame arena holding its pass
parameters and actor rows. A presentation uploads the records the CPU
rewrote at a tick and copies the frame's parameters and rows into the
arena; the encode writes the whole arena once and the pass then writes
every slot's instance and palette straight into the draw's own buffers
(particle-shader.ts). No readback is needed.

The arena exists because the frame data is small and per stream: one
queue write per stream per frame cost more than the pass saved.

Geometry owns the draw buffers; a stream borrows them until the draw is
released.

===========================================================================
*/

import { PARTICLE_ACTOR, PARTICLE_RECORD } from "@/engine/foundation/animation/particle-records";
import type { GeometryDraw, GpuTimingFrame, ParticlePresentation } from "../internal/gpu-contract";
import { particleShader } from "./particle-shader";

// The Params struct at the head of particle-shader.ts's Frame block.
const PARAMS_FLOATS = 24;
// particle-shader.ts @workgroup_size.
const WORKGROUP = 64;
// Arena blocks start at storage binding offsets: the WebGPU default
// minStorageBufferOffsetAlignment.
const ARENA_UNIT = 256;
const ARENA_INITIAL_UNITS = 64;

type Stream = {
	readonly rows: number;
	readonly slots: number;
	readonly frames: ParticlePresentation["frames"];
	readonly records: GPUBuffer;
	readonly materialFrames?: GPUBuffer;
	readonly instances: GPUBuffer;
	readonly bones: GPUBuffer;
	// The arena block: first unit and unit count.
	readonly start: number;
	readonly units: number;
	binding: GPUBindGroup;
};

/*
================
createParticlePresentation
================
*/
export function createParticlePresentation( device: GPUDevice ) {
	let pipeline: GPUComputePipeline | undefined, disposed = false, dispatches = 0, slots = 0;
	const streams = new Map<GeometryDraw, Stream>(), pending = new Set<Stream>();
	// A primitive without material frames reads none; the binding still needs a buffer.
	const noFrames = device.createBuffer( { label: "particle-frames", size: 16, usage: GPUBufferUsage.STORAGE } );
	// The frame arena, its CPU copy and free blocks ([ start, units ], by start).
	let arena = createArena( ARENA_INITIAL_UNITS ),
		mirror = new Float32Array( ARENA_INITIAL_UNITS * ARENA_UNIT / 4 ),
		words = new Uint32Array( mirror.buffer ),
		top = 0;
	const free: [number, number][] = [ [ 0, ARENA_INITIAL_UNITS ] ];
	const ready = device.createComputePipelineAsync( {
		label: "particle-presentation",
		layout: "auto",
		compute: {
			module: device.createShaderModule( { label: "particle-presentation", code: particleShader } ),
			entryPoint: "main"
		}
	} ).then( value => {
		if ( !disposed ) pipeline = value;
	} );

	/*
	================
	createArena
	================
	*/
	function createArena( units: number ): GPUBuffer {
		return device.createBuffer( {
			label: "particle-frame",
			size: units * ARENA_UNIT,
			usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
		} );
	}

	/*
	================
	bind

	The stream's binding over the current arena.
	================
	*/
	function bind( stream: Omit<Stream, "binding"> ): GPUBindGroup {
		return device.createBindGroup( {
			layout: pipeline!.getBindGroupLayout( 0 ),
			entries: [
				{ binding: 0, resource: { buffer: stream.records } },
				{
					binding: 1,
					resource: {
						buffer: arena,
						offset: stream.start * ARENA_UNIT,
						size: (PARAMS_FLOATS + stream.rows * PARTICLE_ACTOR) * 4
					}
				},
				{ binding: 2, resource: { buffer: stream.materialFrames ?? noFrames } },
				{ binding: 3, resource: { buffer: stream.instances } },
				{ binding: 4, resource: { buffer: stream.bones } }
			]
		} );
	}

	/*
	================
	allocate

	The first free block of units, growing the arena (and rebinding every
	stream to the new one) when none fits.
	================
	*/
	function allocate( units: number ): number {
		let at = free.findIndex( block => block[1] >= units );
		if ( at < 0 ) {
			const capacity = mirror.length * 4 / ARENA_UNIT,
				last = free.at( -1 ),
				tail = last && last[0] + last[1] === capacity ? last[1] : 0,
				grown = Math.max( capacity * 2, capacity + units - tail );
			const replacement = createArena( grown ), copy = new Float32Array( grown * ARENA_UNIT / 4 );
			copy.set( mirror );
			arena.destroy();
			arena = replacement;
			mirror = copy;
			words = new Uint32Array( mirror.buffer );
			if ( tail ) last![1] += grown - capacity;
			else free.push( [ capacity, grown - capacity ] );
			for ( const stream of streams.values() ) stream.binding = bind( stream );
			at = free.findIndex( block => block[1] >= units );
		}
		const block = free[at]!, start = block[0];
		block[0] += units;
		block[1] -= units;
		if ( !block[1] ) free.splice( at, 1 );
		top = Math.max( top, start + units );
		return start;
	}

	/*
	================
	reclaim

	Return a block to the free list, merging it with its neighbours.
	================
	*/
	function reclaim( start: number, units: number ) {
		let at = free.findIndex( block => block[0] > start );
		if ( at < 0 ) at = free.length;
		free.splice( at, 0, [ start, units ] );
		const next = free[at + 1];
		if ( next && start + units === next[0] ) {
			free[at]![1] += next[1];
			free.splice( at + 1, 1 );
		}
		const previous = free[at - 1];
		if ( previous && previous[0] + previous[1] === start ) {
			previous[1] += free[at]![1];
			free.splice( at, 1 );
		}
	}

	/*
	================
	createStream

	The stream's buffers, arena block and binding, released again if any
	step throws.
	================
	*/
	function createStream(
		particles: ParticlePresentation,
		instances: GPUBuffer,
		bones: GPUBuffer
	): Stream {
		const owned: GPUBuffer[] = [];
		const storage = ( label: string, bytes: number ) => {
			const buffer = device.createBuffer( {
				label,
				size: bytes,
				usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
			} );
			owned.push( buffer );
			return buffer;
		};
		const units = Math.ceil( (PARAMS_FLOATS + particles.rows * PARTICLE_ACTOR) * 4 / ARENA_UNIT );
		const start = allocate( units );
		try {
			const records = storage( "particle-records", particles.rows * particles.slots * PARTICLE_RECORD * 4 );
			let materialFrames: GPUBuffer | undefined;
			if ( particles.frames ) {
				const { colors, windows } = particles.frames, data = new Float32Array( colors.length + windows.length );
				data.set( colors );
				data.set( windows, colors.length );
				materialFrames = storage( "particle-frames", data.byteLength );
				device.queue.writeBuffer( materialFrames, 0, data.buffer, data.byteOffset, data.byteLength );
			}
			const stream = {
				rows: particles.rows,
				slots: particles.slots,
				frames: particles.frames,
				records,
				materialFrames,
				instances,
				bones,
				start,
				units
			};
			return { ...stream, binding: bind( stream ) };
		} catch ( error ) {
			for ( const buffer of owned ) buffer.destroy();
			reclaim( start, units );
			throw error;
		}
	}

	/*
	================
	release
	================
	*/
	function release( draw: GeometryDraw ) {
		const stream = streams.get( draw );
		if ( !stream ) return;
		pending.delete( stream );
		stream.records.destroy();
		stream.materialFrames?.destroy();
		reclaim( stream.start, stream.units );
		streams.delete( draw );
	}

	return {
		ready,
		/*
		================
		present

		Queue the draw's pass for this frame. The first presentation uploads
		every record; later ones the slots marked dirty.
		================
		*/
		present( draw: GeometryDraw, instances: GPUBuffer, bones: GPUBuffer, particles: ParticlePresentation ) {
			if ( disposed || !pipeline ) throw Error( "Particle presentation is not ready" );
			const count = particles.rows * particles.slots;
			if (
				!Number.isSafeInteger( count ) || count <= 0 || particles.records.length !== count * PARTICLE_RECORD ||
				particles.actors.length !== particles.rows * PARTICLE_ACTOR || particles.axes.length !== 12
			) throw Error( "Invalid particle presentation" );
			let stream = streams.get( draw ), start = particles.dirtyStart, end = particles.dirtyEnd;
			if (
				stream &&
				(stream.rows !== particles.rows || stream.slots !== particles.slots ||
					stream.frames !== particles.frames)
			) {
				throw Error( "Particle presentation changed shape" );
			}
			if ( !stream ) {
				// A draw's buffers are fixed for its life: their size is checked
				// once (reading it crosses into the browser every call).
				if ( count * 160 > instances.size || count * 64 > bones.size ) {
					throw Error( "Particle presentation outside its draw" );
				}
				stream = createStream( particles, instances, bones );
				streams.set( draw, stream );
				start = 0;
				end = count;
			}
			if ( start < end ) {
				if ( start < 0 || end > count ) throw Error( "Invalid particle record range" );
				device.queue.writeBuffer(
					stream.records,
					start * PARTICLE_RECORD * 4,
					particles.records.buffer as ArrayBuffer,
					particles.records.byteOffset + start * PARTICLE_RECORD * 4,
					(end - start) * PARTICLE_RECORD * 4
				);
			}
			const at = stream.start * ARENA_UNIT / 4;
			words[at] = particles.slots;
			words[at + 1] = count;
			words[at + 2] = particles.graph ? 1 : 0;
			words[at + 3] = particles.view;
			mirror[at + 4] = particles.lifetime;
			mirror[at + 5] = particles.loop ? 1 : 0;
			mirror[at + 6] = particles.frames?.fps ?? 0;
			mirror[at + 7] = particles.frames ? particles.frames.colors.length / 4 : 0;
			words[at + 8] = particles.frames?.sampling === "step" ? 1 : 0;
			mirror.set( particles.axes, at + 12 );
			mirror.set( particles.actors, at + PARAMS_FLOATS );
			pending.add( stream );
		},
		/*
		================
		encode

		One arena write, then one dispatch per presented stream.
		================
		*/
		encode( encoder: GPUCommandEncoder, timing?: GpuTimingFrame ) {
			if ( !pending.size ) return;
			device.queue.writeBuffer( arena, 0, mirror.buffer, 0, top * ARENA_UNIT );
			const pass = encoder.beginComputePass( {
				label: "particle-presentation",
				timestampWrites: timing?.pass( "particle-presentation" )
			} );
			pass.setPipeline( pipeline! );
			for ( const stream of pending ) {
				const count = stream.rows * stream.slots;
				pass.setBindGroup( 0, stream.binding );
				pass.dispatchWorkgroups( Math.ceil( count / WORKGROUP ) );
				dispatches++;
				slots += count;
			}
			pass.end();
			pending.clear();
		},
		release,
		stats: () => ({ streams: streams.size, dispatches, slots, arenaBytes: mirror.byteLength }),
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			disposed = true;
			for ( const draw of [ ...streams.keys() ] ) release( draw );
			noFrames.destroy();
			arena.destroy();
			pipeline = undefined;
		}
	};
}
