/*
===========================================================================

terrain-layers.ts - one draw per terrain texture across resident regions

Region residency keeps each region's terrain as its own groups (one per
texture association), so the regions a crossing keeps keep their GPU data.
Drawn one by one, nine regions turned about 105 terrain draws into about
440: the GPU process, already the bottleneck, paid for every one.

A layer is the shared draw of every resident group with the same anchor,
texture, blend and association order (same material, same paint order).
Each member owns a slot of the layer's vertex buffer; selection writes the
member's chosen indices, offset by its slot, into the layer's one index
buffer. Members keep their own CPU geometry, ranges, seams and collision.
A layer grows by rebuilding with twice the capacity; a released member's
slot is reclaimed on the next rebuild. A member's own packed stream
(Geometry.vertices) follows its seam stitching, so a rebuild or a re-admit
after device loss writes its current vertices.

===========================================================================
*/
import type { WorldGroup, WorldScene } from "@/engine/contracts/scene";
import type { GeometryCommands, GeometryDraw, ImageDraw } from "../internal/gpu-contract";

// Packed floats per vertex (packGeometryVertices).
const VERTEX_FLOATS = 14;
const IDENTITY = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );

type Member = { layer: Layer; base: number; vertices: number; chosen: Uint32Array; count: number; };
/*
================
TerrainLayer

One layer: callers hold it (layerOf) to take its current draw (drawInPass).
================
*/
export type TerrainLayer = Layer;
type Layer = {
	key: string;
	draw: GeometryDraw;
	image: ImageDraw | undefined;
	material: NonNullable<WorldGroup["geometry"]["material"]>;
	vertexCapacity: number;
	indexCapacity: number;
	top: number;
	members: Map<WorldGroup, Member>;
	indices: Uint32Array;
	dirty: boolean;
	// The submission pass that last took this layer's draw.
	submitted: number;
};

/*
================
capacityFor

The next power of two at or above twice need, so growth is rare.
================
*/
function capacityFor( need: number ): number {
	return 2 ** Math.ceil( Math.log2( Math.max( 1024, need * 2 ) ) );
}

