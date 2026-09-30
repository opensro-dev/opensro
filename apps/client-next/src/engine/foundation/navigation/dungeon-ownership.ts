/*
===========================================================================

dungeon-ownership.ts - which navmesh cell owns a mover along a chord

Terrain movers enter an object only through its outline (404510 -> 403FB0
-> 428300); a mover already on an object follows its cells and explicit
placement links. An unretained start takes native FindNavCell's surface:
the nearest by |deltaY|, terrain winning ties.

===========================================================================
*/
import type { NavigationIndex } from "./spatial-index";
import type { NavPlacement } from "@/engine/contracts/navigation";
import { navCrossings, navHeight, navLocal, terrainVisit } from "./object-navigation";
import { linkPassages } from "./topology";
import { ownedPlacementStart } from "./owned-start";
export interface NavOwner {
	readonly placement: number;
	readonly cell: number;
}
export interface NavOwnerSpan extends NavOwner {
	readonly from: number;
	readonly to: number;
}
export interface NavOwnerPath {
	readonly spans: readonly NavOwnerSpan[];
	readonly stop: number;
	readonly owner: NavOwner | null;
	/** [visit, crossing] fractions an outline entry skips (428300 moves the walker to the crossing). */
	readonly bridges?: readonly (readonly [number, number])[];
}
// 404510 -> 403fb0 -> 428300: terrain enters an object through its outline.
// Height proximity is an initialization rule, not an outline-crossing gate.
/*
================
terrainOwnerPath
The owner path of a terrain mover: a resident object that wins
FindNavCell, else the first outline entry along the chord.
================
*/
export function terrainOwnerPath(
	objects: readonly NavPlacement[],
	from: readonly number[],
	to: readonly number[],
	preferred?: NavOwner,
	index?: NavigationIndex,
	terrainY?: number
): NavOwnerPath {
	let resident = dungeonOwnerPath( objects, from, to, preferred, index );
	// An unretained mover stands where native FindNavCell puts it (the server's
	// ResolveNavOwner): the nearest surface by |deltaY|, terrain winning ties. A
	// deck level with the ground does not capture a mover walking on terrain.
	if ( resident.owner && !preferred && terrainY !== undefined && resident.spans[0] ) {
		const s0 = resident.spans[0],
			p = objects[s0.placement]!,
			q = navLocal( p, from[0]!, from[1]!, from[2]! ),
			h = navHeight( p.mesh, q[0], q[2], q[1], s0.cell );
		if ( h !== null && Math.abs( terrainY - from[1]! ) <= Math.abs( h + p.y - from[1]! ) ) {
			resident = { spans: [], stop: 0, owner: null };
		}
	}
	if ( resident.owner ) return resident;
	let entry: NavOwner | null = null, fraction = Infinity, entryVisit = Infinity;
	for ( const placement of index?.placements( from, to ) ?? objects.map( ( _, i ) => i ) ) {
		// Native visit order (404510): stepped once the walker stands in one of
		// the placement's registered terrain cells; ordered by (visit, crossing).
		const visit = terrainVisit( objects[placement]!.terrainCells, from, to );
		if ( visit === Infinity || visit > entryVisit ) continue;
		const p = objects[placement]!,
			a = index?.local( p, from ) ?? navLocal( p, from[0]!, from[1]!, from[2]! ),
			b = index?.local( p, to ) ?? navLocal( p, to[0]!, to[1]!, to[2]! ),
			m = p.mesh,
			v = m.vertices;
		const bounds = m.bounds;
		if (
			Math.max( a[0], b[0] ) < bounds[0]! || Math.min( a[0], b[0] ) > bounds[3]! ||
			Math.max( a[2], b[2] ) < bounds[2]! || Math.min( a[2], b[2] ) > bounds[5]!
		) continue;
		const rx = b[0] - a[0], rz = b[2] - a[2];
		for ( const edge of index?.edges( m, a, b ) ?? Array.from( { length: m.edges.length / 6 }, ( _, i ) => i ) ) {
			const i = edge * 6;
			const e = m.edges;
			if ( e[i + 5] !== 0 || (e[i + 4]! & 0x11) ) continue;
			const aa = e[i]! * 3,
				bb = e[i + 1]! * 3,
				ax = v[aa]!,
				az = v[aa + 2]!,
				sx = v[bb]! - ax,
				sz = v[bb + 2]! - az,
				den = rx * sz - rz * sx;
			if ( Math.abs( den ) < 1e-12 ) continue;
			const t = ((ax - a[0]) * sz - (az - a[2]) * sx) / den, u = ((ax - a[0]) * rz - (az - a[2]) * rx) / den;
			if ( t < 0 || t < visit - 1e-9 || t >= 1 || u < 0 || u > 1 ) continue;
			if ( visit === entryVisit && t >= fraction ) continue;
			const cell = e[i + 2]!;
			let cx = 0, cz = 0;
			for ( let k = 0; k < 3; k++ ) {
				const at = m.cells[cell * 3 + k]! * 3;
				cx += v[at]! / 3;
				cz += v[at + 2]! / 3;
			}
			const side = sx * (cz - az) - sz * (cx - ax), start = sx * (a[2] - az) - sz * (a[0] - ax);
			if ( start * side > 0 ) continue;
			entry = { placement, cell };
			fraction = t;
			entryVisit = visit;
		}
	}
	if ( !entry ) return resident;
	const t = Math.min( 1, fraction + 1e-7 ),
		start = from.map( ( v, i ) => v + (to[i]! - v) * t ),
		p = objects[entry.placement]!,
		q = navLocal( p, start[0]!, start[1]!, start[2]! );
	const h = navHeight( p.mesh, q[0], q[2], q[1], entry.cell );
	if ( h === null ) return resident;
	start[1] = h + p.y;
	const path = dungeonOwnerPath( objects, start, to, entry, index );
	return {
		bridges: [ [ entryVisit, fraction ] ],
		owner: path.owner,
		stop: t + (1 - t) * path.stop,
		spans: path.spans.map( ( span, i ) => ({
			...span,
			from: i === 0 ? fraction : t + (1 - t) * span.from,
			to: t + (1 - t) * span.to
		}) )
	};
}
// Ownership cannot be inferred afresh from nearest Y at every point.
// Height gates initial admission only: a straight endpoint-Y chord can lie
// below a connected stair/ramp. Once owned, project onto each visited triangle.
// Retain the current triangle, then follow a resident cell edge or an explicit placement link.
/*
================
dungeonOwnerPath
The owner path of a mover on an object mesh, cell by cell.
================
*/
export function dungeonOwnerPath(
	objects: readonly NavPlacement[],
	from: readonly number[],
	to: readonly number[],
	preferred?: NavOwner,
	index?: NavigationIndex
): NavOwnerPath {
	const point = ( t: number ) => from.map( ( v, i ) => v + (to[i]! - v) * t );
	/*
================
contains
================
	*/
	function contains( owner: NavOwner, t: number, initial = false ) {
		const p = objects[owner.placement]!,
			v = point( t ),
			q = index?.local( p, v ) ?? navLocal( p, v[0]!, v[1]!, v[2]! ),
			h = navHeight( p.mesh, q[0], q[2], q[1], owner.cell );
		return h !== null && (!initial || Math.abs( h - q[1] ) <= 2);
	}
	if (
		preferred &&
		(!Number.isInteger( preferred.placement ) || !Number.isInteger( preferred.cell ) || preferred.placement < 0 ||
			preferred.cell < 0 || !objects[preferred.placement] ||
			preferred.cell >= objects[preferred.placement]!.mesh.cells.length / 3)
	) throw new Error( "Invalid dungeon owner" );
	if ( preferred ) from = ownedPlacementStart( objects[preferred.placement]!, preferred.cell, from );
	let owner: NavOwner | null = null, delta = Infinity;
	// 428F40 dispatches a retained mesh owner to 428930. Its cell plane,
	// rather than an interpolated endpoint-Y chord, owns height on stairs.
	if ( preferred && contains( preferred, 0 ) ) {
		owner = preferred;
		delta = 0;
	}
	for (
		const placement of owner ?
			[] :
			preferred ?
			[ preferred.placement ] :
			index?.placements( from ) ?? objects.map( ( _, i ) => i )
	) {
		if ( preferred && placement !== preferred.placement ) continue;
		const p = objects[placement]!, q = index?.local( p, from ) ?? navLocal( p, from[0]!, from[1]!, from[2]! );
		const bounds = p.mesh.bounds;
		if ( q[0] < bounds[0]! || q[0] > bounds[3]! || q[2] < bounds[2]! || q[2] > bounds[5]! ) continue;
		for (
			const cell of index?.cells( p.mesh, q ) ?? Array.from( { length: p.mesh.cells.length / 3 }, ( _, i ) => i )
		) {
			const h = navHeight( p.mesh, q[0], q[2], q[1], cell );
			if ( h !== null && Math.abs( h - q[1] ) < delta ) {
				delta = Math.abs( h - q[1] );
				owner = { placement, cell };
			}
		}
	}
	if ( !owner || delta > 2 ) return { spans: [], stop: 0, owner: null };
	const cuts = [ 0, 1 ], spans: NavOwnerSpan[] = [], passages = linkPassages( objects, from, to, true );
	for ( const placement of index?.placements( from, to ) ?? objects.map( ( _, i ) => i ) ) {
		navCrossings( objects[placement]!, from, to, cuts, index );
	}
	cuts.sort( ( a, b ) => a - b );
	for ( let i = 1; i < cuts.length; i++ ) {
		const lo = cuts[i - 1]!, hi = cuts[i]!;
		if ( hi - lo < 1e-8 ) continue;
		const t = (lo + hi) / 2;
		if ( !contains( owner, t ) ) {
			const p: NavPlacement = objects[owner.placement]!,
				cell = p.mesh.cells.subarray( owner.cell * 3, owner.cell * 3 + 3 );
			let next: NavOwner | null = null;
			const sample = point( t ),
				local = index?.local( p, sample ) ?? navLocal( p, sample[0]!, sample[1]!, sample[2]! );
			for (
				const c of index?.cells( p.mesh, local ) ??
					Array.from( { length: p.mesh.cells.length / 3 }, ( _, i ) => i )
			) {
				if ( c === owner.cell ) continue;
				// Native cell adjacency is geometric even when a synthetic mesh omits its
				// edge table. Sharing a point is insufficient; two vertices form an edge.
				let shared = 0;
				for ( let k = 0; k < 3; k++ ) if ( cell.includes( p.mesh.cells[c * 3 + k]! ) ) shared++;
				if ( shared >= 2 && contains( { placement: owner.placement, cell: c }, t ) ) {
					next = { placement: owner.placement, cell: c };
					break;
				}
			}
			if ( !next ) {
				for ( const link of passages ) {
					if ( link.source !== owner.placement || lo > link.to || hi < link.from ) continue;
					const target = objects[link.target]!, entry = target.mesh.edges[link.targetEdge * 6 + 2]!;
					if ( contains( { placement: link.target, cell: entry }, t ) ) {
						next = { placement: link.target, cell: entry };
						break;
					}
					if ( t >= link.from && t <= link.to ) {
						next = owner;
						break;
					}
				}
			}
			if ( !next ) return { spans, stop: lo, owner };
			owner = next;
		}
		const last = spans.at( -1 );
		if ( last && last.placement === owner.placement && last.cell === owner.cell ) {
			spans[spans.length - 1] = { ...last, to: hi };
		} else spans.push( { ...owner, from: lo, to: hi } );
	}
	return { spans, stop: 1, owner };
}
