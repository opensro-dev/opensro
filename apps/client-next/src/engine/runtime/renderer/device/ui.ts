/*
===========================================================================

ui.ts - device-owned UI textures, instance storage and draw bindings

Stable buffers and bundles survive data edits. Texture replacement advances
resourceRevision so no retained bind group can refer to a retired texture.

===========================================================================
*/
import type { UiScene, UiQuad } from "@/engine/contracts/ui";
import type { UiDraw } from "@/engine/runtime/renderer/internal/gpu-contract";
import { uiRecordCount, UI_RECORD_LIMIT } from "@/engine/foundation/rendering/text-run";
import { destroyNow, type Retire } from "./retirement";
import type { UiTexture } from "@/engine/contracts/texture";
import {
	decodeNativeTextureLevel,
	nativeTextureBlockBytes,
	nativeTextureLevelBytes,
	validateNativeTexture
} from "@/engine/foundation/assets/native-texture";

// The edge of a block-compressed block; WebGPU needs block-aligned extents.
const BLOCK_SIDE = 4;
// Resident UI texture bytes the slots may hold together.
const UI_TEXTURE_BUDGET_BYTES = 64 << 20;
// Device-owned UI resources. Stable instance storage and draw bundles survive data edits.
/*
================
createUiResources
================
*/
export function createUiResources(
	device: GPUDevice,
	format: GPUTextureFormat,
	// The portrait targets render character draws, so they follow the HDR
	// stage's scene format; the UI quads always render into the presented
	// 8-bit frame.
	sceneFormat: () => GPUTextureFormat,
	retire: Retire = destroyNow
) {
	const shader = device.createShaderModule( {
		label: "ui-quads",
		code: `
/*
================
Quad
================
*/
struct Quad {
	rect: vec4f,
	uv: vec4f,
	color: vec4f,
	clip: vec4f,
	maskRect: vec4f,
	effects: vec4f,
	rightColor: vec4f
};
@group(0) @binding(0) var<storage, read> quads: array<Quad>;
@group(0) @binding(1) var<uniform> viewport: vec4f;
@group(0) @binding(2) var tex: texture_2d<f32>;
@group(0) @binding(3) var samp: sampler;
@group(0) @binding(4) var maskTex: texture_2d<f32>;

/*
================
Out
================
*/
struct Out {
	@builtin(position) position: vec4f,
	@location(0) uv: vec2f,
	@location(1) color: vec4f,
	@location(2) point: vec2f,
	@location(3) @interpolate(flat) clip: vec4f,
	@location(4) maskUv: vec2f,
	@location(5) @interpolate(flat) cutoff: f32
};

/*
================
vs
================
*/
@vertex fn vs( @builtin(vertex_index) v: u32, @builtin(instance_index) i: u32 ) -> Out {
	let corners = array<vec2f, 6>(
		vec2f( 0, 0 ), vec2f( 1, 0 ), vec2f( 0, 1 ),
		vec2f( 0, 1 ), vec2f( 1, 0 ), vec2f( 1, 1 )
	);
	let c = corners[v];
	let q = quads[i];
	let local = ( c - 0.5 ) * q.rect.zw;
	let angle = q.effects.z;
	let p = q.rect.xy + q.rect.zw * 0.5 + vec2f(
		local.x * cos( angle ) - local.y * sin( angle ),
		local.x * sin( angle ) + local.y * cos( angle )
	);
	var o: Out;
	o.position = vec4f( p.x / viewport.x * 2 - 1, 1 - p.y / viewport.y * 2, q.effects.w, 1 );
	var uv = c;
	if ( q.effects.x == 1 ) {
		uv = vec2f( c.y, 1 - c.x );
	} else if ( q.effects.x == 2 ) {
		uv = 1 - c;
	} else if ( q.effects.x == 3 ) {
		uv = vec2f( 1 - c.y, c.x );
	}
	o.uv = q.uv.xy + uv * q.uv.zw;
	o.cutoff = q.effects.y;
	o.color = mix( q.color, q.rightColor, c.x );
	o.point = p;
	o.clip = q.clip;
	o.maskUv = ( p - q.maskRect.xy ) / q.maskRect.zw;
	return o;
}

/*
================
fs
================
*/
@fragment fn fs( i: Out ) -> @location(0) vec4f {
	if ( any( i.point < i.clip.xy ) || any( i.point >= i.clip.xy + i.clip.zw ) ) {
		discard;
	}
	let color = textureSample( tex, samp, i.uv ) * i.color;
	if ( color.a < i.cutoff ) {
		discard;
	}
	return vec4f( color.rgb, color.a * textureSample( maskTex, samp, i.maskUv ).a );
}
`
	} );
	const layout = device.createBindGroupLayout( {
		entries: [
			{ binding: 0, visibility: GPUShaderStage.VERTEX, buffer: { type: "read-only-storage" } },
			{ binding: 1, visibility: GPUShaderStage.VERTEX, buffer: { type: "uniform" } },
			{ binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: {} },
			{ binding: 3, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
			{ binding: 4, visibility: GPUShaderStage.FRAGMENT, texture: {} }
		]
	} );
	let pipeline: GPURenderPipeline | null = null, worldPipeline: GPURenderPipeline | null = null, disposed = false;
	/*
	================
	descriptor
	================
	*/
	const descriptor = ( depthCompare: GPUCompareFunction ): GPURenderPipelineDescriptor => ({
		label: "gpu-ui",
		layout: device.createPipelineLayout( { bindGroupLayouts: [ layout ] } ),
		vertex: { module: shader, entryPoint: "vs" },
		fragment: {
			module: shader,
			entryPoint: "fs",
			targets: [ {
				format,
				blend: {
					color: { srcFactor: "src-alpha", dstFactor: "one-minus-src-alpha", operation: "add" },
					alpha: { srcFactor: "one", dstFactor: "one-minus-src-alpha", operation: "add" }
				}
			} ]
		},
		primitive: { topology: "triangle-list" },
		depthStencil: { format: "depth24plus", depthWriteEnabled: false, depthCompare }
	});
	const ready = Promise.all( [
		device.createRenderPipelineAsync( descriptor( "always" ) ),
		device.createRenderPipelineAsync( descriptor( "less-equal" ) )
	] ).then( ( [overlay, world] ) => {
		if ( !disposed ) {
			pipeline = overlay;
			worldPipeline = world;
		}
	} );
	const storage = device.createBuffer( {
		label: "ui-instances",
		size: 8192 * 112,
		usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
	} );
	const viewport = device.createBuffer( {
		label: "ui-viewport",
		size: 16,
		usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
	} );
	const sampler = device.createSampler( { magFilter: "linear", minFilter: "linear" } );
	const nearestSampler = device.createSampler( { magFilter: "nearest", minFilter: "nearest" } );
	const textures = new Map<string, {
		texture: GPUTexture;
		width: number;
		height: number;
		// What the slot costs the device: RGBA8, or the uploaded block size.
		bytes: number;
		format: GPUTextureFormat;
		// Portrait targets carry their format: the HDR stage can flip it.
		portrait?: GPUTextureFormat;
		// Created once per slot and returned by identity: the same view object
		// every call, instead of a fresh one per frame. (No other owner
		// compares this identity; the frame only uses it as a pass view.)
		view?: GPUTextureView;
	}>();
	// texture -> mask -> one bind group per sampler. Nested maps, not a joined
	// key: the per-quad lookup builds no string.
	const bindings = new Map<string, Map<string, { linear?: GPUBindGroup; nearest?: GPUBindGroup; }>>();
	const packed: (UiQuad | undefined)[] = [];
	// The glyph a packed record holds (-1 or unused for a plain quad).
	const packedGlyph: number[] = [];
	let values = new Float32Array( 0 ), uploaded = new Float32Array( 0 );
	const viewportValues = new Float32Array( 4 ), noMask = [ 0, 0, 1, 1 ] as const;
	let last: UiScene | null = null, draws: readonly UiDraw[] = [], resourceRevision = 0, recordedResources = -1;
	/*
	================
	texture

	A native texture draws its first level. Block-compressed levels upload
	unchanged where the adapter samples BC and the extent is block-aligned
	(as device/images.ts does); elsewhere the level is decoded to RGBA8.
	================
	*/
	function texture( id: string, image: UiTexture | null ) {
		if ( disposed ) return;
		if ( !image ) {
			const released = textures.get( id );
			if ( released ) retire( released.texture );
			if ( textures.delete( id ) ) resourceRevision++;
			return;
		}
		if ( image.width > 4096 || image.height > 4096 ) throw new Error( "UI texture budget exceeded" );
		const native = "kind" in image ? image : null;
		if ( native ) validateNativeTexture( native );
		const blockBytes = native ? nativeTextureBlockBytes( native.format ) : 0;
		const compressed = native !== null && blockBytes > 0 && device.features.has( "texture-compression-bc" ) &&
			native.width % BLOCK_SIDE === 0 && native.height % BLOCK_SIDE === 0;
		const format: GPUTextureFormat = compressed ? native.format : "rgba8unorm";
		const bytes = compressed ?
			nativeTextureLevelBytes( native.format, native.width, native.height ) :
			image.width * image.height * 4;
		let slot = textures.get( id );
		if ( !slot || slot.width !== image.width || slot.height !== image.height || slot.format !== format ) {
			// Native HUD + Inventory + modal demand exceeds 256 small sprites (the
			// disconnect capture hit that cap at 26.3 MiB). Reserve 512 descriptors
			// for composed windows while retaining the independent 64 MiB limit.
			const resident = [ ...textures.values() ].reduce( ( sum, row ) => sum + row.bytes, 0 ) -
				(slot ? slot.bytes : 0);
			if ( (!slot && textures.size >= 512) || resident + bytes > UI_TEXTURE_BUDGET_BYTES ) {
				throw new Error(
					"UI texture residency budget exceeded: count=" + textures.size + " bytes=" + (resident + bytes) +
						" path=" + id
				);
			}
			if ( slot ) retire( slot.texture );
			slot = {
				texture: device.createTexture( {
					label: "ui:" + id,
					size: [ image.width, image.height ],
					format,
					usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST |
						(compressed ? 0 : GPUTextureUsage.RENDER_ATTACHMENT)
				} ),
				width: image.width,
				height: image.height,
				bytes,
				format
			};
			textures.set( id, slot );
			resourceRevision++;
		}
		if ( "kind" in image ) {
			device.queue.writeTexture(
				{ texture: slot.texture },
				(compressed ? image.levels[0]! : decodeNativeTextureLevel( image, 0 )) as Uint8Array<ArrayBuffer>,
				{
					bytesPerRow: compressed ? (image.width / BLOCK_SIDE) * blockBytes : image.width * 4,
					rowsPerImage: compressed ? image.height / BLOCK_SIDE : image.height
				},
				[ image.width, image.height ]
			);
		} else if ( "data" in image ) {
			device.queue.writeTexture( { texture: slot.texture }, image.data, { bytesPerRow: image.width * 4 }, [
				image.width,
				image.height
			] );
		} else {
			device.queue.copyExternalImageToTexture( { source: image, flipY: false }, { texture: slot.texture }, [
				image.width,
				image.height
			] );
		}
	}
	texture( "", { data: new Uint8ClampedArray( [ 255, 255, 255, 255 ] ), width: 1, height: 1, colorSpace: "srgb" } );
	return {
		ready,
		texture,
		/*
		================
		portraitTarget

		Inventory and mall have different native viewports. Retire both the target
		and its cached bindings when switching owners, before encoding any draws.
		================
		*/
		portraitTarget( id = "__portrait", width = 128, height = 128 ) {
			if (
				!Number.isInteger( width ) || !Number.isInteger( height ) || width < 1 || height < 1 || width > 4096 ||
				height > 4096
			) throw Error( "Invalid portrait extent" );
			const target = sceneFormat();
			let slot = textures.get( id );
			if ( !slot || slot.width !== width || slot.height !== height || slot.portrait !== target ) {
				if ( slot ) retire( slot.texture );
				slot = {
					texture: device.createTexture( {
						label: id,
						size: [ width, height ],
						format: target,
						usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.RENDER_ATTACHMENT
					} ),
					width,
					height,
					bytes: width * height * 4,
					format: target,
					portrait: target
				};
				textures.set( id, slot );
				resourceRevision++;
			}
			slot.view ??= slot.texture.createView();
			return slot.view;
		},
		/*
		================
		releasePortraits

		The HDR stage flipped the scene format: the cached portrait targets
		are in the old one, so they retire ahead of the next prepare.
		================
		*/
		releasePortraits() {
			for ( const [id, slot] of textures ) {
				if ( !slot.portrait ) continue;
				retire( slot.texture );
				textures.delete( id );
				resourceRevision++;
			}
		},
		/*
		================
		prepare
		================
		*/
		prepare( scene: UiScene | null ): readonly UiDraw[] {
			if ( !pipeline || !scene ) return [];
			if ( last === scene && recordedResources === resourceRevision ) return draws;
			// One GPU record per plain quad and per glyph of a text run.
			const records = uiRecordCount( scene.quads );
			if ( scene.quads.length > 8192 || records > UI_RECORD_LIMIT ) throw new Error( "UI quad budget exceeded" );
			if ( recordedResources !== resourceRevision ) bindings.clear();
			const needed = records * 28;
			if ( values.length < needed ) {
				const capacity = 2 ** Math.ceil( Math.log2( Math.max( 1, records ) ) ) * 28,
					next = new Float32Array( capacity );
				next.set( uploaded );
				uploaded = next;
				values = new Float32Array( capacity );
				values.set( uploaded );
			}
			const next: UiDraw[] = [];
			let first = needed, lastChanged = -1;
			let previous: GPUBindGroup | null = null;
			let record = 0;
			for ( const quad of scene.quads ) {
				const run = quad.run, count = run ? run.glyphs.length : 1;
				if ( count === 0 ) continue;
				// An absent image is not a solid-color primitive. Keep its draw absent until
				// the texture arrives; resourceRevision rebuilds the command list then.
				const maskKey = quad.mask?.texture ?? "",
					slot = textures.get( quad.texture ),
					mask = textures.get( maskKey );
				if ( !slot || !mask ) {
					for ( let i = 0; i < count; i++ ) packed[record + i] = undefined;
					record += count;
					previous = null;
					continue;
				}
				let textureBindings = bindings.get( quad.texture );
				if ( !textureBindings ) {
					textureBindings = new Map();
					bindings.set( quad.texture, textureBindings );
				}
				let maskBindings = textureBindings.get( maskKey );
				if ( !maskBindings ) {
					maskBindings = {};
					textureBindings.set( maskKey, maskBindings );
				}
				const sampling = quad.sampling ?? "linear";
				let binding = maskBindings[sampling];
				if ( !binding ) {
					binding = device.createBindGroup( {
						layout: pipeline!.getBindGroupLayout( 0 ),
						entries: [
							{ binding: 0, resource: { buffer: storage } },
							{ binding: 1, resource: { buffer: viewport } },
							{ binding: 2, resource: slot.texture.createView() },
							{ binding: 3, resource: quad.sampling === "nearest" ? nearestSampler : sampler },
							{ binding: 4, resource: mask.texture.createView() }
						]
					} );
					maskBindings[sampling] = binding;
				}
				// UiQuad is an immutable publication. Projected labels replace only their
				// own records; retained HUD records keep the already packed GPU bytes. A
				// run's glyph i is the glyph quad expandTextRuns would make: the run's
				// fields with rect = origin + offset and the glyph's own uv.
				for ( let i = 0; i < count; i++ ) {
					const index = record + i;
					if ( packed[index] === quad && packedGlyph[index] === i ) continue;
					const at = index * 28;
					if ( run ) {
						const glyph = run.glyphs[i]!;
						values[at] = quad.rect[0] + glyph.x;
						values[at + 1] = quad.rect[1] + glyph.y;
						values[at + 2] = glyph.width;
						values[at + 3] = glyph.height;
						values.set( glyph.uv, at + 4 );
					} else {
						values.set( quad.rect, at );
						values.set( quad.uv, at + 4 );
					}
					values.set( quad.color, at + 8 );
					values.set( quad.clip, at + 12 );
					values.set( quad.mask?.rect ?? noMask, at + 16 );
					values[at + 20] = quad.uvTurn ?? 0;
					values[at + 21] = quad.alphaCutoff ?? 0;
					values[at + 22] = quad.rotation ?? 0;
					values[at + 23] = quad.depth ?? 0;
					values.set( quad.rightColor ?? quad.color, at + 24 );
					for ( let k = at; k < at + 28; k++ ) {
						if ( values[k] !== uploaded[k] ) {
							first = Math.min( first, k );
							lastChanged = k;
						}
					}
					packed[index] = quad;
					packedGlyph[index] = i;
				}
				const layer = quad.depth !== undefined ? "world" : quad.layer,
					selectedPipeline = quad.depth !== undefined && quad.occlusion !== "none" ?
						worldPipeline! :
						pipeline!;
				if (
					binding === previous && layer === next[next.length - 1]!.layer &&
					selectedPipeline === next[next.length - 1]!.pipeline
				) next[next.length - 1]!.count += count;
				else next.push( { pipeline: selectedPipeline, binding, first: record, count, layer } );
				previous = binding;
				record += count;
			}
			packed.length = records;
			packedGlyph.length = records;
			if ( lastChanged >= first ) {
				const changed = values.subarray( first, lastChanged + 1 );
				device.queue.writeBuffer( storage, first * 4, changed );
				uploaded.set( changed, first );
			}
			if ( viewportValues[0] !== scene.width || viewportValues[1] !== scene.height ) {
				viewportValues[0] = scene.width;
				viewportValues[1] = scene.height;
				device.queue.writeBuffer( viewport, 0, viewportValues );
			}
			// Equal command topology keeps the existing bundle even when positions/text change.
			if (
				next.length !== draws.length ||
				next.some( ( d, i ) =>
					d.pipeline !== draws[i]!.pipeline || d.layer !== draws[i]!.layer ||
					d.binding !== draws[i]!.binding || d.first !== draws[i]!.first || d.count !== draws[i]!.count
				)
			) draws = next;
			last = scene;
			recordedResources = resourceRevision;
			return draws;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			disposed = true;
			for ( const slot of textures.values() ) slot.texture.destroy();
			textures.clear();
			bindings.clear();
			storage.destroy();
			viewport.destroy();
			draws = [];
			packed.length = 0;
			packedGlyph.length = 0;
			last = null;
		}
	};
}
