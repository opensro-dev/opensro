/*
===========================================================================

geometry.ts - GPU geometry resources

Uploads meshes to GPU buffers and bind groups and owns their updates
(instances, bones, indices, positions) and release. Only meshes uploaded
with dynamicVertices keep a CPU mirror for position updates.

===========================================================================
*/
import { createInstancePacking, instanceCapacity } from "@/engine/foundation/rendering/geometry-instances";
import { createWaterReflection } from "./water-reflection";
import { packTextureStage } from "@/engine/foundation/rendering/texture-stage";
import { createCharacterShadows } from "./character-shadows";
import { destroyNow, type Retire } from "./retirement";
import { DeviceDraw } from "./device-draw";
import type { createParticlePresentation } from "./particles";
import type { createGpuAnimationResources } from "./animation";
import { DEFAULT_BLEND, type GeometryPipelineState } from "./pipelines";
import { D3DBLEND_SRCCOLOR, D3DBLEND_ZERO, type BlendPair } from "@/engine/foundation/rendering/blend-state";
import type { Geometry } from "@/engine/contracts/geometry";
import { packGeometryVertices } from "@/engine/foundation/rendering/geometry-vertices";
import type { GeometryCommands, GeometryDraw, ImageDraw } from "@/engine/runtime/renderer/internal/gpu-contract";
/*
================
geometryPipelineState

The pipeline state a material draws with. Blending follows the material's
D3D pair (SRCALPHA/INVSRCALPHA when it names none); a lightmap multiplies
(ZERO/SRCCOLOR). Depth writes follow the material, else a blended draw
writes depth only as a fading object or a decal, as the old pipeline
classes did. Sky, deferred particles and ground decals draw without the
depth test (the second sky, culled, keeps it).
================
*/
export function geometryPipelineState( mat: Geometry["material"] ): GeometryPipelineState {
	const cull = !!mat && !mat.doubleSided;
	if ( mat?.groundDecal ) return { blend: DEFAULT_BLEND, cull, depthWrite: false, depthCompare: "always" };
	const fade = !!(mat?.objectFade || mat?.instanceFade);
	const blend: BlendPair | null = mat?.lightmap ?
		{ source: D3DBLEND_ZERO, destination: D3DBLEND_SRCCOLOR } :
		mat?.blend || fade || mat?.decal || mat?.sky ?
		mat.blendPair ?? DEFAULT_BLEND :
		null;
	const writes = mat?.depthWrite ?? ((!blend || fade || !!mat?.decal) && !mat?.sky && !mat?.lightmap);
	if ( mat?.deferredParticle ) return { blend, cull, depthWrite: false, depthCompare: "always" };
	const untested = !!mat?.sky && !(mat.sky === 2 && cull);
	return { blend, cull, depthWrite: writes, depthCompare: untested ? "always" : "less-equal" };
}

