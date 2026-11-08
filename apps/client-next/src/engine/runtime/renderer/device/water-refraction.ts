/*
===========================================================================

water-refraction.ts - the experimental refracted water pass

Port-only, not native. The retail water surface blends its planar
reflection over the frame (pipelines.ts's skin.z path, 8BA130's projected
mirror sample); what sits under the surface reaches the eye untouched.
The Experimental "Water refraction" row draws the water apart instead:
the main pass renders the opaque scene without the blended water, this
stage copies the frame's colour and its depth (linearised to view-space
metres), and the frame owner then draws the blended water and the
transparent tail over the frame in their native order. The water's
fragment refracts the colour copy by the water's own bump offset and
whitens where the depth behind it comes within a band of its own - the
shore.

The colour copy is a fullscreen load (the retained frame carries
TEXTURE_BINDING; the canvas texture does not); the depth copy is a
fullscreen load into r32float (a depth attachment cannot be sampled
while it is attached, and the renderer is single-sampled, so a plain
load reads every texel). Off, the stage owns nothing, the water draws
stay in the main pass and the frame is byte-identical to the native
order.

===========================================================================
*/
import type { WorldCamera } from "@/engine/contracts/scene";
import type { WaterRefractionDraw } from "../internal/gpu-contract";
import { destroyNow, type Retire } from "./retirement";

// The shading constants pipelines.ts interpolates into the water's
// fragment. The refraction strength scales the water's bump offset (the
// mirror's own dudv pair) into a screen-space shift; the behind weight
// darkens what is seen through the water; the reflection mix is the
// planar reflection's share over it; the shore band is the view-space
// distance, in metres, over which the foam fades from the waterline;
// the foam is the whitening added at the line itself.
export const WATER_REFRACTION_STRENGTH = 0.03;
export const WATER_BEHIND_WEIGHT = 0.85;
export const WATER_REFLECTION_MIX = 0.45;
export const WATER_SHORE_BAND = 2.0;
export const WATER_SHORE_FOAM = 0.4;

// The material's skin.z for a blended water surface. Native water packs
// 1 (geometry.ts); every native test reads skin.z>0.5, so blended water
// still takes the native path while the row is off. The refraction
// branch admits only this value: the opaque water family (the ice)
// draws in the main pass and must never sample this frame's copies.
export const WATER_SKIN_BLENDED = 2;

// The params uniform: the camera's near and far, then padding.
const PARAMS_BYTES = 16;

/*
================
WaterRefractionStage

What prepare hands the device: the frame's copy pass, and the copies'
views for the geometry bindings (16/17). Both views are stable while the
scene size and format hold.
================
*/
export interface WaterRefractionStage extends WaterRefractionDraw {
	readonly colourView: GPUTextureView;
	readonly depthView: GPUTextureView;
}

