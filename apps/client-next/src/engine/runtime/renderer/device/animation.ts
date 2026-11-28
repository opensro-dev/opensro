/*
===========================================================================

animation.ts - GPU skeletal animation: admission, streams and encoding

Admits a model's animation inputs to the GPU once, keeps one input stream
per palette source, and encodes the compute pass that evaluates the
bones. Models the GPU path cannot run fall back to CPU poses.

A stream's inputs (one 16-byte row a pose) are a range of a staged slab
set (buffer-slabs.ts), not a buffer of its own: every pending stream's
rows reach the GPU in one write an encode, not one write a stream. The
range binds with the size the stream's own buffer had, so the shader reads
the same bytes it did. That size is the stream's capacity, never its row
count: a recycled range still holds an earlier stream's rows past this
frame's (a buffer of its own began zeroed), and only the dispatch, one
workgroup a row, says how many rows are read.

A model's inputs are two buffers (gpu-animation-plan.ts): its skeleton,
owned by the model, and its clip set, shared by every admitted model with
the same clips. An assembled character therefore uploads a few kilobytes,
not its body's whole keyframe set again.

===========================================================================
*/

import type {
	CharacterClip,
	CharacterGpuSample,
	CharacterModel,
	CharacterPrimitive
} from "@/engine/contracts/character";
import {
	createGpuClipPlan,
	createGpuSkeletonPlan,
	type GpuClipPlan,
	type GpuSkeletonPlan
} from "@/engine/foundation/animation/gpu-animation-plan";
import type { GpuTimingFrame } from "../internal/gpu-contract";
import { animationShader } from "./animation-shader";
import { destroyNow, type Retire } from "./retirement";
import { createStagedSlabs, type BufferSlice } from "./buffer-slabs";
import { isCharacterAnimationExtension } from "@/engine/foundation/animation/character-render-plan";
type Sample = CharacterGpuSample;
// One sample's input words (animation-shader.ts): the first clip's time,
// clip and palette slot and the mix share, then the second clip's time and
// clip (-1 for none).
const SAMPLE_WORDS = 8;
/*
================
ClipSet

A resident clip set; refs counts the admitted models that bind it.
================
*/
type ClipSet = {
	clips: readonly CharacterClip[];
	buffer: GPUBuffer;
	bytes: number;
	refs: number;
	plan: GpuClipPlan;
};
/*
================
Model

A resident skeleton; refs counts the streams that bind it.
================
*/
type Model = {
	source: CharacterModel;
	buffer: GPUBuffer;
	bytes: number;
	refs: number;
	clips: ClipSet;
	configurations: ReadonlyMap<CharacterPrimitive, Uint32Array>;
};
/*
================
Stream
================
*/
type Stream = {
	model: Model;
	primitive: CharacterPrimitive;
	output: GPUBuffer;
	input: BufferSlice;
	configuration: GPUBuffer;
	binding: GPUBindGroup;
	data: Float32Array;
	words: Uint32Array;
	count: number;
	// Every node's global matrix per palette slot (animation-shader.ts),
	// allocated only once a consumer asks (prepare's globals).
	globals: GPUBuffer | null;
	// Whether the configuration has the globals write on; the palette slots
	// the pending rows write globals for; the slots whose encoded globals
	// stand (globalsOf). A CPU slot of a mixed stream never does.
	globalsOn: boolean;
	pendingSlots: Uint8Array;
	validSlots: Uint8Array;
};
// The globals keep their own budget: asking for them never moves a
// skinning stream onto the CPU.
const STATIC_BYTES = 32 << 20, STREAM_BYTES = 8 << 20, GLOBALS_BYTES = 4 << 20;
// The Configuration uniform of animation-shader.ts, and its globals word.
const CONFIGURATION_BYTES = 48, GLOBALS_FLAG_BYTE = 44;

/*
================
createGpuAnimationResources

Device owns every GPU reference. Geometry owns output palettes; this owner
borrows them only while their source stream is live. No readback is needed.
================
*/
/*
================
writePhase

A clip phase as f32, rounded down: a STEP phase must not round across an
authored f32 key.
================
*/
function writePhase( row: { data: Float32Array; words: Uint32Array; }, at: number, time: number ) {
	row.data[at] = time;
	if ( row.data[at]! > time ) row.words[at]!--;
}

