/*
===========================================================================

terrain-layers.ts - one draw per terrain texture across resident regions

Region residency keeps each region's terrain as its own groups (one per
texture association), so the regions a crossing keeps keep their GPU data.
Drawn one by one, nine regions turned about 105 terrain draws into about
440: the GPU process, already the bottleneck, paid for every one.

A layer is the shared draw of every resident group with the same anchor,
texture, blend and association order (same material, same paint order).
Each member owns a slot of the layer's vertex buffer and a span of its cull
source (its indices offset by that slot). The layer draw is GPU-culled
(index-cull.ts, port-only): selection hands the layer each member's
candidate ranges, flush sends them as one range table, and the cull pass
packs the ranges the live view sees into the draw's index buffer. Members
keep their own CPU geometry, ranges, seams and collision.
A layer that runs out of room rebuilds at twice its live members' size,
which reclaims released members' slots. A removal that leaves the members
under a quarter of the layer rebuilds it smaller the same way: regions
come and go as the camera moves, and a layer sized for its high-water mark
would hold that GPU memory for good. A member's own packed stream
(Geometry.vertices) follows its seam stitching, so a rebuild or a re-admit
after device loss writes its current vertices.

The layer keeps no CPU copy of its vertex buffer: every byte of a member's
slot comes from the member's own stream (streamVertices), so a mirror
would only duplicate the members. Slots past the members are never
indexed.

===========================================================================
*/
import type { TerrainRange, WorldGroup, WorldScene } from "@/engine/contracts/scene";
import type { GeometryCommands, GeometryDraw, ImageDraw } from "../internal/gpu-contract";
import { RANGE_WORDS } from "../device/index-cull";

// Packed floats per vertex (packGeometryVertices).
const VERTEX_FLOATS = 14;
const MIN_LAYER_CAPACITY = 1024;
const LAYER_GROWTH = 2;
// A layer whose members fill less than 1 / LAYER_SHRINK of it is rebuilt.
const LAYER_SHRINK = 4;
const IDENTITY = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );

// base is the member's first vertex in the layer, source its first index
// in the layer's cull source; ranges are its current candidates.
type Member = { layer: Layer; base: number; vertices: number; source: number; ranges: readonly TerrainRange[]; };
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
	// The next free index of the layer's cull source.
	sourceTop: number;
	members: Map<WorldGroup, Member>;
	// The range table flush sends, reused while it is large enough.
	table: Uint32Array;
	dirty: boolean;
	// The submission pass that last took this layer's draw.
	submitted: number;
};

/*
================
capacityFor

Round a known requirement up to whole MIN_LAYER_CAPACITY blocks. A rebuild
adds headroom; initial admission already knows every member of the
incoming scene.
================
*/
function capacityFor( need: number ): number {
	return Math.ceil( Math.max( MIN_LAYER_CAPACITY, need ) / MIN_LAYER_CAPACITY ) * MIN_LAYER_CAPACITY;
}

/*
================
eligible
================
*/
function eligible( group: WorldGroup ): boolean {
	return group.terrainSector !== undefined && !!group.ranges && !!group.material.terrain &&
		!!group.geometry.vertices;
}

