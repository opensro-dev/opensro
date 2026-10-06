/*
===========================================================================

world.ts - decode a published region bundle into a WorldScene

Runs in the asset worker. Placements are instanced per mesh and material;
terrain blocks become association passes per detail level. Results that
depend only on a resource (mesh checks, materials, radii) are computed
once per resource, since a town places the same meshes hundreds of times.

===========================================================================
*/
import { passes, heightRange, tileUvScale } from "@/engine/foundation/rendering/terrain-associations";
import { clothData } from "@/engine/foundation/animation/cloth";

import { decodeSoundTerrain } from "@/engine/foundation/audio/terrain-sounds";
import { dungeonWaterGroup } from "@/engine/foundation/rendering/dungeon-water";
import { createCharacterPose } from "@/engine/foundation/animation/animation-pose";
import { characterRadius } from "@/engine/foundation/animation/character-bounds";
import { skyGroups } from "@/engine/foundation/rendering/sky-geometry";
import { radians } from "@/engine/foundation/math/angles";
import { admitMesh } from "./mesh-admission/mesh-admission";
import { createWorldResources } from "./resources/resources";
import {
	FRONTEND_SCENE_BYTES,
	FRONTEND_DECODE_BYTES,
	WORLD_SCENE_BYTES,
	WORLD_DECODE_BYTES,
	worldSceneBytes
} from "@/engine/foundation/rendering/world-scene";
import type { WorldScene, WorldGroup, WorldMaterial, TerrainRange } from "@/engine/contracts/scene";
import { identity, mapPlacement } from "@/engine/foundation/rendering/world-math";
import type { Bundle, Material, Mesh, ObjectBranch } from "./internal/resource-contract";
import { sceneryMaterial } from "@/engine/foundation/rendering/scenery-modifiers";
import { sceneryParticles } from "@/engine/foundation/rendering/scenery-particles";
import { worldObjectMaterial } from "@/engine/foundation/rendering/world-material";
import { finiteNumbers } from "@/engine/foundation/rendering/geometry-validation";

/*
================
WorldDecodeOptions

origin is the region whose corner is the scene's coordinate origin. An
outdoor scene keeps one origin (its anchor) across region crossings, so a
region's terrain decoded once stays valid while the player moves on.
part "terrain" decodes only the terrain, lightmap and water of the
bundle's own sectors; "objects" decodes everything else; "all" both.
================
*/
/*
================
WorldDecodePart
================
*/
export type WorldDecodePart = "all" | "objects" | "terrain";
export interface WorldDecodeOptions {
	readonly origin?: number;
	readonly part?: WorldDecodePart;
}

