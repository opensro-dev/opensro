/*
===========================================================================

animation.ts - GPU skeletal animation: admission, streams and encoding

Admits a model's animation tracks to a GPU buffer once, keeps one input
stream per palette source, and encodes the compute pass that evaluates
the bones. Models the GPU path cannot run fall back to CPU poses.

===========================================================================
*/

import type { CharacterModel, CharacterPrimitive, CharacterClip } from "@/engine/contracts/character";
import { createGpuAnimationPlan } from "@/engine/foundation/animation/gpu-animation-plan";
import type { GpuTimingFrame } from "../internal/gpu-contract";
import { animationShader } from "./animation-shader";
type Sample = { readonly clip: CharacterClip; readonly time: number; };
type Model = {
	buffer: GPUBuffer;
	bytes: number;
	refs: number;
	configurations: Map<CharacterPrimitive, Uint32Array>;
	clips: Map<CharacterClip, number>;
	eligible: Set<CharacterClip>;
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

/*
================
createGpuAnimationResources
================
*/
// Device owns every GPU reference. Geometry owns output palettes; this owner
// borrows them only while their source stream is live. No readback is needed.
export function createGpuAnimationResources( device: GPUDevice ) {
	let failure: string | null = null;
	let disposed = false,
		pipeline: GPUComputePipeline | undefined,
		staticBytes = 0,
		streamBytes = 0,
		dispatches = 0,
		poses = 0;
	const models = new Map<CharacterModel, Model>(),
		streams = new Map<Float32Array, Stream>(),
		pending = new Set<Stream>(),
		unsupported = new WeakSet<CharacterModel>();
	// CPU-side plan per model. A model whose current clip is not GPU-eligible is
	// refused before admission; without this it rebuilt its plan every frame.
	const plans = new WeakMap<
		CharacterModel,
		{
			plan: NonNullable<ReturnType<typeof createGpuAnimationPlan>>;
			eligible: Set<CharacterModel["clips"][number]>;
		}
	>();
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
	release
	================
	*/
	function release( source: Float32Array ) {
		const row = streams.get( source );
		if ( !row ) return;
		pending.delete( row );
		row.input.destroy();
		row.configuration.destroy();
		streamBytes -= row.data.byteLength + 48;
		streams.delete( source );
		if ( !--row.model.refs ) {
			row.model.buffer.destroy();
			staticBytes -= row.model.bytes;
			for ( const [model, value] of models ) {
				if ( value === row.model ) {
					models.delete( model );
					break;
				}
			}
		}
	}
	return {
		ready,
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
			if (
				!Number.isInteger( capacity ) || samples.length > capacity || source.byteLength > output.size
			) throw Error( "GPU animation palette capacity exceeded" );
			if ( samples.some( s => s !== null && (!Number.isFinite( s.time ) || s.time < 0) ) ) return false;
			if ( !model.primitives.includes( primitive ) ) throw Error( "Unknown GPU palette binding" );
			if ( !streams.has( source ) && streamBytes + capacity * 16 + 48 > STREAM_BYTES ) return false;
			let admitted = models.get( model );
			if ( !admitted ) {
				let cached = plans.get( model );
				if ( !cached ) {
					const built = createGpuAnimationPlan( model );
					if ( !built ) {
						unsupported.add( model );
						return false;
					}
					cached = {
						plan: built,
						eligible: new Set( model.clips.filter( clip => {
							const keys = new Set<string>();
							return clip.channels.length > 0 && clip.channels.every( c => {
								const key = c.node + ":" + c.path;
								if (
									keys.has( key ) || !c.times.length || c.interpolation === "CUBICSPLINE"
								) return false;
								keys.add( key );
								return true;
							} );
						} ) )
					};
					plans.set( model, cached );
				}
				const { plan, eligible } = cached;
				if ( staticBytes + plan.data.byteLength > STATIC_BYTES ) {
					unsupported.add( model );
					return false;
				}
				if ( samples.some( s => s !== null && !eligible.has( s.clip ) ) ) return false;
				const buffer = device.createBuffer( {
					label: "animation-model",
					size: plan.data.byteLength,
					usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
				} );
				try {
					device.queue.writeBuffer( buffer, 0, plan.data.buffer );
				} catch ( error ) {
					buffer.destroy();
					throw error;
				}
				admitted = {
					buffer,
					bytes: plan.data.byteLength,
					refs: 0,
					configurations: plan.configurations,
					clips: plan.clips,
					eligible
				};
				models.set( model, admitted );
				staticBytes += admitted.bytes;
			}
			if (
				samples.some( s =>
					s !== null && (!admitted!.eligible.has( s.clip ) || !Number.isFinite( s.time ) || s.time < 0)
				)
			) return false;
			const config = admitted.configurations.get( primitive );
			if ( !config ) throw Error( "Unknown GPU palette binding" );
			let row = streams.get( source );
			if ( !row ) {
				const data = new Float32Array( capacity * 4 );
				if ( streamBytes + data.byteLength + 48 > STREAM_BYTES ) return false;
				let input: GPUBuffer | undefined, configuration: GPUBuffer | undefined;
				try {
					input = device.createBuffer( {
						label: "animation-inputs",
						size: data.byteLength,
						usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
					} );
					configuration = device.createBuffer( {
						label: "animation-configuration",
						size: 48,
						usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
					} );
					device.queue.writeBuffer( configuration, 0, config.buffer, config.byteOffset, config.byteLength );
					const binding = device.createBindGroup( {
						layout: pipeline.getBindGroupLayout( 0 ),
						entries: [ admitted.buffer, input, output, configuration ].map( ( buffer, binding ) => ({
							binding,
							resource: { buffer }
						}) )
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
					streamBytes += data.byteLength + 48;
				} catch ( error ) {
					input?.destroy();
					configuration?.destroy();
					if ( !admitted.refs ) {
						admitted.buffer.destroy();
						staticBytes -= admitted.bytes;
						models.delete( model );
					}
					throw error;
				}
			}
			if ( row.model !== admitted || row.primitive !== primitive || row.output !== output ) {
				throw Error(
					"GPU animation stream identity changed"
				);
			}
			let count = 0;
			for ( let i = 0; i < samples.length; i++ ) {
				const sample = samples[i];
				if ( !sample ) continue;
				const at = count++ * 4;
				row.data[at] = sample.time; /* Do not round a STEP phase across an authored f32 key. */
				if ( row.data[at]! > sample.time ) row.words[at]!--;
				row.data[at + 1] = admitted.clips.get( sample.clip )!;
				row.data[at + 2] = i;
			}
			row.count = count;
			device.queue.writeBuffer( row.input, 0, row.data.buffer, 0, count * 16 );
			pending.add( row );
			return true;
		},
		cancel( source: Float32Array ) {
			const row = streams.get( source );
			if ( row ) pending.delete( row );
		},
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
			streams: streams.size,
			staticBytes,
			streamBytes,
			dispatches,
			poses
		}),
		dispose() {
			if ( disposed ) return;
			disposed = true;
			for ( const source of streams.keys() ) release( source );
			for ( const row of models.values() ) row.buffer.destroy();
			models.clear();
			pending.clear();
			staticBytes = streamBytes = 0;
			pipeline = undefined;
		}
	};
}
