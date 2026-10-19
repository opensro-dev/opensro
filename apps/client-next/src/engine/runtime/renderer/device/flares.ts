/*
===========================================================================

flares.ts - GPU lens-flare visibility and chain rendering

The device owns the persistent visibility and chain state; no query readback
or CPU wait is required. Only this pass writes it, and texture or depth
identities invalidate its bindings. If the driver fails to compile the
flare pipelines, prepare returns undefined and the frame omits lens flares.

===========================================================================
*/
import { flareEntries } from "@/engine/foundation/rendering/flares";
import type { FlareDraw, FlareInput, ImageDraw } from "../internal/gpu-contract";

/*
================
createFlares
================
*/
export function createFlares(
	device: GPUDevice,
	format: GPUTextureFormat,
	texture: ( image: ImageDraw ) => GPUTexture
) {
	const entries = flareEntries();
	const uniform = device.createBuffer( {
		label: "flare-frame",
		size: 112,
		usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
	} );
	const state = device.createBuffer( {
		label: "flare-state",
		size: 16,
		usage: GPUBufferUsage.STORAGE
	} );
	const common = `struct Frame { samples:array<vec4f,5>, screen:vec4f, timing:vec4f }
@group(0) @binding(0) var<uniform> frame:Frame;
struct State { visibility:f32, x:f32, y:f32, pad:f32 }`;
	const update = device.createShaderModule( {
		label: "flare-visibility",
		code: common + `
@group(0) @binding(1) var<storage,read_write> state:State;
@group(0) @binding(2) var depth:texture_depth_2d;
@compute @workgroup_size(1) fn main(){
	if(frame.timing.y==0){return;}
	var visible=0.0;
	let dimensions=vec2i(textureDimensions(depth));
	var writtenPixels=array<vec2i,5>(vec2i(-1),vec2i(-1),vec2i(-1),vec2i(-1),vec2i(-1));
	var writtenDepth=array<f32,5>(1.0,1.0,1.0,1.0,1.0);
	for(var i=0u;i<5u;i++){
		let clip=frame.samples[i];
		let ndc=clip.xyz/clip.w;
		let pixel=vec2i(floor((ndc.xy*vec2f(.5,-.5)+.5)*vec2f(dimensions)));
		if(clip.w>0&&ndc.z>=0&&ndc.z<=1&&all(pixel>=vec2i(0))&&all(pixel<dimensions)){
			// AEBAA0 writes depth after each query; a later overlapping sample
			// must see that write. Only these five queries can consume this state.
			var previous=textureLoad(depth,pixel,0);
			for(var j=0u;j<i;j++){
				if(all(writtenPixels[j]==pixel)){
					previous=min(previous,writtenDepth[j]);
				}
			}
			if(ndc.z<=previous){
				visible+=select(.175,.3,i==0u);
				writtenPixels[i]=pixel;
				writtenDepth[i]=ndc.z;
			}
		}
	}
	let progress=min(frame.timing.x*10,1.0);
	state.visibility=state.visibility+progress*(visible-state.visibility);
	if(state.visibility>.1){
		let destination=(frame.screen.zw*.5-frame.screen.xy)*2.2;
		state.x+=(destination.x-state.x)*.5;
		state.y+=(destination.y-state.y)*.5;
	}
}`
	} );
	const sizes = entries.map( e => `${e.size}.0` ).join( "," );
	const alphas = entries.map( e => `${e.alpha}.0` ).join( "," );
	const shader = device.createShaderModule( {
		label: "flare-chain",
		code: common + `
@group(0) @binding(1) var<storage,read> state:State;
@group(0) @binding(2) var flare:texture_2d_array<f32>;
@group(0) @binding(3) var sampling:sampler;
// D3DRS_TEXTUREFACTOR is a per-draw constant, not an interpolated color.
struct Vertex { @builtin(position) position:vec4f, @location(0) uv:vec2f, @location(1) @interpolate(flat) alpha:f32 }
@vertex fn vs(@builtin(vertex_index) vertex:u32,@builtin(instance_index) index:u32)->Vertex {
	let sizes=array<f32,30>(${sizes});
	let alphas=array<f32,30>(${alphas});
	let corners=array<vec2f,5>(vec2f(0),vec2f(-1,-1),vec2f(1,-1),vec2f(1,1),vec2f(-1,1));
	let quad=array<u32,6>(1,2,3,1,3,4);
	let fan=array<u32,12>(0,1,2,0,2,3,0,3,4,0,4,1);
	var corner=0u;
	if(index==2u){corner=fan[vertex];}else{corner=quad[vertex];}
	let offset=corners[corner];
	let factor=f32(i32(index)-2)/19;
	let center=frame.screen.xy+vec2f(state.x,state.y)*factor;
	// Native XYZRHW pixels have integer sample centres; WebGPU has half-integer centres.
	let pixel=center+offset*sizes[index]*select(1.0,2.0,index==2u)+vec2f(.5);
	// 8ADAF0..8ADB05 stores BC84D0 (0x3DCCCCCD) in XYZRHW.w.
	// Preserve reciprocal W through raster interpolation, including constant-W
	// rounding, rather than replacing the pretransformed vertex with W=1.
	let clipW=1.0/0.1;
	var out:Vertex;
	out.position=vec4f((pixel/frame.screen.zw*vec2f(2,-2)+vec2f(-1,1))*clipW,.1*clipW,clipW);
	out.uv=offset*.5+.5;
	out.alpha=floor(alphas[index]*state.visibility)/255;
	return out;
}
@fragment fn fs(input:Vertex)->@location(0) vec4f {
	if(frame.timing.y==0||state.visibility<=.1){discard;}
	let color=textureSample(flare,sampling,input.uv,0);
	return vec4f(color.rgb,color.a*input.alpha);
}`
	} );
	const updateLayout = device.createBindGroupLayout( {
		entries: [
			{ binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "uniform" } },
			{ binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
			{ binding: 2, visibility: GPUShaderStage.COMPUTE, texture: { sampleType: "depth" } }
		]
	} );
	const drawLayout = device.createBindGroupLayout( {
		entries: [
			{
				binding: 0,
				visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT,
				buffer: { type: "uniform" }
			},
			{
				binding: 1,
				visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT,
				buffer: { type: "read-only-storage" }
			},
			{
				binding: 2,
				visibility: GPUShaderStage.FRAGMENT,
				texture: { viewDimension: "2d-array" }
			},
			{
				binding: 3,
				visibility: GPUShaderStage.FRAGMENT,
				sampler: { type: "filtering" }
			}
		]
	} );
	let compute: GPUComputePipeline | undefined;
	let add: GPURenderPipeline | undefined;
	let over: GPURenderPipeline | undefined;
	let failure: string | null = null;
	let disposed = false;
	const sampler = device.createSampler( {
		minFilter: "linear",
		magFilter: "linear",
		mipmapFilter: "linear",
		addressModeU: "repeat",
		addressModeV: "repeat"
	} );
	const ready = Promise.resolve()
		.then( () => Promise.all( [ update.getCompilationInfo(), shader.getCompilationInfo() ] ) )
		.then( infos => {
			const errors = infos.flatMap( info => info.messages ).filter( message => message.type === "error" );
			if ( errors.length ) {
				throw new Error( errors.map( message => message.message ).join( "\n" ) );
			}
			return Promise.all( [
				device.createComputePipelineAsync( {
					label: "flare-visibility",
					layout: device.createPipelineLayout( { bindGroupLayouts: [ updateLayout ] } ),
					compute: { module: update, entryPoint: "main" }
				} ),
				...[ false, true ].map( isOver =>
					device.createRenderPipelineAsync( {
						label: isOver ? "flare-over" : "flare-add",
						layout: device.createPipelineLayout( { bindGroupLayouts: [ drawLayout ] } ),
						vertex: { module: shader, entryPoint: "vs" },
						fragment: {
							module: shader,
							entryPoint: "fs",
							targets: [
								{
									format,
									blend: {
										color: {
											srcFactor: "src-alpha",
											dstFactor: isOver ? "one-minus-src-alpha" : "one",
											operation: "add"
										},
										alpha: {
											srcFactor: "one",
											dstFactor: "one-minus-src-alpha",
											operation: "add"
										}
									}
								}
							]
						},
						primitive: { topology: "triangle-list" }
					} )
				)
			] ).then( pipelines => {
				if ( !disposed ) {
					compute = pipelines[0] as GPUComputePipeline;
					add = pipelines[1] as GPURenderPipeline;
					over = pipelines[2] as GPURenderPipeline;
				}
			} );
		} )
		.catch( error => {
			if ( !disposed ) failure = String( error );
		} );
	let previousDepth: GPUTextureView | null = null;
	let binding: GPUBindGroup;
	let previousImages: readonly ImageDraw[] = [];
	let draws: FlareDraw["entries"] = [];
	return {
		ready,
		failed: () => failure !== null,
		/*
		================
		prepare
		================
		*/
		prepare( input: FlareInput, depth: GPUTextureView ): FlareDraw | undefined {
			if (
				input.uniforms.length !== 28 ||
				!input.uniforms.every( Number.isFinite ) ||
				input.textures.length !== 8
			) {
				throw new Error( "Invalid GPU flare input" );
			}
			if ( !compute || !add || !over ) return undefined;
			device.queue.writeBuffer(
				uniform,
				0,
				input.uniforms.buffer as ArrayBuffer,
				input.uniforms.byteOffset,
				input.uniforms.byteLength
			);
			if ( depth !== previousDepth ) {
				previousDepth = depth;
				binding = device.createBindGroup( {
					layout: updateLayout,
					entries: [
						{ binding: 0, resource: { buffer: uniform } },
						{ binding: 1, resource: { buffer: state } },
						{ binding: 2, resource: depth }
					]
				} );
			}
			if (
				previousImages.length !== 8 ||
				input.textures.some( ( image, i ) => image !== previousImages[i] )
			) {
				const bindings = input.textures.map( image =>
					device.createBindGroup( {
						layout: drawLayout,
						entries: [
							{ binding: 0, resource: { buffer: uniform } },
							{ binding: 1, resource: { buffer: state } },
							{ binding: 2, resource: texture( image ).createView( { dimension: "2d-array" } ) },
							{ binding: 3, resource: sampler }
						]
					} )
				);
				draws = entries.flatMap( ( entry, index ) =>
					entry.size ?
						[
							{
								pipeline: entry.over ? over! : add!,
								binding: bindings[entry.texture]!,
								index,
								count: entry.fan ? 12 : 6
							}
						] :
						[]
				);
				previousImages = input.textures.slice();
			}
			return { compute, binding, entries: draws };
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			disposed = true;
			uniform.destroy();
			state.destroy();
			previousImages = [];
			draws = [];
			previousDepth = null;
			compute = undefined;
			add = undefined;
			over = undefined;
		}
	};
}
