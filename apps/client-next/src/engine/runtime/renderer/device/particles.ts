/*
===========================================================================

particles.ts - batched GPU particle presentation and frame input ownership

CPU tick records retain their dirty-range contract. Ordered staging copies
update resident records before each batched dispatch. Geometry retains its
draw buffers: initial copies preserve untouched hidden fields, then each pass
returns its computed instances and palettes before drawing. Arenas and pass
pages are partitioned by the actual device storage and dispatch limits.

A frame can encode twice around deferred visibility. Each encode owns a
different input page, so queue writes cannot overwrite an earlier pass's
inputs before submission. beginFrame reuses pages only after the preceding
frame has submitted. Geometry and the device own that lifecycle.

===========================================================================
*/
import { followMatrix } from "@/engine/foundation/animation/particle-holder-link";
import { ROTATION_WORK, turnByStep } from "@/engine/foundation/animation/particle-presentation";
import {
	ACTOR_FOLLOW,
	ACTOR_FRACTION,
	ACTOR_OPACITY,
	ACTOR_PALETTE,
	ACTOR_TIME,
	PARTICLE_ACTOR,
	PARTICLE_RECORD,
	PARTICLE_VIEW_NONE,
	PARTICLE_VIEW_V,
	RECORD_BIRTH,
	RECORD_FLAGS,
	RECORD_LIVE,
	RECORD_MATRIX,
	RECORD_MATRIX_STEP,
	RECORD_MOTION,
	RECORD_POSITION,
	RECORD_POSITION_MOTION,
	RECORD_PRESENTED,
	RECORD_ROTATION,
	RECORD_ROTATION_STEP,
	RECORD_SCALE,
	RECORD_SCALE_MOTION,
	RECORD_TICK,
	RECORD_VELOCITY
} from "@/engine/foundation/animation/particle-records";
import { multiply } from "@/engine/foundation/math/pose-math";
import { faceEffectMesh } from "@/engine/foundation/rendering/effect-billboard";
import type { GeometryDraw, GpuTimingFrame, ParticlePresentation } from "../internal/gpu-contract";
import { particleShader } from "./particle-shader";
import { destroyNow, type Retire } from "./retirement";

const PARAMS_FLOATS = 24;
const WORKGROUP = 64;
const INSTANCE_BYTES = 160;
const PALETTE_BYTES = 64;
const INSTANCE_FLOATS = INSTANCE_BYTES / 4;
const PALETTE_FLOATS = PALETTE_BYTES / 4;
const TICKS_PER_SECOND = 20;
const INITIAL_BYTES = 16384;
const DEFAULT_STORAGE_BYTES = 128 * 1024 * 1024;
const DEFAULT_WORKGROUP_LIMIT = 65535;
const cpuWork = new Float64Array( ROTATION_WORK );
const cpuMatrix = new Float32Array( 16 );
const cpuRotation = new Float32Array( 16 );
const cpuPalette = new Float32Array( 16 );
const cpuView = new Float32Array( 16 );

/*
================
Stream

Tick records copy only admitted dirty ranges. Frame parameters snapshot at
presentation, before the caller can reuse its scratch rows.
================
*/
type Stream = {
	readonly rows: number;
	readonly slots: number;
	readonly frames: ParticlePresentation["frames"];
	readonly records: Float32Array;
	readonly frame: Float32Array;
	readonly words: Uint32Array;
	readonly material: Float32Array;
	readonly instances: GPUBuffer;
	readonly bones: GPUBuffer;
	readonly outputStart: number;
	readonly arena: Arena;
	initialized: boolean;
	dirtyStart: number;
	dirtyEnd: number;
	empty: boolean;
};

/*
================
Page

One immutable input snapshot per encoded pass in a frame. Bindings follow
output arena growth without changing already encoded commands.
================
*/
type Page = {
	readonly records: Float32Array;
	readonly frame: Float32Array;
	readonly words: Uint32Array;
	readonly material: Float32Array;
	readonly buffers: readonly GPUBuffer[];
	binding: GPUBindGroup;
	output: Output;
	readonly count: number;
};

