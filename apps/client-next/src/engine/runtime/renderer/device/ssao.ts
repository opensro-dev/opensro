/*
===========================================================================

ssao.ts - the experimental ambient-occlusion stage

Port-only, not native. The retail scene has no occlusion term: the
ambient light reaches every surface equally, so corners and the foot of
a character read flat. The Ambient occlusion row copies the frame's
depth into a readable texture after the main pass, then, at half
resolution, reconstructs each texel's view-space position from the
depth alone, rebuilds the receiving surface's own slope from central
depth differences, and compares it against a small spiral of
neighbours (a 12-tap hemisphere, a 3.5-metre view-space radius,
linearised over the camera's near and far): a neighbour that sits
above the surface's plane and inside the radius occludes it, and the
slope's own neighbours never do. One 4-tap blur smooths the
result into the occlusion texture the next frame's surfaces sample and
multiply their ambient term by.

The one-frame lag is deliberate and documented: the occlusion is
computed from the completed frame's depth and applied by the following
frame (the alternative is a depth prepass that re-draws the scene).
A camera cut shows one frame of the previous frame's occlusion; a
moving camera shows none at 60 fps. Off owns nothing and the frame is
byte-identical to the native order.

===========================================================================
*/
import { destroyNow, type Retire } from "./retirement";

// The view-space radius the occlusion is judged over, in metres.
const OCCLUSION_RADIUS = 3.5;
// The spiral's taps; 12 keeps the half-res pass cheap.
const TAPS = 12;

