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
import { readPortraitPixels, type PortraitPixels } from "./portrait-readback";
import type { UiTexture } from "@/engine/contracts/texture";
import {
	decodeNativeTextureLevel,
	nativeTextureBlockBytes,
	nativeTextureFeature,
	nativeTextureLevelBytes,
	validateNativeTexture
} from "@/engine/foundation/assets/native-texture";

// The edge of a block-compressed block; WebGPU needs block-aligned extents.
const BLOCK_SIDE = 4;
// Added to a quad's uv turn (0-3) when it samples nearest; ui-quads decodes it.
const NEAREST_FLAG = 4;
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
	@location(5) @interpolate(flat) cutoff: f32,
	@location(6) @interpolate(flat) uvRect: vec4f,
	@location(7) @interpolate(flat) nearest: f32,
	@location(8) @interpolate(flat) rotated: f32,
	@location(9) @interpolate(flat) rect: vec4f
};

/*
================
areaSample

Port-only, not native: the exact box average of the texels one output
pixel covers, for the UI size slider's fractional scales. The retail
client draws its bitmap font and window art at whole pixels only, so
nearest sampling at 1.5x makes some texels one pixel wide and others two,
and at 0.75x drops whole strokes. Weighting each texel by the area it
covers keeps every texel the same size and every stroke present. Where the
pixel grid matches the texel grid (100%, and whole-number enlargements)
each pixel covers one texel and this equals nearest sampling exactly.
Footprints wider than AREA_TAPS texels (below half size) are not taken.
================
*/
const AREA_TAPS = 3;
// The display transfer the 8-bit frame is encoded in (sRGB, as a power).
const DISPLAY_GAMMA = 2.2;
// UI units each unrotated quad grows by (vs): one texel's footprint at half size.
const EDGE_PAD = 2.0;
const SCALE_EPSILON = 0.002;
const TEXEL_EPSILON = 0.01;
fn areaSample( t: vec2f, d: vec2f, rect: vec4f ) -> vec4f {
	let size = vec2f( textureDimensions( tex ) );
	// The quad's own texels: an atlas glyph never averages its neighbour.
	// A mirrored quad (the tooltip's right edge, uv 1,1,-1,-1) runs its uv
	// backwards; order the ends, or clamp() collapses to one edge and the
	// quad samples nothing (#1973).
	let start = rect.xy * size;
	let end = ( rect.xy + rect.zw ) * size;
	let lowEdge = min( start, end );
	let highEdge = max( start, end );
	let lo = clamp( t - d * 0.5, lowEdge, highEdge );
	let hi = clamp( t + d * 0.5, lowEdge, highEdge );
	let first = floor( lo );
	var sum = vec4f( 0 );
	var weight = 0.0;
	for ( var y = 0; y < AREA_TAPS; y++ ) {
		for ( var x = 0; x < AREA_TAPS; x++ ) {
			let cell = first + vec2f( f32( x ), f32( y ) );
			let overlap = max( min( hi, cell + 1 ) - max( lo, cell ), vec2f( 0 ) );
			let w = overlap.x * overlap.y;
			if ( w > 0 ) {
				let texel = textureLoad( tex, vec2i( clamp( cell, vec2f( 0 ), size - 1 ) ), 0 );
				// Premultiplied, so a transparent texel adds no colour.
				sum += vec4f( pow( texel.rgb, vec3f( DISPLAY_GAMMA ) ) * texel.a, texel.a ) * w;
				weight += w;
			}
		}
	}
	if ( weight <= 0 ) {
		return vec4f( 0 );
	}
	// Over the pixel's whole footprint: the part outside the quad is
	// transparent, so an edge pixel holds only the ink that reaches it.
	let alpha = sum.a / ( d.x * d.y );
	// Colours average in linear light, so a mixed edge keeps its brightness.
	let rgb = select( vec3f( 0 ), pow( sum.rgb / sum.a, vec3f( 1 / DISPLAY_GAMMA ) ), sum.a > 0 );
	return vec4f( rgb, alpha );
}

