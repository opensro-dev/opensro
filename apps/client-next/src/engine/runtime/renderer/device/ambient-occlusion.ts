/*
===========================================================================

ambient-occlusion.ts - the experimental ambient occlusion pass (#2094)

Port-only, not native. The 2005 scene has no occlusion term: the ambient
light reaches every surface equally, so corners and the foot of a
character read flat. While the Experimental row is on, the frame's
depth is totalled once at half resolution - a 12-tap view-space spiral
around each surface point, linearised over the camera's near/far -
and blurred once. The surfaces of the NEXT frame sample the result
(pipelines.ts, binding 15) and scale their ambient term by it; the
diffuse keeps its direction and the unlit path is untouched.

The one-frame lag is deliberate: the alternative is a depth prepass
that re-draws the scene. prepare() with the stage off owns nothing -
no pipeline, no target - and the geometry bindings name a 1x1 white
view instead (geometry.ts), so the off frame is the native one. The
same white view serves a target with no measurement yet: a fresh
texture starts zeroed (factor 0, black), so prepare names the blurred
view only once the first encode has written the live target, and
neither enabling the stage nor a resize can flash the scene black.

===========================================================================
*/
import type { WorldCamera } from "@/engine/contracts/scene";
import { projectionTangents } from "@/engine/foundation/rendering/world-math";
import type { AmbientOcclusionDraw } from "../internal/gpu-contract";
import { destroyNow, type Retire } from "./retirement";

// The spiral's shape: twelve taps turned by the golden angle walk the
// disc around the surface point, nearest first. The radius is
// view-space metres: contact scale, so a doorway's frame and a
// character's feet shade, not whole streets. The bias keeps a flat
// surface from occluding itself on depth precision alone.
export const AO_TAPS = 12;
export const AO_RADIUS = 1.5;
export const AO_STRENGTH = 1.0;
export const AO_BIAS = 0.05;

// The spiral's turn per tap (the golden angle, radians) and how far a
// tap's occluder may sit from the surface point, in radii, before it
// stops counting: occlusion is local, so a wall across the street
// never darkens this side of it.
const AO_GOLDEN_ANGLE = 2.3999632;
const AO_LOCAL_RANGE = 4.0;

