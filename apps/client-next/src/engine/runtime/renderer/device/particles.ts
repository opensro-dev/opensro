/*
===========================================================================

particles.ts - batched GPU particle presentation and frame input ownership

CPU tick records retain their dirty-range contract. Ordered staging copies
update resident records before each batched dispatch. A stream's draw reads
its computed instances and palettes in place, from its slice of the arena
output (ParticleBinding): one initial copy preserves the draw's untouched
hidden fields, and nothing is copied back. Copying every stream's output
into the draw's own buffers each frame was the GPU process's largest
particle cost (#4692). Arenas and pass pages are partitioned by the actual
device storage and dispatch limits. Material frames are resident too
(particle-materials.ts): a page carries only what changes with the frame.

A slice is bound before the frame records its bundles, which is before
encode: present allocates and grows the output and binds the stream, and
encode only copies a grown arena's old contents across.

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
import type { GpuWork } from "@/engine/foundation/rendering/gpu-work";
import type { GeometryDraw, GpuTimingFrame, ParticlePresentation } from "../internal/gpu-contract";
import { bindingOffsetAlignment, type BufferSlice } from "./buffer-slabs";
import {
	createParticleMaterials,
	type MaterialStore,
	type MaterialTable,
	particleCapacity
} from "./particle-materials";
import { particleShader } from "./particle-shader";
import { destroyNow, type Retire } from "./retirement";

const PARAMS_FLOATS = 24;
const WORKGROUP = 64;
const INSTANCE_BYTES = 160;
const PALETTE_BYTES = 64;
const INSTANCE_FLOATS = INSTANCE_BYTES / 4;
const PALETTE_FLOATS = PALETTE_BYTES / 4;
const TICKS_PER_SECOND = 20;
const DEFAULT_STORAGE_BYTES = 128 * 1024 * 1024;
const DEFAULT_WORKGROUP_LIMIT = 65535;
const cpuWork = new Float64Array( ROTATION_WORK );
const cpuMatrix = new Float32Array( 16 );
const cpuRotation = new Float32Array( 16 );
const cpuPalette = new Float32Array( 16 );
const cpuView = new Float32Array( 16 );

/*
================
ParticleBinding

What a stream's draw binds for its instances and palettes: its slice of
the arena output.
================
*/
export type ParticleBinding = {
	readonly instances: BufferSlice["binding"];
	readonly bones: GPUBufferBinding;
};

/*
================
greatestDivisor
================
*/
function greatestDivisor( a: number, b: number ): number {
	while ( b ) [a, b] = [ b, a % b ];
	return a;
}

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
	// The stream's material frames in their resident store; none without
	// frames, or under the CPU fallback, which reads frames itself.
	readonly table: MaterialTable | undefined;
	readonly instances: GPUBuffer;
	readonly bones: GPUBuffer;
	readonly outputStart: number;
	// The slots reserved at outputStart: rows * slots, rounded up so every
	// slice starts on the binding offset alignment.
	readonly outputSlots: number;
	readonly arena: Arena;
	// Points the draw at its slice; called again when the arena's output grows.
	readonly bind: ( binding: ParticleBinding ) => void;
	// The draw reads the arena output (false under the CPU fallback, which
	// writes the draw's own buffers).
	bound: boolean;
	initialized: boolean;
	dirtyStart: number;
	dirtyEnd: number;
	empty: boolean;
};

/*
================
Page

One immutable input snapshot per encoded pass in a frame: dirty records
and frame parameters. Bindings follow output arena growth and the pass's
material store without changing already encoded commands.
================
*/
type Page = {
	readonly records: Float32Array;
	readonly frame: Float32Array;
	readonly words: Uint32Array;
	readonly buffers: readonly GPUBuffer[];
	binding: GPUBindGroup;
	output: Output;
	materials: GPUBuffer;
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
	// The arena's streams this frame. encode replaces it with a new set
	// instead of clearing it (V8 puts a long-lived set's next table in the
	// old generation, where only a full collection frees it).
	pending: Set<Stream>;
	output?: Output;
	// The output a growth replaced: encode copies its contents into the new
	// one before any dispatch or pass reads it (flushGrowth).
	copyFrom?: Output;
};

