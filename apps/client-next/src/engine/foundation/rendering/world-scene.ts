/*
===========================================================================

world-scene.ts - world scene admission and copies

Validates, prepares and copies world scenes for the renderer, including
terrain seam plans and which groups rewrite their vertices.

===========================================================================
*/
import { characterBytes, characterPoseBytes } from "@/engine/foundation/animation/character-budget";
import type { WorldScene } from "@/engine/contracts/scene";
import { copyMaterial, copyGeometry, validateGeometry } from "@/engine/foundation/rendering/geometry";
import type { PreparedWorldScene } from "@/engine/contracts/world-admission";
import type { Geometry } from "@/engine/contracts/geometry";
import { finiteGeometryValues, geometryIndicesInRange } from "./geometry-validation";
import { PICK_BLOCK_INDICES } from "./picking";
// Frontend stage manifests intentionally retain the complete scripted route.
// Constantinople reference measurement: 406 MiB scene, 731 MiB decode scratch.
export const FRONTEND_SCENE_BYTES = 536870912;
export const FRONTEND_RESIDENCY_BYTES = 1073741824;
export const FRONTEND_DECODE_BYTES = 1073741824;
export const WORLD_SCENE_BYTES = 201326592;
// Reserve both maximum-size scene products plus their shared decoded texture
// working set. The former 512 MiB total left only 128 MiB for textures at peak;
// a measured native region crossing needs over 155 MiB without reducing quality.
// Admission still enforces this aggregate bound, including retired products.
export const WORLD_RESIDENCY_BYTES = 671088640; // 2 * WORLD_SCENE_BYTES + 256 MiB
export const WORLD_DECODE_BYTES = 536870912;
// Validate before copying. Count owned arrays, selection scratch and retained
// interleaved vertices; caller-owned metadata is never retained by admission.
export function worldSceneBytes( scene: WorldScene | null ): number {
	if ( scene?.terrainDetail !== undefined && scene.terrainDetail !== "full" && scene.terrainDetail !== "distance" ) {
		throw new Error( "Invalid terrain detail policy" );
	}
	if ( !scene ) {
		return 0;
	}
	if ( !Array.isArray( scene.groups ) || scene.groups.length > 8192 ) {
		throw new Error( "World group budget exceeded" );
	}
	if (
		scene.flareTextures &&
		(scene.flareTextures.length !== 8 ||
			scene.flareTextures.some( p =>
				typeof p !== "string" || !p.startsWith( "/assets/" ) || p.includes( ".." ) || p.includes( "\\" ) ||
				p.length > 1024
			))
	) throw new Error( "Invalid flare texture publication" );
	if (
		scene.dungeonVisibility &&
		(scene.dungeonVisibility.length > 4096 ||
			scene.dungeonVisibility.some( row =>
				row.length > 4097 ||
				row.some( i => !Number.isInteger( i ) || i < 0 || i >= scene.dungeonVisibility!.length )
			))
	) throw Error( "Invalid dungeon visibility" );
	if ( scene.scenery ) {
		if ( !Array.isArray( scene.scenery ) || scene.scenery.length > 8192 ) {
			throw Error( "Scenery emitter budget exceeded" );
		}
		const keys = new Set<string>();
		const scenery: NonNullable<WorldScene["scenery"]> = scene.scenery;
		for ( const e of scenery ) {
			if (
				!e.id || keys.has( e.id ) || !e.placement || !e.model.startsWith( "/assets/effects/programs.json#" ) ||
				e.basis.length !== 9 || !e.basis.every( Number.isFinite ) ||
				!Object.values( e.pose ).every( Number.isFinite ) || typeof e.nightOnly !== "boolean" ||
				!Number.isInteger( e.renderPriority ) || e.renderPriority < 0 || e.renderPriority > 255
			) throw Error( "Invalid scenery emitter" );
			keys.add( e.id );
		}
	}
	const ids = new Set<string>();
	const heightGrids = new Set<readonly number[]>();
	const groups: WorldScene["groups"] = scene.groups;
	let bytes = (scene.scenery ?? []).reduce(
		( n, e ) => n + 256 + (e.id.length + e.placement.length + e.model.length) * 2,
		0
	) + (scene.flareTextures ?? []).reduce( ( sum, p ) => sum + p.length * 2, 0 );
	bytes += (scene.dungeonVisibility ?? []).reduce( ( n, row ) => n + row.length * 8 + 32, 0 );
	if ( Object.keys( scene.models ?? {} ).length > 64 ) throw new Error( "World animated model budget exceeded" );
	for ( const model of Object.values( scene.models ?? {} ) ) {
		bytes += characterBytes( model, [] ) + characterPoseBytes( model );
	}

	for ( const row of scene.soundTerrain ?? [] ) {
		if (
			!Number.isInteger( row.regionId ) || row.regionId < 0 || row.regionId > 65535 ||
			!(row.types instanceof Uint8Array) || row.types.length !== 9216 || row.types.some( n => n > 13 )
		) throw Error( "Invalid sound terrain" );
		bytes += row.types.byteLength + 32;
	}
	if ( scene.environment ) {
		if (
			!Number.isFinite( scene.environment.startTimeOfDay ) || !Number.isFinite( scene.environment.ratePerSecond )
		) {
			throw new Error( "Invalid environment clock" );
		}
		for ( const [name, track] of Object.entries( scene.environment.tracks ) ) {
			bytes += name.length * 2 + track.length * 40;
			for ( const key of track ) {
				if ( !Number.isFinite( key.t ) || Object.values( key ).some( value => !Number.isFinite( value ) ) ) {
					throw new Error( "Invalid environment track" );
				}
			}
		}
	}
	if (
		scene.starRandomState !== undefined &&
		(!Number.isInteger( scene.starRandomState ) || scene.starRandomState < 0 || scene.starRandomState > 0xffffffff)
	) throw new Error( "Invalid published star RNG state" );
	for ( const group of groups ) {
		if (
			group.dungeonBlock !== undefined &&
			(!Number.isInteger( group.dungeonBlock ) || group.dungeonBlock < 0 ||
				!scene.dungeonVisibility?.[group.dungeonBlock])
		) throw Error( "Invalid dungeon block" );
		if ( group.collision ) {
			for ( const c of group.collision ) {
				if (
					!c.object || ![ c.instance, c.order, c.indexStart, c.indexCount ].every( Number.isSafeInteger ) ||
					c.instance < 0 || c.instance >= (group.geometry.instances?.length ?? 16) / 16 || c.order < 0 ||
					c.indexStart < 0 || c.indexCount < 0 || c.indexStart % 3 || c.indexCount % 3 ||
					c.indexStart + c.indexCount > group.geometry.indices.length
				) throw new Error( "Invalid camera collision provenance" );
			}
			bytes += group.collision.reduce( ( n, c ) => n + 64 + c.object.length * 2 + c.indexCount * 4, 0 );
		}
		if ( group.materialOrder ) {
			const order = group.materialOrder;
			if (
				typeof order.set !== "string" || !order.set.length || !Number.isSafeInteger( order.index ) ||
				order.index < 0
			) throw new Error( "Invalid object material order" );
			bytes += order.set.length * 2 + 32;
		}
		if ( group.animation ) {
			const a = group.animation, model = scene.models?.[a.model];
			if ( !model?.primitives[a.primitive] || !model.clips.some( c => c.name === a.clip ) ) {
				throw new Error( "Invalid world animation binding" );
			}
		}
		const g = group.geometry, n = g.positions.length / 3, m = group.material;
		copyMaterial( m );
		if (
			!group.id || ids.has( group.id ) || !(g.positions instanceof Float32Array) || !n ||
			!Number.isInteger( n ) ||
			!(g.indices instanceof Uint32Array) || !g.indices.length || g.indices.length % 3 ||
			!geometryIndicesInRange( g.indices, n )
		) {
			throw new Error( "Invalid world geometry" );
		}
		ids.add( group.id );
		for (
			const [array, length] of [ [ g.positions, n * 3 ], [ g.normals, n * 3 ], [ g.uvs, n * 2 ], [
				g.transform,
				16
			], [ g.instances, g.instances?.length ] ] as const
		) {
			if ( !(array instanceof Float32Array) || array.length !== length || !finiteGeometryValues( array ) ) {
				throw new Error( "Invalid world attributes" );
			}
		}
		if ( !g.instances!.length || g.instances!.length % 16 ) {
			throw new Error( "Invalid world instances" );
		}
		if ( group.visibility ) {
			if ( group.visibility.length !== g.instances!.length / 16 ) {
				throw new Error( "Invalid object visibility count" );
			}
			for ( const v of group.visibility ) {
				if (
					!v.id || ![ v.radius, v.range, v.cellRadius, ...v.cells.flat() ].every( Number.isFinite ) ||
					v.radius < 0 || v.range <= 0 || ![ 7, 15 ].includes( v.cellRadius ) || !v.cells.length ||
					v.cells.length > 1024 || v.cells.some( cell =>
						cell.length !== 2 || !cell.every( Number.isInteger )
					)
				) throw new Error( "Invalid object visibility metadata" );
			}
			// Includes a direct mesh-to-placement slot and the worst-case unique fade owner.
			bytes += group.visibility.length * 192 +
				group.visibility.reduce( ( bytes, v ) => bytes + v.cells.length * 16, 0 );
		}
		const count = g.instances!.length / 16;
		bytes += count * 192; // Camera collision bounds and instance projections.
		bytes += 2 ** Math.ceil( Math.log2( Math.max( 1, count ) ) ) * 160 + count * 160;
		for ( const [array, length] of [ [ g.colors, n * 4 ], [ g.maskUVs, n * 2 ] ] as const ) {
			if (
				array !== undefined &&
				(!(array instanceof Float32Array) || array.length !== length || !finiteGeometryValues( array ))
			) {
				throw new Error( "Invalid optional world attributes" );
			}
		}
		if (
			group.center.length !== 3 || !group.center.every( Number.isFinite ) || !Number.isFinite( group.radius ) ||
			group.radius < 0 ||
			m.color.length !== 4 || !m.color.every( Number.isFinite ) || !Number.isFinite( m.alphaCutoff ) ||
			typeof m.blend !== "boolean" || typeof m.doubleSided !== "boolean" ||
			m.texture !== undefined && typeof m.texture !== "string" ||
			group.instanceRadius !== undefined && (!Number.isFinite( group.instanceRadius ) || group.instanceRadius < 0)
		) {
			throw new Error( "Invalid world metadata" );
		}
		bytes += g.positions.byteLength + g.indices.byteLength * 2 + g.normals!.byteLength + g.uvs!.byteLength +
			(g.colors?.byteLength ?? 0) + (g.maskUVs?.byteLength ?? 0) + g.instances!.byteLength * 2 +
			g.transform.byteLength + n * 56;
		bytes += (g.joints?.byteLength ?? 0) + (g.weights?.byteLength ?? 0) + (g.bones?.byteLength ?? 0);
		if ( !g.bones && !group.ranges ) bytes += Math.ceil( g.indices.length / PICK_BLOCK_INDICES ) * 6 * 8;
		for ( const range of group.ranges ?? [] ) {
			if (
				![ range.indexStart, range.indexCount, range.vertexStart, range.vertexCount, range.lod ].every(
					Number.isSafeInteger
				) ||
				range.indexStart < 0 || range.indexCount < 0 || range.indexCount % 3 ||
				range.indexStart + range.indexCount > g.indices.length ||
				range.vertexStart < 0 || range.vertexCount < 0 || range.vertexStart + range.vertexCount > n ||
				range.lod < 0 || range.lod > 3 ||
				range.cell.length !== 2 || !range.cell.every( Number.isSafeInteger ) || range.center.length !== 3 ||
				!range.center.every( Number.isFinite ) ||
				!Number.isFinite( range.radius ) || range.radius < 0 || range.heights.length !== 289 ||
				!range.heights.every( Number.isFinite )
			) {
				throw new Error( "Invalid terrain range" );
			}
			if ( range.bounds ) {
				const b = range.bounds;
				if (
					b.length !== 6 || !b.every( Number.isFinite ) || b[0] > range.cell[0] * 320 ||
					b[3] < (range.cell[0] + 1) * 320 || b[2] > range.cell[1] * 320 ||
					b[5] < (range.cell[1] + 1) * 320 || range.heights.some( y => y < b[1] || y > b[4] )
				) throw Error( "Invalid terrain cell bounds" );
				bytes += 48;
			}
			if (
				range.water &&
				(![ range.water.type, range.water.waveType ].every( Number.isInteger ) ||
					!Number.isFinite( range.water.height ))
			) throw Error( "Invalid terrain water" );
			// Associations and all four LODs read one immutable cell grid.
			// Count the owned allocation, not every reference to it.
			if ( !heightGrids.has( range.heights ) ) {
				heightGrids.add( range.heights );
				bytes += range.heights.length * 8;
			}
			bytes += 160 + range.vertexCount * 8;
			bytes += 200; // Canonical terrain bound, lookup, numeric slot and visibility mask.
			if ( range.lod === 0 ) bytes += range.vertexCount * 12 + range.indexCount * 4; // Immutable camera collision positions/indices.
		}
	}
	return bytes;
}
// Admission owns this derived plan. Include every boundary vertex, plus any
// interior vertex needing authored-height restoration on its first selection.
// Rebuild from source geometry instead of trusting caller-supplied metadata.
function terrainSeamVertices(
	range: import("@/engine/contracts/scene").TerrainRange,
	positions: Float32Array
): Uint32Array {
	const entries: number[] = [];
	for ( let i = range.vertexStart; i < range.vertexStart + range.vertexCount; i++ ) {
		const x = Math.round( (positions[i * 3]! - range.cell[0] * 320) / 20 ),
			z = Math.round( (positions[i * 3 + 2]! - range.cell[1] * 320) / 20 );
		if ( x < 0 || x > 16 || z < 0 || z > 16 ) throw Error( "Terrain vertex outside height grid" );
		const height = z * 17 + x;
		if (
			x === 0 || x === 16 || z === 0 || z === 16 || positions[i * 3 + 1] !== Math.fround( range.heights[height]! )
		) entries.push( i, height );
	}
	return new Uint32Array( entries );
}