/*
================
perceivedCoverage

The frame blends gamma-encoded colours, so a half-covered pixel of light
text on a dark panel would show at a fifth of its brightness and thin
strokes would look weak. Lift the coverage to the value that blends to the
right light: light text over the dark panels it sits on, and the mirror
rule for dark text (shadows, text on light plates). Full and empty
coverage are unchanged.
================
*/
fn perceivedCoverage( coverage: f32, color: vec3f ) -> f32 {
	let light = dot( color, vec3f( 0.2126, 0.7152, 0.0722 ) ) >= 0.5;
	let lifted = pow( coverage, 1 / DISPLAY_GAMMA );
	let mirrored = 1 - pow( 1 - coverage, 1 / DISPLAY_GAMMA );
	return select( mirrored, lifted, light );
}

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
	let q = quads[i];
	let angle = q.effects.z;
	// An unrotated quad grows by EDGE_PAD UI units on each side, its uv with
	// it: a fractional scale's edge texels cover pixels whose centres lie
	// outside the rect. fs discards the pad unless it takes the area average.
	// Pad only a quad whose texels miss whole pixels: at 100% and at whole
	// enlargements nothing is padded and the default path shades no extra
	// fragments. viewport.z is pixels per UI unit (0 = unknown: pad).
	// A mirrored quad's uv extent is negative; its texel count is not.
	var texels = abs( q.uv.zw ) * vec2f( textureDimensions( tex ) );
	if ( q.effects.x % 2 == 1 ) {
		texels = texels.yx;
	}
	let pixelsPerTexel = viewport.z * q.rect.zw / max( texels, vec2f( 1e-6 ) );
	let onGrid = viewport.z > 0 &&
		all( abs( pixelsPerTexel - round( pixelsPerTexel ) ) < vec2f( SCALE_EPSILON ) ) &&
		all( pixelsPerTexel > vec2f( 0.5 ) );
	let padded = angle == 0 && all( q.rect.zw > vec2f( 0 ) ) && !onGrid;
	let pad = select( vec2f( 0 ), vec2f( EDGE_PAD ) / q.rect.zw, padded );
	let c = corners[v] * ( 1 + 2 * pad ) - pad;
	let local = ( c - 0.5 ) * q.rect.zw;
	let p = q.rect.xy + q.rect.zw * 0.5 + vec2f(
		local.x * cos( angle ) - local.y * sin( angle ),
		local.x * sin( angle ) + local.y * cos( angle )
	);
	var o: Out;
	o.position = vec4f( p.x / viewport.x * 2 - 1, 1 - p.y / viewport.y * 2, q.effects.w, 1 );
	var uv = c;
	// effects.x is the uv turn, plus NEAREST_FLAG for a nearest quad.
	let turn = q.effects.x % 4;
	if ( turn == 1 ) {
		uv = vec2f( c.y, 1 - c.x );
	} else if ( turn == 2 ) {
		uv = 1 - c;
	} else if ( turn == 3 ) {
		uv = vec2f( 1 - c.y, c.x );
	}
	o.uv = q.uv.xy + uv * q.uv.zw;
	o.cutoff = q.effects.y;
	o.color = mix( q.color, q.rightColor, clamp( c.x, 0.0, 1.0 ) );
	o.point = p;
	o.clip = q.clip;
	o.maskUv = ( p - q.maskRect.xy ) / q.maskRect.zw;
	o.uvRect = q.uv;
	o.nearest = select( 0.0, 1.0, q.effects.x >= 4 );
	o.rotated = select( 0.0, 1.0, angle != 0 );
	o.rect = q.rect;
	return o;
}

