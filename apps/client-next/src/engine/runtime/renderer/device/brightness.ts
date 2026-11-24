/*
===========================================================================

brightness.ts - the video page's brightness stage: a gamma resolve

The later client's brightness setting (the video page's
GDR_OPT_VIDEO_SP_BR spin, five steps from "Very dark" to "Very
bright") applies through the D3D9 gamma ramp: the presented frame
takes the step's power curve. This stage is that resolve for the
browser frame. While a step other than Normal is chosen, the whole
presented image - scene, world labels, UI and preview - composes
into this owner's intermediate and one fullscreen pass applies
pow(color, 1 / gamma) into the presented frame as the frame's last
colour pass: a display ramp brightens every pixel alike, where the
HDR stage's tone map is scene-only by design (device/hdr.ts). At
the Normal step the stage owns nothing and the scene renders
straight into the presented frame, byte-identical to the path
without it.

The step exponents are the port's (foundation/rendering/video-options.ts
brightnessGamma): the native per-step values were not located in the
v1.696 binary. The curve keeps black and white exact, as the ramp
does; a triangular half-step of noise where the curve moved the
value keeps its stretch of the 8-bit palette from banding (the
presentation finish gates its dither the same way, device/finish.ts).

===========================================================================
*/
import type { BloomDraw } from "../internal/gpu-contract";
import { destroyNow, type Retire } from "./retirement";

/*
================
createBrightness

Owns the scene intermediate and the gamma pass. prepare() with a
gamma of 1 (the Normal step) owns nothing.
================
*/
export function createBrightness( device: GPUDevice, format: GPUTextureFormat, retire: Retire = destroyNow ) {
	// Compilation and validation failures use the device's existing terminal
	// error path; a device that never leaves Normal never compiles it.
	let pipeline: GPURenderPipeline | null = null;
	let exponentBuffer: GPUBuffer | null = null;
	/*
	================
	ensure
	================
	*/
	const ensure = () => {
		if ( pipeline ) return;
		const module = device.createShaderModule( {
			label: "brightness-gamma",
			code: `
 @group(0) @binding(0) var source:texture_2d<f32>;
 @group(0) @binding(1) var linear:sampler;
 @group(0) @binding(2) var<uniform> exponent:f32;
 struct V {@builtin(position) position:vec4f,@location(0) uv:vec2f};
 @vertex fn vs(@builtin(vertex_index) i:u32)->V{
  let p=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3))[i];
  return V(vec4f(p,0,1),vec2f(p.x*.5+.5,.5-p.y*.5));
 }
 fn noise(uv:vec2f,seed:vec2f)->f32{
  return fract(sin(dot(uv+seed,vec2f(12.9898,78.233)))*43758.5453);
 }
 @fragment fn fs(v:V)->@location(0) vec4f{
  let c=textureSampleLevel(source,linear,v.uv,0).rgb;
  let curved=pow(c,vec3f(exponent));
  let gate=clamp(abs(curved-c)*8.0,vec3f(0),vec3f(1));
  let dither=(noise(v.uv,vec2f(0,0))+noise(v.uv,vec2f(7.13,3.71))-1.0)/255.0;
  return vec4f(curved+vec3f(dither)*gate,1.0);
 }
 `
		} );
		pipeline = device.createRenderPipeline( {
			label: "brightness-gamma",
			layout: "auto",
			vertex: { module, entryPoint: "vs" },
			fragment: { module, entryPoint: "fs", targets: [ { format } ] },
			primitive: { topology: "triangle-list" }
		} );
		exponentBuffer = device.createBuffer( {
			label: "brightness-exponent",
			size: 16,
			usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
		} );
	};
	const sampler = device.createSampler( {
		minFilter: "linear",
		magFilter: "linear",
		addressModeU: "clamp-to-edge",
		addressModeV: "clamp-to-edge"
	} );
	let target: GPUTexture | null = null,
		view: GPUTextureView | null = null,
		binding: GPUBindGroup | null = null,
		width = 0,
		height = 0,
		appliedGamma = 1,
		disposed = false;
	/*
	================
	clear
	================
	*/
	function clear() {
		if ( target ) retire( target );
		target = null;
		view = null;
		binding = null;
		width = height = 0;
		appliedGamma = 1;
	}
	return {
		/*
		================
		prepare

		The scene intermediate for a w x h frame, (re)created when the
		size changed; undefined (and no target) while the step is Normal.
		================
		*/
		prepare( w: number, h: number, gamma: number ): BloomDraw | undefined {
			if ( disposed ) throw Error( "Brightness owner disposed" );
			if ( gamma === 1 ) {
				clear();
				return;
			}
			ensure();
			if ( width !== w || height !== h ) {
				clear();
				width = w;
				height = h;
				target = device.createTexture( {
					label: "brightness-scene-color",
					size: [ w, h ],
					format,
					usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
				} );
				view = target.createView();
				binding = device.createBindGroup( {
					layout: pipeline!.getBindGroupLayout( 0 ),
					entries: [
						{ binding: 0, resource: view },
						{ binding: 1, resource: sampler },
						{ binding: 2, resource: { buffer: exponentBuffer! } }
					]
				} );
			}
			if ( appliedGamma !== gamma ) {
				appliedGamma = gamma;
				device.queue.writeBuffer(
					exponentBuffer!,
					0,
					new Float32Array( [ 1 / gamma ] )
				);
			}
			const epoch = target;
			return {
				view: view!,
				/*
				================
				encode
				================
				*/
				encode( encoder, output: GPUTextureView ) {
					if ( disposed || epoch !== target || !pipeline || !binding ) {
						throw Error( "Stale brightness target" );
					}
					const pass = encoder.beginRenderPass( {
						label: "brightness-gamma",
						colorAttachments: [ { view: output, loadOp: "clear", storeOp: "store" } ]
					} );
					pass.setPipeline( pipeline );
					pass.setBindGroup( 0, binding );
					pass.draw( 3 );
					pass.end();
				}
			};
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			clear();
			exponentBuffer?.destroy();
			exponentBuffer = null;
			disposed = true;
		}
	};
}