/*
================
createAmbientOcclusion

Owns the two half-resolution factor targets and the passes that fill
them. The blurred target is the one the surfaces sample; its view is
stable between resizes, so geometry bindings hold it across frames
(geometry.ts refreshes them when the view changes, as the sun
cascade's bindings are refreshed).
================
*/
export function createAmbientOcclusion( device: GPUDevice, retire: Retire = destroyNow ) {
	let pipeline: GPURenderPipeline | null = null, blurPipeline: GPURenderPipeline | null = null;
	let uniforms: GPUBuffer | null = null;
	/*
	================
	ensure
	================
	*/
	const ensure = () => {
		if ( pipeline ) return;
		const vertex = `
 struct V {@builtin(position) position:vec4f,@location(0) uv:vec2f};
 @vertex fn vs(@builtin(vertex_index) i:u32)->V{
  let p=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3))[i];
  return V(vec4f(p,0,1),vec2f(p.x*.5+.5,.5-p.y*.5));
 }`;
		const module = device.createShaderModule( {
			label: "ambient-occlusion",
			code: `
 @group(0) @binding(0) var sceneDepth:texture_depth_2d;
 @group(0) @binding(1) var<uniform> aoParams:vec4f;
 @group(0) @binding(2) var<uniform> aoSpiral:vec4f;
${vertex}
 fn viewZ(depth:f32)->f32{
  return aoParams.z*aoParams.w/(aoParams.w-depth*(aoParams.w-aoParams.z));
 }
 @fragment fn fs(v:V)->@location(0) vec4f{
  let size=textureDimensions(sceneDepth);
  let px=vec2i(v.position.xy);
  let centre=min(px*2+vec2i(1),vec2i(size)-vec2i(1));
  let depth=textureLoad(sceneDepth,centre,0);
  if(depth>=0.99999){return vec4f(1);}
  let z=viewZ(depth);
  let ndc=vec2f(v.uv.x*2.0-1.0,1.0-v.uv.y*2.0);
  let point=vec3f(ndc.x*aoParams.x*z,ndc.y*aoParams.y*z,z);
  // Each pixel turns the spiral by its own gradient noise, so the
  // twelve taps never draw the same spokes on flat ground.
  let turn=fract(52.9829189*fract(0.06711056*f32(px.x)+0.00583715*f32(px.y)));
  var occlusion=0.0;
  for(var i=0;i<${AO_TAPS};i++){
   let t=(f32(i)+0.5)/${AO_TAPS}.0;
   let angle=(f32(i)+turn)*${AO_GOLDEN_ANGLE};
   let tap=point+vec3f(vec2f(cos(angle),sin(angle))*aoSpiral.x*t,0.0);
   let projected=vec2f(tap.x/(tap.z*aoParams.x),tap.y/(tap.z*aoParams.y));
   let uv=vec2f(projected.x*0.5+0.5,0.5-projected.y*0.5);
   if(uv.x<0.0||uv.x>1.0||uv.y<0.0||uv.y>1.0){continue;}
   let samplePx=clamp(vec2i(uv*vec2f(size)),vec2i(0),vec2i(size)-vec2i(1));
   let gap=z-viewZ(textureLoad(sceneDepth,samplePx,0));
   if(gap>aoSpiral.z){
    let inside=clamp(gap/aoSpiral.x,0.0,1.0);
    let local=clamp(1.0-abs(gap)/(aoSpiral.x*${AO_LOCAL_RANGE}),0.0,1.0);
    occlusion+=inside*local;
   }
  }
  let factor=clamp(1.0-aoSpiral.y*occlusion/${AO_TAPS}.0,0.0,1.0);
  return vec4f(vec3f(factor),1);
 }`
		} );
		pipeline = device.createRenderPipeline( {
			label: "ambient-occlusion",
			layout: "auto",
			vertex: { module, entryPoint: "vs" },
			fragment: { module, entryPoint: "fs", targets: [ { format: "r8unorm" } ] },
			primitive: { topology: "triangle-list" }
		} );
		const blurModule = device.createShaderModule( {
			label: "ambient-occlusion-blur",
			code: `
 @group(0) @binding(0) var source:texture_2d<f32>;
${vertex}
 @fragment fn fs(v:V)->@location(0) vec4f{
  let size=vec2i(textureDimensions(source));
  let px=vec2i(v.position.xy);
  var sum=0.0;
  for(var y=-1;y<=1;y++){
   for(var x=-1;x<=1;x++){
    sum+=textureLoad(source,clamp(px+vec2i(x,y),vec2i(0),size-vec2i(1)),0).r;
   }
  }
  return vec4f(vec3f(sum/9.0),1);
 }`
		} );
		blurPipeline = device.createRenderPipeline( {
			label: "ambient-occlusion-blur",
			layout: "auto",
			vertex: { module: blurModule, entryPoint: "vs" },
			fragment: { module: blurModule, entryPoint: "fs", targets: [ { format: "r8unorm" } ] },
			primitive: { topology: "triangle-list" }
		} );
		uniforms = device.createBuffer( {
			label: "ambient-occlusion-params",
			size: 32,
			usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
		} );
	};
	// encodedEpoch is the blurred target the last completed encode
	// wrote. prepare names the blurred view to the surfaces only while
	// the live target is that one, so a fresh (zeroed) target is never
	// sampled before its first measurement (factor 1 until then).
	let raw: GPUTexture | null = null,
		rawView: GPUTextureView | null = null,
		blurred: GPUTexture | null = null,
		blurredView: GPUTextureView | null = null,
		blurBinding: GPUBindGroup | null = null,
		depthBinding: GPUBindGroup | null = null,
		depthBound: GPUTextureView | null = null,
		encodedEpoch: GPUTexture | null = null,
		width = 0,
		height = 0,
		disposed = false;
	/*
	================
	clear
	================
	*/
	function clear() {
		if ( raw ) retire( raw );
		if ( blurred ) retire( blurred );
		raw = null;
		rawView = null;
		blurred = null;
		blurredView = null;
		blurBinding = null;
		depthBinding = null;
		depthBound = null;
		encodedEpoch = null;
		width = height = 0;
	}
	return {
		/*
		================
		prepare

		The frame's occlusion draw at half the scene's resolution, with
		the camera's projection packed for the pass; undefined (and no
		targets) while the stage is off. The draw names the blurred
		view only once the live target carries a measurement - until
		the first encode lands, the surfaces keep the white view, so
		the factor they sample is 1, never a fresh target's zero.
		================
		*/
		prepare(
			sceneWidth: number,
			sceneHeight: number,
			enabled: boolean,
			camera: WorldCamera
		): AmbientOcclusionDraw | undefined {
			if ( disposed ) throw Error( "Ambient occlusion owner disposed" );
			if ( !enabled ) {
				clear();
				return;
			}
			ensure();
			const w = Math.max( 1, Math.floor( sceneWidth / 2 ) ),
				h = Math.max( 1, Math.floor( sceneHeight / 2 ) );
			if ( width !== w || height !== h ) {
				clear();
				width = w;
				height = h;
				const usage = GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING;
				raw = device.createTexture( {
					label: "ambient-occlusion-raw",
					size: [ w, h ],
					format: "r8unorm",
					usage
				} );
				rawView = raw.createView();
				blurred = device.createTexture( {
					label: "ambient-occlusion",
					size: [ w, h ],
					format: "r8unorm",
					usage
				} );
				blurredView = blurred.createView();
				blurBinding = device.createBindGroup( {
					layout: blurPipeline!.getBindGroupLayout( 0 ),
					entries: [ { binding: 0, resource: rawView } ]
				} );
			}
			const tangents = projectionTangents( camera, sceneWidth / sceneHeight );
			device.queue.writeBuffer(
				uniforms!,
				0,
				new Float32Array( [
					tangents.tanX,
					tangents.tanY,
					camera.near,
					camera.far,
					AO_RADIUS,
					AO_STRENGTH,
					AO_BIAS,
					0
				] )
			);
			const epoch = blurred;
			return {
				view: encodedEpoch === blurred ? blurredView! : null,
				/*
				================
				encode
				================
				*/
				encode( encoder, depth: GPUTextureView ) {
					if ( disposed || epoch !== blurred || !pipeline || !blurPipeline ) {
						throw Error( "Stale ambient occlusion target" );
					}
					if ( depthBound !== depth ) {
						depthBound = depth;
						depthBinding = device.createBindGroup( {
							layout: pipeline.getBindGroupLayout( 0 ),
							entries: [
								{ binding: 0, resource: depth },
								{ binding: 1, resource: { buffer: uniforms! } },
								{ binding: 2, resource: { buffer: uniforms!, offset: 16, size: 16 } }
							]
						} );
					}
					const pass = encoder.beginRenderPass( {
						label: "ambient-occlusion",
						colorAttachments: [ {
							view: rawView!,
							clearValue: { r: 1, g: 1, b: 1, a: 1 },
							loadOp: "clear",
							storeOp: "store"
						} ]
					} );
					pass.setPipeline( pipeline );
					pass.setBindGroup( 0, depthBinding! );
					pass.draw( 3 );
					pass.end();
					const blur = encoder.beginRenderPass( {
						label: "ambient-occlusion-blur",
						colorAttachments: [ {
							view: blurredView!,
							clearValue: { r: 1, g: 1, b: 1, a: 1 },
							loadOp: "clear",
							storeOp: "store"
						} ]
					} );
					blur.setPipeline( blurPipeline );
					blur.setBindGroup( 0, blurBinding! );
					blur.draw( 3 );
					blur.end();
					// The live target now carries a measurement: from
					// the next prepare the surfaces may sample it.
					encodedEpoch = epoch;
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
			if ( uniforms ) retire( uniforms );
			uniforms = null;
			disposed = true;
		}
	};
}