/*
================
createWaterRefraction
================
*/
export function createWaterRefraction( device: GPUDevice, retire: Retire = destroyNow ) {
	let colourModule: GPUShaderModule | null = null,
		depthPipeline: GPURenderPipeline | null = null,
		colourPipeline: GPURenderPipeline | null = null,
		colourPipelineFormat: GPUTextureFormat | null = null,
		params: GPUBuffer | null = null;
	const paramValues = new Float32Array( PARAMS_BYTES / 4 );
	let colour: GPUTexture | null = null,
		depth: GPUTexture | null = null,
		stage: WaterRefractionStage | null = null,
		boundColour: GPUTextureView | null = null,
		colourBinding: GPUBindGroup | null = null,
		boundDepth: GPUTextureView | null = null,
		depthBinding: GPUBindGroup | null = null,
		width = 0,
		height = 0,
		format: GPUTextureFormat | null = null,
		near = 0,
		far = 0,
		disposed = false;
	/*
	================
	ensure

	The format-free parts, created once: the depth linearisation pipeline,
	the colour copy's module and the params uniform. The colour pipeline
	follows the scene format (canvas or the HDR float target).
	================
	*/
	function ensure( sceneFormat: GPUTextureFormat ) {
		if ( !depthPipeline ) {
			const vertex = `
struct V {@builtin(position) position:vec4f}
@vertex fn vs(@builtin(vertex_index) i:u32)->V{
 let p=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3))[i];
 return V(vec4f(p,0,1));
}`;
			const depthModule = device.createShaderModule( {
				label: "water-refraction-depth",
				code: vertex + `
@group(0) @binding(0) var sourceDepth:texture_depth_2d;
@group(0) @binding(1) var<uniform> camera:vec4f;
// The view-space distance of the depth behind each pixel, in metres:
// the same linearisation over near/far the occlusion stage uses.
@fragment fn fs(v:V)->@location(0) f32{
 let depth=textureLoad(sourceDepth,vec2i(v.position.xy),0);
 return camera.x*camera.y/(camera.y-depth*(camera.y-camera.x));
}`
			} );
			depthPipeline = device.createRenderPipeline( {
				label: "water-refraction-depth",
				layout: "auto",
				vertex: { module: depthModule, entryPoint: "vs" },
				fragment: { module: depthModule, entryPoint: "fs", targets: [ { format: "r32float" } ] },
				primitive: { topology: "triangle-list" }
			} );
			colourModule = device.createShaderModule( {
				label: "water-refraction-colour",
				code: vertex + `
@group(0) @binding(0) var source:texture_2d<f32>;
@fragment fn fs(v:V)->@location(0) vec4f{return textureLoad(source,vec2i(v.position.xy),0);}`
			} );
			params = device.createBuffer( {
				label: "water-refraction-params",
				size: PARAMS_BYTES,
				usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
			} );
		}
		if ( colourPipelineFormat === sceneFormat ) return;
		colourPipeline = device.createRenderPipeline( {
			label: "water-refraction-colour",
			layout: "auto",
			vertex: { module: colourModule!, entryPoint: "vs" },
			fragment: { module: colourModule!, entryPoint: "fs", targets: [ { format: sceneFormat } ] },
			primitive: { topology: "triangle-list" }
		} );
		colourPipelineFormat = sceneFormat;
		colourBinding = null;
		boundColour = null;
	}
	/*
	================
	clear

	Retires the copies; the compiled pipelines stay, so turning the row
	back on does not recompile them.
	================
	*/
	function clear() {
		if ( colour ) retire( colour );
		if ( depth ) retire( depth );
		colour = null;
		depth = null;
		stage = null;
		boundColour = boundDepth = null;
		colourBinding = depthBinding = null;
		width = height = 0;
		format = null;
	}
	/*
	================
	encode

	Copies the frame's colour and linearised depth into the stage's
	targets. The frame's views change with every acquire, so each
	binding rebuilds on its source's change and is then kept.
	================
	*/
	function encode( encoder: GPUCommandEncoder, frameColour: GPUTextureView, frameDepth: GPUTextureView ) {
		if ( disposed || !stage || !colourPipeline || !depthPipeline ) throw Error( "Stale water refraction targets" );
		if ( frameColour !== boundColour ) {
			boundColour = frameColour;
			colourBinding = device.createBindGroup( {
				layout: colourPipeline.getBindGroupLayout( 0 ),
				entries: [ { binding: 0, resource: frameColour } ]
			} );
		}
		if ( frameDepth !== boundDepth ) {
			boundDepth = frameDepth;
			depthBinding = device.createBindGroup( {
				layout: depthPipeline.getBindGroupLayout( 0 ),
				entries: [
					{ binding: 0, resource: frameDepth },
					{ binding: 1, resource: { buffer: params! } }
				]
			} );
		}
		const colourPass = encoder.beginRenderPass( {
			label: "water-refraction-colour",
			colorAttachments: [ { view: stage.colourView, loadOp: "clear", storeOp: "store" } ]
		} );
		colourPass.setPipeline( colourPipeline );
		colourPass.setBindGroup( 0, colourBinding! );
		colourPass.draw( 3 );
		colourPass.end();
		const depthPass = encoder.beginRenderPass( {
			label: "water-refraction-depth",
			colorAttachments: [ { view: stage.depthView, loadOp: "clear", storeOp: "store" } ]
		} );
		depthPass.setPipeline( depthPipeline );
		depthPass.setBindGroup( 0, depthBinding! );
		depthPass.draw( 3 );
		depthPass.end();
	}
	return {
		/*
		================
		prepare

		The copy targets for a w x h frame in the scene's format, (re)made
		when the size or format changed, with the camera's near/far packed
		for the depth linearisation; undefined (owning nothing) while the
		row is off.
		================
		*/
		prepare(
			w: number,
			h: number,
			sceneFormat: GPUTextureFormat,
			enabled: boolean,
			camera: WorldCamera
		): WaterRefractionStage | undefined {
			if ( disposed ) throw Error( "Water refraction owner disposed" );
			if ( !enabled ) {
				clear();
				return;
			}
			ensure( sceneFormat );
			if ( near !== camera.near || far !== camera.far ) {
				near = camera.near;
				far = camera.far;
				paramValues[0] = near;
				paramValues[1] = far;
				device.queue.writeBuffer( params!, 0, paramValues );
			}
			if ( stage && width === w && height === h && format === sceneFormat ) return stage;
			clear();
			width = w;
			height = h;
			format = sceneFormat;
			const usage = GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING;
			colour = device.createTexture( { label: "water-refraction-colour", size: [ w, h ], format, usage } );
			depth = device.createTexture( {
				label: "water-refraction-depth",
				size: [ w, h ],
				format: "r32float",
				usage
			} );
			stage = { colourView: colour.createView(), depthView: depth.createView(), encode };
			return stage;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			clear();
			if ( params ) retire( params );
			params = null;
			disposed = true;
		}
	};
}
