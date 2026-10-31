/*
===========================================================================

msaa.ts - the experimental 4x multisampled scene buffers

Port-only, not native. Experimental > Lighting > MSAA 4x renders the scene
through a 4x multisampled colour buffer and its 4x depth, resolving into
whichever target the frame's stage chain chose (the plain frame, the HDR
float intermediate, the bloom input or the render-scale frame): the main
pass names the 4x colour as its attachment and the resolved target as its
resolveTarget, so the resolve is the driver's own. Everything downstream -
the tone map, the bloom chain, the upscale, the UI passes - reads the
resolved target unchanged. The original client presents single-sampled.

The multisampled depth cannot be resolved by the driver, and two readers
need a single-sampled scene depth: the world-labels pass (full-resolution
labels stay occluded, the render-scale depth resolve's rule) and the flare
visibility probe. resolveDepth() writes the minimum of the four samples
into the frame's single-sampled depth - the nearest surface wins, so a
label behind any coverage stays behind it.

===========================================================================
*/
import { destroyNow, type Retire } from "./retirement";

/*
================
createMsaa

Owns the 4x scene buffers. prepare() with the stage off owns nothing; the
frame then renders single-sampled, byte-identical to the native path.
================
*/
export function createMsaa( device: GPUDevice, retire: Retire = destroyNow ) {
	let pipeline: GPURenderPipeline | null = null;
	let colour: GPUTexture | null = null,
		depth: GPUTexture | null = null,
		colourView: GPUTextureView | null = null,
		depthView: GPUTextureView | null = null,
		bound: GPUTextureView | null = null,
		binding: GPUBindGroup | null = null,
		width = 0,
		height = 0,
		format: GPUTextureFormat | null = null,
		disposed = false;
	/*
	================
	clear
	================
	*/
	function clear() {
		if ( colour ) retire( colour );
		if ( depth ) retire( depth );
		colour = null;
		depth = null;
		colourView = null;
		depthView = null;
		bound = null;
		binding = null;
		width = height = 0;
		format = null;
	}
	/*
	================
	ensurePipeline

	The depth downsample: one fullscreen triangle, the minimum of the four
	samples per texel written as fragment depth (finish.ts's depth upscale
	owns the matching single-sampled shape).
	================
	*/
	function ensurePipeline() {
		if ( pipeline ) return;
		const module = device.createShaderModule( {
			label: "msaa-depth",
			code: `
@group(0) @binding(0) var source:texture_depth_2d;
struct V {@builtin(position) position:vec4f,@location(0) uv:vec2f}
@vertex fn vs(@builtin(vertex_index) i:u32)->V{
 let p=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3))[i];
 return V(vec4f(p,0,1),vec2f(p.x*.5+.5,.5-p.y*.5));
}
@fragment fn fs(v:V)->@builtin(frag_depth) f32{
 let size=textureDimensions(source);
 let at=clamp(vec2i(v.uv*vec2f(size)),vec2i(0),vec2i(size)-1);
 return min(min(textureLoad(source,at,0),textureLoad(source,at,1)),
            min(textureLoad(source,at,2),textureLoad(source,at,3)));
}`
		} );
		pipeline = device.createRenderPipeline( {
			label: "msaa-depth",
			layout: "auto",
			vertex: { module, entryPoint: "vs" },
			fragment: { module, entryPoint: "fs", targets: [] },
			depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "always" }
		} );
	}
	return {
		/*
		================
		prepare

		The 4x buffers for a w x h frame in the scene target's format,
		(re)created when the size or format changed; undefined (and no
		target) while the stage is off.
		================
		*/
		prepare( w: number, h: number, sceneFormat: GPUTextureFormat, enabled: boolean ) {
			if ( disposed ) throw Error( "MSAA owner disposed" );
			if ( !enabled ) {
				clear();
				return;
			}
			// Compilation and validation failures use the device's existing
			// terminal error path; a device that never enables this stage
			// never compiles the depth resolve.
			ensurePipeline();
			if ( width !== w || height !== h || format !== sceneFormat ) {
				clear();
				width = w;
				height = h;
				format = sceneFormat;
				colour = device.createTexture( {
					label: "msaa-scene-colour",
					size: [ w, h ],
					sampleCount: 4,
					format,
					usage: GPUTextureUsage.RENDER_ATTACHMENT
				} );
				depth = device.createTexture( {
					label: "msaa-scene-depth",
					size: [ w, h ],
					sampleCount: 4,
					format: "depth24plus",
					usage: GPUTextureUsage.RENDER_ATTACHMENT
				} );
				colourView = colour.createView();
				depthView = depth.createView();
			}
			const epoch = depth;
			return {
				view: colourView!,
				depth: depthView!,
				/*
				================
				resolveDepth

				Port-only, not native: the four samples' minimum into the
				frame's single-sampled depth, before the labels and the flare
				probe read it.
				================
				*/
				resolveDepth( encoder: GPUCommandEncoder, target: GPUTextureView ) {
					if ( disposed || epoch !== depth || !pipeline ) throw Error( "Stale MSAA buffers" );
					if ( target !== bound ) {
						bound = target;
						binding = device.createBindGroup( {
							layout: pipeline.getBindGroupLayout( 0 ),
							entries: [ { binding: 0, resource: depthView! } ]
						} );
					}
					const pass = encoder.beginRenderPass( {
						label: "msaa-depth",
						colorAttachments: [],
						depthStencilAttachment: {
							view: target,
							depthClearValue: 1,
							depthLoadOp: "clear",
							depthStoreOp: "store"
						}
					} );
					pass.setPipeline( pipeline );
					pass.setBindGroup( 0, binding! );
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
			disposed = true;
		}
	};
}
