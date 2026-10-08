/*
===========================================================================

bloom.ts - the native glow pass and its float-quality sibling

Owns the bloom targets, shaders and pipelines, and encodes either chain
onto the frame's encoder: the native five-pass replication
(SWorld_RenderBloom 8A99B0), or - Experimental > Image > Smooth
bloom - the same capture, kernel and composite constants carried by an
rgba16float two-level chain without the 2005 per-tap quantization.

===========================================================================
*/
import type { BloomDraw } from "../internal/gpu-contract";
import { BLOOM_BLEND_BYTE } from "@/engine/foundation/rendering/blend-state";
import { destroyNow, type Retire } from "./retirement";

/*
================
createBloom

8A99B0: capture -> 512 downsample -> horizontal subtract/accumulate ->
vertical accumulate -> source BLENDFACTOR + destination SRCALPHA.
Constructor 8BB530 installs radius 13, alpha 198, blend 192, input scale
128, threshold 40 and kernel alpha bytes 80,70,50.

Port-only, not native: the float chain keeps every constant but drops the quantization and adds a
second 256 blur level, so large glows keep their gradient instead of
banding at eight bits per tap.
================
*/
export function createBloom( device: GPUDevice, format: GPUTextureFormat, retire: Retire = destroyNow ) {
	const module = device.createShaderModule( {
		label: "native-bloom",
		code: `
 @group(0) @binding(0) var source:texture_2d<f32>;
 @group(0) @binding(1) var linear:sampler;
 struct V {@builtin(position) position:vec4f,@location(0) uv:vec2f};
 @vertex fn vs(@builtin(vertex_index) i:u32)->V{
  let p=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3))[i];
  return V(vec4f(p,0,1),vec2f(p.x*.5+.5,.5-p.y*.5));
 }
 fn quantizeColor(v:vec3f)->vec3f{return round(clamp(v,vec3f(0),vec3f(1))*255)/255;}
 fn tap(uv:vec2f,subtract:bool)->vec3f{
  let c=textureSampleLevel(source,linear,uv,0).rgb;
  let inside=all(uv>=vec2f(0))&&all(uv<=vec2f(1));
  return select(vec3f(0),max(vec3f(0),c-select(0.0,40.0/255,subtract)),inside);
 }
 fn blur(uv:vec2f,axis:vec2f,subtract:bool)->vec4f{
  var c=quantizeColor(tap(uv,subtract)*(80.0/255));
  for(var i=1;i<=2;i++){
   let d=axis*(2.6*f32(i)/512);let weight=select(70.0,50.0,i==2)/255;
   c=quantizeColor(c+tap(uv-d,subtract)*weight);c=quantizeColor(c+tap(uv+d,subtract)*weight);
  }
  return vec4f(c,1);
 }
 @fragment fn down(v:V)->@location(0) vec4f{return vec4f(textureSample(source,linear,v.uv).rgb*(128.0/255),1);}
 @fragment fn horizontal(v:V)->@location(0) vec4f{return blur(v.uv,vec2f(1,0),true);}
 @fragment fn vertical(v:V)->@location(0) vec4f{return blur(v.uv,vec2f(0,1),false);}
 @fragment fn original(v:V)->@location(0) vec4f{return vec4f(textureSample(source,linear,v.uv).rgb,1);}
 @fragment fn composite(v:V)->@location(0) vec4f{return vec4f(textureSample(source,linear,v.uv).rgb,198.0/255);}
 `
	} );

	const sampler = device.createSampler( { minFilter: "linear", magFilter: "linear" } );
	// The composite's D3DRS_BLENDFACTOR: this owner's blend byte in RGB,
	// alpha 255 (SWorld_CompositeBloom 8AA560).
	const blendFactor: GPUColor = [ BLOOM_BLEND_BYTE / 255, BLOOM_BLEND_BYTE / 255, BLOOM_BLEND_BYTE / 255, 1 ];
	const layout = device.createBindGroupLayout( {
		entries: [ { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: {} }, {
			binding: 1,
			visibility: GPUShaderStage.FRAGMENT,
			sampler: {}
		} ]
	} );
	const pipelineLayout = device.createPipelineLayout( { bindGroupLayouts: [ layout ] } );
	const compositeBlend = {
		color: { operation: "add" as const, srcFactor: "constant" as const, dstFactor: "src-alpha" as const },
		alpha: { operation: "add" as const, srcFactor: "zero" as const, dstFactor: "one" as const }
	};
	let pipelines: GPURenderPipeline[] = [];
	// The float chain's blur levels live in rgba16float; the capture target
	// keeps the canvas format because the main pass renders into it.
	let floatPipelines: GPURenderPipeline[] = [];
	const ready = module.getCompilationInfo().then( info => {
		const errors = info.messages.filter( m => m.type === "error" );
		if ( errors.length ) throw Error( errors.map( m => m.message ).join( "\n" ) );
		pipelines = [ "down", "horizontal", "vertical", "original", "composite" ].map( entryPoint =>
			device.createRenderPipeline( {
				label: "bloom-" + entryPoint,
				layout: pipelineLayout,
				vertex: { module, entryPoint: "vs" },
				fragment: {
					module,
					entryPoint,
					targets: [ {
						format,
						...(entryPoint === "composite" ? { blend: compositeBlend } : {})
					} ]
				},
				primitive: { topology: "triangle-list" }
			} )
		);
	} );
	/*
	================
	floatEnsure

	Port-only, not native. Compile only on first enabled use. Compilation
	and validation failures use the device's existing terminal error path;
	a device that never enables this stage never compiles its shader.
	================
	*/
	const floatEnsure = () => {
		if ( floatPipelines.length ) return;
		const floatModule = device.createShaderModule( {
			label: "float-bloom",
			code: `
	 @group(0) @binding(0) var source:texture_2d<f32>;
	 @group(0) @binding(1) var linear:sampler;
	 struct V {@builtin(position) position:vec4f,@location(0) uv:vec2f};
	 @vertex fn vs(@builtin(vertex_index) i:u32)->V{
	  let p=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3))[i];
	  return V(vec4f(p,0,1),vec2f(p.x*.5+.5,.5-p.y*.5));
	 }
	 fn tap(uv:vec2f,subtract:bool)->vec3f{
	  let c=textureSampleLevel(source,linear,uv,0).rgb;
	  let inside=all(uv>=vec2f(0))&&all(uv<=vec2f(1));
	  return select(vec3f(0),max(vec3f(0),c-select(0.0,40.0/255,subtract)),inside);
	 }
	 fn blur(uv:vec2f,axis:vec2f,span:f32,subtract:bool)->vec4f{
	  var c=tap(uv,subtract)*(80.0/255);
	  for(var i=1;i<=2;i++){
	   let d=axis*(2.6*f32(i)/span);let weight=select(70.0,50.0,i==2)/255;
	   c=c+tap(uv-d,subtract)*weight;c=c+tap(uv+d,subtract)*weight;
	  }
	  return vec4f(c,1);
	 }
	 @fragment fn down(v:V)->@location(0) vec4f{return vec4f(textureSample(source,linear,v.uv).rgb*(128.0/255),1);}
	 @fragment fn h1(v:V)->@location(0) vec4f{return blur(v.uv,vec2f(1,0),512.0,true);}
	 @fragment fn v1(v:V)->@location(0) vec4f{return blur(v.uv,vec2f(0,1),512.0,false);}
	 @fragment fn down2(v:V)->@location(0) vec4f{return vec4f(textureSample(source,linear,v.uv).rgb,1);}
	 @fragment fn h2(v:V)->@location(0) vec4f{return blur(v.uv,vec2f(1,0),256.0,false);}
	 @fragment fn v2(v:V)->@location(0) vec4f{return blur(v.uv,vec2f(0,1),256.0,false);}
	 @fragment fn original(v:V)->@location(0) vec4f{return vec4f(textureSample(source,linear,v.uv).rgb,1);}
	 @fragment fn composite(v:V)->@location(0) vec4f{return vec4f(textureSample(source,linear,v.uv).rgb,198.0/255);}
	 `
		} );
		floatPipelines = [ "down", "h1", "v1", "down2", "h2", "v2", "original", "composite" ].map( entryPoint =>
			device.createRenderPipeline( {
				label: "float-bloom-" + entryPoint,
				layout: pipelineLayout,
				vertex: { module: floatModule, entryPoint: "vs" },
				fragment: {
					module: floatModule,
					entryPoint,
					targets: [ {
						format: entryPoint === "original" || entryPoint === "composite" ? format : "rgba16float",
						...(entryPoint === "composite" ? { blend: compositeBlend } : {})
					} ]
				},
				primitive: { topology: "triangle-list" }
			} )
		);
	};
	let targets: GPUTexture[] = [],
		views: GPUTextureView[] = [],
		bindings: GPUBindGroup[] = [],
		width = 0,
		height = 0,
		quality = false,
		revision = 0,
		disposed = false;
	/*
	================
	clear

	Releases the targets; a draw prepared before is stale from here.
	================
	*/
	function clear() {
		revision++;
		for ( const t of targets ) retire( t );
		targets = [];
		views = [];
		bindings = [];
		width = height = 0;
	}
	return {
		ready,
		/*
		================
		prepare

		The bloom draw for a w x h frame, (re)creating the targets when the
		size or quality changed; undefined (and no targets) while bloom is
		off. Both capture targets keep the canvas format. The float chain uses
		two rgba16float ping-pong blur levels (512 and 256); the native chain keeps its three
		8-bit targets byte-compatible with 8A99B0.
		================
		*/
		prepare( w: number, h: number, enabled: boolean, float: boolean ): BloomDraw | undefined {
			if ( disposed ) throw Error( "Bloom owner disposed" );
			if ( !enabled ) {
				clear();
				return;
			}
			if ( float ) floatEnsure();
			if ( width !== w || height !== h || quality !== float ) {
				clear();
				width = w;
				height = h;
				quality = float;
				// Target 0 is the frame's capture the main pass renders into, so
				// it keeps the canvas format its pipelines target; only the blur
				// levels go rgba16float. The rest ping-pong the blur.
				const sizes = float ?
					[ [ w, h ], [ 512, 512 ], [ 512, 512 ], [ 256, 256 ], [ 256, 256 ] ] :
					[ [ w, h ], [ 512, 512 ], [ 512, 512 ] ];
				for ( let index = 0; index < sizes.length; index++ ) {
					const t = device.createTexture( {
						label: float ? "float-bloom-target" : "native-bloom-target",
						size: sizes[index]!,
						format: float && index > 0 ? "rgba16float" : format,
						usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
					} );
					targets.push( t );
					const view = t.createView();
					views.push( view );
					bindings.push(
						device.createBindGroup( {
							layout,
							entries: [ { binding: 0, resource: view }, { binding: 1, resource: sampler } ]
						} )
					);
				}
			}
			const epoch = revision;
			return {
				view: views[0]!,
				/*
				================
				encode
				================
				*/
				encode( encoder, target ) {
					if ( disposed || epoch !== revision ) throw Error( "Stale bloom target" );
					if ( quality && !floatPipelines.length ) throw Error( "Float bloom pipelines are not ready" );
					// [out, input, pipeline, blend]
					const steps: readonly (readonly [number, number, number, boolean])[] = quality ?
						[
							[ 1, 0, 0, false ],
							[ 2, 1, 1, false ],
							[ 1, 2, 2, false ],
							[ 3, 1, 3, false ],
							[ 4, 3, 4, false ],
							[ 3, 4, 5, false ],
							[ -1, 0, 6, false ],
							[ -1, 3, 7, true ]
						] :
						[ [ 1, 0, 0, false ], [ 2, 1, 1, false ], [ 1, 2, 2, false ], [ -1, 0, 3, false ], [
							-1,
							1,
							4,
							true
						] ];
					for ( const [out, input, pipeline, blend] of steps ) {
						const pass = encoder.beginRenderPass( {
							label: "bloom-" + pipeline,
							colorAttachments: [ {
								view: out === -1 ? target : views[out!]!,
								loadOp: blend ? "load" : "clear",
								storeOp: "store"
							} ]
						} );
						pass.setPipeline( (quality ? floatPipelines : pipelines)[pipeline!]! );
						pass.setBindGroup( 0, bindings[input!]! );
						if ( blend ) pass.setBlendConstant( blendFactor );
						pass.draw( 3 );
						pass.end();
					}
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