/*
================
createParticlePresentation
================
*/
export function createParticlePresentation( device: GPUDevice, retire: Retire = destroyNow, work?: GpuWork ) {
	let pipeline: GPUComputePipeline | undefined,
		failure: string | null = null,
		disposed = false,
		dispatches = 0,
		slots = 0,
		pageIndex = 0;
	const streams = new Map<GeometryDraw, Stream>(), pages: Page[] = [];
	// The arenas with streams this frame: a new set after each encode.
	let pending = new Set<Arena>();
	const arenas: Arena[] = [], oldOutputs: Output[] = [], batch: Stream[] = [];
	const storageLimit = Math.min(
		device.limits.maxStorageBufferBindingSize ?? DEFAULT_STORAGE_BYTES,
		device.limits.maxBufferSize ?? DEFAULT_STORAGE_BYTES
	);
	const slotLimit = Math.floor( storageLimit / (PARTICLE_RECORD * 4) );
	const groupLimit = device.limits.maxComputeWorkgroupsPerDimension ?? DEFAULT_WORKGROUP_LIMIT;
	// The fewest slots whose instance and palette bytes are both whole
	// multiples of the binding offset alignment (8 slots at 256 bytes).
	const alignment = bindingOffsetAlignment( device );
	const instanceStep = alignment / greatestDivisor( alignment, INSTANCE_BYTES );
	const paletteStep = alignment / greatestDivisor( alignment, PALETTE_BYTES );
	const slotAlignment = instanceStep / greatestDivisor( instanceStep, paletteStep ) * paletteStep;
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
		return particleCapacity( bytes, storageLimit );
	}
	const materials = createParticleMaterials( device, storageLimit, retire, work );

	/*
	================
	allocateOutput
	================
	*/
	function allocateOutput( needed: number ): { arena: Arena; outputStart: number; outputSlots: number; } {
		const count = Math.ceil( needed / slotAlignment ) * slotAlignment;
		if ( count > slotLimit ) throw Error( "Particle stream exceeds device storage limit" );
		for ( const arena of arenas ) {
			const index = arena.free.findIndex( block => block[1] >= count );
			if ( index < 0 && arena.top + count > slotLimit ) continue;
			arena.live++;
			if ( index < 0 ) {
				const outputStart = arena.top;
				arena.top += count;
				return { arena, outputStart, outputSlots: count };
			}
			const block = arena.free[index]!, outputStart = block[0];
			block[0] += count;
			block[1] -= count;
			if ( !block[1] ) arena.free.splice( index, 1 );
			return { arena, outputStart, outputSlots: count };
		}
		const arena: Arena = { top: count, live: 1, free: [], pending: new Set() };
		arenas.push( arena );
		return { arena, outputStart: 0, outputSlots: count };
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
	bindStream

	Point the stream's draw at its slice of the arena's current output.
	================
	*/
	function bindStream( stream: Stream ) {
		const output = stream.arena.output!, size = stream.rows * stream.slots;
		stream.bind( {
			instances: {
				buffer: output.instances,
				offset: stream.outputStart * INSTANCE_BYTES,
				size: size * INSTANCE_BYTES
			},
			bones: { buffer: output.bones, offset: stream.outputStart * PALETTE_BYTES, size: size * PALETTE_BYTES }
		} );
	}

	/*
	================
	growOutput

	Give the arena an output that holds its slots, at presentation. The
	replaced output stays readable until the next frame begins after submit
	(passes already recorded this frame may name it), and encode copies its
	contents across first (flushGrowth). Every stream of the arena moves to
	the new output now, before the frame records its bundles.
	================
	*/
	function growOutput( arena: Arena ) {
		const output = arena.output;
		if ( output && output.count >= arena.top ) return;
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
		// Two growths before one encode: the first replacement was never
		// written, so the copy still comes from the older output.
		if ( output ) {
			if ( arena.copyFrom ) oldOutputs.push( output );
			else arena.copyFrom = output;
		}
		arena.output = { count, records, instances, bones };
		for ( const stream of streams.values() ) {
			if ( stream.arena === arena && stream.bound ) bindStream( stream );
		}
	}

	/*
	================
	flushGrowth

	Copy a grown arena's earlier contents into its output, ahead of this
	frame's dispatches and passes.
	================
	*/
	function flushGrowth( encoder: GPUCommandEncoder, arena: Arena ): Output {
		const output = arena.output!, old = arena.copyFrom;
		if ( old ) {
			encoder.copyBufferToBuffer( old.instances, 0, output.instances, 0, old.count * INSTANCE_BYTES );
			encoder.copyBufferToBuffer( old.bones, 0, output.bones, 0, old.count * PALETTE_BYTES );
			encoder.copyBufferToBuffer( old.records, 0, output.records, 0, old.count * PARTICLE_RECORD * 4 );
			oldOutputs.push( old );
			arena.copyFrom = undefined;
		}
		return output;
	}

	/*
	================
	bindPage
	================
	*/
	function bindPage( buffers: readonly GPUBuffer[], output: Output, tables: GPUBuffer ): GPUBindGroup {
		return device.createBindGroup( {
			layout: pipeline!.getBindGroupLayout( 0 ),
			entries: [ output!.records, buffers[1]!, tables, output!.instances, output!.bones ].map( (
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
	function pageFor( count: number, frameFloats: number, output: Output, tables: GPUBuffer ): Page {
		const index = pageIndex++, previous = pages[index];
		if ( previous && previous.count >= count && previous.frame.length >= frameFloats ) {
			if ( previous.output !== output || previous.materials !== tables ) {
				previous.binding = bindPage( previous.buffers, output, tables );
				previous.output = output!;
				previous.materials = tables;
			}
			return previous;
		}
		const records = new Float32Array( capacity( count * PARTICLE_RECORD * 4 ) / 4 );
		const frame = new Float32Array( capacity( frameFloats * 4 ) / 4 );
		const sizes = [ records.byteLength, frame.byteLength ];
		const buffers: GPUBuffer[] = [];
		try {
			for ( const size of sizes ) {
				buffers.push( device.createBuffer( {
					label: "particle-batch",
					size,
					usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC
				} ) );
			}
			const binding = bindPage( buffers, output, tables );
			const countCapacity = records.length / PARTICLE_RECORD;
			const page = {
				records,
				frame,
				words: new Uint32Array( frame.buffer ),
				buffers,
				binding,
				output: output!,
				materials: tables,
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
		reclaimOutput( stream.arena, stream.outputStart, stream.outputSlots );
		if ( stream.table ) materials.release( stream.table );
		streams.delete( draw );
	}

	/*
	================
	encodeBatch

	One device-bounded dispatch over immutable input staging, resident output
	and the one material store its streams' tables live in.
	================
	*/
	function encodeBatch(
		encoder: GPUCommandEncoder,
		timing: GpuTimingFrame | undefined,
		selected: readonly Stream[],
		target: Output,
		store: MaterialStore | undefined
	) {
		let count = 0, dirtyCount = 0, groups = 0, frameFloats = 0;
		for ( const stream of selected ) {
			const size = stream.rows * stream.slots;
			count += size;
			dirtyCount += Math.max( 0, stream.dirtyEnd - stream.dirtyStart );
			groups += Math.ceil( size / WORKGROUP );
			frameFloats += stream.frame.length;
		}
		const page = pageFor( dirtyCount, frameFloats + groups * 4, target, materials.buffer( store ) );
		let dirtySlot = 0, group = 0, at = groups * 4;
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
			// Read here, not at presentation: a pack moves a table (beginFrame).
			page.words[at + 9] = stream.table ? stream.table.start / 4 : 0;
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
		}
		if ( dirtySlot ) {
			device.queue.writeBuffer( page.buffers[0]!, 0, page.records.buffer, 0, dirtySlot * PARTICLE_RECORD * 4 );
			work?.noteUpload( dirtySlot * PARTICLE_RECORD * 4 );
		}
		device.queue.writeBuffer( page.buffers[1]!, 0, page.frame.buffer, 0, at * 4 );
		work?.noteUpload( at * 4 );
		const pass = encoder.beginComputePass( {
			label: "particle-presentation",
			timestampWrites: timing?.pass( "particle-presentation" )
		} );
		pass.setPipeline( pipeline! );
		pass.setBindGroup( 0, page.binding );
		pass.dispatchWorkgroups( groups );
		pass.end();
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
		work?.noteUpload( instances.byteLength );
		device.queue.writeBuffer( stream.bones, 0, bones.buffer, 0, bones.byteLength );
		work?.noteUpload( bones.byteLength );
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
			materials.beginFrame();
		},
		/*
		================
		present
		================
		*/
		present(
			draw: GeometryDraw,
			instances: GPUBuffer,
			bones: GPUBuffer,
			particles: ParticlePresentation,
			bind: ( binding: ParticleBinding ) => void
		) {
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
				stream = {
					rows: particles.rows,
					slots: particles.slots,
					frames: particles.frames,
					records: new Float32Array( count * PARTICLE_RECORD ),
					frame,
					words: new Uint32Array( frame.buffer ),
					table: particles.frames && pipeline ? materials.acquire( particles.frames ) : undefined,
					instances,
					bones,
					...allocateOutput( count ),
					bind,
					bound: !!pipeline,
					initialized: false,
					dirtyStart: 0,
					dirtyEnd: count,
					empty: false
				};
				streams.set( draw, stream );
				if ( stream.bound ) {
					const output = stream.arena.output;
					if ( output && output.count >= stream.arena.top ) bindStream( stream );
					else growOutput( stream.arena );
				}
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

		Partition independent streams by resident arena, material store and
		per-pass limits. No stream or update is dropped when a batch reaches
		a device limit.
		================
		*/
		encode( encoder: GPUCommandEncoder, timing?: GpuTimingFrame ) {
			if ( !pipeline ) {
				for ( const arena of pending ) {
					for ( const stream of arena.pending ) encodeCpuStream( stream );
					arena.pending = new Set();
				}
				pending = new Set();
				return;
			}
			for ( const arena of pending ) {
				const target = flushGrowth( encoder, arena );
				let groups = 0, frameBytes = 0, store: MaterialStore | undefined;
				for ( const stream of arena.pending ) {
					const nextGroups = Math.ceil( stream.rows * stream.slots / WORKGROUP );
					const nextFrameBytes = stream.frame.byteLength + nextGroups * 16;
					if ( nextGroups > groupLimit || nextFrameBytes > storageLimit ) {
						throw Error( "Particle stream exceeds device dispatch limit" );
					}
					// A pass binds one store: a stream of another starts the next pass.
					const nextStore = stream.table?.store;
					if (
						batch.length &&
						(groups + nextGroups > groupLimit || frameBytes + nextFrameBytes > storageLimit ||
							store !== undefined && nextStore !== undefined && nextStore !== store)
					) {
						encodeBatch( encoder, timing, batch, target, store );
						batch.length = 0;
						groups = frameBytes = 0;
						store = undefined;
					}
					batch.push( stream );
					groups += nextGroups;
					frameBytes += nextFrameBytes;
					store ??= nextStore;
				}
				if ( batch.length ) encodeBatch( encoder, timing, batch, target, store );
				batch.length = 0;
				arena.pending = new Set();
			}
			pending = new Set();
		},
		release,
		stats: () => ({
			streams: streams.size,
			dispatches,
			slots,
			arenaBytes: pages.reduce( ( sum, page ) => sum + page.frame.byteLength, 0 ),
			materials: materials.stats()
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
			materials.dispose();
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
