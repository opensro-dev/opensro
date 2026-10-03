/*
===========================================================================

bloom.ts - the native glow pass

Owns the bloom targets, shader and pipelines, and encodes the native
five-pass chain (SWorld_RenderBloom 8A99B0) onto the frame's encoder.

===========================================================================
*/
import type { BloomDraw } from "../internal/gpu-contract";
import { BLOOM_BLEND_BYTE } from "@/engine/foundation/rendering/blend-state";

/*
================
createBloom

8A99B0: capture -> 512 downsample -> horizontal subtract/accumulate ->
vertical accumulate -> source BLENDFACTOR + destination SRCALPHA.
Constructor 8BB530 installs radius 13, alpha 198, blend 192, input scale
128, threshold 40 and kernel alpha bytes 80,70,50.
================
*/
export function createBloom( device: GPUDevice, format: GPUTextureFormat ) {
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
	let pipelines: GPURenderPipeline[] = [];
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
						...(entryPoint === "composite" ?
							{
								blend: {
									color: {
										operation: "add" as const,
										srcFactor: "constant" as const,
										dstFactor: "src-alpha" as const
									},
									alpha: {
										operation: "add" as const,
										srcFactor: "zero" as const,
										dstFactor: "one" as const
									}
								}
							} :
							{})
					} ]
				},
				primitive: { topology: "triangle-list" }
			} )
		);
	} );
	let targets: GPUTexture[] = [],
		views: GPUTextureView[] = [],
		bindings: GPUBindGroup[] = [],
		width = 0,
		height = 0,
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
		for ( const t of targets ) t.destroy();
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
		size changed; undefined (and no targets) while bloom is off.
		================
		*/
		prepare( w: number, h: number, enabled: boolean ): BloomDraw | undefined {
			if ( disposed ) throw Error( "Bloom owner disposed" );
			if ( !enabled ) {
				clear();
				return;
			}
			if ( width !== w || height !== h ) {
				clear();
				width = w;
				height = h;
				for ( const size of [ [ w, h ], [ 512, 512 ], [ 512, 512 ] ] ) {
					const t = device.createTexture( {
						label: "native-bloom-target",
						size,
						format,
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
					const steps = [ [ 1, 0, 0 ], [ 2, 1, 1 ], [ 1, 2, 2 ], [ -1, 0, 3 ], [ -1, 1, 4 ] ];
					for ( const [out, input, pipeline] of steps ) {
						const pass = encoder.beginRenderPass( {
							label: "native-bloom-" + pipeline,
							colorAttachments: [ {
								view: out === -1 ? target : views[out!]!,
								loadOp: pipeline === 4 ? "load" : "clear",
								storeOp: "store"
							} ]
						} );
						pass.setPipeline( pipelines[pipeline!]! );
						pass.setBindGroup( 0, bindings[input!]! );
						if ( pipeline === 4 ) pass.setBlendConstant( blendFactor );
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