export function copyWorldScene( scene: WorldScene ): WorldScene {
	const heightGrids = new Map<readonly number[], readonly number[]>();
	function copyHeights( source: readonly number[] ) {
		let copy = heightGrids.get( source );
		if ( !copy ) {
			copy = [ ...source ];
			heightGrids.set( source, copy );
		}
		return copy;
	}
	return {
		...scene,
		scenery: scene.scenery ? structuredClone( scene.scenery ) : undefined,
		soundTerrain: scene.soundTerrain?.map( row => ({ ...row, types: row.types.slice() }) ),
		dungeonVisibility: scene.dungeonVisibility?.map( row => row.slice() ),
		flareTextures: scene.flareTextures?.slice(),
		models: scene.models ? structuredClone( scene.models ) : undefined,
		environment: scene.environment ? structuredClone( scene.environment ) : undefined,
		warnings: [ ...scene.warnings ],
		groups: scene.groups.map( group => {
			const material = copyMaterial( group.material );
			return {
				...group,
				collision: group.collision?.map( c => ({ ...c }) ),
				materialOrder: group.materialOrder ? { ...group.materialOrder } : undefined,
				visibility: group.visibility?.map( v => ({
					...v,
					cells: v.cells.map( cell => [ ...cell ] as [number, number] )
				}) ),
				animation: group.animation ? { ...group.animation } : undefined,
				material,
				center: [ ...group.center ] as [
					number,
					number,
					number
				],
				cell: group.cell ?
					[ ...group.cell ] as [
						number,
						number
					] :
					undefined,
				ranges: group.ranges?.map( range => ({
					...range,
					seamVertices: terrainSeamVertices( range, group.geometry.positions ),
					bounds: range.bounds ?
						[ ...range.bounds ] as [number, number, number, number, number, number] :
						undefined,
					cell: [ ...range.cell ] as [
						number,
						number
					],
					center: [ ...range.center ] as [
						number,
						number,
						number
					],
					heights: copyHeights( range.heights ),
					water: range.water ? { ...range.water } : undefined
				}) ),
				geometry: {
					...copyGeometry(
						group.geometry,
						scene.residency === "frontend" ? FRONTEND_SCENE_BYTES : WORLD_SCENE_BYTES,
						Infinity
					),
					world: true,
					material,
					dynamicVertices: group.ranges !== undefined
				}
			};
		} )
	};
}

