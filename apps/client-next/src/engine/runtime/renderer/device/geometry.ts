import { packTextureStage } from "@/engine/foundation/rendering/texture-stage";
import { createCharacterShadows } from "./character-shadows";
import type { createGpuAnimationResources } from "./animation";
import type { Geometry } from "@/engine/contracts/geometry";
import { packGeometryVertices } from "@/engine/foundation/rendering/geometry-vertices";
import type { GeometryCommands, GeometryDraw, ImageDraw } from "@/engine/runtime/renderer/internal/gpu-contract";
export function createGeometryResources(
	created: GPUDevice,
	current: () => GPUDevice,
	fail: ( error: unknown ) => void,
	pipelines: () => GPURenderPipeline[],
	texture: ( image: ImageDraw ) => GPUTexture,
	worldSampler: GPUSampler,
	lightmapSampler: GPUSampler,
	environment: GPUBuffer,
	sampling?: ( filtered: boolean, detail: number ) => GPUSampler,
	animation?: ReturnType<typeof createGpuAnimationResources>,
	format: GPUTextureFormat = "rgba8unorm"
) {
	let filtered = true, detail = 2, mixedCpuUploadBytes = 0;
	const geometryBuffers = new Map<GeometryDraw, GPUBuffer[]>();
	const worldUniform = created.createBuffer( { size: 64, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST } );
	const white = created.createTexture( {
		size: [ 1, 1 ],
		format: "rgba8unorm",
		usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST
	} );
	created.queue.writeTexture( { texture: white }, Uint8Array.of( 255, 255, 255, 255 ), { bytesPerRow: 4 }, [ 1, 1 ] );
	const instanceCapacity = ( count: number ) => 2 ** Math.ceil( Math.log2( Math.max( 1, count ) ) );
	// writeBuffer copies its source bytes before returning. One device-owned
	// scratch stream can serve every synchronous update without per-draw storage.
	let instanceScratch = new Float32Array( 0 );
	function packInstances(
		instances: Float32Array,
		opacity?: Float32Array,
		appearance?: Float32Array,
		pointLights?: Float32Array,
		paletteOffsets?: Uint32Array
	) {
		const count = instances.length / 16;
		if (
			!Number.isInteger( count ) ||
			opacity && (opacity.length !== count || !opacity.every( v => Number.isFinite( v ) && v >= 0 && v <= 1 ))
		) throw new Error( "Invalid instance opacity" );
		if ( pointLights && (pointLights.length !== count * 12 || !pointLights.every( Number.isFinite )) ) {
			throw Error( "Invalid point light stream" );
		}
		if ( appearance && (appearance.length !== count * 8 || !appearance.every( Number.isFinite )) ) {
			throw new Error( "Invalid instance appearance" );
		}
		if ( paletteOffsets && (paletteOffsets.length !== count || paletteOffsets.some( v => v >= 16777216 )) ) {
			throw Error( "Invalid palette offsets" );
		}
		if ( instanceScratch.length < count * 40 ) instanceScratch = new Float32Array( instanceCapacity( count ) * 40 );
		const packed = instanceScratch;
		for ( let i = 0; i < count; i++ ) {
			const offset = i * 40;
			for ( let j = 0; j < 16; j++ ) packed[offset + j] = instances[i * 16 + j]!;
			for ( let j = 0; j < 12; j++ ) packed[offset + 28 + j] = pointLights?.[i * 12 + j] ?? 0;
			packed[offset + 16] = opacity?.[i] ?? 1;
			packed[offset + 17] = paletteOffsets?.[i] ?? 0;
			packed[offset + 18] = paletteOffsets ? 1 : 0;
			packed[offset + 19] = 0;
			if ( appearance ) { for ( let j = 0; j < 8; j++ ) packed[offset + 20 + j] = appearance[i * 8 + j]!; }
			else {
				packed.fill( 1, offset + 20, offset + 26 );
				packed[offset + 26] = packed[offset + 27] = 0;
			}
		}
		return packed.subarray( 0, count * 40 );
	}
	const defaultSkin = created.createBuffer( { size: 32, usage: GPUBufferUsage.STORAGE } ),
		defaultBones = created.createBuffer( { size: 64, usage: GPUBufferUsage.STORAGE } );
	type SharedPalette = { source: Float32Array; buffer: GPUBuffer; refs: number; revision: number; };
	const sharedPalettes = new Map<Float32Array, SharedPalette>();
	function releasePalette( palette: SharedPalette | undefined ) {
		if ( palette && !--palette.refs ) {
			animation?.release( palette.source );
			palette.buffer.destroy();
			sharedPalettes.delete( palette.source );
		}
	}
	function validatePaletteOffsets(
		offsets: Uint32Array | undefined,
		bones: GPUBuffer | undefined,
		jointMaximum: number
	) {
		if ( !offsets || !bones || offsets.some( offset => offset + jointMaximum >= bones.size / 64 ) ) {
			throw Error( "Palette offset outside bone storage" );
		}
	}
	const geometryBinding = (
		uniform: GPUBuffer,
		storage: GPUBuffer,
		material: GPUBuffer,
		pipeline: GPURenderPipeline,
		image?: ImageDraw,
		skin = defaultSkin,
		bones = defaultBones,
		environmentImage?: ImageDraw
	) => current().createBindGroup( {
		layout: pipeline.getBindGroupLayout( 0 ),
		entries: [
			{ binding: 0, resource: { buffer: uniform } },
			{ binding: 1, resource: { buffer: storage } },
			{ binding: 2, resource: { buffer: material } },
			{
				binding: 3,
				resource: pipeline === pipelines()[6] || pipeline === pipelines()[7] || pipeline === pipelines()[14] ||
						pipeline === pipelines()[15] ?
					lightmapSampler :
					worldSampler
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
		uniform: GPUBuffer;
		material: GPUBuffer;
		image?: ImageDraw;
		environmentImage?: ImageDraw;
		vertices?: Float32Array;
		skin: GPUBuffer;
		bones: GPUBuffer;
		palette?: SharedPalette;
		jointMaximum: number;
		selection: { indexCount: number; instanceCount: number; binding: GPUBindGroup; };
	}>();
	let shadows: ReturnType<typeof createCharacterShadows> | undefined;
	const commands: GeometryCommands = Object.freeze( {
		characterShadows(
			requests: readonly import("../internal/gpu-contract").CharacterShadowRequest[],
			blob?: ImageDraw
		) {
			if ( !shadows && !requests.length ) return [];
			shadows ??= createCharacterShadows( current(), worldUniform, texture, draw => {
				const meta = metadata.get( draw ), buffers = geometryBuffers.get( draw );
				return meta && buffers ?
					{ instances: buffers[3]!, material: meta.material, skin: meta.skin, bones: meta.bones } :
					undefined;
			}, format );
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
		updateBones( draw: GeometryDraw, bones: Float32Array, revision?: number ) {
			const meta = metadata.get( draw );
			if ( !meta || meta.bones === defaultBones || bones.byteLength > meta.bones.size ) {
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
		updateIndices( draw: GeometryDraw, indices: Uint32Array ) {
			const gpu = current(), buffers = geometryBuffers.get( draw );
			if ( !buffers || indices.byteLength > buffers[1]!.size ) {
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
			const selection = metadata.get( draw )!.selection;
			selection.indexCount = indices.length;
			selection.instanceCount = 1;
		},
		updatePositions(
			draw: GeometryDraw,
			positions: Float32Array,
			colors?: Float32Array,
			uvs?: Float32Array,
			ranges?: readonly (readonly [number, number])[]
		) {
			const gpu = current(), meta = metadata.get( draw ), count = positions.length / 3;
			if ( !meta ) throw Error( "Invalid position update" );
			const vertices = meta.vertices;
			if ( !vertices ) throw Error( "Geometry was uploaded without dynamicVertices" );
			if (
				count !== vertices.length / 14 || colors && colors.length !== count * 4 ||
				uvs && uvs.length !== count * 2
			) throw Error( "Invalid position update" );
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
					offset,
					vertices.buffer as ArrayBuffer,
					vertices.byteOffset + offset,
					(end - start) * 56
				);
			}
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
			if ( meta.palette ) validatePaletteOffsets( paletteOffsets, meta.bones, meta.jointMaximum );
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
				const selection = metadata.get( draw )!.selection;
				selection.indexCount = draw.count;
				selection.instanceCount = count;
				return draw;
			}
			gpu.pushErrorScope( "validation" );
			try {
				if ( count > draw.instanceCapacity ) {
					const capacity = instanceCapacity( count ),
						storage = gpu.createBuffer( {
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
						const selection = metadata.get( draw )!.selection;
						selection.binding = geometryBinding(
							metadata.get( draw )!.uniform,
							storage,
							metadata.get( draw )!.material,
							draw.pipeline,
							metadata.get( draw )!.image,
							metadata.get( draw )!.skin,
							metadata.get( draw )!.bones,
							metadata.get( draw )!.environmentImage
						);
						const replacement = Object.freeze( {
							...draw,
							get indexCount() {
								return selection.indexCount;
							},
							get instanceCount() {
								return selection.instanceCount;
							},
							get binding() {
								return selection.binding;
							},
							instanceCapacity: capacity
						} );
						buffers[3]!.destroy();
						buffers[3] = storage;
						geometryBuffers.delete( draw );
						geometryBuffers.set( replacement, buffers );
						metadata.set( replacement, metadata.get( draw )! );
						metadata.delete( draw );
						draw = replacement;
					} catch ( error ) {
						storage.destroy();
						throw error;
					}
				}
				const selection = metadata.get( draw )!.selection;
				selection.indexCount = draw.count;
				selection.instanceCount = count;
				return draw;
			} finally {
				gpu.popErrorScope().then( error => {
					if ( error ) {
						fail( error.message );
					}
				} ).catch( fail );
			}
		},
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
		updateTextureTransform( draw: GeometryDraw, matrix: Float32Array ) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Unknown geometry draw" );
			if ( matrix.length !== 8 || !matrix.every( Number.isFinite ) ) throw Error( "Invalid texture transform" );
			current().queue.writeBuffer( meta.material, 144, matrix as Float32Array<ArrayBuffer> );
		},
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
		upload( data: Geometry, image?: ImageDraw, paletteOffsets?: Uint32Array, environmentImage?: ImageDraw ) {
			const gpu = current(), buffers: GPUBuffer[] = [];
			let palette: SharedPalette | undefined;
			gpu.pushErrorScope( "validation" );
			const buffer = ( data: Float32Array | Uint32Array, usage: number ) => {
				const result = gpu.createBuffer( { size: data.byteLength, usage: usage | GPUBufferUsage.COPY_DST } );
				buffers.push( result );
				gpu.queue.writeBuffer( result, 0, data.buffer as ArrayBuffer, data.byteOffset, data.byteLength );
				return result;
			};
			try {
				const interleaved = packGeometryVertices( data );
				const vertices = buffer( interleaved, GPUBufferUsage.VERTEX ),
					indices = buffer( data.indices, GPUBufferUsage.INDEX ),
					uniform = buffer( data.transform, GPUBufferUsage.UNIFORM );
				const instances = data.instances ??
						new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ),
					count = instances.length / 16,
					capacity = instanceCapacity( count ),
					packed = packInstances( instances, undefined, undefined, undefined, paletteOffsets );
				const storage = gpu.createBuffer( {
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
					skinBuffer = buffer( joints, GPUBufferUsage.STORAGE );
					if ( paletteOffsets ) {
						palette = sharedPalettes.get( data.bones );
						if ( !palette ) {
							const storage = gpu.createBuffer( {
								size: data.bones.byteLength,
								usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
							} );
							palette = { source: data.bones, buffer: storage, refs: 0, revision: -1 };
							sharedPalettes.set( data.bones, palette );
						}
						palette.refs++;
						boneBuffer = palette.buffer;
					} else boneBuffer = buffer( data.bones, GPUBufferUsage.STORAGE );
				}
				if ( paletteOffsets ) {
					if ( !palette ) throw Error( "Palette offsets require skinned geometry" );
					validatePaletteOffsets( paletteOffsets, boneBuffer, jointMaximum );
				}
				const mat = data.material;
				if ( mat?.environmentReflection && !environmentImage ) {
					throw Error( "Reflective material requires its owned environment texture" );
				}
				const material = buffer(
					new Float32Array( [
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
						mat?.textureAlphaSquared ? 1 : 0,
						0,
						0,
						0,
						0,
						0,
						0,
						0,
						0,
						0,
						...packTextureStage( mat?.textureStage )
					] ),
					GPUBufferUsage.UNIFORM
				);
				const selected = pipelines()[
					mat?.groundDecal ?
						44 + (mat.doubleSided ? 0 : 1) :
						(mat?.deferredParticle ? 22 : 0) + (mat?.multiplyAddBlend ?
							20 :
							mat?.inverseSourceColorBlend ?
							18 :
							mat?.sourceColorBlend ?
							16 :
							mat?.decal ?
							14 :
							mat?.sky ?
							(mat.sky === 2 ? 10 : 8) :
							mat?.depthWrite === true ?
							12 :
							mat?.depthWrite === false ?
							(mat.additive ? 4 : 2) :
							(mat?.objectFade || mat?.instanceFade) ?
							12 :
							mat?.lightmap ?
							6 :
							mat?.additive ?
							4 :
							mat?.blend ?
							2 :
							0) +
						(mat && !mat.doubleSided ? 1 : 0)
				]!;
				const binding = geometryBinding(
					data.world ? worldUniform! : uniform,
					storage,
					material,
					selected,
					image,
					skinBuffer,
					boneBuffer,
					environmentImage
				);
				const selection = { indexCount: data.indices.length, instanceCount: count, binding };
				const draw = Object.freeze( {
					deferredParticle: mat?.deferredParticle,
					blended: data.material?.sky ? false : data.material?.blend ?? false,
					pipeline: selected,
					get binding() {
						return selection.binding;
					},
					vertices,
					indices,
					instanceCapacity: capacity,
					count: data.indices.length,
					get indexCount() {
						return selection.indexCount;
					},
					get instanceCount() {
						return selection.instanceCount;
					}
				} );
				geometryBuffers.set( draw, buffers );
				metadata.set( draw, {
					uniform: data.world ? worldUniform! : uniform,
					material,
					image,
					environmentImage,
					...(data.dynamicVertices ? { vertices: interleaved } : {}),
					skin: skinBuffer,
					bones: boneBuffer,
					palette,
					jointMaximum,
					selection
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
		release( draw: GeometryDraw ) {
			for ( const buffer of geometryBuffers.get( draw ) ?? [] ) {
				buffer.destroy();
			}
			releasePalette( metadata.get( draw )?.palette );
			geometryBuffers.delete( draw );
			metadata.delete( draw );
		}
	} );
	return {
		commands,
		ready: animation?.ready ?? Promise.resolve(),
		prepare( encoder: GPUCommandEncoder, timing?: import("../internal/gpu-contract").GpuTimingFrame ) {
			animation?.encode( encoder, timing );
			shadows?.encode( encoder );
		},
		textureOptions( nextFiltered: boolean, nextDetail: number ) {
			if ( filtered === nextFiltered && detail === nextDetail ) return;
			if ( !Number.isInteger( nextDetail ) || nextDetail < 0 || nextDetail > 2 ) {
				throw Error( "Invalid texture detail" );
			}
			filtered = nextFiltered;
			detail = nextDetail;
			if ( !sampling ) throw Error( "Texture settings capability unavailable" );
			worldSampler = sampling( filtered, detail );
			for ( const [draw, meta] of metadata ) {
				meta.selection.binding = geometryBinding(
					meta.uniform,
					geometryBuffers.get( draw )![3]!,
					meta.material,
					draw.pipeline,
					meta.image,
					meta.skin,
					meta.bones,
					meta.environmentImage
				);
			}
		},
		worldView( transform: Float32Array ) {
			current().queue.writeBuffer( worldUniform, 0, transform.buffer as ArrayBuffer, transform.byteOffset, 64 );
		},
		dispose() {
			shadows?.dispose();
			animation?.dispose();
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
