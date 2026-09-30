/*
===========================================================================

character.ts - decodes a character GLB into a CharacterSource

Interprets the glTF nodes, skins, meshes and clips of an admitted GLB under
the structural budgets. Embedded PNGs stay encoded here; the loader turns
them into ImageBitmaps and hands the page only their sizes.

===========================================================================
*/
import { NATIVE_TEXTURE_MIME } from "@/engine/foundation/assets/native-texture";
import { validateEquipmentGlows } from "@/engine/foundation/rendering/equipment-glow";
import { sceneryMaterial, type SceneryModifiers } from "@/engine/foundation/rendering/scenery-modifiers";
import { NATIVE_CHARACTER_LIGHTING } from "@/engine/foundation/rendering/video-options";
import { CHARACTER_MODEL_BYTES } from "@/engine/foundation/animation/character-budget";
import type { ModelDocument } from "@/engine/contracts/model";
import type {
	CharacterSource,
	CharacterNode,
	CharacterChannel,
	CharacterPrimitive
} from "@/engine/contracts/character";
import { identity } from "@/engine/foundation/rendering/world-math";
interface Document {
	extras?: {
		sroEquipmentGlows?: Record<
			string,
			readonly import("@/engine/foundation/rendering/equipment-glow").EquipmentGlow[]
		>;
	};
	nodes: {
		name?: string;
		children?: number[];
		translation?: number[];
		rotation?: number[];
		scale?: number[];
		matrix?: number[];
		mesh?: number;
		skin?: number;
	}[];
	meshes: {
		name?: string;
		primitives: {
			mode?: number;
			attributes: Record<string, number>;
			indices: number;
			material?: number;
		}[];
	}[];
	skins?: {
		joints: number[];
		inverseBindMatrices?: number;
	}[];
	accessors: {
		bufferView?: number;
		byteOffset?: number;
		componentType: number;
		count: number;
		type: string;
		normalized?: boolean;
		sparse?: unknown;
	}[];
	bufferViews: {
		byteOffset?: number;
		byteLength: number;
		byteStride?: number;
	}[];
	materials?: {
		extras?: {
			sroMaterialIndex?: number;
			sroBsrModifiers?: SceneryModifiers;
			sroAmbientFactor?: number[];
			sroFadeAlphaOnly?: boolean;
			sroUnlit?: boolean;
			sroEnvironment?: { textureId: number; mode: number; };
			sroEnvironmentTexture?: number;
		};
		name?: string;
		doubleSided?: boolean;
		alphaMode?: string;
		alphaCutoff?: number;
		pbrMetallicRoughness?: {
			baseColorFactor?: number[];
			baseColorTexture?: {
				index: number;
			};
		};
	}[];
	textures?: {
		source: number;
	}[];
	images?: {
		bufferView?: number;
		mimeType?: string;
		uri?: string;
	}[];
	animations?: {
		name?: string;
		samplers: {
			input: number;
			output: number;
			interpolation?: string;
		}[];
		channels: {
			sampler: number;
			target: {
				node: number;
				path: string;
			};
		}[];
	}[];
}
export function createCharacterDecoder() {
	return {
		decode( document: ModelDocument ): CharacterSource {
			const j = document.json as unknown as Document, buffer = new DataView( document.binary );
			// Native particle-only BSRs have a skeleton but no mesh section.
			// glTF omits meshes entirely for these valid attachment resources.
			const meshes = j.meshes ?? [];
			if (
				!Array.isArray( j.nodes ) || j.nodes.length > 1023 || !Array.isArray( meshes ) || meshes.length > 256
			) {
				throw new Error( "Character node/mesh budget exceeded" );
			}
			let expanded = 0;
			function reserve( bytes: number ) {
				expanded += bytes;
				if ( !Number.isSafeInteger( bytes ) || bytes < 0 || expanded > CHARACTER_MODEL_BYTES ) {
					throw new Error( "Character expansion exceeds decoded byte budget" );
				}
			}
			function accessor( index: number, width: number ): Float32Array {
				const a = j.accessors[index], v = a?.bufferView === undefined ? undefined : j.bufferViews[a.bufferView];
				if (
					!a || !v || a.sparse ||
					({ SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4, MAT4: 16 } as Record<string, number>)[a.type] !== width ||
					a.count * width > 8000000
				) {
					throw new Error( "Unsupported character accessor" );
				}
				const size = ({ 5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4 } as Record<number, number>)[
					a.componentType
				];
				if ( !size ) {
					throw new Error( "Unsupported character component" );
				}
				reserve( a.count * width * 4 );
				const result = new Float32Array( a.count * width ),
					start = (v.byteOffset ?? 0) + (a.byteOffset ?? 0),
					stride = v.byteStride ?? width * size;
				for ( let n = 0; n < a.count; n++ ) {
					for ( let c = 0; c < width; c++ ) {
						const at = start + n * stride + c * size;
						let value = a.componentType === 5126 ?
							buffer.getFloat32( at, true ) :
							a.componentType === 5125 ?
							buffer.getUint32( at, true ) :
							a.componentType === 5123 ?
							buffer.getUint16( at, true ) :
							a.componentType === 5122 ?
							buffer.getInt16( at, true ) :
							a.componentType === 5121 ?
							buffer.getUint8( at ) :
							buffer.getInt8( at );
						if ( a.normalized ) {
							value = a.componentType === 5121 ?
								value / 255 :
								a.componentType === 5123 ?
								value / 65535 :
								Math.max( -1, value / (a.componentType === 5120 ? 127 : 32767) );
						}
						if ( !Number.isFinite( value ) ) {
							throw new Error( "Non-finite character accessor" );
						}
						result[n * width + c] = value;
					}
				}
				return result;
			}
			const parents = new Int32Array( j.nodes.length ).fill( -1 );
			for ( let n = 0; n < j.nodes.length; n++ ) {
				for ( const child of j.nodes[n]!.children ?? [] ) {
					if ( !Number.isInteger( child ) || child < 0 || child >= parents.length || parents[child] !== -1 ) {
						throw new Error( "Invalid character hierarchy" );
					}
					parents[child] = n;
				}
			}
			for ( let n = 0; n < parents.length; n++ ) {
				let p = n, depth = 0;
				while ( p !== -1 ) {
					if ( ++depth > parents.length ) {
						throw new Error( "Cyclic character hierarchy" );
					}
					p = parents[p]!;
				}
			}
			const nodes: CharacterNode[] = j.nodes.map( ( node, i ) => ({
				name: node.name ?? String( i ),
				parent: parents[i]!,
				translation: node.translation ?? [ 0, 0, 0 ],
				rotation: node.rotation ?? [ 0, 0, 0, 1 ],
				scale: node.scale ?? [ 1, 1, 1 ],
				matrix: node.matrix
			}) );
			const primitives: CharacterPrimitive[] = [];
			for ( let n = 0; n < j.nodes.length; n++ ) {
				const node = j.nodes[n]!;
				if ( node.mesh === undefined ) {
					continue;
				}
				const mesh = meshes[node.mesh];
				if ( !mesh ) {
					throw new Error( "Invalid character mesh reference" );
				}
				const skin = node.skin === undefined ? undefined : j.skins?.[node.skin];
				if ( node.skin !== undefined && !skin ) {
					throw new Error( "Invalid character skin" );
				}
				const joints = skin?.joints ?? [ n ];
				if ( !joints.length || joints.length > 512 || joints.some( i => !nodes[i] ) ) {
					throw new Error( "Invalid character joints" );
				}
				if ( skin?.inverseBindMatrices === undefined ) {
					reserve( joints.length * 16 * 4 );
				}
				const inverseBind = skin?.inverseBindMatrices === undefined ?
					new Float32Array( joints.flatMap( () => Array.from( identity() ) ) ) :
					accessor( skin.inverseBindMatrices, 16 );
				if ( inverseBind.length !== joints.length * 16 ) {
					throw new Error( "Invalid inverse bind count" );
				}
				for ( const p of mesh.primitives ) {
					if ( primitives.length >= 1024 ) {
						throw new Error( "Character primitive budget exceeded" );
					}
					if ( p.mode !== undefined && p.mode !== 4 ) {
						throw new Error( "Unsupported character primitive" );
					}
					const positions = accessor( p.attributes.POSITION!, 3 ), count = positions.length / 3;
					// Account for copies and generated attributes before allocation as well as decoded accessors.
					reserve(
						count * 4 * 4 + (skin ? 0 : count * 4 * 4) +
							(p.attributes.NORMAL === undefined ? count * 3 * 4 : 0) +
							(p.attributes.TEXCOORD_0 === undefined ? count * 2 * 4 : 0) + 64
					);
					reserve( (j.accessors[p.indices]?.count ?? 0) * 4 );
					const weights = skin ? accessor( p.attributes.WEIGHTS_0!, 4 ) : new Float32Array( count * 4 ),
						indices = new Uint32Array( accessor( p.indices, 1 ) );
					const jointIndices = skin ?
						new Uint32Array( accessor( p.attributes.JOINTS_0!, 4 ) ) :
						new Uint32Array( count * 4 );
					if (
						weights.some( value => value < 0 ) || weights.length !== count * 4 ||
						jointIndices.length !== count * 4 || jointIndices.some( i => i >= joints.length ) ||
						indices.some( i => i >= count )
					) {
						throw new Error( "Invalid character vertex bindings" );
					}
					for ( let i = 0; i < count; i++ ) {
						if ( !skin ) {
							weights[i * 4] = 1;
						}
						const sum = weights[i * 4]! + weights[i * 4 + 1]! + weights[i * 4 + 2]! + weights[i * 4 + 3]!;
						if ( sum <= 0 ) {
							throw new Error( "Unweighted character vertex" );
						}
						for ( let c = 0; c < 4; c++ ) {
							weights[i * 4 + c]! /= sum;
						}
					}
					const mat = p.material === undefined ? undefined : j.materials?.[p.material],
						texture = mat?.pbrMetallicRoughness?.baseColorTexture?.index;
					// The exporter maps BMT texture-alpha bit 0x200 to MASK.
					// Opaque garment alpha can carry sheen, not coverage: keep
					// that admission policy when the actor enters a fade batch.
					const color = mat?.pbrMetallicRoughness?.baseColorFactor ?? [ 1, 1, 1, 1 ];
					const ambient = mat?.extras?.sroAmbientFactor ?? [ 1, 1, 1, 1 ];
					const reflection = mat?.extras?.sroEnvironment,
						reflectionTexture = mat?.extras?.sroEnvironmentTexture;
					if (
						reflection && (reflection.mode !== 1 || ![ 0, 1 ].includes( reflection.textureId ))
					) throw Error( "Unsupported character environment reflection" );
					const environmentImage = reflectionTexture === undefined ?
						undefined :
						j.textures?.[reflectionTexture]?.source;
					if (
						reflection && reflection.textureId !== 0xffffffff &&
						(environmentImage === undefined || !j.images?.[environmentImage])
					) throw Error( "Missing character environment texture" );
					if ( ambient.length !== 4 || !ambient.every( Number.isFinite ) ) {
						throw Error(
							"Invalid character ambient material"
						);
					}
					// A9EC10 initializes actor ambient to .6; A5D450 uploads it to c10.
					// Ordinary A5E0E0 subsets replace only c11 with BMT diffuse.
					// Compatibility mode restores the previous BMT ambient input.
					const actorAmbient: [number, number, number] = NATIVE_CHARACTER_LIGHTING ?
						[ .6, .6, .6 ] :
						[ ambient[0]!, ambient[1]!, ambient[2]! ];
					// vss2
					// computes directional skin lighting before raster interpolation.
					// A5D450 selects MODULATE2X after that clamp, also used by
					// A91970 for ordinary CRT materials (not a brightness tweak).
					primitives.push( {
						name: node.name ?? mesh.name ?? String( primitives.length ),
						node: n,
						joints,
						inverseBind,
						environmentImage,
						image: texture === undefined ? -1 : j.textures?.[texture]?.source ?? -1,
						geometry: {
							positions,
							indices,
							normals: p.attributes.NORMAL === undefined ?
								new Float32Array( count * 3 ) :
								accessor( p.attributes.NORMAL, 3 ),
							uvs: p.attributes.TEXCOORD_0 === undefined ?
								new Float32Array( count * 2 ) :
								accessor( p.attributes.TEXCOORD_0, 2 ),
							joints: jointIndices,
							weights,
							transform: identity(),
							material: {
								environmentReflection: !!reflection && reflection.textureId !== 0xffffffff,
								stageFactor: 2,
								objectLight: 1,
								ambient: actorAmbient,
								color: [ color[0]!, color[1]!, color[2]!, color[3]! ],
								alphaCutoff: mat?.alphaMode === "MASK" ? mat.alphaCutoff ?? 0.5 : 0,
								blend: mat?.alphaMode === "BLEND",
								textureAlpha: mat?.alphaMode === "MASK" || mat?.alphaMode === "BLEND",
								fadeAlphaOnly: mat?.extras?.sroFadeAlphaOnly ??
									(mat?.alphaMode !== "MASK" && mat?.alphaMode !== "BLEND"),
								doubleSided: mat?.doubleSided ?? false,
								unlit: mat?.extras?.sroUnlit ?? false
							}
						}
					} );
				}
			}
			// Apply each original BMT-indexed override before model admission. GLB
			// primitive/material indices are exporter grouping identities only.
			let primitiveIndex = 0;
			for ( const node of j.nodes ) {
				if ( node.mesh !== undefined ) {
					for ( const p of meshes[node.mesh]!.primitives ) {
						const primitive = primitives[primitiveIndex++]!,
							mat = p.material === undefined ? undefined : j.materials?.[p.material];
						if ( !mat?.extras?.sroBsrModifiers ) continue;
						const index = mat.extras.sroMaterialIndex;
						if ( !Number.isSafeInteger( index ) || index! < 0 ) {
							throw Error(
								"Missing native BMT material index"
							);
						}
						primitives[primitiveIndex - 1] = {
							...primitive,
							modifierSource: {
								material: primitive.geometry.material!,
								index: index!,
								modifiers: mat.extras.sroBsrModifiers
							},
							geometry: {
								...primitive.geometry,
								material: sceneryMaterial(
									primitive.geometry.material!,
									mat.extras.sroBsrModifiers,
									index!,
									message => {
										throw Error( message );
									}
								)
							}
						};
					}
				}
			}
			const clips = (j.animations ?? []).map( ( clip, i ) => {
				const channels: CharacterChannel[] = clip.channels.map( channel => {
					const sampler = clip.samplers[channel.sampler], path = channel.target.path;
					if (
						!sampler || !nodes[channel.target.node] ||
						![ "translation", "rotation", "scale" ].includes( path )
					) {
						throw new Error( "Unsupported character animation channel" );
					}
					const times = accessor( sampler.input, 1 ),
						values = accessor( sampler.output, path === "rotation" ? 4 : 3 ),
						interpolation = sampler.interpolation ?? "LINEAR";
					if (
						![ "LINEAR", "STEP", "CUBICSPLINE" ].includes( interpolation ) || !times.length ||
						times.some( ( t, i ) => t < 0 || i > 0 && t <= times[i - 1]! ) ||
						values.length !==
							times.length * (path === "rotation" ? 4 : 3) * (interpolation === "CUBICSPLINE" ? 3 : 1)
					) {
						throw new Error( "Invalid animation sampler" );
					}
					return {
						node: channel.target.node,
						path: path as CharacterChannel["path"],
						interpolation: interpolation as CharacterChannel["interpolation"],
						times,
						values
					};
				} );
				return {
					name: clip.name ?? String( i ),
					duration: Math.max( 0, ...channels.map( c => c.times[c.times.length - 1]! ) ),
					channels
				};
			} );
			if ( (j.images?.length ?? 0) > 64 ) {
				throw new Error( "Character image count exceeds budget" );
			}
			const images = (j.images ?? []).map( image => {
				const view = image.bufferView === undefined ? undefined : j.bufferViews[image.bufferView];
				if (
					!view || image.uri || (image.mimeType !== "image/png" && image.mimeType !== NATIVE_TEXTURE_MIME)
				) {
					throw new Error( "Character texture must be embedded PNG or native mips" );
				}
				reserve( view.byteLength );
				return {
					bytes: new Uint8Array( document.binary, view.byteOffset ?? 0, view.byteLength ).slice(),
					mime: image.mimeType
				};
			} );
			// Babylon's glTF AUTO import contributes Ry(PI) * Sz(-1), i.e.
			// Sx(-1), below the native placement/forward correction. Preserve
			// that resource-space conversion once for skin, root motion and props.
			const root = nodes.length;
			for ( let i = 0; i < root; i++ ) if ( nodes[i]!.parent < 0 ) nodes[i] = { ...nodes[i]!, parent: root };
			nodes.push( {
				name: "__gltf_left_handed__",
				parent: -1,
				translation: [ 0, 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ -1, 1, 1 ]
			} );
			for ( const primitive of primitives ) {
				const indices = primitive.geometry.indices;
				for ( let i = 0; i < indices.length; i += 3 ) {
					const second = indices[i + 1]!;
					indices[i + 1] = indices[i + 2]!;
					indices[i + 2] = second;
				}
			}
			validateEquipmentGlows( j.extras?.sroEquipmentGlows, images.length );
			return { nodes, primitives, clips, images, equipmentGlows: j.extras?.sroEquipmentGlows };
		}
	};
}