// Decoder output is exclusively worker-owned. Validate the same render contract
// as the copying API, then transfer it instead of making another geometry copy.
export function prepareWorldScene( scene: WorldScene ): PreparedWorldScene {
	const bytes = worldSceneBytes( scene ),
		limit = scene.residency === "frontend" ? FRONTEND_SCENE_BYTES : WORLD_SCENE_BYTES;
	if ( bytes > limit ) throw new Error( "World CPU residency budget exceeded" );
	const references = new Map<ArrayBufferLike, number>();
	for ( const group of scene.groups ) {
		for ( const value of Object.values( group.geometry ) ) {
			if ( ArrayBuffer.isView( value ) ) {
				references.set( value.buffer, (references.get( value.buffer ) ?? 0) + 1 );
			}
		}
	}
	const modelBuffers = new Set(
		worldSceneTransfers( { id: "models", originRegion: 0, warnings: [], groups: [], models: scene.models } )
	);
	// Terrain stitching and animation write these two streams. Preserve the old
	// per-group isolation if the decoder shared them with another group/model.
	// Unique buffers and all immutable streams can move without copying.
	const mutable = ( value: Float32Array ) =>
		references.get( value.buffer )! > 1 || modelBuffers.has( value.buffer as ArrayBuffer ) ? value.slice() : value;
	const groups = scene.groups.map( group => {
		const g = group.geometry;
		validateGeometry( g, limit, Infinity );
		const material = copyMaterial( group.material );
		const geometry = {
			world: true,
			material,
			positions: mutable( g.positions ),
			indices: g.indices,
			transform: g.transform,
			normals: g.normals,
			uvs: g.uvs,
			colors: g.colors,
			maskUVs: g.maskUVs,
			instances: g.instances,
			joints: g.joints,
			weights: g.weights,
			bones: g.bones ? mutable( g.bones ) : undefined,
			// Terrain stitches its seams by rewriting positions (world.ts).
			dynamicVertices: group.ranges !== undefined
		} satisfies Record<keyof Geometry, unknown>;
		return {
			...group,
			ranges: group.ranges?.map( range => ({
				...range,
				seamVertices: terrainSeamVertices( range, geometry.positions )
			}) ),
			material,
			geometry
		};
	} );
	const stars = groups.filter( group => group.material.sky === 2 );
	if ( stars.length > 1 ) throw new Error( "Duplicate world star groups" );
	const starBytes = worldSceneBytes( { id: "stars", originRegion: 0, warnings: [], groups: stars } );
	return {
		scene: { ...scene, dungeonVisibility: scene.dungeonVisibility?.map( row => row.slice() ), groups },
		bytes,
		starBytes
	};
}

// Includes skin/model storage, which the old hand-written transfer list omitted.
// This traverses descriptors, never individual typed-array elements.
export function worldSceneTransfers( scene: WorldScene ): ArrayBuffer[] {
	const buffers = new Set<ArrayBuffer>(), seen = new Set<object>();
	function visit( value: unknown ): void {
		if ( value === null || typeof value !== "object" || seen.has( value ) ) return;
		seen.add( value );
		if ( ArrayBuffer.isView( value ) ) {
			if ( !(value.buffer instanceof ArrayBuffer) ) {
				throw new Error( "World transfer requires exclusive ArrayBuffers" );
			}
			buffers.add( value.buffer );
			return;
		}
		if ( value instanceof ArrayBuffer ) {
			buffers.add( value );
			return;
		}
		for ( const child of Object.values( value ) ) visit( child );
	}
	visit( scene );
	return [ ...buffers ];
}