/*
================
Output

Resident slots preserve the fields an expired particle does not overwrite.
================
*/
type Output = {
	readonly count: number;
	readonly records: GPUBuffer;
	readonly instances: GPUBuffer;
	readonly bones: GPUBuffer;
};

/*
================
Arena

Each arena fits the device's largest storage binding. Streams keep their
slot ranges until release; empty arenas are retired at the next frame.
================
*/
type Arena = {
	top: number;
	live: number;
	readonly free: [number, number][];
	readonly pending: Set<Stream>;
	output?: Output;
};

/*
================
createParticlePresentation
================
*/
export function createParticlePresentation( device: GPUDevice, retire: Retire = destroyNow ) {
	let pipeline: GPUComputePipeline | undefined,
		failure: string | null = null,
		disposed = false,
		dispatches = 0,
		slots = 0,
		pageIndex = 0;
	const streams = new Map<GeometryDraw, Stream>(), pending = new Set<Arena>(), pages: Page[] = [];
	const arenas: Arena[] = [], oldOutputs: Output[] = [], batch: Stream[] = [];
	const storageLimit = Math.min(
		device.limits.maxStorageBufferBindingSize ?? DEFAULT_STORAGE_BYTES,
		device.limits.maxBufferSize ?? DEFAULT_STORAGE_BYTES
	);
	const slotLimit = Math.floor( storageLimit / (PARTICLE_RECORD * 4) );
	const groupLimit = device.limits.maxComputeWorkgroupsPerDimension ?? DEFAULT_WORKGROUP_LIMIT;
	const ready = Promise.resolve()
		.then( () =>
			device.createComputePipelineAsync( {
				label: "particle-presentation",
				layout: "auto",
				compute: {
					module: device.createShaderModule( { label: "particle-presentation", code: particleShader } ),
					entryPoint: "main"
				}
			} )
		)
		.then( value => {
			if ( !disposed ) pipeline = value;
		} )
		.catch( error => {
			if ( !disposed ) failure = String( error );
		} );

	/*
	================
	capacity
	================
	*/
	function capacity( bytes: number ): number {
		if ( bytes > storageLimit ) throw Error( "Particle buffer exceeds device storage limit" );
		let size = INITIAL_BYTES;
		while ( size < bytes ) size *= 2;
		return Math.min( size, storageLimit );
	}

	/*
	================
	allocateOutput
	================
	*/
	function allocateOutput( count: number ): { arena: Arena; outputStart: number; } {
		if ( count > slotLimit ) throw Error( "Particle stream exceeds device storage limit" );
		for ( const arena of arenas ) {
			const index = arena.free.findIndex( block => block[1] >= count );
			if ( index < 0 && arena.top + count > slotLimit ) continue;
			arena.live++;
			if ( index < 0 ) {
				const outputStart = arena.top;
				arena.top += count;
				return { arena, outputStart };
			}
			const block = arena.free[index]!, outputStart = block[0];
			block[0] += count;
			block[1] -= count;
			if ( !block[1] ) arena.free.splice( index, 1 );
			return { arena, outputStart };
		}
		const arena: Arena = { top: count, live: 1, free: [], pending: new Set() };
		arenas.push( arena );
		return { arena, outputStart: 0 };
	}

	/*
	================
	reclaimOutput
	================
	*/
	function reclaimOutput( arena: Arena, start: number, count: number ) {
		const free = arena.free;
		arena.live--;
		let at = free.findIndex( block => block[0] > start );
		if ( at < 0 ) at = free.length;
		free.splice( at, 0, [ start, count ] );
		const next = free[at + 1];
		if ( next && start + count === next[0] ) {
			free[at]![1] += next[1];
			free.splice( at + 1, 1 );
		}
		const previous = free[at - 1];
		if ( previous && previous[0] + previous[1] === start ) {
			previous[1] += free[at]![1];
			free.splice( at, 1 );
		}
		const tail = free.at( -1 );
		if ( tail && tail[0] + tail[1] === arena.top ) {
			arena.top = tail[0];
			free.pop();
		}
	}

	/*
	================
	ensureOutput

	The copy is encoded after previous deferred work, so it preserves that
	work too. Old buffers survive until the next frame begins after submit.
	================
	*/
	function ensureOutput( encoder: GPUCommandEncoder, arena: Arena ): Output {
		const output = arena.output;
		if ( output && output.count >= arena.top ) return output;
		const count = Math.floor( capacity( arena.top * PARTICLE_RECORD * 4 ) / (PARTICLE_RECORD * 4) );
		const usage = GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_SRC | GPUBufferUsage.COPY_DST;
		const instances = device.createBuffer( {
			label: "particle-output-instances",
			size: count * INSTANCE_BYTES,
			usage
		} );
		let bones: GPUBuffer | undefined, records: GPUBuffer;
		try {
			bones = device.createBuffer( { label: "particle-output-bones", size: count * PALETTE_BYTES, usage } );
			records = device.createBuffer( {
				label: "particle-resident-records",
				size: count * PARTICLE_RECORD * 4,
				usage
			} );
		} catch ( error ) {
			instances.destroy();
			bones?.destroy();
			throw error;
		}
		if ( output ) {
			encoder.copyBufferToBuffer( output.instances, 0, instances, 0, output.count * INSTANCE_BYTES );
			encoder.copyBufferToBuffer( output.bones, 0, bones, 0, output.count * PALETTE_BYTES );
			encoder.copyBufferToBuffer( output.records, 0, records, 0, output.count * PARTICLE_RECORD * 4 );
			oldOutputs.push( output );
		}
		arena.output = { count, records, instances, bones };
		return arena.output;
	}

	/*
	================
	bindPage
	================
	*/
	function bindPage( buffers: readonly GPUBuffer[], output: Output ): GPUBindGroup {
		return device.createBindGroup( {
			layout: pipeline!.getBindGroupLayout( 0 ),
			entries: [ output!.records, buffers[1]!, buffers[2]!, output!.instances, output!.bones ].map( (
				buffer,
				binding
			) => ({ binding, resource: { buffer } }) )
		} );
	}

	/*
	================
	pageFor

	This ordinal has not been encoded in the current frame. Growth retires
	its previous buffers through the device's ordinary frame lifetime owner.
	================
	*/
	function pageFor( count: number, frameFloats: number, materialFloats: number, output: Output ): Page {
		const index = pageIndex++, previous = pages[index];
		if (
			previous && previous.count >= count && previous.frame.length >= frameFloats &&
			previous.material.length >= materialFloats
		) {
			if ( previous.output !== output ) {
				previous.binding = bindPage( previous.buffers, output );
				previous.output = output!;
			}
			return previous;
		}
		const records = new Float32Array( capacity( count * PARTICLE_RECORD * 4 ) / 4 );
		const frame = new Float32Array( capacity( frameFloats * 4 ) / 4 );
		const material = new Float32Array( capacity( materialFloats * 4 ) / 4 );
		const sizes = [
			records.byteLength,
			frame.byteLength,
			material.byteLength
		];
		const buffers: GPUBuffer[] = [];
		try {
			for ( const size of sizes ) {
				buffers.push( device.createBuffer( {
					label: "particle-batch",
					size,
					usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC
				} ) );
			}
			const binding = bindPage( buffers, output );
			const countCapacity = records.length / PARTICLE_RECORD;
			const page = {
				records,
				frame,
				words: new Uint32Array( frame.buffer ),
				material,
				buffers,
				binding,
				output: output!,
				count: countCapacity
			};
			pages[index] = page;
			if ( previous ) { for ( const buffer of previous.buffers ) retire( buffer ); }
			return page;
		} catch ( error ) {
			for ( const buffer of buffers ) buffer.destroy();
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
		stream.arena.pending.delete( stream );
		if ( !stream.arena.pending.size ) pending.delete( stream.arena );
		reclaimOutput( stream.arena, stream.outputStart, stream.rows * stream.slots );
		streams.delete( draw );
	}

	/*
	================
	encodeBatch

	One device-bounded dispatch over immutable input staging and resident output.
	================
	*/
	function encodeBatch(
		encoder: GPUCommandEncoder,
		timing: GpuTimingFrame | undefined,
		selected: readonly Stream[],
		target: Output
	) {
		let count = 0, dirtyCount = 0, groups = 0, frameFloats = 0, materialFloats = 0;
		for ( const stream of selected ) {
			const size = stream.rows * stream.slots;
			count += size;
			dirtyCount += Math.max( 0, stream.dirtyEnd - stream.dirtyStart );
			groups += Math.ceil( size / WORKGROUP );
			frameFloats += stream.frame.length;
			materialFloats += stream.material.length;
		}
		const page = pageFor( dirtyCount, frameFloats + groups * 4, materialFloats, target );
		let dirtySlot = 0, group = 0, at = groups * 4, material = 0;
		for ( const stream of selected ) {
			const size = stream.rows * stream.slots;
			if ( stream.dirtyStart < stream.dirtyEnd ) {
				const dirty = stream.dirtyEnd - stream.dirtyStart;
				page.records.set(
					stream.records.subarray( stream.dirtyStart * PARTICLE_RECORD, stream.dirtyEnd * PARTICLE_RECORD ),
					dirtySlot * PARTICLE_RECORD
				);
				encoder.copyBufferToBuffer(
					page.buffers[0]!,
					dirtySlot * PARTICLE_RECORD * 4,
					target.records,
					(stream.outputStart + stream.dirtyStart) * PARTICLE_RECORD * 4,
					dirty * PARTICLE_RECORD * 4
				);
				dirtySlot += dirty;
				stream.dirtyStart = size;
				stream.dirtyEnd = 0;
			}
			page.frame.set( stream.frame, at );
			page.words[at + 9] = material / 4;
			page.material.set( stream.material, material );
			for ( let offset = 0; offset < size; offset += WORKGROUP ) {
				page.words[group++] = at / 4;
				page.words[group++] = stream.outputStart;
				page.words[group++] = stream.outputStart;
				page.words[group++] = offset;
			}
			if ( !stream.initialized ) {
				encoder.copyBufferToBuffer(
					stream.instances,
					0,
					target.instances,
					stream.outputStart * INSTANCE_BYTES,
					size * INSTANCE_BYTES
				);
				encoder.copyBufferToBuffer(
					stream.bones,
					0,
					target.bones,
					stream.outputStart * PALETTE_BYTES,
					size * PALETTE_BYTES
				);
				stream.initialized = true;
			}
			at += stream.frame.length;
			material += stream.material.length;
		}
		if ( dirtySlot ) {
			device.queue.writeBuffer( page.buffers[0]!, 0, page.records.buffer, 0, dirtySlot * PARTICLE_RECORD * 4 );
		}
		device.queue.writeBuffer( page.buffers[1]!, 0, page.frame.buffer, 0, at * 4 );
		if ( material ) device.queue.writeBuffer( page.buffers[2]!, 0, page.material.buffer, 0, material * 4 );
		const pass = encoder.beginComputePass( {
			label: "particle-presentation",
			timestampWrites: timing?.pass( "particle-presentation" )
		} );
		pass.setPipeline( pipeline! );
		pass.setBindGroup( 0, page.binding );
		pass.dispatchWorkgroups( groups );
		pass.end();
		for ( const stream of selected ) {
			const size = stream.rows * stream.slots;
			encoder.copyBufferToBuffer(
				target.instances,
				stream.outputStart * INSTANCE_BYTES,
				stream.instances,
				0,
				size * INSTANCE_BYTES
			);
			encoder.copyBufferToBuffer(
				target.bones,
				stream.outputStart * PALETTE_BYTES,
				stream.bones,
				0,
				size * PALETTE_BYTES
			);
		}
		dispatches++;
		slots += count;
	}

	/*
	================
	encodeCpuStream

	Degraded fallback when the device driver rejects the particle-presentation
	compute pipeline: evaluate the stream's slots on the CPU and upload its
	instance and palette buffers directly.
	================
	*/
	function encodeCpuStream( stream: Stream ) {
		const size = stream.rows * stream.slots;
		const instances = new Float32Array( size * INSTANCE_FLOATS );
		const bones = new Float32Array( size * PALETTE_FLOATS );
		const data = stream.records, frame = stream.frame, words = stream.words;
		const graph = words[2] === 1, view = words[3]!, lifetime = frame[4]!, loop = frame[5]! > 0.5;
		const fps = frame[6]!, frameCount = frame[7]!, stepSampling = words[8] === 1;
		for ( let k = 0; k < 3; k++ ) {
			cpuView[k * 4] = frame[12 + k]!;
			cpuView[k * 4 + 1] = frame[16 + k]!;
			cpuView[k * 4 + 3] = frame[20 + k]!;
		}
		for ( let slot = 0; slot < size; slot++ ) {
			const at = slot * PARTICLE_RECORD;
			const row = PARAMS_FLOATS + Math.floor( slot / stream.slots ) * PARTICLE_ACTOR;
			let age = frame[row + ACTOR_TIME]! - data[at + RECORD_BIRTH]!;
			if ( loop && age >= 0 && lifetime > 0 ) age %= lifetime;
			const flags = data[at + RECORD_FLAGS]!;
			if ( !(flags & RECORD_LIVE) || age < 0 || age >= lifetime ) continue;
			const rawFraction = graph ?
				frame[row + ACTOR_FRACTION]! :
				age * TICKS_PER_SECOND - data[at + RECORD_TICK]!;
			const fraction = Math.max( 0, Math.min( 1, rawFraction ) );
			const step = at + RECORD_MATRIX_STEP;
			turnByStep(
				data.subarray( at + RECORD_MATRIX, at + RECORD_MATRIX + 16 ),
				data[step]!,
				data[step + 1]!,
				data[step + 2]!,
				data[step + 3]!,
				fraction,
				cpuMatrix,
				0,
				cpuWork
			);
			for ( let axis = 0; axis < 3; axis++ ) {
				cpuMatrix[12 + axis] = data[at + RECORD_MATRIX + 12 + axis]! +
					data[at + RECORD_MOTION + axis]! * fraction;
			}
			followMatrix( flags, frame, row + ACTOR_FOLLOW, cpuMatrix, 0 );
			const presented = (flags & RECORD_PRESENTED) !== 0;
			for ( let i = 0; i < 16; i++ ) cpuPalette[i] = frame[row + ACTOR_PALETTE + i]!;
			if ( presented ) {
				for ( let axis = 0; axis < 3; axis++ ) {
					const scale = data[at + RECORD_SCALE + axis]! + data[at + RECORD_SCALE_MOTION + axis]! * fraction;
					for ( let k = 0; k < 3; k++ ) cpuPalette[axis * 4 + k]! *= scale;
					if ( !graph ) {
						cpuPalette[12 + axis]! += data[at + RECORD_POSITION + axis]! +
							data[at + RECORD_POSITION_MOTION + axis]! * fraction;
					}
				}
			}
			if ( view !== PARTICLE_VIEW_NONE ) {
				const velocity = presented ?
					[
						data[at + RECORD_VELOCITY]!,
						data[at + RECORD_VELOCITY + 1]!,
						data[at + RECORD_VELOCITY + 2]!
					] as const :
					undefined;
				faceEffectMesh(
					cpuPalette,
					0,
					cpuMatrix,
					cpuView,
					view === PARTICLE_VIEW_V ? "v" : "camera",
					velocity
				);
			}
			if ( presented ) {
				const rotStep = at + RECORD_ROTATION_STEP;
				turnByStep(
					data.subarray( at + RECORD_ROTATION, at + RECORD_ROTATION + 16 ),
					data[rotStep]!,
					data[rotStep + 1]!,
					data[rotStep + 2]!,
					data[rotStep + 3]!,
					fraction,
					cpuRotation,
					0,
					cpuWork
				);
				multiply( cpuPalette.slice(), cpuRotation, cpuPalette );
			}
			const outAt = slot * INSTANCE_FLOATS;
			instances.set( cpuMatrix, outAt );
			instances[outAt + 16] = frame[row + ACTOR_OPACITY]!;
			if ( frameCount > 0 && stream.frames ) {
				const colors = stream.frames.colors, windows = stream.frames.windows;
				const position = Math.max( 0, Math.min( frameCount - 1, age * fps ) );
				const index = Math.floor( position );
				const next = Math.min( frameCount - 1, index + 1 );
				const blend = stepSampling ? 0 : position - index;
				for ( let c = 0; c < 4; c++ ) {
					instances[outAt + 20 + c] = colors[index * 4 + c]! * (1 - blend) + colors[next * 4 + c]! * blend;
					instances[outAt + 24 + c] = windows[index * 4 + c]!;
				}
			} else {
				instances[outAt + 20] = 1;
				instances[outAt + 21] = 1;
				instances[outAt + 22] = 1;
				instances[outAt + 23] = 1;
				instances[outAt + 24] = 1;
				instances[outAt + 25] = 1;
			}
			bones.set( cpuPalette, slot * PALETTE_FLOATS );
		}
		device.queue.writeBuffer( stream.instances, 0, instances.buffer, 0, instances.byteLength );
		device.queue.writeBuffer( stream.bones, 0, bones.buffer, 0, bones.byteLength );
		stream.dirtyStart = size;
		stream.dirtyEnd = 0;
		stream.initialized = true;
		dispatches++;
		slots += size;
	}

	return {
		ready,
		failed: () => failure !== null,
		/*
		================
		beginFrame
		================
		*/
		beginFrame() {
			for ( let i = arenas.length - 1; i >= 0; i-- ) {
				const arena = arenas[i]!;
				if ( arena.live ) continue;
				if ( arena.output ) oldOutputs.push( arena.output );
				arenas.splice( i, 1 );
			}
			for ( const old of oldOutputs ) {
				retire( old.instances );
				retire( old.bones );
				retire( old.records );
			}
			oldOutputs.length = 0;
			pageIndex = 0;
		},
		/*
		================
		present
		================
		*/
		present( draw: GeometryDraw, instances: GPUBuffer, bones: GPUBuffer, particles: ParticlePresentation ) {
			if ( disposed || (!pipeline && !failure) ) throw Error( "Particle presentation is not ready" );
			const count = particles.rows * particles.slots;
			if (
				!Number.isSafeInteger( particles.rows ) || particles.rows <= 0 ||
				!Number.isSafeInteger( particles.slots ) || particles.slots <= 0 ||
				!Number.isSafeInteger( count ) || count <= 0 || particles.records.length !== count * PARTICLE_RECORD ||
				particles.actors.length !== particles.rows * PARTICLE_ACTOR || particles.axes.length !== 12
			) {
				throw Error( "Invalid particle presentation" );
			}
			let stream = streams.get( draw ), start = particles.dirtyStart, end = particles.dirtyEnd;
			if (
				stream &&
				(stream.rows !== particles.rows || stream.slots !== particles.slots ||
					stream.frames !== particles.frames)
			) {
				throw Error( "Particle presentation changed shape" );
			}
			if ( !stream ) {
				const groups = Math.ceil( count / WORKGROUP );
				const materialBytes = particles.frames ?
					particles.frames.colors.byteLength + particles.frames.windows.byteLength :
					0;
				if (
					count > slotLimit || groups > groupLimit || materialBytes > storageLimit ||
					(PARAMS_FLOATS + particles.rows * PARTICLE_ACTOR) * 4 + groups * 16 > storageLimit
				) {
					throw Error( "Particle stream exceeds device storage limit" );
				}
				if ( count * INSTANCE_BYTES > instances.size || count * PALETTE_BYTES > bones.size ) {
					throw Error( "Particle presentation outside its draw" );
				}
				const frame = new Float32Array( PARAMS_FLOATS + particles.rows * PARTICLE_ACTOR );
				const material = new Float32Array(
					particles.frames ? particles.frames.colors.length + particles.frames.windows.length : 0
				);
				if ( particles.frames ) {
					material.set( particles.frames.colors );
					material.set( particles.frames.windows, particles.frames.colors.length );
				}
				stream = {
					rows: particles.rows,
					slots: particles.slots,
					frames: particles.frames,
					records: new Float32Array( count * PARTICLE_RECORD ),
					frame,
					words: new Uint32Array( frame.buffer ),
					material,
					instances,
					bones,
					...allocateOutput( count ),
					initialized: false,
					dirtyStart: 0,
					dirtyEnd: count,
					empty: false
				};
				streams.set( draw, stream );
				start = 0;
				end = count;
			}
			if ( start < end ) {
				if ( start < 0 || end > count ) throw Error( "Invalid particle record range" );
				stream.records.set(
					particles.records.subarray( start * PARTICLE_RECORD, end * PARTICLE_RECORD ),
					start * PARTICLE_RECORD
				);
				stream.dirtyStart = Math.min( stream.dirtyStart, start );
				stream.dirtyEnd = Math.max( stream.dirtyEnd, end );
			}
			if ( particles.live === 0 && stream.empty ) return;
			stream.empty = particles.live === 0;
			const words = stream.words, frame = stream.frame;
			words[0] = particles.slots;
			words[1] = count;
			words[2] = particles.graph ? 1 : 0;
			words[3] = particles.view;
			frame[4] = particles.lifetime;
			frame[5] = particles.loop ? 1 : 0;
			frame[6] = particles.frames?.fps ?? 0;
			frame[7] = particles.frames ? particles.frames.colors.length / 4 : 0;
			words[8] = particles.frames?.sampling === "step" ? 1 : 0;
			frame.set( particles.axes, 12 );
			frame.set( particles.actors, PARAMS_FLOATS );
			stream.arena.pending.add( stream );
			pending.add( stream.arena );
		},
		/*
		================
		encode

		Partition independent streams by resident arena and per-pass limits.
		No stream or update is dropped when a batch reaches a device limit.
		================
		*/
		encode( encoder: GPUCommandEncoder, timing?: GpuTimingFrame ) {
			if ( !pipeline ) {
				for ( const arena of pending ) {
					for ( const stream of arena.pending ) encodeCpuStream( stream );
					arena.pending.clear();
				}
				pending.clear();
				return;
			}
			for ( const arena of pending ) {
				const target = ensureOutput( encoder, arena );
				let groups = 0, frameBytes = 0, materialBytes = 0;
				for ( const stream of arena.pending ) {
					const nextGroups = Math.ceil( stream.rows * stream.slots / WORKGROUP );
					const nextFrameBytes = stream.frame.byteLength + nextGroups * 16;
					if (
						nextGroups > groupLimit || nextFrameBytes > storageLimit ||
						stream.material.byteLength > storageLimit
					) {
						throw Error( "Particle stream exceeds device dispatch limit" );
					}
					if (
						batch.length &&
						(groups + nextGroups > groupLimit || frameBytes + nextFrameBytes > storageLimit ||
							materialBytes + stream.material.byteLength > storageLimit)
					) {
						encodeBatch( encoder, timing, batch, target );
						batch.length = 0;
						groups = frameBytes = materialBytes = 0;
					}
					batch.push( stream );
					groups += nextGroups;
					frameBytes += nextFrameBytes;
					materialBytes += stream.material.byteLength;
				}
				if ( batch.length ) encodeBatch( encoder, timing, batch, target );
				batch.length = 0;
				arena.pending.clear();
			}
			pending.clear();
		},
		release,
		stats: () => ({
			streams: streams.size,
			dispatches,
			slots,
			arenaBytes: pages.reduce( ( sum, page ) => sum + page.frame.byteLength, 0 )
		}),
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			disposed = true;
			streams.clear();
			pending.clear();
			for ( const page of pages ) for ( const buffer of page.buffers ) buffer.destroy();
			for ( const arena of arenas ) {
				arena.output?.instances.destroy();
				arena.output?.bones.destroy();
				arena.output?.records.destroy();
			}
			for ( const old of oldOutputs ) {
				old.instances.destroy();
				old.bones.destroy();
				old.records.destroy();
			}
			oldOutputs.length = 0;
			arenas.length = 0;
			batch.length = 0;
			pages.length = 0;
			pipeline = undefined;
		}
	};
}
