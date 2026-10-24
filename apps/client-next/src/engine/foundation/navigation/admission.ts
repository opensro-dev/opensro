/*
===========================================================================

admission.ts - validates a published object navigation product

The navigation owner installs products the asset worker publishes. Before
any of it reaches the clipper, every placement, mesh, edge, outline grid,
event table and terrain registration is bounds-checked, and the product's
resident size is measured against the 64 MiB navigation budget.

===========================================================================
*/
import { validateLinks } from "@/engine/foundation/navigation/topology";
import type { NavPlacement, NavigationProduct } from "@/engine/contracts/navigation";

/*
================
admitObjectProduct

Throws on any malformed or over-budget part; returns the resident bytes.
================
*/
export function admitObjectProduct( product: NavigationProduct, regionId: number ): number {
	if (
		product.regionId !== regionId || !Array.isArray( product.objects ) || product.objects.length > 32768
	) throw new Error( "Invalid navigation product" );
	const seen = new Set<NavPlacement["mesh"]>(),
		seenObstacles = new Set<NavPlacement["obstacles"]>(),
		seenTerrainCells = new Set<NavPlacement["terrainCells"]>();
	let bytes = product.objects.length * 40;
	const placements: readonly NavPlacement[] = product.objects;
	validateLinks( placements );
	for ( const p of placements ) {
		if ( !p ) throw new Error( "Invalid collision placement" );
		if ( p.terrainCells !== undefined && !seenTerrainCells.has( p.terrainCells ) ) {
			if (
				!Array.isArray( p.terrainCells ) || p.terrainCells.length > 65536 ||
				p.terrainCells.some( r =>
					!Array.isArray( r ) || r.length !== 4 || !r.every( Number.isFinite ) || r[0] > r[2] ||
					r[1] > r[3]
				)
			) throw Error( "Invalid terrain object cells" );
			seenTerrainCells.add( p.terrainCells );
			bytes += p.terrainCells.length * 32;
		}
		if (
			!p || ![ p.x, p.y, p.z, p.yaw ].every( Number.isFinite ) || !p.mesh ||
			p.floor !== undefined && (!Number.isInteger( p.floor ) || p.floor < 0 || p.floor > 0xffffffff)
		) throw new Error( "Invalid collision placement" );
		if (
			p.obstacles &&
			(!Array.isArray( p.obstacles ) || p.obstacles.length > 65536 ||
				p.obstacles.some( o =>
					![ o.x, o.y, o.z, o.radiusSquared ].every( Number.isFinite ) || o.radiusSquared < 0
				))
		) throw new Error( "Invalid navigation obstacle" );
		bytes += (p.links?.length ?? 0) * 12;
		if ( p.obstacles && !seenObstacles.has( p.obstacles ) ) {
			bytes += p.obstacles.length * 32;
			seenObstacles.add( p.obstacles );
		}
		if ( bytes > 64 * 1024 * 1024 ) throw new Error( "Navigation residency budget" );
		const m = p.mesh;
		if ( seen.has( m ) ) continue;
		seen.add( m );
		if (
			!(m.vertices instanceof Float32Array) || !(m.cells instanceof Uint16Array) ||
			!(m.edges instanceof Uint32Array) || m.vertices.length % 3 || m.cells.length % 3 ||
			m.edges.length % 6 || !m.vertices.every( Number.isFinite ) || m.bounds.length !== 6 ||
			!m.bounds.every( Number.isFinite ) || m.cells.some( i => i >= m.vertices.length / 3 )
		) throw new Error( "Invalid collision mesh" );
		if (
			m.vertexDirections &&
			(!(m.vertexDirections instanceof Uint8Array) ||
				m.vertexDirections.length !== m.vertices.length / 3)
		) throw Error( "Invalid navigation vertex directions" );
		if (
			m.cellWords &&
				(!(m.cellWords instanceof Uint16Array) || m.cellWords.length !== m.cells.length / 3) ||
			m.cellEvents &&
				(!(m.cellEvents instanceof Uint8Array) || m.cellEvents.length !== m.cells.length / 3) ||
			m.edgeEvents &&
				(!(m.edgeEvents instanceof Uint8Array) || m.edgeEvents.length !== m.edges.length / 6) ||
			m.eventNames &&
				(!Array.isArray( m.eventNames ) || m.eventNames.length > 65536 ||
					m.eventNames.some( name => typeof name !== "string" || name.length > 65536 ))
		) throw new Error( "Invalid navigation events" );
		if ( m.outlineGrid ) {
			const g = m.outlineGrid;
			if (
				![ g.x, g.z ].every( Number.isFinite ) || ![ g.nx, g.nz ].every( n =>
					Number.isInteger( n ) && n >= 0 && n <= 65536
				) || g.nx * g.nz > 1048576 || !(g.offsets instanceof Uint32Array) ||
				!(g.edgeIds instanceof Uint16Array) || g.offsets.length !== g.nx * g.nz + 1 ||
				g.offsets[0] !== 0 || g.offsets.at( -1 ) !== g.edgeIds.length ||
				g.edgeIds.length > 8 * 1024 * 1024
			) throw Error( "Invalid navigation outline grid" );
			for ( let i = 1; i < g.offsets.length; i++ ) {
				if ( g.offsets[i]! < g.offsets[i - 1]! ) throw Error( "Invalid navigation grid offsets" );
			}
			for ( const id of g.edgeIds ) {
				if ( id >= m.edges.length / 6 || m.edges[id * 6 + 5] !== 0 ) {
					throw Error( "Invalid navigation grid edge" );
				}
			}
			bytes += g.offsets.byteLength + g.edgeIds.byteLength;
		}
		bytes += (m.vertexDirections?.byteLength ?? 0) + (m.cellWords?.byteLength ?? 0) +
			(m.cellEvents?.byteLength ?? 0) + (m.edgeEvents?.byteLength ?? 0) +
			(m.eventNames?.reduce( ( n, name ) => n + name.length * 2, 0 ) ?? 0);
		bytes += m.vertices.byteLength + m.cells.byteLength + m.edges.byteLength;
		if ( bytes > 64 * 1024 * 1024 ) throw new Error( "Navigation residency budget" );
		for ( let i = 0; i < m.edges.length; i += 6 ) {
			if (
				m.edges[i]! >= m.vertices.length / 3 || m.edges[i + 1]! >= m.vertices.length / 3 ||
				m.edges[i + 2]! >= m.cells.length / 3 ||
				m.edges[i + 3] !== 65535 && m.edges[i + 3]! >= m.cells.length / 3 ||
				m.edges[i + 4]! > 255 || m.edges[i + 5]! > 1
			) throw new Error( "Invalid collision edge" );
		}
	}
	return bytes;
}