/*
================
createTerrainLayers
================
*/
export function createTerrainLayers() {
	const layers = new Map<string, Layer>(), members = new Map<WorldGroup, Member>();

	/*
	================
	keyOf
	================
	*/
	function keyOf( scene: WorldScene, group: WorldGroup ): string {
		const m = group.material;
		return `${scene.originRegion}|${m.texture}|${m.blend}|${m.order}`;
	}

	/*
	================
	allocate

	A layer's GPU geometry for the given capacities: an empty mirror and a
	zero index buffer, filled by writeVertices and updateIndices.
	================
	*/
	function allocate( geometry: GeometryCommands, layer: Omit<Layer, "draw">, vertices: number, indices: number ) {
		return geometry.upload(
			{
				world: true,
				material: layer.material,
				positions: new Float32Array( vertices * 3 ),
				indices: new Uint32Array( indices ),
				transform: IDENTITY(),
				instances: IDENTITY(),
				dynamicVertices: true,
				vertices: new Float32Array( vertices * VERTEX_FLOATS )
			},
			layer.image
		);
	}

	/*
	================
	rebuild

	A new draw sized for the live members plus need more vertices and
	indices, with every member re-written at a compact slot.
	================
	*/
	function rebuild( geometry: GeometryCommands, layer: Layer, vertexNeed: number, indexNeed: number ) {
		let vertices = vertexNeed, indices = indexNeed;
		for ( const [group, member] of layer.members ) {
			vertices += member.vertices;
			indices += group.geometry.indices.length;
		}
		layer.vertexCapacity = Math.max( layer.vertexCapacity, capacityFor( vertices ) );
		layer.indexCapacity = Math.max( layer.indexCapacity, capacityFor( indices ) );
		geometry.release( layer.draw );
		layer.draw = allocate( geometry, layer, layer.vertexCapacity, layer.indexCapacity );
		layer.indices = new Uint32Array( layer.indexCapacity );
		layer.top = 0;
		for ( const [group, member] of layer.members ) {
			member.base = layer.top;
			geometry.writeVertices( layer.draw, member.base, group.geometry.vertices! );
			layer.top += member.vertices;
		}
		layer.dirty = true;
	}

	return {
		/*
		================
		eligible

		Region terrain texture associations draw through layers; lightmaps and
		water stay single draws (their textures are per region already).
		================
		*/
		eligible: ( group: WorldGroup ) =>
			group.terrainSector !== undefined && !!group.ranges && !!group.material.terrain &&
			!!group.geometry.vertices,
		/*
		================
		admit

		Makes group a member of its layer and returns the layer's draw.
		================
		*/
		admit( geometry: GeometryCommands, scene: WorldScene, group: WorldGroup, image: ImageDraw | undefined ) {
			const key = keyOf( scene, group ),
				vertices = group.geometry.positions.length / 3,
				indices = group.geometry.indices.length;
			let layer = layers.get( key );
			if ( !layer ) {
				const shell = {
					key,
					image,
					material: group.geometry.material ?? group.material,
					vertexCapacity: capacityFor( vertices ),
					indexCapacity: capacityFor( indices ),
					top: 0,
					members: new Map<WorldGroup, Member>(),
					indices: new Uint32Array( 0 ),
					dirty: true,
					submitted: -1
				};
				layer = { ...shell, draw: allocate( geometry, shell, shell.vertexCapacity, shell.indexCapacity ) };
				layer.indices = new Uint32Array( layer.indexCapacity );
				layers.set( key, layer );
			}
			let indexTotal = indices;
			for ( const other of layer.members.keys() ) indexTotal += other.geometry.indices.length;
			if ( layer.top + vertices > layer.vertexCapacity || indexTotal > layer.indexCapacity ) {
				rebuild( geometry, layer, vertices, indices );
			}
			const member: Member = {
				layer,
				base: layer.top,
				vertices,
				chosen: new Uint32Array( indices ),
				count: 0
			};
			geometry.writeVertices( layer.draw, member.base, group.geometry.vertices! );
			layer.top += vertices;
			layer.members.set( group, member );
			members.set( group, member );
			layer.dirty = true;
			return layer.draw;
		},
		/*
		================
		member
		================
		*/
		member: ( group: WorldGroup ) => members.has( group ),
		/*
		================
		drawOf

		The current draw of a member's layer. A rebuild replaces the layer's
		draw, so callers resolve it here rather than keep the one admit returned.
		================
		*/
		drawOf: ( group: WorldGroup ) => members.get( group )!.layer.draw,
		/*
		================
		layerOf

		The layer group draws through, or undefined when the group is not a
		member (it submits its own draw). Membership changes only through
		admit, remove and clear.
		================
		*/
		layerOf: ( group: WorldGroup ): TerrainLayer | undefined => members.get( group )?.layer,
		/*
		================
		drawInPass

		The draw layer submits in pass: its current draw at its first member
		in the pass, null for the other members.
		================
		*/
		drawInPass( layer: TerrainLayer, pass: number ): GeometryDraw | null {
			if ( layer.submitted === pass ) return null;
			layer.submitted = pass;
			return layer.draw;
		},
		/*
		================
		remove

		Releases group's slot; an empty layer releases its draw.
		================
		*/
		remove( geometry: GeometryCommands, group: WorldGroup ) {
			const member = members.get( group );
			if ( !member ) return;
			members.delete( group );
			const layer = member.layer;
			layer.members.delete( group );
			layer.dirty = true;
			if ( !layer.members.size ) {
				geometry.release( layer.draw );
				layers.delete( layer.key );
			}
		},
		/*
		================
		select

		The member's chosen indices (member-local, the first count of indices).
		================
		*/
		select( group: WorldGroup, indices: Uint32Array, count: number ) {
			const member = members.get( group )!;
			member.chosen.set( indices.subarray( 0, count ) );
			member.count = count;
			member.layer.dirty = true;
		},
		/*
		================
		updatePositions

		Seam stitching of a member: kept in its own packed stream and written
		at its slot of the layer.
		================
		*/
		updatePositions(
			geometry: GeometryCommands,
			group: WorldGroup,
			positions: Float32Array,
			ranges: readonly (readonly [number, number])[]
		) {
			const member = members.get( group )!, own = group.geometry.vertices!;
			for ( const [start, length] of ranges ) {
				for ( let i = start; i < start + length; i++ ) {
					own[i * VERTEX_FLOATS] = positions[i * 3]!;
					own[i * VERTEX_FLOATS + 1] = positions[i * 3 + 1]!;
					own[i * VERTEX_FLOATS + 2] = positions[i * 3 + 2]!;
				}
			}
			geometry.updatePositions( member.layer.draw, positions, undefined, undefined, ranges, member.base );
		},
		/*
		================
		flush

		Writes the merged index buffer of every layer whose selection changed.
		================
		*/
		flush( geometry: GeometryCommands ) {
			for ( const layer of layers.values() ) {
				if ( !layer.dirty ) continue;
				let at = 0;
				for ( const member of layer.members.values() ) {
					const base = member.base, chosen = member.chosen;
					for ( let i = 0; i < member.count; i++ ) layer.indices[at + i] = chosen[i]! + base;
					at += member.count;
				}
				geometry.updateIndices( layer.draw, layer.indices.subarray( 0, at ) );
				layer.dirty = false;
			}
		},
		/*
		================
		clear

		Device loss: the GPU resources are gone; members re-admit on upload.
		================
		*/
		clear() {
			layers.clear();
			members.clear();
		},
		/*
		================
		dispose
		================
		*/
		dispose( geometry: GeometryCommands | null ) {
			if ( geometry ) { for ( const layer of layers.values() ) geometry.release( layer.draw ); }
			layers.clear();
			members.clear();
		},
		stats: () => ({ layers: layers.size, members: members.size })
	};
}