/*
================
createTerrainLayers
================
*/
export function createTerrainLayers() {
	const layers = new Map<string, Layer>(), members = new Map<WorldGroup, Member>();
	const sceneCapacities = new WeakMap<WorldScene, Map<string, { vertices: number; indices: number; }>>();
	// Bumped whenever a layer's draw is replaced or released. A list built
	// before the bump may hold a draw whose buffers are destroyed when the
	// releasing frame closes, so it must not be drawn again.
	let revision = 0;

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
	capacityPlan

	The complete immutable scene is known before its first GPU admission.
	Reserve each new layer once instead of repeatedly uploading its existing
	members as the remaining groups arrive over subsequent frames.
	================
	*/
	function capacityPlan( scene: WorldScene ) {
		let plan = sceneCapacities.get( scene );
		if ( plan ) return plan;
		plan = new Map();
		for ( const group of scene.groups ) {
			if ( !eligible( group ) ) continue;
			const key = keyOf( scene, group );
			let capacity = plan.get( key );
			if ( !capacity ) {
				capacity = { vertices: 0, indices: 0 };
				plan.set( key, capacity );
			}
			capacity.vertices += group.geometry.positions.length / 3;
			capacity.indices += group.geometry.indices.length;
		}
		sceneCapacities.set( scene, plan );
		return plan;
	}

	/*
	================
	allocate

	A layer's GPU geometry for the given capacities: an unindexed vertex
	buffer filled by streamVertices, and an index buffer the cull pass
	fills from a source of the same capacity. No dynamicVertices: the
	members hold the vertices.
	================
	*/
	function allocate( geometry: GeometryCommands, layer: Omit<Layer, "draw">, vertices: number, indices: number ) {
		const draw = geometry.upload(
			{
				world: true,
				material: layer.material,
				positions: new Float32Array( vertices * 3 ),
				indices: new Uint32Array( indices ),
				transform: IDENTITY(),
				instances: IDENTITY()
			},
			layer.image
		);
		geometry.cullDraw( draw, indices );
		return draw;
	}

	/*
	================
	writeSource

	A member's indices, offset by its vertex slot, at its span of the
	layer's cull source.
	================
	*/
	function writeSource( geometry: GeometryCommands, layer: Layer, group: WorldGroup, member: Member ) {
		const own = group.geometry.indices, shifted = new Uint32Array( own.length );
		for ( let i = 0; i < own.length; i++ ) shifted[i] = own[i]! + member.base;
		geometry.cullSource( layer.draw, member.source, shifted );
	}

	/*
	================
	rebuild

	A new draw sized for the live members plus need more vertices and
	indices, with LAYER_GROWTH headroom, and every member re-written at a
	compact slot. It may be smaller than the draw it replaces.
	================
	*/
	function rebuild( geometry: GeometryCommands, layer: Layer, vertexNeed: number, indexNeed: number ) {
		let vertices = vertexNeed, indices = indexNeed;
		for ( const [group, member] of layer.members ) {
			vertices += member.vertices;
			indices += group.geometry.indices.length;
		}
		layer.vertexCapacity = capacityFor( vertices * LAYER_GROWTH );
		layer.indexCapacity = capacityFor( indices * LAYER_GROWTH );
		geometry.release( layer.draw );
		revision++;
		layer.draw = allocate( geometry, layer, layer.vertexCapacity, layer.indexCapacity );
		layer.top = 0;
		layer.sourceTop = 0;
		for ( const [group, member] of layer.members ) {
			member.base = layer.top;
			member.source = layer.sourceTop;
			geometry.streamVertices( layer.draw, member.base, group.geometry.vertices! );
			writeSource( geometry, layer, group, member );
			layer.top += member.vertices;
			layer.sourceTop += group.geometry.indices.length;
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
		eligible,
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
				const planned = capacityPlan( scene ).get( key );
				const shell = {
					key,
					image,
					material: group.geometry.material ?? group.material,
					vertexCapacity: capacityFor( planned?.vertices ?? vertices * LAYER_GROWTH ),
					indexCapacity: capacityFor( planned?.indices ?? indices * LAYER_GROWTH ),
					top: 0,
					sourceTop: 0,
					members: new Map<WorldGroup, Member>(),
					table: new Uint32Array( 0 ),
					dirty: true,
					submitted: -1
				};
				layer = { ...shell, draw: allocate( geometry, shell, shell.vertexCapacity, shell.indexCapacity ) };
				layers.set( key, layer );
			}
			// Released members leave holes in both spans; a rebuild compacts them.
			if ( layer.top + vertices > layer.vertexCapacity || layer.sourceTop + indices > layer.indexCapacity ) {
				rebuild( geometry, layer, vertices, indices );
			}
			const member: Member = { layer, base: layer.top, vertices, source: layer.sourceTop, ranges: [] };
			geometry.streamVertices( layer.draw, member.base, group.geometry.vertices! );
			writeSource( geometry, layer, group, member );
			layer.top += vertices;
			layer.sourceTop += indices;
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

		Releases group's slot; an empty layer releases its draw, and a
		mostly empty one is rebuilt smaller.
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
				revision++;
				layers.delete( layer.key );
				return;
			}
			let live = 0;
			for ( const other of layer.members.values() ) live += other.vertices;
			if ( layer.vertexCapacity > MIN_LAYER_CAPACITY && live * LAYER_SHRINK < layer.vertexCapacity ) {
				rebuild( geometry, layer, 0, 0 );
			}
		},
		/*
		================
		select

		The member's candidate ranges: the cull pass draws those the view sees.
		================
		*/
		select( group: WorldGroup, ranges: readonly TerrainRange[] ) {
			const member = members.get( group )!;
			member.ranges = ranges;
			member.layer.dirty = true;
		},
		/*
		================
		updatePositions

		Seam stitching of a member: kept in its own packed stream and each
		range of it streamed to its slot of the layer. Positions are checked
		before the stream changes, as geometry's own updatePositions checks.
		================
		*/
		updatePositions(
			geometry: GeometryCommands,
			group: WorldGroup,
			positions: Float32Array,
			ranges: readonly (readonly [number, number])[]
		) {
			const member = members.get( group )!, own = group.geometry.vertices!, count = member.vertices;
			if ( positions.length !== count * 3 ) throw Error( "Invalid position update" );
			for ( const [start, length] of ranges ) {
				if (
					!Number.isInteger( start ) || !Number.isInteger( length ) || start < 0 || length < 0 ||
					start + length > count
				) throw Error( "Invalid position range" );
				for ( let i = start * 3; i < (start + length) * 3; i++ ) {
					if ( !Number.isFinite( positions[i]! ) ) throw Error( "Invalid position update" );
				}
			}
			for ( const [start, length] of ranges ) {
				if ( !length ) continue;
				for ( let i = start; i < start + length; i++ ) {
					own[i * VERTEX_FLOATS] = positions[i * 3]!;
					own[i * VERTEX_FLOATS + 1] = positions[i * 3 + 1]!;
					own[i * VERTEX_FLOATS + 2] = positions[i * 3 + 2]!;
				}
				geometry.streamVertices(
					member.layer.draw,
					member.base + start,
					own.subarray( start * VERTEX_FLOATS, (start + length) * VERTEX_FLOATS )
				);
			}
		},
		/*
		================
		view

		The frame's frustum and the water mirror's, which the cull pass tests
		every layer's ranges against (index-cull.ts). Without a layer there is
		nothing to cull.
		================
		*/
		view( geometry: GeometryCommands, main: Float64Array, mirror: Float64Array | null ) {
			if ( layers.size ) geometry.cullView( main, mirror );
		},
		/*
		================
		flush

		Sends the range table of every layer whose candidates changed: each
		range's sphere and its span of the layer's cull source.
		================
		*/
		flush( geometry: GeometryCommands ) {
			for ( const layer of layers.values() ) {
				if ( !layer.dirty ) continue;
				let count = 0;
				for ( const member of layer.members.values() ) count += member.ranges.length;
				if ( layer.table.length < count * RANGE_WORDS ) {
					layer.table = new Uint32Array( count * RANGE_WORDS * 2 );
				}
				const words = layer.table, floats = new Float32Array( words.buffer );
				let at = 0;
				for ( const member of layer.members.values() ) {
					for ( const range of member.ranges ) {
						floats[at] = range.center[0];
						floats[at + 1] = range.center[1];
						floats[at + 2] = range.center[2];
						floats[at + 3] = range.radius;
						words[at + 4] = member.source + range.indexStart;
						words[at + 5] = range.indexCount;
						at += RANGE_WORDS;
					}
				}
				geometry.cullRanges( layer.draw, words, count );
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
			revision++;
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
			revision++;
		},
		/*
		================
		revision

		Changes whenever any layer's draw was replaced or released: a draw
		list built at an older revision must be rebuilt before it is drawn.
		================
		*/
		revision: () => revision,
		stats: () => ({ layers: layers.size, members: members.size })
	};
}
