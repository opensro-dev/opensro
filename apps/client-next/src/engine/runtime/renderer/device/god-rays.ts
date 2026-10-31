/*
===========================================================================

god-rays.ts - the experimental screen-space sun rays

Port-only, not native. The retail sun is an authored disc and the sky a
two-colour gradient; nothing scatters toward the eye. The Sun rays row
copies the completed frame's depth into a readable texture, then, at
half resolution, marches each pixel's ray toward the sun's screen
position: every sample that lands on the sky (the cleared far depth)
scatters light, every sample on geometry blocks it. The accumulated
light, weighted by its distance from the sun, lands in an rgba8 rays
texture that one additive pass composites over the scene - light shafts
where the sky shows through the world's own gaps.

The sun's screen position, its clip w (the ray fades behind the eye) and
the strength ride the stage's own uniform, written per frame by the
renderer from the same view-projection the frame drew with. Off owns
nothing and the frame is byte-identical to the native order.

===========================================================================
*/
import type { GodRaysDraw } from "../internal/gpu-contract";
import { destroyNow, type Retire } from "./retirement";

// March steps toward the sun; 24 keeps the half-res pass cheap.
const STEPS = 24;
// The screen distance beyond which the rays are gone, measured in frame
// heights so a wide frame does not stretch the falloff.
const RAY_RADIUS = 0.9;
// The scatter's peak gain: the light added at the sun's screen position
// when the whole path to it is sky and the frame carries the strength.
const GAIN = 0.35;