/*
================
createGeometryResources
================
*/
export function createGeometryResources(
	created: GPUDevice,
	current: () => GPUDevice,
	fail: ( error: unknown ) => void,
	pipelines: ( state: GeometryPipelineState ) => GPURenderPipeline,
	texture: ( image: ImageDraw ) => GPUTexture,
	worldSampler: GPUSampler,
	lightmapSampler: GPUSampler,
	environment: GPUBuffer,
	sampling?: ( filtered: boolean, detail: number, anisotropic: boolean ) => GPUSampler,
	animation?: ReturnType<typeof createGpuAnimationResources>,
	format: GPUTextureFormat = "rgba8unorm",
	particles?: ReturnType<typeof createParticlePresentation>,
	// A frame may still name a released buffer: the device decides when it dies.
	retire: Retire = destroyNow,
	// The clamped sampler for the anisotropy stage; without it the lightmap
	// sampler stays the one given.
	lightmapSampling?: ( anisotropic: boolean ) => GPUSampler
) {
	let filtered = true, detail = 2, anisotropic = false, mixedCpuUploadBytes = 0;
	const waterReflection = createWaterReflection( created, format, retire );
	// 8BA130 fixes water MIN/MAG to linear independently of the video filter.
	const waterSampler = worldSampler;
	const reflectedBindings = new WeakMap<GeometryDraw, { source: GPUBindGroup; draw: GeometryDraw; }>();
	const geometryBuffers = new Map<GeometryDraw, GPUBuffer[]>();
	// Released draws and their release records (releasedDraw).
	const releasedDraws = new WeakMap<GeometryDraw, import("../internal/gpu-contract").DrawRelease>();
	// Every buffer is labelled: a validation error names the resource it rejects.
	const worldUniform = created.createBuffer( {
		label: "geometry-world",
		size: 64,
		usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
	} );
	const white = created.createTexture( {
		size: [ 1, 1 ],
		format: "rgba8unorm",
		usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST
	} );
	created.queue.writeTexture( { texture: white }, Uint8Array.of( 255, 255, 255, 255 ), { bytesPerRow: 4 }, [ 1, 1 ] );
	const packInstances = createInstancePacking();
	const defaultSkin = created.createBuffer( {
			label: "geometry-default-skin",
			size: 32,
			usage: GPUBufferUsage.STORAGE
		} ),
		defaultBones = created.createBuffer( {
			label: "geometry-default-bones",
			size: 64,
			usage: GPUBufferUsage.STORAGE
		} );
	type SharedPalette = { source: Float32Array; buffer: GPUBuffer; refs: number; revision: number; };
	const sharedPalettes = new Map<Float32Array, SharedPalette>();
	/*
	================
	releasePalette
	================
	*/
	function releasePalette( palette: SharedPalette | undefined ) {
		if ( palette && !--palette.refs ) {
			animation?.release( palette.source );
			retire( palette.buffer );
			sharedPalettes.delete( palette.source );
		}
	}
	/*
	================
	validatePaletteOffsets
	================
	*/
	function validatePaletteOffsets( offsets: Uint32Array | undefined, boneBytes: number, jointMaximum: number ) {
		if ( !offsets ) throw Error( "Palette offset outside bone storage" );
		const joints = boneBytes / 64;
		for ( let i = 0; i < offsets.length; i++ ) {
			if ( offsets[i]! + jointMaximum >= joints ) throw Error( "Palette offset outside bone storage" );
		}
	}
	const geometryBinding = (
		uniform: GPUBuffer,
		storage: GPUBuffer,
		material: GPUBuffer,
		pipeline: GPURenderPipeline,
		clampedSampling: boolean,
		image?: ImageDraw,
		skin = defaultSkin,
		bones = defaultBones,
		environmentImage?: ImageDraw,
		capture = false
	) => current().createBindGroup( {
		layout: pipeline.getBindGroupLayout( 0 ),
		entries: [
			{ binding: 9, resource: capture ? white.createView() : waterReflection.view() ?? white.createView() },
			{ binding: 10, resource: { buffer: capture ? waterReflection.capture : waterReflection.main } },
			{ binding: 11, resource: lightmapSampler },
			{ binding: 12, resource: waterSampler },
			{ binding: 0, resource: { buffer: uniform } },
			{ binding: 1, resource: { buffer: storage } },
			{ binding: 2, resource: { buffer: material } },
			{
				binding: 3,
				resource: clampedSampling ? lightmapSampler : worldSampler
			},
			{ binding: 4, resource: (image ? texture( image ) : white).createView( { dimension: "2d-array" } ) },
			{ binding: 5, resource: { buffer: environment } },
			{ binding: 6, resource: { buffer: skin } },
			{ binding: 7, resource: { buffer: bones } },
			{
				binding: 8,
				resource: (environmentImage ? texture( environmentImage ) : white).createView( {
					dimension: "2d-array"
				} )
			}
		]
	} );
	const metadata = new Map<GeometryDraw, {
		water: boolean;
		state: GeometryPipelineState;
		uniform: GPUBuffer;
		material: GPUBuffer;
		image?: ImageDraw;
		environmentImage?: ImageDraw;
		vertices?: Float32Array;
		skin: GPUBuffer;
		bones: GPUBuffer;
		// The bone and index buffers' sizes, read once: a GPUBuffer's size
		// crosses into the browser on every read, and updates run every frame.
		boneBytes: number;
		indexBytes: number;
		palette?: SharedPalette;
		jointMaximum: number;
		// Lightmaps and decals sample with clamped addressing.
		clampedSampling: boolean;
		// The material uniform's TEXTUREFACTOR offset in bytes (its last vec4).
		textureFactorOffset: number;
		// The draw handle itself, which this owner updates in place.
		selection: DeviceDraw;
	}>();
	let shadows: ReturnType<typeof createCharacterShadows> | undefined;
	const commands: GeometryCommands = Object.freeze( {
		/*
        ================
        waterReflection

        Capture variants share geometry and palettes, but use an independent
        bind group. The native orbit camera preserves handedness; 8BA130 selects
        D3DCULL_CCW (clockwise fronts). Water never appears in its own image.
        ================
        */
		waterReflection(
			input: { matrix?: Float32Array; height: number; above: boolean; seconds: number; },
			draws: readonly GeometryDraw[]
		) {
			const { matrix, height, above, seconds } = input;
			if ( waterReflection.update( matrix, height, above, seconds ) ) {
				for ( const [draw, meta] of metadata ) {
					DeviceDraw.rebind(
						meta.selection,
						geometryBinding(
							meta.uniform,
							geometryBuffers.get( draw )![3]!,
							meta.material,
							draw.pipeline,
							meta.clampedSampling,
							meta.image,
							meta.skin,
							meta.bones,
							meta.environmentImage
						)
					);
				}
			}
			if ( !matrix ) return undefined;
			const reflected: GeometryDraw[] = [];
			for ( const draw of draws ) {
				const meta = metadata.get( draw );
				if ( !meta || meta.water || draw.deferredParticle ) continue;
				let cached = reflectedBindings.get( draw );
				if ( !cached || cached.source !== draw.binding ) {
					const pipeline = pipelines( meta.state );
					const binding = geometryBinding(
						meta.uniform,
						geometryBuffers.get( draw )![3]!,
						meta.material,
						pipeline,
						meta.clampedSampling,
						meta.image,
						meta.skin,
						meta.bones,
						meta.environmentImage,
						true
					);
					cached = {
						source: draw.binding,
						draw: {
							...draw,
							pipeline,
							binding,
							vertices: draw.vertices,
							indices: draw.indices,
							count: draw.count,
							indexCount: draw.indexCount,
							instanceCount: draw.instanceCount,
							instanceCapacity: draw.instanceCapacity
						}
					};
					reflectedBindings.set( draw, cached );
				}
				reflected.push( { ...cached.draw, indexCount: draw.indexCount, instanceCount: draw.instanceCount } );
			}
			return {
				encode( encoder: GPUCommandEncoder ) {
					waterReflection.encode( encoder, reflected );
				}
			};
		},
		characterShadows(
			requests: readonly import("../internal/gpu-contract").CharacterShadowRequest[],
			blob?: ImageDraw
		) {
			if ( !shadows && !requests.length ) return [];
			shadows ??= createCharacterShadows(
				current(),
				worldUniform,
				texture,
				draw => {
					const meta = metadata.get( draw ), buffers = geometryBuffers.get( draw );
					return meta && buffers ?
						{ instances: buffers[3]!, material: meta.material, skin: meta.skin, bones: meta.bones } :
						undefined;
				},
				format,
				retire
			);
			return shadows.prepare( requests, blob );
		},
		...(animation ?
			{
				gpuAnimationStats: () => ({ ...animation.stats(), cpuUploadBytes: mixedCpuUploadBytes }),
				prepareGpuBones: (
					source: Float32Array,
					model: import("@/engine/contracts/character").CharacterModel,
					primitive: import("@/engine/contracts/character").CharacterPrimitive,
					samples: readonly ({
						readonly clip: import("@/engine/contracts/character").CharacterClip;
						readonly time: number;
					} | null)[],
					revision: number
				) => {
					current();
					const palette = sharedPalettes.get( source );
					if ( !palette ) return false;
					if (
						!Number.isSafeInteger( revision ) || revision < 0 || revision < palette.revision
					) throw Error( "Invalid GPU palette revision" );
					if ( palette.revision === revision ) return true;
					if ( !animation.prepare( source, palette.buffer, model, primitive, samples ) ) return false;
					// CPU writes and GPU jobs own disjoint canonical pose slots.
					const stride = primitive.joints.length * 64;
					for ( let i = 0; i < samples.length; ) {
						if ( samples[i] !== null ) {
							i++;
							continue;
						}
						const first = i;
						while ( i < samples.length && samples[i] === null ) i++;
						const bytes = (i - first) * stride;
						current().queue.writeBuffer(
							palette.buffer,
							first * stride,
							source.buffer as ArrayBuffer,
							source.byteOffset + first * stride,
							bytes
						);
						mixedCpuUploadBytes += bytes;
					}
					palette.revision = revision;
					return true;
				}
			} :
			{}),
		/*
		================
		presentParticles
		================
		*/
		presentParticles( draw: GeometryDraw, presentation: import("../internal/gpu-contract").ParticlePresentation ) {
			const meta = metadata.get( draw ), buffers = geometryBuffers.get( draw );
			if ( !particles ) throw Error( "Particle presentation unavailable" );
			if ( !meta || !buffers || meta.bones === defaultBones || meta.palette ) {
				throw Error( "Particle presentation requires an owned skinned draw" );
			}
			if ( meta.selection.instanceCount !== presentation.rows * presentation.slots ) {
				throw Error( "Particle presentation must draw one instance a slot" );
			}
			particles.present( draw, buffers[3]!, meta.bones, presentation );
		},
		/*
		================
		updateBones
		================
		*/
		updateBones( draw: GeometryDraw, bones: Float32Array, revision?: number ) {
			const meta = metadata.get( draw );
			if ( !meta || meta.bones === defaultBones || bones.byteLength > meta.boneBytes ) {
				throw new Error( "Invalid bone palette" );
			}
			if ( meta.palette ) {
				if (
					typeof revision !== "number" || !Number.isSafeInteger( revision ) || revision < 0 ||
					revision < meta.palette.revision || bones.buffer !== meta.palette.source.buffer ||
					bones.byteOffset !== meta.palette.source.byteOffset
				) throw Error( "Invalid shared palette revision" );
				if ( meta.palette.revision === revision ) return 0;
			}
			if ( meta.palette ) animation?.cancel( meta.palette.source );
			current().queue.writeBuffer(
				meta.bones,
				0,
				bones.buffer as ArrayBuffer,
				bones.byteOffset,
				bones.byteLength
			);
			if ( meta.palette ) meta.palette.revision = revision!;
			return bones.byteLength;
		},
		/*
		================
		updateIndices
		================
		*/
		updateIndices( draw: GeometryDraw, indices: Uint32Array ) {
			const gpu = current(), buffers = geometryBuffers.get( draw ), meta = metadata.get( draw );
			if ( !buffers || !meta || indices.byteLength > meta.indexBytes ) {
				throw new Error( "Invalid index selection" );
			}
			if ( indices.byteLength ) {
				gpu.queue.writeBuffer(
					buffers[1]!,
					0,
					indices.buffer as ArrayBuffer,
					indices.byteOffset,
					indices.byteLength
				);
			}
			DeviceDraw.select( meta.selection, indices.length, 1 );
		},
		updatePositions(
			draw: GeometryDraw,
			positions: Float32Array,
			colors?: Float32Array,
			uvs?: Float32Array,
			ranges?: readonly (readonly [number, number])[],
			slot?: number
		) {
			const gpu = current(), meta = metadata.get( draw ), count = positions.length / 3;
			if ( !meta ) throw Error( "Invalid position update" );
			const mirror = meta.vertices;
			if ( !mirror ) throw Error( "Geometry was uploaded without dynamicVertices" );
			// A whole mesh (no slot) covers its draw exactly; a layer member at
			// any slot, the first one included, fits inside it.
			const base = slot ?? 0;
			if (
				!Number.isInteger( base ) || base < 0 ||
				(slot === undefined ? count !== mirror.length / 14 : base + count > mirror.length / 14) ||
				colors && colors.length !== count * 4 || uvs && uvs.length !== count * 2
			) throw Error( "Invalid position update" );
			const vertices = mirror.subarray( base * 14, (base + count) * 14 );
			// Ranges are vertex start/count pairs. Validate the complete transaction
			// before changing the retained CPU mirror or submitting any GPU writes.
			const spans: [number, number][] = [];
			for ( const [start, length] of ranges ?? [ [ 0, count ] ] ) {
				if (
					!Number.isInteger( start ) || !Number.isInteger( length ) || start < 0 || length < 0 ||
					start + length > count
				) throw Error( "Invalid position range" );
				if ( length ) spans.push( [ start, start + length ] );
			}
			spans.sort( ( a, b ) => a[0] - b[0] );
			let merged = 0;
			for ( const span of spans ) {
				const previous = spans[merged - 1];
				if ( previous && span[0] <= previous[1] ) previous[1] = Math.max( previous[1], span[1] );
				else spans[merged++] = span;
			}
			spans.length = merged;
			for ( const [start, end] of spans ) {
				for ( let i = start; i < end; i++ ) {
					for ( let c = 0; c < 3; c++ ) {
						if ( !Number.isFinite( positions[i * 3 + c] ) ) throw Error( "Invalid position update" );
					}
					if ( colors ) {
						for ( let c = 0; c < 4; c++ ) {
							if ( !Number.isFinite( colors[i * 4 + c] ) ) throw Error( "Invalid position update" );
						}
					}
					if ( uvs ) {
						for ( let c = 0; c < 2; c++ ) {
							if ( !Number.isFinite( uvs[i * 2 + c] ) ) throw Error( "Invalid position update" );
						}
					}
				}
			}
			for ( const [start, end] of spans ) {
				for ( let i = start; i < end; i++ ) {
					for ( let c = 0; c < 3; c++ ) vertices[i * 14 + c] = positions[i * 3 + c]!;
					if ( colors ) { for ( let c = 0; c < 4; c++ ) vertices[i * 14 + 8 + c] = colors[i * 4 + c]!; }
					if ( uvs ) { for ( let c = 0; c < 2; c++ ) vertices[i * 14 + 6 + c] = uvs[i * 2 + c]!; }
				}
				const offset = start * 56;
				gpu.queue.writeBuffer(
					draw.vertices,
					base * 56 + offset,
					vertices.buffer as ArrayBuffer,
					vertices.byteOffset + offset,
					(end - start) * 56
				);
			}
		},
		/*
		================
		writeVertices
		================
		*/
		writeVertices( draw: GeometryDraw, base: number, vertices: Float32Array ) {
			const meta = metadata.get( draw ), mirror = meta?.vertices;
			if ( !mirror ) throw Error( "Geometry was uploaded without dynamicVertices" );
			if (
				!Number.isInteger( base ) || base < 0 || vertices.length % 14 ||
				base * 14 + vertices.length > mirror.length
			) throw Error( "Invalid vertex write" );
			mirror.set( vertices, base * 14 );
			current().queue.writeBuffer(
				draw.vertices,
				base * 56,
				mirror.buffer as ArrayBuffer,
				mirror.byteOffset + base * 56,
				vertices.byteLength
			);
		},
		updateInstances(
			draw: GeometryDraw,
			instances: Float32Array,
			opacity?: Float32Array,
			appearance?: Float32Array,
			pointLights?: Float32Array,
			paletteOffsets?: Uint32Array
		): GeometryDraw {
			const gpu = current(), buffers = geometryBuffers.get( draw );
			if ( !buffers ) {
				throw new Error( "Stale geometry handle" );
			}
			const meta = metadata.get( draw )!;
			if ( meta.palette ) validatePaletteOffsets( paletteOffsets, meta.boneBytes, meta.jointMaximum );
			else if ( paletteOffsets ) throw Error( "Palette offsets require shared bone storage" );
			const count = instances.length / 16,
				packed = packInstances( instances, opacity, appearance, pointLights, paletteOffsets );
			if ( count <= draw.instanceCapacity ) {
				// As with position/index/bone writes, the device's uncaptured
				// error listener owns write failures. Scopes are for allocation.
				if ( packed.byteLength ) {
					gpu.queue.writeBuffer(
						buffers[3]!,
						0,
						packed.buffer as ArrayBuffer,
						packed.byteOffset,
						packed.byteLength
					);
				}
				DeviceDraw.select( meta.selection, draw.count, count );
				return draw;
			}
			gpu.pushErrorScope( "validation" );
			try {
				if ( count > draw.instanceCapacity ) {
					const capacity = instanceCapacity( count ),
						storage = gpu.createBuffer( {
							label: "geometry-instances",
							size: capacity * 160,
							usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
						} );
					try {
						gpu.queue.writeBuffer(
							storage,
							0,
							packed.buffer as ArrayBuffer,
							packed.byteOffset,
							packed.byteLength
						);
						DeviceDraw.rebind(
							meta.selection,
							geometryBinding(
								meta.uniform,
								storage,
								meta.material,
								draw.pipeline,
								meta.clampedSampling,
								meta.image,
								meta.skin,
								meta.bones,
								meta.environmentImage
							),
							capacity
						);
						shadows?.forget( draw );
						// The frame's recorded passes may still read the old storage.
						retire( buffers[3]! );
						buffers[3] = storage;
					} catch ( error ) {
						storage.destroy();
						throw error;
					}
				}
				DeviceDraw.select( meta.selection, draw.count, count );
				return draw;
			} finally {
				gpu.popErrorScope().then( error => {
					if ( error ) {
						fail( error.message );
					}
				} ).catch( fail );
			}
		},
		/*
		================
		updateTextureFactor

		The draw's D3DRS_TEXTUREFACTOR (rgba in [0, 1]), as a material
		modifier's pulse sets it each tick.
		================
		*/
		updateTextureFactor( draw: GeometryDraw, rgba: Float32Array ) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Unknown geometry draw" );
			if ( rgba.length !== 4 || !rgba.every( v => Number.isFinite( v ) && v >= 0 && v <= 1 ) ) {
				throw Error( "Invalid texture factor" );
			}
			current().queue.writeBuffer( meta.material, meta.textureFactorOffset, rgba as Float32Array<ArrayBuffer> );
		},
		/*
		================
		updateMaterialColors
		================
		*/
		updateMaterialColors( draw: GeometryDraw, rgb: Float32Array, flags: number ) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Unknown geometry draw" );
			if ( rgb.length !== 3 || !rgb.every( Number.isFinite ) ) throw Error( "Invalid material colors" );
			if ( flags & 2 ) current().queue.writeBuffer( meta.material, 0, rgb as Float32Array<ArrayBuffer> );
			if ( flags & 1 ) current().queue.writeBuffer( meta.material, 128, rgb as Float32Array<ArrayBuffer> );
		},
		updateEquipmentGlow(
			draw: GeometryDraw,
			color: Float32Array,
			uv: Float32Array,
			gain: number,
			alphaTest: boolean,
			enabled: boolean
		) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Unknown geometry draw" );
			if (
				color.length !== 3 || uv.length !== 2 || ![ ...color, ...uv ].every( Number.isFinite ) ||
				![ 1, 2 ].includes( gain )
			) throw Error( "Invalid equipment glow uniforms" );
			current().queue.writeBuffer(
				meta.material,
				192,
				Float32Array.of( ...color, enabled ? gain : 0, ...uv, alphaTest ? 1 : 0, 0 )
			);
		},
		/*
		================
		updateTextureTransform
		================
		*/
		updateTextureTransform( draw: GeometryDraw, matrix: Float32Array ) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Unknown geometry draw" );
			if ( matrix.length !== 8 || !matrix.every( Number.isFinite ) ) throw Error( "Invalid texture transform" );
			current().queue.writeBuffer( meta.material, 144, matrix as Float32Array<ArrayBuffer> );
		},
		/*
		================
		updateTransform
		================
		*/
		updateTransform( draw: GeometryDraw, transform: Float32Array ) {
			const gpu = current(), buffers = geometryBuffers.get( draw );
			if ( !buffers ) {
				throw new Error( "Stale geometry handle" );
			}
			gpu.queue.writeBuffer(
				buffers[2]!,
				0,
				transform.buffer as ArrayBuffer,
				transform.byteOffset,
				transform.byteLength
			);
		},
		/*
		================
		upload
		================
		*/
		upload( data: Geometry, image?: ImageDraw, paletteOffsets?: Uint32Array, environmentImage?: ImageDraw ) {
			const gpu = current(), buffers: GPUBuffer[] = [];
			let palette: SharedPalette | undefined;
			gpu.pushErrorScope( "validation" );
			const buffer = ( label: string, data: Float32Array | Uint32Array, usage: number ) => {
				const result = gpu.createBuffer( {
					label,
					size: data.byteLength,
					usage: usage | GPUBufferUsage.COPY_DST
				} );
				buffers.push( result );
				gpu.queue.writeBuffer( result, 0, data.buffer as ArrayBuffer, data.byteOffset, data.byteLength );
				return result;
			};
			try {
				// The asset worker packs terrain (Geometry.vertices); pack anything else here.
				const interleaved = data.vertices?.length === data.positions.length / 3 * 14 ?
					data.vertices :
					packGeometryVertices( data );
				const vertices = buffer( "geometry-vertices", interleaved, GPUBufferUsage.VERTEX ),
					indices = buffer( "geometry-indices", data.indices, GPUBufferUsage.INDEX ),
					uniform = buffer( "geometry-transform", data.transform, GPUBufferUsage.UNIFORM );
				const instances = data.instances ??
						new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ),
					count = instances.length / 16,
					capacity = instanceCapacity( count ),
					packed = packInstances( instances, undefined, undefined, undefined, paletteOffsets );
				const storage = gpu.createBuffer( {
					label: "geometry-instances",
					size: capacity * 160,
					usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
				} );
				buffers.push( storage );
				if ( instances.byteLength ) {
					gpu.queue.writeBuffer(
						storage,
						0,
						packed.buffer as ArrayBuffer,
						packed.byteOffset,
						packed.byteLength
					);
				}
				let skinBuffer = defaultSkin, boneBuffer = defaultBones;
				let jointMaximum = 0;
				if ( data.joints ) {
					for ( const joint of data.joints ) jointMaximum = Math.max( jointMaximum, joint );
				}
				if ( data.joints && data.weights && data.bones ) {
					const packed = new ArrayBuffer( data.joints.length * 8 ),
						joints = new Uint32Array( packed ),
						weights = new Float32Array( packed );
					for ( let v = 0; v < data.joints.length / 4; v++ ) {
						for ( let c = 0; c < 4; c++ ) {
							joints[v * 8 + c] = data.joints[v * 4 + c]!;
							weights[v * 8 + 4 + c] = data.weights[v * 4 + c]!;
						}
					}
					skinBuffer = buffer( "geometry-skin", joints, GPUBufferUsage.STORAGE );
					if ( paletteOffsets ) {
						palette = sharedPalettes.get( data.bones );
						if ( !palette ) {
							const storage = gpu.createBuffer( {
								label: "geometry-palette",
								size: data.bones.byteLength,
								usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
							} );
							palette = { source: data.bones, buffer: storage, refs: 0, revision: -1 };
							sharedPalettes.set( data.bones, palette );
						}
						palette.refs++;
						boneBuffer = palette.buffer;
					} else boneBuffer = buffer( "geometry-bones", data.bones, GPUBufferUsage.STORAGE );
				}
				if ( paletteOffsets ) {
					if ( !palette ) throw Error( "Palette offsets require skinned geometry" );
					validatePaletteOffsets( paletteOffsets, boneBuffer.size, jointMaximum );
				}
				const mat = data.material;
				if ( mat?.environmentReflection && !environmentImage ) {
					throw Error( "Reflective material requires its owned environment texture" );
				}
				const materialData = new Float32Array( [
						...(mat?.color ?? [ 0.75, 0.78, 0.82, 1 ]),
						mat?.alphaCutoff ?? 0,
						mat?.unlit === false ? 0 : 1,
						mat?.terrain ? 1 : 0,
						mat?.stageFactor ?? 1,
						data.bones ?
							(mat?.sharedPose ?
								-data.bones.length / 16 :
								data.bones.length / 16 / Math.max( 1, count )) :
							0,
						mat?.lightmap ? 1 : 0,
						mat?.water ? 1 : 0,
						mat?.sky ?? 0,
						1,
						1,
						0,
						0,
						mat?.objectLight ?? 1,
						mat?.objectLight !== undefined ? 1 : 0,
						mat?.instanceFade ? 2 : mat?.objectFade ? 1 : 0,
						mat?.fadeAlphaOnly ? 1 : 0,
						mat?.fogDisabled ? 1 : 0,
						mat?.fog ? 1 : 0,
						mat?.textureAlpha === false ? 1 : 0,
						mat?.instanceMaterialTint ? 1 : 0,
						...(mat?.fog ?
							[
								((mat.fog.color >>> 16) & 255) / 255,
								((mat.fog.color >>> 8) & 255) / 255,
								(mat.fog.color & 255) / 255,
								mat.fog.nearPlane,
								mat.fog.farPlane,
								mat.fog.intensity,
								0,
								0
							] :
							[ 0, 0, 0, 0, 0, 0, 0, 0 ]),
						...(mat?.ambient ?? [ 1, 1, 1 ]),
						mat?.surfaceAlpha ? 1 : 0,
						1,
						0,
						0,
						0,
						0,
						1,
						0,
						0,
						mat?.environmentReflection ? 1 : 0,
						mat?.alphaCompare ?? 7,
						0,
						mat?.water && environmentImage ? 1 : 0,
						0,
						0,
						0,
						0,
						0,
						0,
						0,
						0,
						...packTextureStage( mat?.textureStage ),
						// The stage policy: x, DIFFUSE is the BSR shader's oD0.
						mat?.shaderDiffuse ? 1 : 0,
						0,
						0,
						0,
						// D3DRS_TEXTUREFACTOR, read by a stage's TFACTOR argument.
						...(mat?.textureFactor ?? [ 1, 1, 1, 1 ])
					] ),
					material = buffer( "geometry-material", materialData, GPUBufferUsage.UNIFORM );
				const selected = pipelines( geometryPipelineState( mat ) ),
					clampedSampling = !!(mat?.lightmap || mat?.decal) && !mat?.groundDecal;
				const binding = geometryBinding(
					data.world ? worldUniform! : uniform,
					storage,
					material,
					selected,
					clampedSampling,
					image,
					skinBuffer,
					boneBuffer,
					environmentImage
				);
				const draw = new DeviceDraw(
					{
						deferredParticle: mat?.deferredParticle,
						blended: data.material?.sky ? false : data.material?.blend ?? false,
						pipeline: selected,
						vertices,
						indices,
						count: data.indices.length
					},
					binding,
					capacity,
					count
				);
				geometryBuffers.set( draw, buffers );
				metadata.set( draw, {
					water: !!mat?.water,
					state: geometryPipelineState( mat ),
					uniform: data.world ? worldUniform! : uniform,
					material,
					image,
					environmentImage,
					...(data.dynamicVertices ? { vertices: interleaved } : {}),
					skin: skinBuffer,
					bones: boneBuffer,
					boneBytes: boneBuffer.size,
					indexBytes: indices.size,
					palette,
					jointMaximum,
					clampedSampling,
					textureFactorOffset: materialData.byteLength - 16,
					selection: draw
				} );
				return draw;
			} catch ( error ) {
				releasePalette( palette );
				for ( const item of buffers ) {
					item.destroy();
				}
				throw error;
			} finally {
				gpu.popErrorScope().then( error => {
					if ( error ) {
						fail( error.message );
					}
				} ).catch( fail );
			}
		},
		/*
		================
		releasedDraw
		================
		*/
		releasedDraw( draw: GeometryDraw ) {
			return releasedDraws.get( draw );
		},
		/*
		================
		release
		================
		*/
		release( draw: GeometryDraw ) {
			// The releasing stack names the owner if another one still lists it.
			// Keep the first record: a repeated release must not hide the original.
			if ( !releasedDraws.has( draw ) ) {
				releasedDraws.set( draw, {
					atMs: performance.now(),
					stack: new Error( "geometry release" ).stack ?? ""
				} );
			}
			shadows?.forget( draw );
			for ( const buffer of geometryBuffers.get( draw ) ?? [] ) {
				retire( buffer );
			}
			releasePalette( metadata.get( draw )?.palette );
			particles?.release( draw );
			geometryBuffers.delete( draw );
			metadata.delete( draw );
		}
	} );
	return {
		commands,
		ready: Promise.all( [ animation?.ready, particles?.ready ] ),
		/*
		================
		prepare
		================
		*/
		prepare( encoder: GPUCommandEncoder, timing?: import("../internal/gpu-contract").GpuTimingFrame ) {
			animation?.encode( encoder, timing );
			particles?.encode( encoder, timing );
			shadows?.encode( encoder );
		},
		/*
		================
		textureOptions
		================
		*/
		textureOptions( nextFiltered: boolean, nextDetail: number, nextAnisotropic = anisotropic ) {
			if ( filtered === nextFiltered && detail === nextDetail && anisotropic === nextAnisotropic ) return;
			if ( !Number.isInteger( nextDetail ) || nextDetail < 0 || nextDetail > 2 ) {
				throw Error( "Invalid texture detail" );
			}
			filtered = nextFiltered;
			detail = nextDetail;
			anisotropic = nextAnisotropic;
			if ( !sampling ) throw Error( "Texture settings capability unavailable" );
			worldSampler = sampling( filtered, detail, anisotropic );
			if ( lightmapSampling ) lightmapSampler = lightmapSampling( anisotropic );
			for ( const [draw, meta] of metadata ) {
				DeviceDraw.rebind(
					meta.selection,
					geometryBinding(
						meta.uniform,
						geometryBuffers.get( draw )![3]!,
						meta.material,
						draw.pipeline,
						meta.clampedSampling,
						meta.image,
						meta.skin,
						meta.bones,
						meta.environmentImage
					)
				);
			}
		},
		/*
		================
		worldView
		================
		*/
		worldView( transform: Float32Array ) {
			current().queue.writeBuffer( worldUniform, 0, transform.buffer as ArrayBuffer, transform.byteOffset, 64 );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			waterReflection.dispose();
			shadows?.dispose();
			animation?.dispose();
			particles?.dispose();
			for ( const buffers of geometryBuffers.values() ) {
				for ( const buffer of buffers ) {
					buffer.destroy();
				}
			}
			for ( const palette of sharedPalettes.values() ) palette.buffer.destroy();
			sharedPalettes.clear();
			geometryBuffers.clear();
			metadata.clear();
			worldUniform.destroy();
			white.destroy();
			defaultSkin.destroy();
			defaultBones.destroy();
		}
	};
}
