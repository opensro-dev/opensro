/*
===========================================================================

sun-shadow.ts - the experimental sun shadow cascade

Port-only, not native. Experimental > Lighting > Sun shadows renders one
orthographic depth pass of the frame's opaque casters (world objects and
characters; terrain and everything blended stays out) from the shading
light's position, and the world shader (pipelines.ts) resolves receivers
with hardware PCF taps. The native per-character silhouette projections
stand untouched beside it; this cascade complements them.

A caster's borrowed draw is valid only for the frame that prepared it, the
same borrowing contract as the silhouette shadows: geometry.ts calls
forget when a borrowed draw's buffers die.

===========================================================================
*/
import {
	SHADOW_DEPTH,
	SHADOW_EXTENT,
	SHADOW_SIZE,
	sunShadowMatrix
} from "@/engine/foundation/rendering/sun-shadow-math";
import type { GeometryDraw, GpuTimingFrame } from "../internal/gpu-contract";
import { destroyNow, type Retire } from "./retirement";

// Slope-scaled depth bias on the caster pass: the receiver compares its own
// depth against the caster's, and the bias moves a surface's cascade depth
// behind itself so a lit face cannot shade its own texels (acne).
const SUN_SHADOW_BIAS = 4;
const SUN_SHADOW_BIAS_SLOPE = 7;

/*
================
ShadowBinding

Borrowed resources; identity changes invalidate the caster's binding cache.
================
*/
export interface ShadowBinding {
	readonly instances: GPUBufferBinding;
	readonly material: GPUBufferBinding;
	readonly skin: GPUBuffer;
	readonly bones: GPUBuffer;
	// The draw's bound albedo, already resolved to its GPU texture; the
	// caster alpha-tests the same pages its receiver shades.
	readonly image: GPUTexture | undefined;
}