/*
================
createGodRays
================
*/
export function createGodRays( device: GPUDevice, format: GPUTextureFormat, retire: Retire = destroyNow ) {
	let depth: GPUTexture | null = null,
		rays: GPUTexture | null = null,
		depthView: GPUTextureView | null = null,
		raysView: GPUTextureView | null = null,
		convert: GPURenderPipeline | null = null,
		march: GPURenderPipeline | null = null,
		composite: GPURenderPipeline | null = null,
		sampler: GPUSampler | null = null,
		depthBinding: GPUBindGroup | null = null,
		marchBinding: GPUBindGroup | null = null,
		compositeBinding: GPUBindGroup | null = null,
		boundDepthSource: GPUTextureView | null = null,
		width = 0,
		height = 0,
		disposed = false;
	/*
	================
	clear

	The pipelines survive a resize; only the size-bound targets and their
	bind groups go.
	================
	*/
	function clear() {
		if ( depth ) retire( depth );
		if ( rays ) retire( rays );
		depth = rays = null;
		depthView = raysView = null;
		depthBinding = marchBinding = compositeBinding = null;
		boundDepthSource = null;
		width = height = 0;
	}
	function ensurePipelines() {
		if ( convert && march && composite ) return;
		sampler ??= device.createSampler( {
			minFilter: "linear",
			magFilter: "linear",
			addressModeU: "clamp-to-edge",
			addressModeV: "clamp-to-edge"
		} );
		const module = device.createShaderModule( {
			label: "god-rays",
			code: `
struct V {@builtin(position) position:vec4f,@location(0) uv:vec2f}
@vertex fn vs(@builtin(vertex_index) i:u32)->V{
 let p=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3))[i];
 return V(vec4f(p,0,1),vec2f(p.x*.5+.5,.5-p.y*.5));
}
@group(0) @binding(0) var depthSource:texture_depth_2d;
@fragment fn convertPass(v:V)->@location(0) f32{
 let size=vec2f(textureDimensions(depthSource));
 let at=clamp(vec2i(v.uv*size),vec2i(0),vec2i(size)-vec2i(1));
 return textureLoad(depthSource,at,0);
}
// The march reads the converted depth; r32float is unfilterable, so every
// read is a textureLoad on the pass's own texel grid.
@group(0) @binding(1) var depthCopy:texture_2d<f32>;
@group(0) @binding(2) var<uniform> sun:vec4f;
@fragment fn marchPass(v:V)->@location(0) vec4f{
 // The sun's screen position (xy), its clip w (z) and the strength (w).
 let size=vec2f(textureDimensions(depthCopy));
 let sunScreen=sun.xy;
 let behind=sun.z<=0.0;
 let toSun=sunScreen-v.uv;
 // Distance in frame heights so the radius does not stretch with the aspect.
 let dist=length(vec2f(toSun.x*f32(size.x)/f32(size.y),toSun.y));
 var light=0.0;
 if(!behind&&dist<${RAY_RADIUS.toFixed( 1 )}){
  let step=toSun/f32(${STEPS});
  var uv=v.uv;
  for(var i=0;i<${STEPS};i=i+1){
   uv=uv+step;
   let at=clamp(vec2i(uv*size),vec2i(0),vec2i(size)-vec2i(1));
   // The sky is the cleared far depth: it scatters; geometry resets the
   // run, so only the sky the sun shines through beside a silhouette
   // reaches the eye - the shaft keeps its hard native-feeling edge.
   if(textureLoad(depthCopy,at,0).r>=0.9995){light=light+1.0;}else{light=0.0;}
  }
  light=light/f32(${STEPS});
 }
 let falloff=select(0.0,clamp(1.0-dist/${RAY_RADIUS.toFixed( 1 )},0.0,1.0),!behind);
 return vec4f(vec3f(light*falloff*sun.w*${GAIN}),1.0);
}
@group(0) @binding(3) var raysMap:texture_2d<f32>;
@group(0) @binding(4) var linear:sampler;
@fragment fn compositePass(v:V)->@location(0) vec4f{
 return vec4f(textureSampleLevel(raysMap,linear,v.uv,0).rgb,0.0);
}`
		} );
		convert = device.createRenderPipeline( {
			label: "god-rays-convert",
			layout: "auto",
			vertex: { module, entryPoint: "vs" },
			fragment: { module, entryPoint: "convertPass", targets: [ { format: "r32float" } ] },
			primitive: { topology: "triangle-list" }
		} );
		// The march's depth copy is unfilterable and its sun a plain uniform;
		// the explicit layout keeps "auto" from widening the texture binding.
		const marchLayout = device.createBindGroupLayout( {
			entries: [
				{ binding: 1, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "unfilterable-float" } },
				{ binding: 2, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } }
			]
		} );
		march = device.createRenderPipeline( {
			label: "god-rays-march",
			layout: device.createPipelineLayout( { bindGroupLayouts: [ marchLayout ] } ),
			vertex: { module, entryPoint: "vs" },
			fragment: { module, entryPoint: "marchPass", targets: [ { format: "rgba8unorm" } ] },
			primitive: { topology: "triangle-list" }
		} );
		composite = device.createRenderPipeline( {
			label: "god-rays-composite",
			layout: "auto",
			vertex: { module, entryPoint: "vs" },
			fragment: {
				module,
				entryPoint: "compositePass",
				targets: [ {
					format,
					blend: {
						color: { srcFactor: "one", dstFactor: "one", operation: "add" },
						alpha: { srcFactor: "zero", dstFactor: "one", operation: "add" }
					}
				} ]
			},
			primitive: { topology: "triangle-list" }
		} );
	}
	// The stage's uniform: the sun's screen state, written per frame by the
	// renderer.
	const sunUniform = device.createBuffer( {
		label: "god-rays-sun",
		size: 16,
		usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
	} );
	const sunValues = new Float32Array( 4 );
	return {
		/*
		================
		prepare

		The half-resolution depth copy and rays targets for a w x h frame,
		(re)created when the size changed; undefined (owning nothing) while
		the row is off.
		================
		*/
		prepare( w: number, h: number, enabled: boolean ): GodRaysDraw | undefined {
			if ( disposed ) throw Error( "God rays owner disposed" );
			if ( !enabled ) {
				clear();
				return;
			}
			ensurePipelines();
			const halfW = Math.max( 1, Math.ceil( w / 2 ) ), halfH = Math.max( 1, Math.ceil( h / 2 ) );
			if ( width !== w || height !== h ) {
				clear();
				width = w;
				height = h;
				depth = device.createTexture( {
					label: "god-rays-depth",
					size: [ halfW, halfH ],
					format: "r32float",
					usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
				} );
				rays = device.createTexture( {
					label: "god-rays-rays",
					size: [ halfW, halfH ],
					format: "rgba8unorm",
					usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
				} );
				depthView = depth.createView();
				raysView = rays.createView();
				marchBinding = device.createBindGroup( {
					layout: march!.getBindGroupLayout( 0 ),
					entries: [ { binding: 1, resource: depthView }, { binding: 2, resource: { buffer: sunUniform } } ]
				} );
				compositeBinding = device.createBindGroup( {
					layout: composite!.getBindGroupLayout( 0 ),
					entries: [ { binding: 3, resource: raysView }, { binding: 4, resource: sampler! } ]
				} );
			}
			const epoch = depth;
			return {
				/*
				================
				sun

				The renderer's [u, v, clip w, strength] for this frame, written
				straight to the queue before the frame encodes.
				================
				*/
				sun( u: number, v: number, w: number, strength: number ) {
					if ( disposed ) throw Error( "Stale god rays targets" );
					sunValues[0] = u;
					sunValues[1] = v;
					sunValues[2] = w;
					sunValues[3] = strength;
					device.queue.writeBuffer( sunUniform, 0, sunValues );
				},
				/*
				================
				encode

				The three half-res passes over the completed scene: convert the
				stored depth, march toward the sun, composite the rays into the
				scene additively. The frame's own depth view can change under
				the stage, so its binding follows the view it last recorded.
				================
				*/
				encode( encoder, frameDepth: GPUTextureView, target: GPUTextureView ) {
					if ( disposed || epoch !== depth || !depthView || !raysView || !convert || !march || !composite ) {
						throw Error( "Stale god rays targets" );
					}
					if ( !depthBinding || boundDepthSource !== frameDepth ) {
						boundDepthSource = frameDepth;
						depthBinding = device.createBindGroup( {
							layout: convert.getBindGroupLayout( 0 ),
							entries: [ { binding: 0, resource: frameDepth } ]
						} );
					}
					const convertPass = encoder.beginRenderPass( {
						label: "god-rays-convert",
						colorAttachments: [ { view: depthView, loadOp: "clear", storeOp: "store" } ]
					} );
					convertPass.setPipeline( convert );
					convertPass.setBindGroup( 0, depthBinding );
					convertPass.draw( 3 );
					convertPass.end();
					const marchPass = encoder.beginRenderPass( {
						label: "god-rays-march",
						colorAttachments: [ { view: raysView, loadOp: "clear", storeOp: "store" } ]
					} );
					marchPass.setPipeline( march );
					marchPass.setBindGroup( 0, marchBinding );
					marchPass.draw( 3 );
					marchPass.end();
					const compositePass = encoder.beginRenderPass( {
						label: "god-rays-composite",
						colorAttachments: [ { view: target, loadOp: "load", storeOp: "store" } ]
					} );
					compositePass.setPipeline( composite );
					compositePass.setBindGroup( 0, compositeBinding );
					compositePass.draw( 3 );
					compositePass.end();
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
			sunUniform.destroy();
			disposed = true;
		}
	};
}