/*
================
createSSAO
================
*/
export function createSSAO( device: GPUDevice, retire: Retire = destroyNow ) {
	let depth: GPUTexture | null = null,
		occlusion: GPUTexture | null = null,
		blurred: GPUTexture | null = null,
		depthView: GPUTextureView | null = null,
		occlusionTextureView: GPUTextureView | null = null,
		blurredView: GPUTextureView | null = null,
		convert: GPURenderPipeline | null = null,
		trace: GPURenderPipeline | null = null,
		blur: GPURenderPipeline | null = null,
		sampler: GPUSampler | null = null,
		depthBinding: GPUBindGroup | null = null,
		traceBinding: GPUBindGroup | null = null,
		blurBinding: GPUBindGroup | null = null,
		width = 0,
		height = 0,
		disposed = false;
	function clear() {
		if ( depth ) retire( depth );
		if ( occlusion ) retire( occlusion );
		if ( blurred ) retire( blurred );
		depth = occlusion = blurred = null;
		depthView = occlusionTextureView = blurredView = null;
		convert = trace = blur = null;
		depthBinding = traceBinding = blurBinding = null;
		width = height = 0;
	}
	function ensurePipelines() {
		if ( trace && blur && sampler ) return;
		sampler = device.createSampler( {
			minFilter: "linear",
			magFilter: "linear",
			addressModeU: "clamp-to-edge",
			addressModeV: "clamp-to-edge"
		} );
		const module = device.createShaderModule( {
			label: "ssao",
			code: `
struct V {@builtin(position) position:vec4f,@location(0) uv:vec2f}
@vertex fn vs(@builtin(vertex_index) i:u32)->V{
 let p=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3))[i];
 return V(vec4f(p,0,1),vec2f(p.x*.5+.5,.5-p.y*.5));
}
@group(0) @binding(0) var source:texture_2d<f32>;
@group(0) @binding(1) var linear:sampler;
@group(0) @binding(2) var depthSource:texture_depth_2d;
@group(0) @binding(3) var<uniform> camera:vec4f;
fn viewZ(d:f32)->f32{
 let near=max(camera.x,0.1);
 let far=max(camera.y,near+1.0);
 return near*far/(far-d*(far-near));
}
// camera.z/w carry the projection's half-angles (tanX, tanY), so a texel's
// linearised depth plus its uv reconstructs its view-space position.
fn viewPos(uv:vec2f,z:f32)->vec3f{
 return vec3f((uv.x*2.0-1.0)*camera.z*z,(1.0-uv.y*2.0)*camera.w*z,z);
}
@fragment fn convertPass(v:V)->@location(0) f32{
 let size=vec2f(textureDimensions(depthSource));
 let at=clamp(vec2i(v.uv*size),vec2i(0),vec2i(size)-vec2i(1));
 return textureLoad(depthSource,at,0);
}
@fragment fn tracePass(v:V)->@location(0) f32{
 // r32float is unfilterable: every read is a textureLoad at the pass's
 // own texel grid, so the taps land on exact converted depths.
 let size=vec2f(textureDimensions(source));
 let one=1.0/size;
 let at=clamp(vec2i(v.uv*size),vec2i(0),vec2i(size)-vec2i(1));
 let z=viewZ(textureLoad(source,at,0).r);
 let P=viewPos(v.uv,z);
 // The receiving surface's own slope, reconstructed from central depth
 // differences: a tilted plane's neighbours lie IN that plane, and without
 // this test the whole ground shades itself as it tilts toward the camera.
 let limit=vec2i(size)-vec2i(1);
 let zdx1=viewZ(textureLoad(source,clamp(at+vec2i(1,0),vec2i(0),limit),0).r);
 let zdx0=viewZ(textureLoad(source,clamp(at-vec2i(1,0),vec2i(0),limit),0).r);
 let zdy1=viewZ(textureLoad(source,clamp(at+vec2i(0,1),vec2i(0),limit),0).r);
 let zdy0=viewZ(textureLoad(source,clamp(at-vec2i(0,1),vec2i(0),limit),0).r);
 let dPdu=viewPos(v.uv+vec2f(one.x,0.0),zdx1)-viewPos(v.uv-vec2f(one.x,0.0),zdx0);
 let dPdv=viewPos(v.uv+vec2f(0.0,one.y),zdy1)-viewPos(v.uv-vec2f(0.0,one.y),zdy0);
 var N=normalize(cross(dPdu,dPdv));
 if(N.z>0.0){N=-N;}
 var occluded=0.0;
 let golden=2.399963;
 for(var tap=0;tap<${TAPS};tap=tap+1){
  let angle=f32(tap)*golden;
  let r=${OCCLUSION_RADIUS.toFixed( 1 )}*sqrt((f32(tap)+0.5)/f32(${TAPS}));
  let offset=vec2f(cos(angle),sin(angle))*r/max(z,0.1)*0.5;
  let sampleAt=clamp(at+vec2i(offset*size),vec2i(0),limit);
  let sampleZ=viewZ(textureLoad(source,sampleAt,0).r);
  let delta=viewPos((vec2f(sampleAt)+vec2f(0.5))*one,sampleZ)-P;
  // A caster's weight is how far above the receiving surface's plane it
  // sits (the slope's own neighbours read ~0 and never occlude), faded by
  // its distance: a full-height wall beside the texel shades it, the same
  // wall three metres away barely does.
  let dist=length(delta);
  // The tap's perpendicular height above the receiving plane: a real caster
  // stands decimetres above it, while depth-quantisation noise on far
  // ground stands centimetres and fades to nothing against the half-metre
  // scale. A vertical wall seen from above barely changes depth, so the
  // gate reads the reconstructed geometry, not the raw depth delta.
  let above=clamp(dot(N,delta)/0.5,0.0,1.0);
  let within=clamp(1.0-dist/${OCCLUSION_RADIUS.toFixed( 1 )},0.0,1.0);
  occluded=occluded+above*within;
 }
 return clamp(1.0-occluded/f32(${TAPS})*2.6,0.0,1.0);
}
@fragment fn blurPass(v:V)->@location(0) f32{
 let size=vec2f(textureDimensions(source));
 let one=1.0/size;
 var sum=textureSampleLevel(source,linear,v.uv,0).r*4.0;
 sum=sum+textureSampleLevel(source,linear,v.uv+vec2f(one.x,0),0).r;
 sum=sum+textureSampleLevel(source,linear,v.uv-vec2f(one.x,0),0).r;
 sum=sum+textureSampleLevel(source,linear,v.uv+vec2f(0,one.y),0).r;
 sum=sum+textureSampleLevel(source,linear,v.uv-vec2f(0,one.y),0).r;
 return sum/8.0;
}`
		} );
		const layout = (
			entries: {
				binding: number;
				visibility: number;
				texture?: { sampleType: "float" | "unfilterable-float" | "depth"; };
				sampler?: { type: "filtering" | "non-filtering"; };
				buffer?: { type: "uniform"; };
			}[]
		) => device.createBindGroupLayout( {
			entries: entries.map( e => ({
				binding: e.binding,
				visibility: e.visibility,
				...(e.texture ? { texture: e.texture } : {}),
				...(e.sampler ? { sampler: e.sampler } : {}),
				...(e.buffer ? { buffer: e.buffer } : {})
			}) )
		} );
		const uniform = { visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } as const };
		convert = device.createRenderPipeline( {
			label: "ssao-convert",
			// convertPass samples only the frame depth, so its auto layout
			// would not carry the camera uniform the bind group also binds -
			// an entry the layout lacks invalidates the whole frame's submit.
			layout: device.createPipelineLayout( {
				bindGroupLayouts: [ layout( [
					{ binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth" } },
					{ binding: 3, ...uniform }
				] ) ]
			} ),
			vertex: { module, entryPoint: "vs" },
			fragment: { module, entryPoint: "convertPass", targets: [ { format: "r32float" } ] },
			primitive: { topology: "triangle-list" }
		} );
		trace = device.createRenderPipeline( {
			label: "ssao-trace",
			layout: device.createPipelineLayout( {
				bindGroupLayouts: [ layout( [
					// r32float is unfilterable: the trace reads it with
					// textureLoad only, so the layout must say so.
					{ binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "unfilterable-float" } },
					{ binding: 3, ...uniform }
				] ) ]
			} ),
			vertex: { module, entryPoint: "vs" },
			fragment: { module, entryPoint: "tracePass", targets: [ { format: "r8unorm" } ] },
			primitive: { topology: "triangle-list" }
		} );
		blur = device.createRenderPipeline( {
			label: "ssao-blur",
			layout: "auto",
			vertex: { module, entryPoint: "vs" },
			fragment: { module, entryPoint: "blurPass", targets: [ { format: "r8unorm" } ] },
			primitive: { topology: "triangle-list" }
		} );
	}
	// The camera's near and far, written once per frame the encode runs.
	const cameraUniform = device.createBuffer( {
		label: "ssao-camera",
		size: 16,
		usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
	} );
	const cameraValues = new Float32Array( 4 );
	return {
		/*
		================
		prepare

		The half-resolution depth, occlusion and blur targets for a w x h
		frame, (re)created when the size changed; undefined (owning
		nothing) while the row is off.
		================
		*/
		prepare( w: number, h: number, enabled: boolean ) {
			if ( disposed ) throw Error( "SSAO owner disposed" );
			if ( !enabled ) {
				clear();
				return;
			}
			const halfW = Math.max( 1, Math.ceil( w / 2 ) ), halfH = Math.max( 1, Math.ceil( h / 2 ) );
			if ( width !== w || height !== h ) {
				clear();
				width = w;
				height = h;
				depth = device.createTexture( {
					label: "ssao-depth",
					size: [ halfW, halfH ],
					format: "r32float",
					usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
				} );
				occlusion = device.createTexture( {
					label: "ssao-occlusion",
					size: [ halfW, halfH ],
					format: "r8unorm",
					usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
				} );
				blurred = device.createTexture( {
					label: "ssao-blurred",
					size: [ halfW, halfH ],
					format: "r8unorm",
					usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
				} );
				// One view per texture, cached at creation like the water lane: a
				// fresh view per frame defeats the geometry bindings' identity
				// check and rebuilds every draw's bind group every frame.
				depthView = depth.createView();
				occlusionTextureView = occlusion.createView();
				blurredView = blurred.createView();
			}
			ensurePipelines();
			return {
				occlusionView: blurredView!,
				/*
				================
				encode

				The three half-res passes over the completed frame's depth:
				convert, trace, blur. Called once per frame, after the main
				pass; the occlusion the next frame samples is the blurred
				result.
				================
				*/
				encode(
					encoder: GPUCommandEncoder,
					frameDepth: GPUTextureView,
					near: number,
					far: number,
					tanX: number,
					tanY: number
				) {
					// Written before the stale guard: the success path needs the
					// camera as much as the throw's diagnostics do. The original
					// placement inside the guard never wrote them, so every trace
					// linearised its depths with the clamped 0.1/1.1 pair.
					cameraValues[0] = near;
					cameraValues[1] = far;
					cameraValues[2] = tanX;
					cameraValues[3] = tanY;
					device.queue.writeBuffer( cameraUniform, 0, cameraValues );
					if (
						disposed || !depth || !occlusion || !blurred || !depthView || !occlusionTextureView ||
						!blurredView || !convert || !trace || !blur || !sampler
					) {
						throw Error( "Stale SSAO targets" );
					}
					if ( !depthBinding ) {
						depthBinding = device.createBindGroup( {
							layout: convert.getBindGroupLayout( 0 ),
							entries: [
								{ binding: 2, resource: frameDepth },
								{ binding: 3, resource: { buffer: cameraUniform } }
							]
						} );
					}
					if ( !traceBinding ) {
						traceBinding = device.createBindGroup( {
							layout: trace.getBindGroupLayout( 0 ),
							entries: [
								{ binding: 0, resource: depthView },
								{ binding: 3, resource: { buffer: cameraUniform } }
							]
						} );
					}
					if ( !blurBinding ) {
						blurBinding = device.createBindGroup( {
							layout: blur.getBindGroupLayout( 0 ),
							entries: [
								{ binding: 0, resource: occlusionTextureView },
								{ binding: 1, resource: sampler }
							]
						} );
					}
					const convertPass = encoder.beginRenderPass( {
						label: "ssao-convert",
						colorAttachments: [ { view: depthView, loadOp: "clear", storeOp: "store" } ]
					} );
					convertPass.setPipeline( convert );
					convertPass.setBindGroup( 0, depthBinding );
					convertPass.draw( 3 );
					convertPass.end();
					const tracePass = encoder.beginRenderPass( {
						label: "ssao-trace",
						colorAttachments: [ { view: occlusionTextureView, loadOp: "clear", storeOp: "store" } ]
					} );
					tracePass.setPipeline( trace );
					tracePass.setBindGroup( 0, traceBinding );
					tracePass.draw( 3 );
					tracePass.end();
					const blurPass = encoder.beginRenderPass( {
						label: "ssao-blur",
						colorAttachments: [ { view: blurredView, loadOp: "clear", storeOp: "store" } ]
					} );
					blurPass.setPipeline( blur );
					blurPass.setBindGroup( 0, blurBinding );
					blurPass.draw( 3 );
					blurPass.end();
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
			// The uniform lives outside clear(): prepare() reuses it across
			// resizes and off frames, so only the owner's end retires it.
			retire( cameraUniform );
			disposed = true;
		}
	};
}