/*
================
createWorldDecoder
================
*/
export function createWorldDecoder( budget = WORLD_DECODE_BYTES ) {
	const resources = createWorldResources();
	return {
		resolve: resources.resolve,
		resolveTerrain: resources.resolveTerrain,
		/*
		================
		decode

		One resolved bundle into a scene: the part options asks for, in the
		coordinates of options.origin (WorldDecodeOptions).
		================
		*/
		decode( bytes: Uint8Array | Bundle, frontend = false, options: WorldDecodeOptions = {} ): WorldScene {
			let reserved = 0;
			/*
================
reserve
================
			*/
			const reserve = ( bytes: number ) => {
				reserved += bytes;
				if ( !Number.isSafeInteger( reserved ) || reserved > (frontend ? FRONTEND_DECODE_BYTES : budget) ) {
					throw new Error(
						`World decode scratch budget exceeded: ${reserved} bytes > ${
							frontend ? FRONTEND_DECODE_BYTES : budget
						} bytes`
					);
				}
			};
			const b = bytes instanceof Uint8Array ?
				JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) ) as Bundle :
				bytes;
			if (
				!b.source || !Number.isInteger( b.source.sectorX ) || !Number.isInteger( b.source.sectorY ) ||
				!b.terrain || !b.objects?.resources
			) throw new Error( "Unsupported world bundle" );
			const region = b.source.sectorX | (b.source.sectorY << 8),
				origin = options.origin ?? region,
				part = options.part ?? "all",
				groups: WorldGroup[] = [],
				warnings: string[] = [];
			// A dungeon is its own coordinate space; only outdoor scenes share an anchor.
			if (
				!Number.isInteger( origin ) || origin < 0 || origin > 0xffff ||
				(region & 0x8000 ? origin !== region : (origin & 0x8000) !== 0)
			) throw new Error( "Invalid world decode origin" );
			if ( part !== "all" && part !== "objects" && part !== "terrain" ) {
				throw new Error( "Invalid world decode part" );
			}
			const objectPart = part !== "terrain", terrainPart = part !== "objects";
			if (
				b.dungeonBlocks &&
				(!(region & 0x8000) || !Array.isArray( b.dungeonBlocks ) || b.dungeonBlocks.length > 4096 ||
					b.dungeonBlocks.some( ( block, index ) =>
						block.index !== index || !Array.isArray( block.visibleBlocks )
					))
			) throw Error( "Invalid dungeon block publication" );
			const refs = new Map( b.objects.resources.bsr.map( row => [ row.objectId, row ] ) ),
				meshes = new Map( b.objects.resources.meshes.map( row => [ row.sourcePath.toLowerCase(), row ] ) ),
				sets = new Map(
					b.objects.resources.materialSets.map( row => [ row.sourcePath.toLowerCase(), row.materials ] )
				);
			const instances = new Map<
					string,
					{
						mesh: Mesh;
						collision: NonNullable<WorldGroup["collision"]>[number][];
						block?: number;
						material: WorldMaterial;
						materialOrder: NonNullable<WorldGroup["materialOrder"]>;
						matrices: number[];
						visibility: NonNullable<WorldGroup["visibility"]>[number][];
						center: number[];
						radius: number;
					}
				>(),
				seen = new Set<string>();
			const animatedByPath = new Map(
					(b.animated ?? []).map( entry => [ entry.sourcePath.toLowerCase(), entry ] )
				),
				animatedPlacements = new Map<
					string,
					{
						orders: { object: string; branch: number; paths: string[]; }[];
						matrices: number[];
						visibility: NonNullable<WorldGroup["visibility"]>[number][];
					}
				>();
			const placementCells = new Map<string, [number, number][]>();
			// Per-resource results. A town places the same meshes and references
			// hundreds of times; everything here depends only on the resource.
			const lowered = new Map<string, string>();
			/*
================
lower
================
			*/
			const lower = ( text: string ): string => {
				let value = lowered.get( text );
				if ( value === undefined ) {
					value = text.toLowerCase();
					lowered.set( text, value );
				}
				return value;
			};
			const meshChecks = new Map<
				Mesh,
				{ valid: boolean; radius: number; material: string; uvs: Float32Array; }
			>();
			/*
================
meshCheck

Cache UV admission with geometry checks so repeated placements share the same
finite conversion of authored missing texture coordinates.
================
			*/
			const meshCheck = ( mesh: Mesh ) => {
				let check = meshChecks.get( mesh );
				if ( !check ) {
					check = admitMesh( mesh );
					meshChecks.set( mesh, check );
				}
				return check;
			};
			const refMaterials = new Map<ObjectBranch, {
				materials: Material[];
				byName: Map<string, { material: Material; index: number; }>;
				key: string;
				set: string;
			}>();
			/*
================
refMaterial
================
			*/
			const refMaterial = ( ref: ObjectBranch ) => {
				let entry = refMaterials.get( ref );
				if ( !entry ) {
					const materials = ref.materialPaths.flatMap( path => sets.get( lower( path ) ) ?? [] );
					const byName = new Map<string, { material: Material; index: number; }>();
					// find() takes the first match and indexOf the first occurrence.
					for ( let i = 0; i < materials.length; i++ ) {
						const name = lower( materials[i]!.name );
						if ( !byName.has( name ) ) {
							byName.set( name, { material: materials[i]!, index: materials.indexOf( materials[i]! ) } );
						}
					}
					const set = ref.materialPaths.join( ";" );
					entry = {
						materials,
						byName,
						set,
						key: `${set}:${
							ref.modifiers &&
								(ref.modifiers.materialModifiers.length || ref.modifiers.textureModifiers.length) ?
								ref.sourcePath :
								""
						}`
					};
					refMaterials.set( ref, entry );
				}
				return entry;
			};
			/*
================
materialFor
================
			*/
			const materialFor = ( ref: ObjectBranch, material: Material, index: number ): WorldMaterial =>
				sceneryMaterial(
					worldObjectMaterial( material ),
					ref.modifiers,
					index,
					message => warnings.push( `${ref.sourcePath}: ${message}` )
				);
			// Every instance used to build its material only to drop it when the group
			// existed. A group still builds its own (no shared nested arrays); other
			// instances only need its warnings, once per reference and material.
			const warned = new Map<ObjectBranch, Set<Material>>();
			/*
================
noteMaterial
================
			*/
			const noteMaterial = ( ref: ObjectBranch, material: Material, index: number ): void => {
				let seenMaterials = warned.get( ref );
				if ( !seenMaterials ) {
					seenMaterials = new Set();
					warned.set( ref, seenMaterials );
				}
				if ( seenMaterials.has( material ) ) return;
				seenMaterials.add( material );
				materialFor( ref, material, index );
			};
			const compoundReady = new Map<ObjectBranch, boolean>(), fadeRadii = new Map<ObjectBranch, number>();
			const placements = objectPart ? b.objects.placements : [];
			for ( const p of placements ) {
				const key = [ Number( p.regionId ), p.objectId, p.uid, p.position.x, p.position.y, p.position.z ].join(
						":"
					),
					source = Number( p.sourceSector?.sectorId ?? p.regionId );
				const cell: [number, number] = [
					((source & 255) - (origin & 255)) * 6 + (p.blockX ?? Math.floor( p.position.x / 320 )),
					((source >>> 8) - (origin >>> 8)) * 6 + (p.blockZ ?? Math.floor( p.position.z / 320 ))
				];
				const cells = placementCells.get( key ) ?? [];
				if ( !cells.some( c => c[0] === cell[0] && c[1] === cell[1] ) ) {
					reserve( 32 );
					cells.push( cell );
				}
				placementCells.set( key, cells );
			}
			const models: Record<string, import("@/engine/contracts/character").CharacterModel> = {},
				scenery: import("@/engine/contracts/scenery").SceneryEmitter[] = [];
			for ( const p of placements ) {
				const home = Number( p.regionId ),
					key = `${home}:${p.objectId}:${p.uid}:${p.position.x}:${p.position.y}:${p.position.z}`;
				if ( seen.has( key ) ) continue;
				seen.add( key );
				const rootRef = refs.get( p.objectId );
				if ( !rootRef ) {
					warnings.push( `Object resource absent: ${p.objectId}` );
					continue;
				}
				const branches = rootRef.branches ?? [ rootRef ];
				if ( branches.length > 512 ) throw new Error( "Compound branch budget exceeded" );
				let ready = compoundReady.get( rootRef );
				if ( ready === undefined ) {
					ready = !rootRef.branches ||
						!branches.some( branch =>
							branch.meshPaths.some( path => !meshes.has( lower( path ) ) ) ||
							branch.materialPaths.some( path => !sets.has( lower( path ) ) )
						);
					compoundReady.set( rootRef, ready );
				}
				if ( !ready ) {
					warnings.push( `Compound dependencies absent: ${p.objectId}` );
					continue;
				}
				let fadeRadius = fadeRadii.get( rootRef );
				if ( fadeRadius === undefined ) {
					let found = false, lowX = Infinity, lowZ = Infinity, highX = -Infinity, highZ = -Infinity;
					for ( const path of rootRef.renderMeshSection?.paths ?? rootRef.meshPaths ) {
						const mesh = meshes.get( lower( path ) );
						if ( !mesh ) continue;
						found = true;
						lowX = Math.min( lowX, mesh.bounds.min[0]! );
						lowZ = Math.min( lowZ, mesh.bounds.min[2]! );
						highX = Math.max( highX, mesh.bounds.max[0]! );
						highZ = Math.max( highZ, mesh.bounds.max[2]! );
					}
					fadeRadius = found ? Math.hypot( highX - lowX, highZ - lowZ ) / 2 : 0;
					fadeRadii.set( rootRef, fadeRadius );
				}
				const visibility = {
					id: key,
					radius: fadeRadius,
					range: p.lodGroupIndex === 2 ? 2020 : 480,
					sceneryRange: p.lodGroupIndex === 2,
					cellRadius: p.lodGroupIndex === 2 ? 15 : 7,
					cells: placementCells.get( key )!
				};
				for ( const [branchIndex, ref] of branches.entries() ) {
					const refEntry = refMaterial( ref ),
						matrix = mapPlacement(
							home,
							origin,
							p.position.x,
							p.position.y,
							p.position.z,
							radians( p.yaw )
						);
					const animation = animatedByPath.get( ref.sourcePath === undefined ? "" : lower( ref.sourcePath ) );
					const emitters = sceneryParticles(
						ref.modifiers?.particleModifiers,
						key,
						branchIndex,
						origin,
						matrix,
						message => warnings.push( `${ref.sourcePath}: ${message}` )
					);
					reserve( emitters.length * 512 );
					for ( const emitter of emitters ) scenery.push( emitter );
					if ( animation?.model ) {
						reserve( 256 );
						let placements = animatedPlacements.get( animation.glbPublicPath );
						if ( !placements ) {
							placements = { orders: [], matrices: [], visibility: [] };
							animatedPlacements.set( animation.glbPublicPath, placements );
							models[animation.glbPublicPath] = animation.model;
						}
						const m = matrix.slice();
						for ( let i = 8; i < 12; i++ ) m[i] = -m[i]!;
						placements.matrices.push( ...m );
						placements.visibility.push( visibility );
						placements.orders.push( {
							object: key,
							branch: branchIndex,
							paths: ref.renderMeshSection?.paths ?? ref.meshPaths
						} );
					}
					for ( const [meshIndex, path] of (ref.renderMeshSection?.paths ?? ref.meshPaths).entries() ) {
						if (
							animation?.model &&
							animation.skinnedMeshPaths.some( p => lower( p ) === lower( path ) )
						) continue;
						const mesh = meshes.get( lower( path ) );
						if ( !mesh ) {
							warnings.push( `Mesh absent: ${path}` );
							continue;
						}
						const check = meshCheck( mesh );
						if ( !check.valid ) {
							warnings.push( `Invalid mesh omitted: ${path}` );
							continue;
						}
						const found = refEntry.byName.get( check.material );
						if ( !found ) {
							warnings.push( `Material absent: ${mesh.metadata.materialName}` );
							continue;
						}
						noteMaterial( ref, found.material, found.index );
						const id = `${path}:${refEntry.key}:${p.dungeonBlock ?? "outdoor"}${
								mesh.cloth ? ":cloth:" + key + ":" + branchIndex : ""
							}`,
							radius = check.radius;
						let group = instances.get( id );
						if ( !group ) {
							reserve( mesh.positions.length / 3 * 224 + mesh.indices.length * 16 );
							group = {
								mesh,
								block: p.dungeonBlock,
								collision: [],
								material: {
									...materialFor( ref, found.material, found.index ),
									fog: p.dungeonBlock === undefined ?
										undefined :
										b.dungeonBlocks?.[p.dungeonBlock]?.fog,
									objectFade: p.dungeonBlock === undefined
								},
								materialOrder: { set: refEntry.set, index: found.index },
								matrices: [],
								visibility: [],
								center: [ matrix[12]!, matrix[13]!, matrix[14]! ],
								radius
							};
							instances.set( id, group );
						} else {
							const center = group.center;
							group.radius = Math.max(
								group.radius,
								Math.hypot(
									matrix[12]! - center[0]!,
									matrix[13]! - center[1]!,
									matrix[14]! - center[2]!
								) + radius
							);
						}
						// Native A40044 skips parts without the loaded BMS object-nav payload.
						// Keep their draw instances, but never admit them as camera obstacles.
						reserve( 256 );
						if ( (mesh.headerOffsets?.[7] ?? 0) !== 0 ) {
							group.collision.push( {
								instance: group.matrices.length / 16,
								object: key,
								order: branchIndex * 65536 + meshIndex,
								indexStart: 0,
								indexCount: mesh.indices.length
							} );
						}
						for ( let i = 0; i < 16; i++ ) group.matrices.push( matrix[i]! );
						group.visibility.push( visibility );
					}
				}
			}
			for ( const [id, g] of instances ) {
				groups.push( {
					id: `object:${id}`,
					dungeonBlock: g.block,
					collision: g.collision,
					visibility: g.block === undefined ? g.visibility : undefined,
					materialOrder: g.materialOrder,
					instanceRadius: meshCheck( g.mesh ).radius,
					geometry: {
						world: true,
						positions: new Float32Array( g.mesh.positions ),
						cloth: clothData( g.mesh.cloth, g.mesh.positions.length / 3 ),
						normals: new Float32Array( g.mesh.normals ),
						uvs: meshCheck( g.mesh ).uvs.slice(),
						indices: new Uint32Array( g.mesh.indices ),
						instances: new Float32Array( g.matrices ),
						transform: identity()
					},
					material: g.material,
					center: [ g.center[0]!, g.center[1]!, g.center[2]! ],
					radius: g.radius
				} );
			}
			for ( const entry of objectPart ? b.animated ?? [] : [] ) {
				const animatedPlacement = animatedPlacements.get( entry.glbPublicPath );
				if ( !entry.model || !animatedPlacement ) continue;
				const { matrices, visibility } = animatedPlacement;
				const model = entry.model, pose = createCharacterPose( model ), radius = characterRadius( model );
				pose.evaluate( entry.clipName, 0 );
				const ref = b.objects.resources.bsr.flatMap( row => row.branches ?? [ row ] ).find( row =>
					row.sourcePath?.toLowerCase() === entry.sourcePath.toLowerCase()
				);
				if ( !ref ) throw Error( "Animated world material provenance is absent" );
				const materials = ref.materialPaths.flatMap( path => sets.get( path.toLowerCase() ) ?? [] );
				for ( let i = 0; i < model.primitives.length; i++ ) {
					const p = model.primitives[i]!, mat = materials.find( row => row.name === p.name );
					if ( !mat ) throw Error( "Animated world material is absent: " + p.name );
					reserve(
						p.geometry.positions.length / 3 * 224 + p.geometry.indices.length * 16 + matrices.length * 8 +
							p.joints.length * 128
					);
					const bones = new Float32Array( p.joints.length * 16 );
					pose.palette( p, bones );
					const material = {
						...sceneryMaterial(
							worldObjectMaterial( mat ),
							ref.modifiers,
							materials.indexOf( mat ),
							message => warnings.push( `${ref.sourcePath}: ${message}` )
						),
						sharedPose: true
					};
					const collision: NonNullable<WorldGroup["collision"]>[number][] = [];
					// Animated GLBs merge equal-material BMS parts in input order. Recover their
					// index intervals from the source mesh census, without duplicating skin poses.
					const sources = entry.skinnedMeshPaths.map( path => ({
						path,
						mesh: meshes.get( path.toLowerCase() )
					}) ).filter( row => row.mesh?.metadata.materialName === p.name );
					if (
						sources.reduce( ( n, row ) => n + row.mesh!.indices.length, 0 ) !== p.geometry.indices.length
					) throw new Error( "Animated collision part provenance is absent" );
					for ( const [instance, placement] of animatedPlacement.orders.entries() ) {
						let indexStart = 0;
						for ( const row of sources ) {
							const part = placement.paths.findIndex( path =>
								path.toLowerCase() === row.path.toLowerCase()
							);
							if ( part < 0 ) throw new Error( "Animated collision part is outside its BSR" );
							if ( (row.mesh!.headerOffsets?.[7] ?? 0) !== 0 ) {
								collision.push( {
									instance,
									object: placement.object,
									order: placement.branch * 65536 + part,
									indexStart,
									indexCount: row.mesh!.indices.length
								} );
							}
							indexStart += row.mesh!.indices.length;
						}
					}
					// Cloth state belongs to a placement; static meshes can still share a draw.
					for ( let first = 0; first < matrices.length; first += p.cloth ? 16 : matrices.length ) {
						const instance = first / 16;
						groups.push( {
							id: `animated:${entry.glbPublicPath}:${i}${p.cloth ? ":" + instance : ""}`,
							collision: p.cloth ?
								collision.filter( row => row.instance === instance ).map( row => ({
									...row,
									instance: 0
								}) ) :
								collision,
							visibility: p.cloth ? visibility.slice( instance, instance + 1 ) : visibility,
							animation: { model: entry.glbPublicPath, primitive: i, clip: entry.clipName },
							instanceRadius: radius,
							center: [ 0, 0, 0 ],
							radius: 100000,
							material,
							geometry: {
								...p.geometry,
								cloth: p.cloth,
								world: true,
								instances: new Float32Array( p.cloth ? matrices.slice( first, first + 16 ) : matrices ),
								bones,
								material
							}
						} );
					}
				}
			}
			const sectors = (b.terrain.sectors ?? [ { ...b.source, blocks: b.terrain.blocks } ]).map( sector => ({
				...sector,
				lightmapPublicPath: sector.lightmapPublicPath ??
					b.terrainTextures.sectors?.find( t => t.sectorX === sector.sectorX && t.sectorY === sector.sectorY )
						?.lightmapPublicPath ??
					(sector.sectorX === b.source.sectorX && sector.sectorY === b.source.sectorY ?
						b.terrainTextures.lightmapPublicPath :
						undefined)
			}) );
			const tiles = new Map(
				b.terrainTextures.tileCatalog.referencedTiles.map( row => [ row.textureId, row.imagePublicPath ] )
			);
			for ( const sector of terrainPart ? sectors : [] ) {
				for ( const block of sector.blocks ) {
					if (
						block.heights.length !== 289 || block.textureData.length !== 289 ||
						!finiteNumbers( block.heights ) || !block.textureData.every( v =>
							Number.isSafeInteger( v ) && v >= 0 && v <= 65535
						)
					) throw new Error( "Malformed native terrain block" );
					const cx = (sector.sectorX - (origin & 255)) * 6 + block.blockX,
						cz = (sector.sectorY - (origin >>> 8)) * 6 + block.blockZ,
						terrainSector = sector.sectorX | (sector.sectorY << 8);
					const heights = heightRange( block.heights );
					const cellBounds = [
						cx * 320,
						heights.min,
						cz * 320,
						(cx + 1) * 320,
						heights.max,
						(cz + 1) * 320
					] as const;
					for ( let lod = 0; lod < 4; lod++ ) {
						const builders = new Map<
								number,
								{
									positions: number[];
									normals: number[];
									uvs: number[];
									colors: number[];
									indices: number[];
									maskUVs: number[];
								}
							>(),
							step = 1 << lod;
						for ( const p of passes( block.textureData, step ) ) {
							reserve( 4 * 224 + 6 * 16 );
							const id = p.key * 2 + (p.mask === 15 ? 0 : 1);
							let g = builders.get( id );
							if ( !g ) {
								reserve( 289 * 8 + 256 );
								g = { positions: [], normals: [], uvs: [], colors: [], indices: [], maskUVs: [] };
								builders.set( id, g );
							}
							const base = g.positions.length / 3;
							const uv = .25 * tileUvScale( p.key & 7 );
							// Corners (0,0) (step,0) (0,step) (step,step), in that order.
							for ( let corner = 0; corner < 4; corner++ ) {
								const dx = (corner & 1) * step, dz = (corner >>> 1) * step;
								const x = p.x + dx, z = p.z + dz, h = block.heights[z * 17 + x]!;
								g.positions.push( cx * 320 + x * 20, h, cz * 320 + z * 20 );
								g.normals.push( 0, 1, 0 );
								g.uvs.push( (block.blockX * 16 + x) * uv, (block.blockZ * 16 + z) * uv );
								g.colors.push( p.mask & 1, (p.mask >>> 1) & 1, (p.mask >>> 2) & 1, (p.mask >>> 3) & 1 );
								g.maskUVs.push( dx / step, dz / step );
							}
							if ( ((p.x / step) & 1) === ((p.z / step) & 1) ) {
								g.indices.push( base, base + 2, base + 3, base, base + 3, base + 1 );
							} else g.indices.push( base + 1, base, base + 2, base + 1, base + 2, base + 3 );
						}
						if ( sector.lightmapPublicPath ) {
							const positions: number[] = [],
								normals: number[] = [],
								uvs: number[] = [],
								indices: number[] = [];
							for ( let z = 0; z <= 16; z += step ) {
								for ( let x = 0; x <= 16; x += step ) {
									positions.push( cx * 320 + x * 20, block.heights[z * 17 + x]!, cz * 320 + z * 20 );
									normals.push( 0, 1, 0 );
									uvs.push( (block.blockX * 16 + x) / 96, (block.blockZ * 16 + z) / 96 );
								}
							}
							const axis = 16 / step + 1;
							for ( let z = 0; z < axis - 1; z++ ) {
								for ( let x = 0; x < axis - 1; x++ ) {
									const a = z * axis + x;
									if ( (x & 1) === (z & 1) ) {
										indices.push( a, a + axis, a + axis + 1, a, a + axis + 1, a + 1 );
									} else indices.push( a + 1, a, a + axis, a + 1, a + axis, a + axis + 1 );
								}
							}
							reserve( positions.length / 3 * 224 + indices.length * 16 + 289 * 8 );
							const min = heights.min,
								max = heights.max,
								center = [ cx * 320 + 160, (min + max) / 2, cz * 320 + 160 ] as const,
								radius = Math.hypot( 160, (max - min) / 2, 160 );
							groups.push( {
								id: `lightmap:${cx}:${cz}:${lod}`,
								terrainSector,
								center,
								radius,
								ranges: [ {
									bounds: cellBounds,
									cell: [ cx, cz ],
									lod,
									indexStart: 0,
									indexCount: indices.length,
									vertexStart: 0,
									vertexCount: positions.length / 3,
									center,
									radius,
									heights: block.heights,
									water: block.water ? { ...block.water } : undefined
								} ],
								material: {
									texture: sector.lightmapPublicPath,
									color: [ 1, 1, 1, 1 ],
									alphaCutoff: 0,
									blend: true,
									doubleSided: true,
									unlit: true,
									lightmap: true
								},
								geometry: {
									world: true,
									positions: new Float32Array( positions ),
									normals: new Float32Array( normals ),
									uvs: new Float32Array( uvs ),
									indices: new Uint32Array( indices ),
									instances: identity(),
									transform: identity()
								}
							} );
						}
						for ( const [id, g] of builders ) {
							const key = Math.floor( id / 2 ), texture = tiles.get( key >>> 6 );
							if ( !texture ) {
								warnings.push( `Terrain texture absent: ${key >>> 6}` );
								continue;
							}
							const min = heights.min, max = heights.max;
							groups.push( {
								id: `terrain:${cx}:${cz}:${lod}:${id}`,
								terrainSector,
								cell: [ cx, cz ],
								lod,
								center: [ cx * 320 + 160, (min + max) / 2, cz * 320 + 160 ],
								radius: Math.hypot( 160, (max - min) / 2, 160 ),
								ranges: [ {
									bounds: cellBounds,
									cell: [ cx, cz ],
									lod,
									indexStart: 0,
									indexCount: g.indices.length,
									vertexStart: 0,
									vertexCount: g.positions.length / 3,
									center: [ cx * 320 + 160, (min + max) / 2, cz * 320 + 160 ],
									radius: Math.hypot( 160, (max - min) / 2, 160 ),
									heights: block.heights,
									water: block.water ? { ...block.water } : undefined
								} ],
								material: {
									color: [ 1, 1, 1, 1 ],
									texture,
									alphaCutoff: 0,
									blend: !!(id & 1),
									doubleSided: true,
									unlit: true,
									terrain: true,
									order: key
								},
								geometry: {
									world: true,
									positions: new Float32Array( g.positions ),
									normals: new Float32Array( g.normals ),
									uvs: new Float32Array( g.uvs ),
									colors: new Float32Array( g.colors ),
									maskUVs: new Float32Array( g.maskUVs ),
									indices: new Uint32Array( g.indices ),
									instances: identity(),
									transform: identity()
								}
							} );
						}
					}
					if (
						block.water?.type === 1 && block.water.waveType !== 0 && !b.water?.specialTexturePublicPath
					) {
						throw new Error( "Special water texture absent" );
					}
					if (
						!(region & 0x8000) && b.water?.normalFramePublicPaths[0] &&
						(block.water?.type === 0 || block.water?.type === 1 && block.water.waveType !== 0)
					) {
						const axis = block.water.type === 0 ? 17 : 2,
							positions: number[] = [],
							uvs: number[] = [],
							colors: number[] = [],
							normals: number[] = [],
							indices: number[] = [];
						let wet = false;
						const y = Math.fround( block.water.height );
						for ( let z = 0; z < axis; z++ ) {
							for ( let x = 0; x < axis; x++ ) {
								const depth = Math.fround( (y - Math.fround( block.heights[z * 17 + x]! )) * 0.5 ),
									alpha = axis === 2 ? 1 : Math.max( 0, Math.min( 15, Math.trunc( depth ) ) ) / 15;
								wet ||= alpha > 0;
								positions.push( cx * 320 + x / (axis - 1) * 320, y, cz * 320 + z / (axis - 1) * 320 );
								uvs.push( x / (axis - 1) * 4, z / (axis - 1) * 4 );
								colors.push( 1, 1, 1, alpha );
								normals.push( 0, 1, 0 );
							}
						}
						if ( wet ) {
							for ( let z = 0; z < axis - 1; z++ ) {
								for ( let x = 0; x < axis - 1; x++ ) {
									const a = z * axis + x;
									indices.push( a, a + axis, a + 1, a + 1, a + axis, a + axis + 1 );
								}
							}
							reserve( positions.length * 32 + indices.length * 16 );
							groups.push( {
								id: `water:${cx}:${cz}`,
								terrainSector,
								center: [ cx * 320 + 160, y, cz * 320 + 160 ],
								radius: 227,
								material: {
									color: [ 1, 1, 1, 1 ],
									water: block.water.type === 0,
									texture: block.water.type === 1 ?
										b.water.specialTexturePublicPath :
										b.water.normalFramePublicPaths[0],
									frames: block.water.type === 1 ? undefined : b.water.normalFramePublicPaths,
									alphaCutoff: 0,
									blend: true,
									doubleSided: true,
									unlit: true
								},
								geometry: {
									world: true,
									positions: new Float32Array( positions ),
									normals: new Float32Array( normals ),
									uvs: new Float32Array( uvs ),
									colors: new Float32Array( colors ),
									indices: new Uint32Array( indices ),
									instances: identity(),
									transform: identity()
								}
							} );
						}
					}
				}
			}
			// Association identity, not cell identity, owns the draw resource. All LOD
			// ranges stay resident; selection compacts indices into one persistent span.
			const terrain = new Map<string, WorldGroup[]>(),
				merged = groups.filter( g => !g.material.terrain && !g.material.lightmap );
			for ( const group of groups ) {
				if ( group.material.terrain || group.material.lightmap ) {
					// A split request keeps each sector a resident unit; a whole scene batches
					// across its sectors (fewer draws while nothing is retained between scenes).
					const sectorKey = part === "all" ? "" : `${group.terrainSector}:`,
						key = `${sectorKey}${group.material.texture}:${group.material.blend}:${group.material.order}`;
					let list = terrain.get( key );
					if ( !list ) {
						list = [];
						terrain.set( key, list );
					}
					list.push( group );
				}
			}
			for ( const [key, list] of terrain ) {
				const vertices = list.reduce( ( n, g ) => n + g.geometry.positions.length / 3, 0 ),
					count = list.reduce( ( n, g ) => n + g.geometry.indices.length, 0 );
				const positions = new Float32Array( vertices * 3 ),
					normals = new Float32Array( vertices * 3 ),
					uvs = new Float32Array( vertices * 2 ),
					colors = list.some( g => g.geometry.colors ) ? new Float32Array( vertices * 4 ) : undefined,
					maskUVs = list.some( g => g.geometry.maskUVs ) ? new Float32Array( vertices * 2 ) : undefined,
					indices = new Uint32Array( count ),
					ranges: TerrainRange[] = [];
				let v = 0, i = 0;
				for ( const group of list ) {
					const g = group.geometry;
					positions.set( g.positions, v * 3 );
					normals.set( g.normals!, v * 3 );
					uvs.set( g.uvs!, v * 2 );
					if ( colors ) {
						if ( g.colors ) colors.set( g.colors, v * 4 );
						else colors.fill( 1, v * 4, (v + g.positions.length / 3) * 4 );
					}
					if ( g.maskUVs ) maskUVs!.set( g.maskUVs, v * 2 );
					for ( let n = 0; n < g.indices.length; n++ ) indices[i + n] = g.indices[n]! + v;
					ranges.push( { ...group.ranges![0]!, vertexStart: v, indexStart: i } );
					v += g.positions.length / 3;
					i += g.indices.length;
				}
				merged.push( {
					id: `terrain-batch:${key}`,
					terrainSector: part === "all" ? undefined : list[0]!.terrainSector,
					center: [ 960, 0, 960 ],
					radius: 10000,
					material: list[0]!.material,
					ranges,
					geometry: {
						world: true,
						positions,
						normals,
						uvs,
						colors,
						maskUVs,
						indices,
						instances: identity(),
						transform: identity()
					}
				} );
			}
			if ( objectPart && b.dungeonWater?.length ) {
				if ( !(region & 0x8000) ) throw Error( "Dungeon water in outdoor bundle" );
				for ( const surface of b.dungeonWater ) {
					merged.push( dungeonWaterGroup( surface, b.water?.normalFramePublicPaths ?? [] ) );
				}
			}
			if ( objectPart && b.sky && !(region & 0x8000) ) merged.unshift( ...skyGroups( b.sky ) );
			const scene: WorldScene = {
				scenery,
				soundTerrain: objectPart ? decodeSoundTerrain( b ) : undefined,
				dungeonVisibility: b.dungeonBlocks?.map( block => [ block.index, ...block.visibleBlocks ] ),
				waterBump: b.water?.reflectionBumpPublicPath,
				flareTextures: region & 0x8000 || !objectPart ? undefined : b.sky?.flareTexturePublicPaths,
				starRandomState: objectPart ? b.sky?.starPrimitive?.nativeRand?.stateAfterConstruction : undefined,
				residency: frontend ? "frontend" : undefined,
				models,
				environment: objectPart ? b.sky?.environment : undefined,
				id: `region:${region}:${origin}:${part}`,
				originRegion: origin,
				groups: merged,
				warnings: [ ...new Set( warnings ) ]
			};
			const residentBytes = worldSceneBytes( scene ),
				residentLimit = frontend ? FRONTEND_SCENE_BYTES : Math.min( budget, WORLD_SCENE_BYTES );
			if ( residentBytes > residentLimit ) {
				throw new Error(
					`World scene residency budget exceeded: ${residentBytes} bytes > ${residentLimit} bytes (${merged.length} groups, ${sectors.length} sectors)`
				);
			}
			return scene;
		}
	};
}
