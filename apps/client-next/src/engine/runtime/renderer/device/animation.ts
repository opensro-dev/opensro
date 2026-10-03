/*
===========================================================================

animation.ts - GPU skeletal animation: admission, streams and encoding

Admits a model's animation inputs to the GPU once, keeps one input stream
per palette source, and encodes the compute pass that evaluates the
bones. Models the GPU path cannot run fall back to CPU poses.

A model's inputs are two buffers (gpu-animation-plan.ts): its skeleton,
owned by the model, and its clip set, shared by every admitted model with
the same clips. An assembled character therefore uploads a few kilobytes,
not its body's whole keyframe set again.

===========================================================================
*/

import type { CharacterModel, CharacterPrimitive, CharacterClip } from "@/engine/contracts/character";
import {
	createGpuClipPlan,
	createGpuSkeletonPlan,
	type GpuClipPlan,
	type GpuSkeletonPlan
} from "@/engine/foundation/animation/gpu-animation-plan";
import type { GpuTimingFrame } from "../internal/gpu-contract";
import { animationShader } from "./animation-shader";
import { destroyNow, type Retire } from "./retirement";
type Sample = { readonly clip: CharacterClip; readonly time: number; };
// A resident clip set; refs counts the admitted models that bind it.
type ClipSet = {
	clips: readonly CharacterClip[];
	buffer: GPUBuffer;
	bytes: number;
	refs: number;
	plan: GpuClipPlan;
};
// A resident skeleton; refs counts the streams that bind it.
type Model = {
	buffer: GPUBuffer;
	bytes: number;
	refs: number;
	clips: ClipSet;
	configurations: ReadonlyMap<CharacterPrimitive, Uint32Array>;
};
type Stream = {
	model: Model;
	primitive: CharacterPrimitive;
	output: GPUBuffer;
	input: GPUBuffer;
	configuration: GPUBuffer;
	binding: GPUBindGroup;
	data: Float32Array;
	words: Uint32Array;
	count: number;
};
const STATIC_BYTES = 32 << 20, STREAM_BYTES = 8 << 20;
// The Configuration uniform of animation-shader.ts.
const CONFIGURATION_BYTES = 48;