/*
================
fs
================
*/
@fragment fn fs( i: Out ) -> @location(0) vec4f {
	// Derivatives first, in uniform control flow.
	let t = i.uv * vec2f( textureDimensions( tex ) );
	let d = fwidth( t );
	// Output pixels per UI unit: 1 at 100%, 2 on a 2x display, 1.5 at 150%.
	let scale = 1 / max( fwidth( i.point ).x, 1e-6 );
	let sampled = textureSample( tex, samp, i.uv );
	if ( any( i.point < i.clip.xy ) || any( i.point >= i.clip.xy + i.clip.zw ) ) {
		discard;
	}
	// One rule for every path that scales the retail bitmaps (the UI size
	// slider, a fitted window, the region banner, display density), decided
	// per quad by its own texels per output pixel. A nearest quad (all text)
	// at a whole-number magnification keeps nearest sampling and is pixel
	// for pixel unchanged; at any other ratio it takes the area average with
	// perceived coverage. Artwork drawn texel for texel takes the area
	// average at a fractional UI scale; stretched art keeps its linear filter.
	let magnification = 1 / max( d, vec2f( 1e-6 ) );
	let aligned = all( abs( magnification - round( magnification ) ) < vec2f( SCALE_EPSILON ) ) &&
		all( magnification > vec2f( 0.5 ) );
	let whole = abs( scale - round( scale ) ) < SCALE_EPSILON;
	let texelForTexel = all( abs( d * scale - 1 ) < vec2f( TEXEL_EPSILON ) );
	let text = i.nearest == 1 && !aligned;
	let art = i.nearest == 0 && !whole && texelForTexel;
	let area = i.rotated == 0 && all( d <= vec2f( f32( AREA_TAPS - 1 ) ) ) && ( text || art );
	var texel = sampled;
	if ( area ) {
		texel = areaSample( t, d, i.uvRect );
		if ( i.nearest == 1 ) {
			texel.a = perceivedCoverage( texel.a, i.color.rgb );
		}
	} else if ( i.rotated == 0 && ( any( i.point < i.rect.xy ) || any( i.point >= i.rect.xy + i.rect.zw ) ) ) {
		// The vertex pad, outside the quad: the unpadded quad's pixels only.
		discard;
	}
	let color = texel * i.color;
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
			// The vertex stage reads the texture's size to decide the edge pad.
			{ binding: 2, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, texture: {} },
			{ binding: 3, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
			{ binding: 4, visibility: GPUShaderStage.FRAGMENT, texture: {} }
		]
	} );
	let pipeline: GPURenderPipeline | null = null,
		worldPipeline: GPURenderPipeline | null = null,
		additivePipeline: GPURenderPipeline | null = null,
		disposed = false;
	/*
	================
	descriptor
	================
	*/
	const descriptor = ( depthCompare: GPUCompareFunction, additive = false ): GPURenderPipelineDescriptor => ({
		label: "gpu-ui",
		layout: device.createPipelineLayout( { bindGroupLayouts: [ layout ] } ),
		vertex: { module: shader, entryPoint: "vs" },
		fragment: {
			module: shader,
			entryPoint: "fs",
			targets: [ {
				format,
				blend: additive ?
					{
						color: { srcFactor: "src-alpha", dstFactor: "one", operation: "add" },
						alpha: { srcFactor: "zero", dstFactor: "one", operation: "add" }
					} :
					{
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
		device.createRenderPipelineAsync( descriptor( "less-equal" ) ),
		device.createRenderPipelineAsync( descriptor( "always", true ) )
	] ).then( ( [overlay, world, additive] ) => {
		if ( !disposed ) {
			pipeline = overlay;
			worldPipeline = world;
			additivePipeline = additive;
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
		const compressed = native !== null && blockBytes > 0 &&
			device.features.has( nativeTextureFeature( native.format )! ) &&
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
						// COPY_SRC lets the 3D viewer read a still back (readPortrait).
						usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.RENDER_ATTACHMENT |
							GPUTextureUsage.COPY_SRC
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
		readPortrait

		The portrait target's last rendered pixels as packed 8-bit RGBA, for
		the 3D viewer's still (port-only, portrait-readback.ts).
		================
		*/
		readPortrait( id: string ): Promise<PortraitPixels> {
			const slot = textures.get( id );
			if ( !slot?.portrait ) return Promise.reject( Error( "No portrait target " + id ) );
			return readPortraitPixels(
				device,
				{ label: id, texture: slot.texture, width: slot.width, height: slot.height, format: slot.format },
				retire
			);
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
		prepare( scene: UiScene | null, targetWidth = 0 ): readonly UiDraw[] {
			if ( !pipeline || !scene ) return [];
			// Before the cache return: a density change can keep the same scene.
			// Output pixels per UI unit; 0 when the caller does not say (the
			// shader then pads every quad, which is correct, only slower).
			const pixelScale = targetWidth > 0 ? targetWidth / scene.width : 0;
			if (
				viewportValues[0] !== scene.width || viewportValues[1] !== scene.height ||
				viewportValues[2] !== pixelScale
			) {
				viewportValues[0] = scene.width;
				viewportValues[1] = scene.height;
				viewportValues[2] = pixelScale;
				device.queue.writeBuffer( viewport, 0, viewportValues );
			}
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
					// Nearest quads carry NEAREST_FLAG in the turn slot (the shader's
					// area sampling at fractional UI sizes reads it).
					values[at + 20] = (quad.uvTurn ?? 0) + (quad.sampling === "nearest" ? NEAREST_FLAG : 0);
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
						quad.additive && quad.depth === undefined ?
						additivePipeline! :
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