/*
================
createSunShadow

environment receives the cascade matrix at the port-only tail of the
environment block (byte 384); binding hands out each draw's GPU buffers.
================
*/
export function createSunShadow(
	device: GPUDevice,
	environment: GPUBuffer,
	matrixOffset: number,
	binding: ( draw: GeometryDraw ) => ShadowBinding | undefined,
	retire: Retire = destroyNow
) {
	// Everything compiles and allocates on the stage's first enabled use:
	// a native-only device never pays for the cascade.
	let shadowPipeline: GPURenderPipeline | null = null,
		white: GPUTexture | null = null,
		sampler: GPUSampler | null = null,
		projection: GPUBuffer | null = null;
	/*
	================
	ensure
	================
	*/
	const ensure = () => {
		if ( shadowPipeline ) return;
		const caster = device.createShaderModule( {
			label: "sun-shadow-caster",
			code: `
// The environment block's real layout down to the fields the caster reads:
// settings is the 9th vec4 (byte 128), lunar the 15th (byte 224). WGSL
// takes no explicit offsets, so the struct pads to them.
struct Environment {pad0:vec4f,pad1:vec4f,pad2:vec4f,pad3:vec4f,pad4:vec4f,pad5:vec4f,pad6:vec4f,pad7:vec4f,settings:vec4f,pad8:vec4f,pad9:vec4f,pad10:vec4f,pad11:vec4f,pad12:vec4f,lunar:vec4f}
struct Material {color:vec4f,options:vec4f,skin:vec4f,window:vec4f,lighting:vec4f,policy:vec4f,localFog:vec4f,localFogSettings:vec4f,ambient:vec4f,uvU:vec4f,uvV:vec4f,reflection:vec4f}
struct Instance {matrix:mat4x4f,opacity:vec4f,color:vec4f,window:vec4f,pointPosition:vec4f,pointAmbient:vec4f,pointDiffuse:vec4f}
struct Skin {joints:vec4u,weights:vec4f}
@group(0) @binding(0) var<uniform> projection:mat4x4f;
@group(0) @binding(1) var<storage,read> instances:array<Instance>;
@group(0) @binding(2) var<uniform> material:Material;
@group(0) @binding(3) var textureSampler:sampler;
@group(0) @binding(4) var albedo:texture_2d_array<f32>;
@group(0) @binding(5) var<uniform> env:Environment;
@group(0) @binding(6) var<storage,read> skin:array<Skin>;
@group(0) @binding(7) var<storage,read> bones:array<mat4x4f>;
struct Out {@builtin(position) position:vec4f,@location(0) uv:vec2f}
@vertex fn vs(@location(0) position:vec3f,@location(2) uv:vec2f,@builtin(instance_index) i:u32,@builtin(vertex_index) v:u32)->Out {
 var p=vec4f(position,1);let n=material.skin.x;
 if(n!=0){let s=skin[v];let base=select(select(i*u32(abs(n)),0u,n<0),u32(instances[i].opacity.y),instances[i].opacity.z>0);
  p=(bones[base+s.joints.x]*p)*s.weights.x+(bones[base+s.joints.y]*p)*s.weights.y+(bones[base+s.joints.z]*p)*s.weights.z+(bones[base+s.joints.w]*p)*s.weights.w;}
 var o:Out;o.position=projection*instances[i].matrix*p;
 // The same authored UV transform the world shader samples with: a caster
 // must alpha-test the exact texel its receiver shades.
 let movingUV=vec2f(dot(vec3f(uv,1),material.uvU.xyz),dot(vec3f(uv,1),material.uvV.xyz));
 o.uv=(movingUV*material.window.xy+material.window.zw)*instances[i].window.xy+instances[i].window.zw;
 return o;
}
// Depth-only with the world shader's D3D9 alpha test: casters without an
// authored alpha (policy.z) always pass, others truncate to 12 fractional
// bits and compare the byte, as the 2005 HAL did.
@fragment fn fs(v:Out){
 let layer=u32(env.settings.z);
 let tex=textureSampleLevel(albedo,textureSampler,v.uv,i32(layer%textureNumLayers(albedo)),0);
 let alpha=select(tex.a,1.0,material.policy.z>0.5);
 let alphaFixed=floor(clamp(alpha,0.0,1.0)*4096.0);
 let alphaByte=ceil(alphaFixed*(255.0/4096.0)-0.5);
 let reference=round(material.options.x*255.0);var accepted=true;
 switch(u32(material.reflection.y)){
  case 1u:{accepted=false;} case 2u:{accepted=alphaByte<reference;}
  case 3u:{accepted=alphaByte==reference;} case 4u:{accepted=alphaByte<=reference;}
  case 5u:{accepted=alphaByte>reference;} case 6u:{accepted=alphaByte!=reference;}
  case 8u:{} default:{accepted=alphaByte>=reference;}
 }
 if(!accepted||material.skin.w>0.0){discard;}
}`
		} );
		shadowPipeline = device.createRenderPipeline( {
			label: "sun-shadow-caster",
			layout: "auto",
			vertex: {
				module: caster,
				entryPoint: "vs",
				buffers: [ {
					arrayStride: 56,
					attributes: [
						{ shaderLocation: 0, offset: 0, format: "float32x3" },
						{ shaderLocation: 2, offset: 24, format: "float32x2" }
					]
				} ]
			},
			fragment: { module: caster, entryPoint: "fs", targets: [] },
			primitive: { topology: "triangle-list", cullMode: "none" },
			depthStencil: {
				format: "depth24plus",
				depthWriteEnabled: true,
				depthCompare: "less",
				depthBias: SUN_SHADOW_BIAS,
				depthBiasSlopeScale: SUN_SHADOW_BIAS_SLOPE
			}
		} );
		white = device.createTexture( {
			size: [ 1, 1 ],
			format: "rgba8unorm",
			usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST
		} );
		device.queue.writeTexture(
			{ texture: white },
			Uint8Array.of( 255, 255, 255, 255 ),
			{ bytesPerRow: 4 },
			[ 1, 1 ]
		);
		// Opaque world objects and characters wrap their authored UVs just
		// like the visible world pass; clamping repeats an edge's alpha.
		sampler = device.createSampler( {
			minFilter: "linear",
			magFilter: "linear",
			addressModeU: "repeat",
			addressModeV: "repeat"
		} );
		projection = device.createBuffer( {
			label: "sun-shadow-projection",
			size: 64,
			usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
		} );
	};
	// The hardware PCF pair: linear comparison taps on the cascade. The
	// comparison sampler and the always-lit dummy view live in geometry.ts
	// (every world draw binds them); this owner builds nothing until the
	// stage is first enabled.
	/*
	================
	cascade
	================
	*/
	const cascade = () => {
		shadowCascade ??= device.createTexture( {
			label: "sun-shadow-cascade",
			size: [ SHADOW_SIZE, SHADOW_SIZE ],
			format: "depth24plus",
			usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
		} );
		cascadeTarget ??= shadowCascade.createView();
		return shadowCascade;
	};
	// Caster bind groups borrow their draw's buffers for this frame only;
	// identity changes (storage growth, image swap) invalidate the cache.
	const casters = new WeakMap<
		GeometryDraw,
		{ buffers: ShadowBinding; image: GPUTexture | undefined; binding: GPUBindGroup; }
	>();
	let shadowCascade: GPUTexture | null = null, cascadeTarget: GPUTextureView | null = null;
	let enabled = false;
	return {
		/*
		================
		active / cascadeView

		geometry.ts binds the cascade view at bindings 13/14 of every world
		draw while the stage is on; while it is off the draws keep the
		always-lit dummy and the gated shader branch never runs.
		================
		*/
		active: () => enabled,
		cascadeView: () => {
			cascade();
			return cascadeTarget!;
		},
		/*
		================
		prepare

		Publish the cascade matrix for this eye and light, and name whether
		the stage is on. The renderer keeps the casters list.
		================
		*/
		prepare( on: boolean, eye: readonly number[], light: readonly number[] ) {
			enabled = on;
			if ( !on ) return;
			ensure();
			cascade();
			const matrix = sunShadowMatrix( eye, light, SHADOW_EXTENT, SHADOW_DEPTH );
			device.queue.writeBuffer( projection!, 0, matrix );
			device.queue.writeBuffer( environment, matrixOffset, matrix );
		},
		/*
		================
		encode

		One depth-only pass of the frame's casters, before the main pass.
		================
		*/
		encode( encoder: GPUCommandEncoder, draws: readonly GeometryDraw[], timing?: GpuTimingFrame ) {
			if ( !enabled ) return;
			const pass = encoder.beginRenderPass( {
				label: "sun-shadow-cascade",
				timestampWrites: timing?.pass( "sun-shadow-cascade" ),
				colorAttachments: [],
				depthStencilAttachment: {
					view: cascadeTarget!,
					depthClearValue: 1,
					depthLoadOp: "clear",
					depthStoreOp: "store"
				}
			} );
			pass.setPipeline( shadowPipeline! );
			for ( const draw of draws ) {
				if ( draw.indexCount <= 0 || draw.instanceCount <= 0 ) continue;
				const source = binding( draw );
				if ( !source ) continue;
				let cached = casters.get( draw );
				if (
					!cached || cached.buffers.instances !== source.instances ||
					cached.buffers.material !== source.material || cached.buffers.skin !== source.skin ||
					cached.buffers.bones !== source.bones || cached.image !== source.image
				) {
					cached = {
						buffers: source,
						image: source.image,
						binding: device.createBindGroup( {
							layout: shadowPipeline!.getBindGroupLayout( 0 ),
							entries: [
								{ binding: 0, resource: { buffer: projection! } },
								{ binding: 1, resource: source.instances },
								{ binding: 2, resource: source.material },
								{ binding: 3, resource: sampler! },
								{
									binding: 4,
									resource: source.image ?
										source.image.createView( { dimension: "2d-array" } ) :
										white!.createView( { dimension: "2d-array" } )
								},
								{ binding: 5, resource: { buffer: environment } },
								{ binding: 6, resource: { buffer: source.skin } },
								{ binding: 7, resource: { buffer: source.bones } }
							]
						} )
					};
					casters.set( draw, cached );
				}
				pass.setBindGroup( 0, cached.binding );
				pass.setVertexBuffer( 0, draw.vertices );
				pass.setIndexBuffer( draw.indices, "uint32" );
				pass.drawIndexed( draw.indexCount, draw.instanceCount, draw.firstIndex ?? 0, draw.baseVertex ?? 0 );
			}
			pass.end();
		},
		/*
		================
		forget

		draw's buffers are being destroyed: no cascade pass may still bind it.
		================
		*/
		forget( draw: GeometryDraw ) {
			casters.delete( draw );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( shadowCascade ) retire( shadowCascade );
			if ( white ) retire( white );
			if ( projection ) retire( projection );
		}
	};
}
