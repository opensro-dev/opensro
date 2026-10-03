/*
===========================================================================

character-shadows.ts - per-character projected shadows on the GPU

Renders each shadow caster's silhouette from the fixed light (8A3AE0),
filters it, and draws it on the terrain receiver. Slots persist across
frames; a receiver that has not changed is neither packed nor uploaded.

A slot's caster parts are borrowed actor draws. They are valid only for the
frame that prepared them: encode renders a slot once, after its prepare, and
geometry calls forget when a borrowed draw's buffers are destroyed (its
release, or its instance storage growing) so no submit reads a dead buffer.

===========================================================================
*/

import type { CharacterShadowRequest, GeometryDraw, ImageDraw } from "../internal/gpu-contract";
import { packGeometryVertices } from "@/engine/foundation/rendering/geometry-vertices";
export interface ShadowBinding {
	readonly instances: GPUBuffer;
	readonly material: GPUBuffer;
	readonly skin: GPUBuffer;
	readonly bones: GPUBuffer;
}
/*
================
createCharacterShadows
================
*/
// Device-local resources: borrowed actor buffers are used only in this frame,
// after GPU pose evaluation. A shadow never owns or mutates an actor palette.
export function createCharacterShadows(
	device: GPUDevice,
	view: GPUBuffer,
	texture: ( image: ImageDraw ) => GPUTexture,
	binding: ( draw: GeometryDraw ) => ShadowBinding | undefined,
	format: GPUTextureFormat
) {
	const caster = device.createShaderModule( {
		label: "character-shadow-caster",
		code: `
struct Instance {world:mat4x4f,opacity:vec4f,color:vec4f,window:vec4f,lights:array<vec4f,3>};
struct Skin {joints:vec4u,weights:vec4f};
@group(0) @binding(0) var<uniform> projection:mat4x4f;
@group(0) @binding(1) var<storage,read> instances:array<Instance>;
@group(0) @binding(2) var<uniform> material:array<vec4f,14>;
@group(0) @binding(3) var<storage,read> skin:array<Skin>;
@group(0) @binding(4) var<storage,read> bones:array<mat4x4f>;
@vertex fn vs(@location(0) position:vec3f,@builtin(vertex_index) v:u32,@builtin(instance_index) i:u32)->@builtin(position) vec4f {
 var p=vec4f(position,1);let n=material[2].x;
 if(n!=0){let s=skin[v];let base=select(select(i*u32(abs(n)),0u,n<0),u32(instances[i].opacity.y),instances[i].opacity.z>0);
 p=(bones[base+s.joints.x]*p)*s.weights.x+(bones[base+s.joints.y]*p)*s.weights.y+(bones[base+s.joints.z]*p)*s.weights.z+(bones[base+s.joints.w]*p)*s.weights.w;}
 return projection*instances[i].world*p;
}
@fragment fn fs()->@location(0) vec4f{return vec4f(0,0,0,1);}`
	} );
	const receiver = device.createShaderModule( {
		label: "character-shadow-receiver",
		code: `
@group(0) @binding(0) var<uniform> view:mat4x4f;
@group(0) @binding(1) var image:texture_2d_array<f32>;
@group(0) @binding(2) var filtering:sampler;
struct Out {@builtin(position) point:vec4f,@location(0) uv:vec2f,@location(1) alpha:f32};
@vertex fn vs(@location(0) p:vec3f,@location(1) uv:vec2f,@location(2) color:vec4f)->Out{var o:Out;o.point=view*vec4f(p,1);o.uv=uv;o.alpha=color.a;return o;}
@fragment fn fs(o:Out)->@location(0) vec4f{let sample=textureSample(image,filtering,o.uv,0);if(any(o.uv<vec2f(0))||any(o.uv>vec2f(1))){discard;}return vec4f(0,0,0,sample.a*o.alpha);}`
	} );
	const resample = device.createShaderModule( {
		label: "character-shadow-filter",
		code: `
@group(0) @binding(0) var image:texture_2d<f32>;
struct Out {@builtin(position) p:vec4f,@location(0) uv:vec2f};
@vertex fn vs(@builtin(vertex_index) i:u32)->Out{var a=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3));var o:Out;o.p=vec4f(a[i],0,1);o.uv=vec2f(a[i].x*.5+.5,.5-a[i].y*.5);return o;}
// Separable triangle reconstruction across the 96 -> 29 pixel footprint.
@fragment fn fs(o:Out)->@location(0) vec4f{let center=o.uv*96.0-vec2f(.5);let first=vec2i(floor(center));var sum=0.0;var weight=0.0;
 for(var y=-3;y<=4;y++){for(var x=-3;x<=4;x++){let p=first+vec2i(x,y);let w=max(vec2f(0),vec2f(1)-abs(vec2f(p)-center)/(96.0/29.0));let k=w.x*w.y;sum+=textureLoad(image,clamp(p,vec2i(0),vec2i(95)),0).a*k;weight+=k;}}
 return vec4f(0,0,0,sum/weight);}`
	} );
	const silhouette = device.createRenderPipeline( {
		label: "character-shadow-silhouette",
		layout: "auto",
		vertex: {
			module: caster,
			entryPoint: "vs",
			buffers: [ { arrayStride: 56, attributes: [ { shaderLocation: 0, offset: 0, format: "float32x3" } ] } ]
		},
		fragment: { module: caster, entryPoint: "fs", targets: [ { format: "rgba8unorm" } ] },
		primitive: { topology: "triangle-list", cullMode: "none" }
	} );
	const filter = device.createRenderPipeline( {
		label: "character-shadow-resample",
		layout: "auto",
		vertex: { module: resample, entryPoint: "vs" },
		fragment: { module: resample, entryPoint: "fs", targets: [ { format: "rgba8unorm" } ] },
		primitive: { topology: "triangle-list" }
	} );
	const ground = device.createRenderPipeline( {
		label: "character-shadow-ground",
		layout: "auto",
		vertex: {
			module: receiver,
			entryPoint: "vs",
			buffers: [ {
				arrayStride: 56,
				attributes: [ { shaderLocation: 0, offset: 0, format: "float32x3" }, {
					shaderLocation: 1,
					offset: 24,
					format: "float32x2"
				}, { shaderLocation: 2, offset: 32, format: "float32x4" } ]
			} ]
		},
		fragment: {
			module: receiver,
			entryPoint: "fs",
			targets: [ {
				format,
				blend: {
					color: { srcFactor: "src-alpha", dstFactor: "one-minus-src-alpha" },
					alpha: { srcFactor: "one", dstFactor: "one-minus-src-alpha" }
				}
			} ]
		},
		primitive: { topology: "triangle-list", cullMode: "none" },
		depthStencil: {
			format: "depth24plus",
			depthWriteEnabled: false,
			depthCompare: "less-equal",
			depthBias: -1,
			depthBiasSlopeScale: -1
		}
	} );
	const sampler = device.createSampler( {
		minFilter: "linear",
		magFilter: "linear",
		addressModeU: "clamp-to-edge",
		addressModeV: "clamp-to-edge"
	} );
	type Slot = {
		projection: GPUBuffer;
		source: GPUTexture;
		filtered: GPUTexture;
		filterBinding: GPUBindGroup;
		vertices: GPUBuffer;
		indices: GPUBuffer;
		draw: GeometryDraw;
		parts: { draw: GeometryDraw; instance: number; binding: GPUBindGroup; }[];
		// Prepared this frame and not yet rendered: a silhouette to generate.
		fresh: boolean;
		uploaded?: object;
	};
	// Packed receiver vertices per receiver: a still character keeps its
	// receiver object, so neither the packing nor the upload repeats.
	const packed = new WeakMap<object, Float32Array>();
	const slots: Slot[] = [];
	/*
	================
	retire
	================
	*/
	function retire( s: Slot ) {
		s.projection.destroy();
		s.source.destroy();
		s.filtered.destroy();
		s.vertices.destroy();
		s.indices.destroy();
	}
	return {
		/*
		================
		prepare

		This frame's receiver draws. Every slot first drops last frame's
		borrowed parts, so a request skipped below (its blob image not resident
		yet) cannot render a caster from an earlier frame.
		================
		*/
		prepare( requests: readonly CharacterShadowRequest[], blob?: ImageDraw ): readonly GeometryDraw[] {
			if ( requests.length > 10 ) throw Error( "Character shadow limit exceeded" );
			while ( slots.length > requests.length ) retire( slots.pop()! );
			for ( const s of slots ) {
				if ( !s ) continue;
				s.parts = [];
				s.fresh = false;
			}
			const draws: GeometryDraw[] = [];
			for ( let i = 0; i < requests.length; i++ ) {
				const r = requests[i]!;
				if ( r.blob && !blob ) continue;
				let vertices = packed.get( r.receiver );
				if ( !vertices ) {
					vertices = packGeometryVertices( r.receiver );
					packed.set( r.receiver, vertices );
				}
				const indices = r.receiver.indices;
				let s = slots[i];
				if ( s && (s.vertices.size < vertices.byteLength || s.indices.size < indices.byteLength) ) {
					retire( s );
					s = undefined;
				}
				if ( !s ) {
					const source = device.createTexture( {
						label: "character-shadow-96",
						size: [ 96, 96 ],
						format: "rgba8unorm",
						usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
					} );
					const filtered = device.createTexture( {
						label: "character-shadow-32",
						size: [ 32, 32 ],
						format: "rgba8unorm",
						usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
					} );
					s = {
						source,
						filtered,
						projection: device.createBuffer( {
							size: 64,
							usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
						} ),
						filterBinding: device.createBindGroup( {
							layout: filter.getBindGroupLayout( 0 ),
							entries: [ { binding: 0, resource: source.createView() } ]
						} ),
						vertices: device.createBuffer( {
							size: Math.max( 56, vertices.byteLength ),
							usage: GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST
						} ),
						indices: device.createBuffer( {
							size: Math.max( 4, indices.byteLength ),
							usage: GPUBufferUsage.INDEX | GPUBufferUsage.COPY_DST
						} ),
						draw: null!,
						parts: [],
						fresh: false
					};
					slots[i] = s;
				}
				s.fresh = !r.blob;
				device.queue.writeBuffer( s.projection, 0, r.matrix as Float32Array<ArrayBuffer> );
				if ( s.uploaded !== r.receiver ) {
					device.queue.writeBuffer( s.vertices, 0, vertices as Float32Array<ArrayBuffer> );
					device.queue.writeBuffer( s.indices, 0, indices as Uint32Array<ArrayBuffer> );
					s.uploaded = r.receiver;
				}
				s.parts = [];
				if ( !r.blob ) {
					for ( const part of r.parts ) {
						const b = binding( part.draw );
						if ( !b || part.instance >= part.draw.instanceCount ) continue;
						s.parts.push( {
							...part,
							binding: device.createBindGroup( {
								layout: silhouette.getBindGroupLayout( 0 ),
								entries: [
									{ binding: 0, resource: { buffer: s.projection } },
									{ binding: 1, resource: { buffer: b.instances } },
									{ binding: 2, resource: { buffer: b.material } },
									{ binding: 3, resource: { buffer: b.skin } },
									{ binding: 4, resource: { buffer: b.bones } }
								]
							} )
						} );
					}
				}
				s.draw = {
					pipeline: ground,
					binding: device.createBindGroup( {
						layout: ground.getBindGroupLayout( 0 ),
						entries: [ { binding: 0, resource: { buffer: view } }, {
							binding: 1,
							resource: (r.blob ? texture( blob! ) : s.filtered).createView( { dimension: "2d-array" } )
						}, { binding: 2, resource: sampler } ]
					} ),
					vertices: s.vertices,
					indices: s.indices,
					count: indices.length,
					indexCount: indices.length,
					instanceCount: 1,
					instanceCapacity: 1
				};
				draws.push( s.draw );
			}
			return draws;
		},
		/*
		================
		encode

		Generates and filters the silhouette of each slot prepared since the
		last encode. A frame that encodes twice (the deferred particle tail)
		renders each slot once.
		================
		*/
		encode( encoder: GPUCommandEncoder ) {
			for ( const s of slots ) {
				if ( !s?.fresh ) continue;
				s.fresh = false;
				const p = encoder.beginRenderPass( {
					label: "character-shadow-generate",
					colorAttachments: [ {
						view: s.source.createView(),
						loadOp: "clear",
						storeOp: "store",
						clearValue: [ 0, 0, 0, 0 ]
					} ]
				} );
				p.setViewport( 1, 1, 94, 94, 0, 1 );
				p.setPipeline( silhouette );
				for ( const part of s.parts ) {
					p.setBindGroup( 0, part.binding );
					p.setVertexBuffer( 0, part.draw.vertices );
					p.setIndexBuffer( part.draw.indices, "uint32" );
					p.drawIndexed( part.draw.indexCount, 1, 0, 0, part.instance );
				}
				p.end();
				const f = encoder.beginRenderPass( {
					label: "character-shadow-filter",
					colorAttachments: [ {
						view: s.filtered.createView(),
						loadOp: "clear",
						storeOp: "store",
						clearValue: [ 0, 0, 0, 0 ]
					} ]
				} );
				f.setViewport( 1, 1, 29, 29, 0, 1 );
				f.setPipeline( filter );
				f.setBindGroup( 0, s.filterBinding );
				f.draw( 3 );
				f.end();
			}
		},
		/*
		================
		forget

		draw's buffers are being destroyed: no slot may still render it.
		================
		*/
		forget( draw: GeometryDraw ) {
			for ( const s of slots ) {
				if ( s?.parts.some( part => part.draw === draw ) ) {
					s.parts = s.parts.filter( part => part.draw !== draw );
				}
			}
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			for ( const s of slots ) if ( s ) retire( s );
			slots.length = 0;
		}
	};
}