export function createGpuAnimationResources( device: GPUDevice, retire: Retire = destroyNow ) {
	let failure: string | null = null;
	let disposed = false,
		pipeline: GPUComputePipeline | undefined,
		staticBytes = 0,
		streamBytes = 0,
		dispatches = 0,
		inputUploads = 0,
		poses = 0,
		globalBytes = 0;
	const models = new Map<CharacterModel, Model>(),
		clipSets = new Map<readonly CharacterClip[], ClipSet>(),
		streams = new Map<Float32Array, Stream>(),
		pending = new Set<Stream>(),
		unsupported = new WeakSet<CharacterModel>(),
		// The globals binding of every stream no consumer asked: never written.
		placeholder = device.createBuffer( {
			label: "animation-globals-none",
			size: 64,
			usage: GPUBufferUsage.STORAGE
		} ),
		inputs = createStagedSlabs(
			device,
			"animation-inputs",
			GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
			retire
		);
	// CPU-side plans. A model whose current clip is not GPU-eligible is refused
	// before admission; without these it rebuilt its plan every frame. A null
	// entry records a clip set or skeleton the GPU must not run.
	const clipPlans = new WeakMap<readonly CharacterClip[], GpuClipPlan | null>(),
		skeletonPlans = new WeakMap<CharacterModel, GpuSkeletonPlan | null>();
	const ready = Promise.resolve()
		.then( () =>
			device.createComputePipelineAsync( {
				label: "skeletal-animation",
				layout: "auto",
				compute: {
					module: device.createShaderModule( { label: "skeletal-animation", code: animationShader } ),
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
	clipPlanFor
	================
	*/
	function clipPlanFor( clips: readonly CharacterClip[] ): GpuClipPlan | null {
		let plan = clipPlans.get( clips );
		if ( plan === undefined ) {
			plan = createGpuClipPlan( clips );
			clipPlans.set( clips, plan );
		}
		return plan;
	}

	/*
	================
	skeletonPlanFor
	================
	*/
	function skeletonPlanFor( model: CharacterModel, clipNodes: number ): GpuSkeletonPlan | null {
		let plan = skeletonPlans.get( model );
		if ( plan === undefined ) {
			plan = createGpuSkeletonPlan( model, clipNodes );
			skeletonPlans.set( model, plan );
		}
		return plan;
	}

	/*
	================
	staticBuffer

	A storage buffer holding data, destroyed again if the write throws.
	================
	*/
	function staticBuffer( label: string, data: Float32Array ): GPUBuffer {
		const buffer = device.createBuffer( {
			label,
			size: data.byteLength,
			usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
		} );
		try {
			device.queue.writeBuffer( buffer, 0, data.buffer, data.byteOffset, data.byteLength );
		} catch ( error ) {
			buffer.destroy();
			throw error;
		}
		return buffer;
	}

	/*
	================
	retireModel

	Destroys an admitted model's skeleton and lets go of its clip set.
	================
	*/
	function retireModel( key: CharacterModel, model: Model ) {
		retire( model.buffer );
		staticBytes -= model.bytes;
		models.delete( key );
		if ( !--model.clips.refs ) {
			retire( model.clips.buffer );
			staticBytes -= model.clips.bytes;
			clipSets.delete( model.clips.clips );
		}
	}

	/*
	================
	admit

	The resident inputs of model, admitting its skeleton and, when no other
	model holds it, its clip set. Null when the GPU must not run the model
	or the static budget is full.
	================
	*/
	function admit( model: CharacterModel ): Model | null {
		const admitted = models.get( model );
		if ( admitted ) return admitted;
		const clipPlan = clipPlanFor( model.clips );
		const skeletonPlan = clipPlan ? skeletonPlanFor( model, clipPlan.nodes ) : null;
		if ( !clipPlan || !skeletonPlan ) {
			unsupported.add( model );
			return null;
		}
		const shared = clipSets.get( model.clips );
		const bytes = skeletonPlan.data.byteLength + (shared ? 0 : clipPlan.data.byteLength);
		if ( staticBytes + bytes > STATIC_BYTES ) {
			unsupported.add( model );
			return null;
		}
		let clips = shared;
		if ( !clips ) {
			clips = {
				clips: model.clips,
				buffer: staticBuffer( "animation-clips", clipPlan.data ),
				bytes: clipPlan.data.byteLength,
				refs: 0,
				plan: clipPlan
			};
			clipSets.set( model.clips, clips );
			staticBytes += clips.bytes;
		}
		let buffer: GPUBuffer;
		try {
			buffer = staticBuffer( "animation-skeleton", skeletonPlan.data );
		} catch ( error ) {
			if ( !clips.refs ) {
				clips.buffer.destroy();
				staticBytes -= clips.bytes;
				clipSets.delete( model.clips );
			}
			throw error;
		}
		const entry: Model = {
			source: model,
			buffer,
			bytes: skeletonPlan.data.byteLength,
			refs: 0,
			clips,
			configurations: skeletonPlan.configurations
		};
		clips.refs++;
		models.set( model, entry );
		staticBytes += entry.bytes;
		return entry;
	}

	/*
	================
	setGlobals

	Turn a stream's globals write on or off for its pending rows. The buffer
	is allocated at the first request that fits the globals budget and kept
	until the stream is released; the configuration word changes only when
	the setting does. The pending slots are the rows' GPU-sampled ones.
	================
	*/
	function setGlobals( row: Stream, wanted: boolean, capacity: number, samples: readonly (Sample | null)[] ) {
		let on = wanted;
		if ( on && !row.globals ) {
			const bytes = capacity * row.model.source.nodes.length * 64;
			if ( globalBytes + bytes > GLOBALS_BYTES ) on = false;
			else {
				row.globals = device.createBuffer( {
					label: "animation-globals",
					size: bytes,
					// COPY_SRC: the WebGPU parity test reads them back; the renderer never does.
					usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_SRC
				} );
				globalBytes += bytes;
				row.binding = device.createBindGroup( {
					layout: pipeline!.getBindGroupLayout( 0 ),
					entries: [
						{ binding: 0, resource: { buffer: row.model.buffer } },
						{ binding: 1, resource: { buffer: row.model.clips.buffer } },
						{ binding: 2, resource: row.input.binding },
						{ binding: 3, resource: { buffer: row.output } },
						{ binding: 4, resource: { buffer: row.configuration } },
						{ binding: 5, resource: { buffer: row.globals } }
					]
				} );
			}
		}
		row.pendingSlots.fill( 0 );
		if ( on ) { for ( let i = 0; i < samples.length; i++ ) row.pendingSlots[i] = samples[i] ? 1 : 0; }
		if ( on === row.globalsOn ) return;
		row.globalsOn = on;
		device.queue.writeBuffer( row.configuration, GLOBALS_FLAG_BYTE, Uint32Array.of( on ? 1 : 0 ) );
	}
	/*
	================
	release
	================
	*/
	function release( source: Float32Array ) {
		const row = streams.get( source );
		if ( !row ) return;
		pending.delete( row );
		// A pass encoded earlier this frame may still bind the row.
		inputs.free( row.input );
		retire( row.configuration );
		if ( row.globals ) retire( row.globals );
		row.validSlots.fill( 0 );
		globalBytes -= row.globals?.size ?? 0;
		streamBytes -= row.data.byteLength + CONFIGURATION_BYTES;
		streams.delete( source );
		if ( !--row.model.refs ) {
			for ( const [model, value] of models ) {
				if ( value === row.model ) {
					retireModel( model, value );
					break;
				}
			}
		}
	}
	return {
		ready,
		/*
		================
		prepare

		Queues the bones of samples into source's palette; false leaves them to
		the CPU path. A consumer of the holders' sockets passes globals for
		one stream per pose: its GPU-sampled slots also write every node's
		global matrix (globalsOf). Globals that do not fit their budget are
		left out, never the palette: the consumer keeps its CPU socket path.
		================
		*/
		prepare(
			source: Float32Array,
			output: GPUBuffer,
			model: CharacterModel,
			primitive: CharacterPrimitive,
			samples: readonly (Sample | null)[],
			globals = false
		): boolean {
			if ( disposed ) throw Error( "Disposed GPU animation owner" );
			// Any prepare replaces what the stream's globals held, a refusal
			// to the CPU path included.
			const previousRow = streams.get( source );
			previousRow?.validSlots.fill( 0 );
			if (
				!pipeline || unsupported.has( model ) || !samples.some( s => s !== null ) || !primitive.joints.length
			) return false;
			const capacity = source.length / (primitive.joints.length * 16);
			// A row keeps its output (checked below on every prepare), so the
			// output's size is read for a new row only: reading it crosses into
			// the browser. Either way it is refused before anything is admitted.
			if (
				!Number.isInteger( capacity ) || samples.length > capacity ||
				!streams.has( source ) && source.byteLength > output.size
			) throw Error( "GPU animation palette capacity exceeded" );
			if (
				samples.some( s =>
					s !== null &&
					(!Number.isFinite( s.time ) || s.time < 0 ||
						s.next && (!Number.isFinite( s.nextTime ) || s.nextTime < 0))
				)
			) return false;
			if ( !model.primitives.includes( primitive ) ) throw Error( "Unknown GPU palette binding" );
			if (
				!streams.has( source ) && streamBytes + capacity * SAMPLE_WORDS * 4 + CONFIGURATION_BYTES > STREAM_BYTES
			) {
				return false;
			}
			// Eligibility belongs to the clip set; check it before admitting anything.
			const clipPlan = models.get( model )?.clips.plan ?? clipPlanFor( model.clips );
			if ( !clipPlan ) {
				unsupported.add( model );
				return false;
			}
			if (
				samples.some( s =>
					s !== null && (!clipPlan.eligible.has( s.clip ) || s.next && !clipPlan.eligible.has( s.next ))
				)
			) {
				return false;
			}
			const previous = streams.get( source );
			if ( previous && previous.model.source !== model ) {
				if (
					previous.primitive !== primitive || previous.output !== output ||
					!isCharacterAnimationExtension( previous.model.source, model )
				) throw Error( "GPU animation stream identity changed" );
				// Catalog packing may change skeleton offsets. Rebind only compute
				// inputs; the geometry-owned output and all mesh buffers stay live.
				release( source );
			}
			const admitted = admit( model );
			if ( !admitted ) return false;
			const config = admitted.configurations.get( primitive );
			if ( !config ) throw Error( "Unknown GPU palette binding" );
			let row = streams.get( source );
			if ( !row ) {
				const data = new Float32Array( capacity * SAMPLE_WORDS );
				if ( streamBytes + data.byteLength + CONFIGURATION_BYTES > STREAM_BYTES ) return false;
				let input: BufferSlice | undefined, configuration: GPUBuffer | undefined;
				try {
					input = inputs.allocate( data.byteLength );
					configuration = device.createBuffer( {
						label: "animation-configuration",
						size: CONFIGURATION_BYTES,
						usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
					} );
					device.queue.writeBuffer( configuration, 0, config.buffer, config.byteOffset, config.byteLength );
					const binding = device.createBindGroup( {
						layout: pipeline.getBindGroupLayout( 0 ),
						entries: [
							{ binding: 0, resource: { buffer: admitted.buffer } },
							{ binding: 1, resource: { buffer: admitted.clips.buffer } },
							{ binding: 2, resource: input.binding },
							{ binding: 3, resource: { buffer: output } },
							{ binding: 4, resource: { buffer: configuration } },
							{ binding: 5, resource: { buffer: placeholder } }
						]
					} );
					row = {
						model: admitted,
						primitive,
						output,
						input,
						configuration,
						binding,
						data,
						words: new Uint32Array( data.buffer ),
						count: 0,
						globals: null,
						globalsOn: false,
						pendingSlots: new Uint8Array( capacity ),
						validSlots: new Uint8Array( capacity )
					};
					streams.set( source, row );
					admitted.refs++;
					streamBytes += data.byteLength + CONFIGURATION_BYTES;
				} catch ( error ) {
					if ( input ) inputs.free( input );
					configuration?.destroy();
					if ( !admitted.refs ) retireModel( model, admitted );
					throw error;
				}
			}
			if ( row.model !== admitted || row.primitive !== primitive || row.output !== output ) {
				throw Error( "GPU animation stream identity changed" );
			}
			let count = 0;
			for ( let i = 0; i < samples.length; i++ ) {
				const sample = samples[i];
				if ( !sample ) continue;
				const at = count++ * SAMPLE_WORDS;
				writePhase( row, at, sample.time );
				row.data[at + 1] = clipPlan.index.get( sample.clip )!;
				row.data[at + 2] = i;
				row.data[at + 3] = sample.share;
				writePhase( row, at + 4, sample.nextTime );
				row.data[at + 5] = sample.next ? clipPlan.index.get( sample.next )! : -1;
			}
			row.count = count;
			inputs.write( row.input, row.data, count * SAMPLE_WORDS * 4 );
			setGlobals( row, globals, capacity, samples );
			pending.add( row );
			return true;
		},
		/*
		================
		cancel
		================
		*/
		cancel( source: Float32Array ) {
			const row = streams.get( source );
			if ( !row ) return;
			pending.delete( row );
			row.validSlots.fill( 0 );
		},
		/*
		================
		encode

		One write carries every pending stream's rows before the pass. A
		stream prepared again after an earlier encode this frame is pending
		again; its rows go up in the next encode's write. Both writes precede
		the frame's submit, so an earlier pass of the same command buffer
		reads the new rows too, as it did from the stream's own buffer.
		================
		*/
		encode( encoder: GPUCommandEncoder, timing?: GpuTimingFrame ) {
			if ( disposed ) throw Error( "Disposed GPU animation owner" );
			if ( !pending.size ) return;
			inputUploads += inputs.flush();
			const pass = encoder.beginComputePass( {
				label: "skeletal-animation",
				timestampWrites: timing?.pass( "skeletal-animation" )
			} );
			pass.setPipeline( pipeline! );
			for ( const row of pending ) {
				if ( row.globalsOn ) row.validSlots.set( row.pendingSlots );
				pass.setBindGroup( 0, row.binding );
				pass.dispatchWorkgroups( row.count );
				dispatches++;
				poses += row.count;
			}
			pass.end();
			pending.clear();
		},
		release,
		/*
		================
		globalsOf

		The node globals of source's stream: palette slot s, node n is the
		matrix at (s * nodes + n) * 64, read later in the same command buffer,
		and valid(s) says whether the GPU wrote slot s for the stream's latest
		encoded prepare. A later prepare (a CPU refusal included), a cancel or
		a release clears every slot until its rows are encoded again; a CPU
		slot of a mixed stream is never valid. The slots outlive frames while
		nothing prepares the stream again (an unchanged pose skips its
		prepare): whether a slot's pose is still the one written is the
		palette owner's revision check (palette-streams.ts).
		================
		*/
		globalsOf(
			source: Float32Array
		): { buffer: GPUBuffer; nodes: number; valid( slot: number ): boolean; } | null {
			const row = streams.get( source );
			if ( !row?.globals ) return null;
			const slots = row.validSlots;
			return { buffer: row.globals, nodes: row.model.source.nodes.length, valid: slot => slots[slot] === 1 };
		},
		stats: () => ({
			enabled: !disposed,
			ready: !!pipeline,
			failure,
			models: models.size,
			clipSets: clipSets.size,
			streams: streams.size,
			staticBytes,
			streamBytes,
			globalBytes,
			dispatches,
			inputUploads,
			poses
		}),
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			disposed = true;
			for ( const source of streams.keys() ) release( source );
			for ( const [model, row] of [ ...models ] ) retireModel( model, row );
			inputs.dispose();
			placeholder.destroy();
			models.clear();
			clipSets.clear();
			pending.clear();
			staticBytes = streamBytes = globalBytes = 0;
			pipeline = undefined;
		}
	};
}
