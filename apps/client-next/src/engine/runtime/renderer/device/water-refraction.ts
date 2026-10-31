/*
===========================================================================

water-refraction.ts - the experimental refracted water pass

Port-only, not native. The retail water surface blends its planar
reflection over the frame (pipelines.ts's skin.z path, 8BA130's projected
mirror sample); what sits under the surface reaches the eye untouched.
The Experimental "Water refraction" row draws the water apart instead:
the main pass renders the scene without the water surfaces, this stage
copies the frame's colour and converts its depth into readable forms,
and one overlay pass redraws the water - the same draws, the same
pipelines - whose fragment now refracts the copied colour by the water's
own bump offset and whitens where the water meets the shore (the depth
behind the fragment is within a small band of the fragment's own depth).

The copies: the colour through a fullscreen sample pass (the retained
frame carries TEXTURE_BINDING; the canvas texture does not), the depth
through a fullscreen load into r32float (a depth attachment cannot be
resolved by the driver). Off, the water draws stay in the main pass and
the frame is byte-identical to the native order.

===========================================================================
*/
import type { GeometryDraw } from "../internal/gpu-contract";
import { destroyNow, type Retire } from "./retirement";

/*
================
createWaterRefraction
================
*/
export function createWaterRefraction( device: GPUDevice, retire: Retire = destroyNow ) {
	let colour: GPUTexture | null = null,
		depth: GPUTexture | null = null,
		colourView: GPUTextureView | null = null,
		depthView: GPUTextureView | null = null,
		blit: GPURenderPipeline | null = null,
		convert: GPURenderPipeline | null = null,
		sampler: GPUSampler | null = null,
		boundColour: GPUTextureView | null = null,
		colourBinding: GPUBindGroup | null = null,
		boundDepth: GPUTextureView | null = null,
		depthBinding: GPUBindGroup | null = null,
		width = 0,
		height = 0,
		format: GPUTextureFormat | null = null,
		disposed = false;
	function clear() {
		if ( colour ) retire( colour );
		if ( depth ) retire( depth );
		colour = null;
		depth = null;
		colourView = null;
		depthView = null;
		blit = convert = null;
		boundColour = boundDepth = null;
		colourBinding = depthBinding = null;
		width = height = 0;
		format = null;
	}
	function ensurePipelines() {
		if ( blit && convert && sampler ) return;
		sampler = device.createSampler( {
			minFilter: "linear",
			magFilter: "linear",
			addressModeU: "clamp-to-edge",
			addressModeV: "clamp-to-edge"
		} );
		const copy = device.createShaderModule( {
			label: "water-refraction-copy",
			code: `
struct V {@builtin(position) position:vec4f,@location(0) uv:vec2f}
@vertex fn vs(@builtin(vertex_index) i:u32)->V{
 let p=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3))[i];
 return V(vec4f(p,0,1),vec2f(p.x*.5+.5,.5-p.y*.5));
}
@group(0) @binding(0) var source:texture_2d<f32>;
@group(0) @binding(1) var linear:sampler;
@fragment fn colourPass(v:V)->@location(0) vec4f{return textureSampleLevel(source,linear,v.uv,0);}
@group(0) @binding(2) var sourceDepth:texture_depth_2d;
@fragment fn depthPass(v:V)->@location(0) f32{
 let size=vec2f(textureDimensions(sourceDepth));
 let at=clamp(vec2i(v.uv*size),vec2i(0),vec2i(size)-vec2i(1));
 return textureLoad(sourceDepth,at,0);
}`
		} );
		blit = device.createRenderPipeline( {
			label: "water-refraction-colour-copy",
			layout: "auto",
			vertex: { module: copy, entryPoint: "vs" },
			fragment: { module: copy, entryPoint: "colourPass", targets: [ { format: format! } ] },
			primitive: { topology: "triangle-list" }
		} );
		convert = device.createRenderPipeline( {
			label: "water-refraction-depth-convert",
			layout: "auto",
			vertex: { module: copy, entryPoint: "vs" },
			fragment: { module: copy, entryPoint: "depthPass", targets: [ { format: "r32float" } ] },
			primitive: { topology: "triangle-list" }
		} );
	}
	function drawOverlay(
		encoder: GPUCommandEncoder,
		target: GPUTextureView,
		targetDepth: GPUTextureView,
		draws: readonly GeometryDraw[]
	) {
		const water = encoder.beginRenderPass( {
			label: "water-refraction-overlay",
			colorAttachments: [ { view: target, loadOp: "load", storeOp: "store" } ],
			depthStencilAttachment: {
				view: targetDepth,
				depthLoadOp: "load",
				depthStoreOp: "store"
			}
		} );
		for ( const draw of draws ) {
			if ( !draw.indexCount || !draw.instanceCount ) continue;
			water.setPipeline( draw.pipeline );
			water.setBindGroup( 0, draw.binding );
			water.setVertexBuffer( 0, draw.vertices );
			water.setIndexBuffer( draw.indices, "uint32" );
			water.drawIndexed(
				draw.indexCount,
				draw.instanceCount,
				draw.firstIndex ?? 0,
				draw.baseVertex ?? 0,
				0
			);
		}
		water.end();
	}
	return {
		/*
		================
		prepare

		The copy targets for a w x h frame in the scene view's format,
		(re)created when the size or format changed; undefined (owning
		nothing) while the row is off.
		================
		*/
		prepare( w: number, h: number, sceneFormat: GPUTextureFormat, enabled: boolean ) {
			if ( disposed ) throw Error( "Water refraction owner disposed" );
			if ( !enabled ) {
				clear();
				return;
			}
			if ( width !== w || height !== h || format !== sceneFormat ) {
				clear();
				width = w;
				height = h;
				format = sceneFormat;
				colour = device.createTexture( {
					label: "water-refraction-colour",
					size: [ w, h ],
					format,
					usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
				} );
				depth = device.createTexture( {
					label: "water-refraction-depth",
					size: [ w, h ],
					format: "r32float",
					usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
				} );
				// Cached once per texture: the geometry owner's identity check
				// then rebinds only on resize or format change, not per frame.
				colourView = colour.createView();
				depthView = depth.createView();
			}
			ensurePipelines();
			return {
				colourView: colourView!,
				depthView: depthView!,
				/*
				================
				encode

				Copy the frame's colour and depth, then redraw the water over
				the frame. Same draws, same pipelines: the fragment's stages2
				branch (pipelines.ts) does the refracting. The frame's views
				change with every acquire, so each binding rebuilds on its
				source's first change and is then kept.
				================
				*/
				overlay(
					encoder: GPUCommandEncoder,
					target: GPUTextureView,
					targetDepth: GPUTextureView,
					draws: readonly GeometryDraw[]
				) {
					if ( disposed || !colour || !depth ) throw Error( "Stale water refraction targets" );
					drawOverlay( encoder, target, targetDepth, draws );
				},
				encode(
					encoder: GPUCommandEncoder,
					frameColour: GPUTextureView,
					frameDepth: GPUTextureView,
					target: GPUTextureView,
					targetDepth: GPUTextureView,
					draws: readonly GeometryDraw[]
				) {
					if ( disposed || !colour || !depth || !blit || !convert || !sampler ) {
						throw Error( "Stale water refraction targets" );
					}
					if ( frameColour !== boundColour ) {
						boundColour = frameColour;
						colourBinding = device.createBindGroup( {
							layout: blit.getBindGroupLayout( 0 ),
							entries: [
								{ binding: 0, resource: frameColour },
								{ binding: 1, resource: sampler }
							]
						} );
					}
					if ( frameDepth !== boundDepth ) {
						boundDepth = frameDepth;
						depthBinding = device.createBindGroup( {
							layout: convert.getBindGroupLayout( 0 ),
							entries: [ { binding: 2, resource: frameDepth } ]
						} );
					}
					const copyPass = encoder.beginRenderPass( {
						label: "water-refraction-copies",
						colorAttachments: [ { view: colourView!, loadOp: "clear", storeOp: "store" } ]
					} );
					copyPass.setPipeline( blit );
					copyPass.setBindGroup( 0, colourBinding! );
					copyPass.draw( 3 );
					copyPass.end();
					const depthPass = encoder.beginRenderPass( {
						label: "water-refraction-depth",
						colorAttachments: [ { view: depthView!, loadOp: "clear", storeOp: "store" } ]
					} );
					depthPass.setPipeline( convert );
					depthPass.setBindGroup( 0, depthBinding! );
					depthPass.draw( 3 );
					depthPass.end();
					drawOverlay( encoder, target, targetDepth, draws );
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
			disposed = true;
		}
	};
}