/*
================
createGpuAnimationResources

Device owns every GPU reference. Geometry owns output palettes; this owner
borrows them only while their source stream is live. No readback is needed.
================
*/
export function createGpuAnimationResources( device: GPUDevice, retire: Retire = destroyNow ) {
	let failure: string | null = null;
	let disposed = false,
		pipeline: GPUComputePipeline | undefined,
		staticBytes = 0,
		streamBytes = 0,
		dispatches = 0,
		poses = 0;
	const models = new Map<CharacterModel, Model>(),
		clipSets = new Map<readonly CharacterClip[], ClipSet>(),
		streams = new Map<Float32Array, Stream>(),
		pending = new Set<Stream>(),
		unsupported = new WeakSet<CharacterModel>();
	// CPU-side plans. A model whose current clip is not GPU-eligible is refused
	// before admission; without these it rebuilt its plan every frame. A null
	// entry records a clip set or skeleton the GPU must not run.
	const clipPlans = new WeakMap<readonly CharacterClip[], GpuClipPlan | null>(),
		skeletonPlans = new WeakMap<CharacterModel, GpuSkeletonPlan | null>();
	const ready = device.createComputePipelineAsync( {
		label: "skeletal-animation",
		layout: "auto",
		compute: {
			module: device.createShaderModule( { label: "skeletal-animation", code: animationShader } ),
			entryPoint: "main"
		}
	} ).then( value => {
		if ( !disposed ) pipeline = value;
	} ).catch( error => {
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
	release
	================
	*/
	function release( source: Float32Array ) {
		const row = streams.get( source );
		if ( !row ) return;
		pending.delete( row );
		// A pass encoded earlier this frame may still bind the row.
		retire( row.input );
		retire( row.configuration );
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
		the CPU path.
		================
		*/
		prepare(
			source: Float32Array,
			output: GPUBuffer,
			model: CharacterModel,
			primitive: CharacterPrimitive,
			samples: readonly (Sample | null)[]
		): boolean {
			if ( disposed ) throw Error( "Disposed GPU animation owner" );
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
			if ( samples.some( s => s !== null && (!Number.isFinite( s.time ) || s.time < 0) ) ) return false;
			if ( !model.primitives.includes( primitive ) ) throw Error( "Unknown GPU palette binding" );
			if ( !streams.has( source ) && streamBytes + capacity * 16 + CONFIGURATION_BYTES > STREAM_BYTES ) {
				return false;
			}
			// Eligibility belongs to the clip set; check it before admitting anything.
			const clipPlan = models.get( model )?.clips.plan ?? clipPlanFor( model.clips );
			if ( !clipPlan ) {
				unsupported.add( model );
				return false;
			}
			if ( samples.some( s => s !== null && !clipPlan.eligible.has( s.clip ) ) ) return false;
			const admitted = admit( model );
			if ( !admitted ) return false;
			const config = admitted.configurations.get( primitive );
			if ( !config ) throw Error( "Unknown GPU palette binding" );
			let row = streams.get( source );
			if ( !row ) {
				const data = new Float32Array( capacity * 4 );
				if ( streamBytes + data.byteLength + CONFIGURATION_BYTES > STREAM_BYTES ) return false;
				let input: GPUBuffer | undefined, configuration: GPUBuffer | undefined;
				try {
					input = device.createBuffer( {
						label: "animation-inputs",
						size: data.byteLength,
						usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
					} );
					configuration = device.createBuffer( {
						label: "animation-configuration",
						size: CONFIGURATION_BYTES,
						usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
					} );
					device.queue.writeBuffer( configuration, 0, config.buffer, config.byteOffset, config.byteLength );
					const binding = device.createBindGroup( {
						layout: pipeline.getBindGroupLayout( 0 ),
						entries: [ admitted.buffer, admitted.clips.buffer, input, output, configuration ].map( (
							buffer,
							binding
						) => ({ binding, resource: { buffer } }) )
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
						count: 0
					};
					streams.set( source, row );
					admitted.refs++;
					streamBytes += data.byteLength + CONFIGURATION_BYTES;
				} catch ( error ) {
					input?.destroy();
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
				const at = count++ * 4;
				row.data[at] = sample.time; /* Do not round a STEP phase across an authored f32 key. */
				if ( row.data[at]! > sample.time ) row.words[at]!--;
				row.data[at + 1] = clipPlan.index.get( sample.clip )!;
				row.data[at + 2] = i;
			}
			row.count = count;
			device.queue.writeBuffer( row.input, 0, row.data.buffer, 0, count * 16 );
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
			if ( row ) pending.delete( row );
		},
		/*
		================
		encode
		================
		*/
		encode( encoder: GPUCommandEncoder, timing?: GpuTimingFrame ) {
			if ( disposed ) throw Error( "Disposed GPU animation owner" );
			if ( !pending.size ) return;
			const pass = encoder.beginComputePass( {
				label: "skeletal-animation",
				timestampWrites: timing?.pass( "skeletal-animation" )
			} );
			pass.setPipeline( pipeline! );
			for ( const row of pending ) {
				pass.setBindGroup( 0, row.binding );
				pass.dispatchWorkgroups( row.count );
				dispatches++;
				poses += row.count;
			}
			pass.end();
			pending.clear();
		},
		release,
		stats: () => ({
			enabled: !disposed,
			ready: !!pipeline,
			failure,
			models: models.size,
			clipSets: clipSets.size,
			streams: streams.size,
			staticBytes,
			streamBytes,
			dispatches,
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
			models.clear();
			clipSets.clear();
			pending.clear();
			staticBytes = streamBytes = 0;
			pipeline = undefined;
		}
	};
}
