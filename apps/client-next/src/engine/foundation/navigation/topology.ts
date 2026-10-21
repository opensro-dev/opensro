/*
===========================================================================

topology.ts - object navmesh links and passages between placements

Decodes the native object link table (403fb0) and finds the passages a
chord crosses from one placed object to another.

===========================================================================
*/

import { navHeight, navLocal } from "./object-navigation";
import type { NavPlacement, NavLink } from "@/engine/contracts/navigation";
import { base64Bytes } from "@/engine/foundation/assets/base64";
import { ownedCellStart } from "./owned-start";
import { edgeResponse } from "./contact-response";
import type { NavOwner, NavOwnerSpan } from "./dungeon-ownership";

/*
================
linkedEntry

403FB0 resolves the authored link, then 428CB0 repairs the reached point
inside the receiving edge's triangle. A second edge crossing on the same
finite chord is not required; the two authored edges can have a gap.
================
*/
export function linkedEntry(
	objects: readonly NavPlacement[],
	from: readonly number[],
	to: readonly number[],
	spans: readonly NavOwnerSpan[]
): { fraction: number; point: readonly number[]; owner: NavOwner; status: number; } | null {
	let best: ReturnType<typeof linkedEntry> = null;
	for ( const span of spans ) {
		const p = objects[span.placement]!, m = p.mesh;
		if ( !p.links?.length ) continue;
		const a = navLocal( p, from[0]!, from[1]!, from[2]! ), b = navLocal( p, to[0]!, to[1]!, to[2]! );
		const dx = b[0] - a[0], dz = b[2] - a[2];
		for ( const link of p.links ) {
			const e = link.edge * 6, flags = m.edges[e + 4]!;
			if ( !(flags & 8) || m.edges[e + 2] !== span.cell ) continue;
			const va = m.edges[e]! * 3, vb = m.edges[e + 1]! * 3;
			const ax = m.vertices[va]!,
				az = m.vertices[va + 2]!,
				sx = m.vertices[vb]! - ax,
				sz = m.vertices[vb + 2]! - az;
			const den = dx * sz - dz * sx;
			if ( Math.abs( den ) < 1e-12 ) continue;
			const t = ((ax - a[0]) * sz - (az - a[2]) * sx) / den;
			const u = ((ax - a[0]) * dz - (az - a[2]) * dx) / den;
			if (
				t <= 0 || t > 1 || t < span.from - 1e-9 || t > span.to + 1e-9 || u < 0 || u > 1 ||
				best && t >= best.fraction
			) continue;
			let cx = 0, cz = 0;
			for ( let i = 0; i < 3; i++ ) {
				const at = m.cells[span.cell * 3 + i]! * 3;
				cx += m.vertices[at]! / 3;
				cz += m.vertices[at + 2]! / 3;
			}
			if ( (sx * (cz - az) - sz * (cx - ax)) * (sx * (a[2] - az) - sz * (a[0] - ax)) <= 0 ) continue;
			const response = edgeResponse(
				[ cx, cz ],
				[ a[0] + dx * t, a[2] + dz * t ],
				[ b[0], b[2] ],
				flags,
				true,
				1
			);
			if ( response.status & 1 ) continue;
			const x = response.point[0], z = response.point[1], c = Math.cos( p.yaw ), s = Math.sin( p.yaw );
			const target = objects[link.target]!, cell = target.mesh.edges[link.targetEdge * 6 + 2]!;
			const q = navLocal( target, c * x - s * z + p.x, from[1]!, s * x + c * z + p.z );
			const entry = ownedCellStart( target.mesh, cell, q[0], q[2] );
			const y = navHeight( target.mesh, entry[0], entry[1], q[1], cell );
			if ( y === null ) continue;
			const tc = Math.cos( target.yaw ), ts = Math.sin( target.yaw );
			best = {
				fraction: t,
				owner: { placement: link.target, cell },
				status: response.status,
				point: [
					tc * entry[0] - ts * entry[1] + target.x,
					y + target.y,
					ts * entry[0] + tc * entry[1] + target.z
				]
			};
		}
	}
	return best;
}
/*
================
objectLinks
================
*/
// 403fb0: u16 neighbor object, u16 neighbor outline edge, u16 source outline edge.
export function objectLinks( count: number = 0, encoded?: string ) {
	if ( !Number.isInteger( count ) || count < 0 || count > 65535 ) {
		throw new Error( "Invalid object link count" );
	}
	if ( !count ) {
		if ( encoded ) {
			throw new Error( "Unexpected object links" );
		}
		return [];
	}
	if ( typeof encoded !== "string" || encoded.length > count * 8 + 8 ) {
		throw new Error( "Missing object link bytes" );
	}
	const raw = base64Bytes( encoded );
	if ( raw.length !== count * 6 ) {
		throw new Error( "Object link length" );
	}
	const v = new DataView( raw.buffer ), links: NavLink[] = [];
	for ( let i = 0; i < count; i++ ) {
		links.push( {
			target: v.getUint16( i * 6, true ),
			targetEdge: v.getUint16( i * 6 + 2, true ),
			edge: v.getUint16( i * 6 + 4, true )
		} );
	}
	return links;
}
/*
================
endpoint
================
*/
function endpoint( p: NavPlacement, edge: number, end: number ) {
	const at = p.mesh.edges[edge * 6 + end]! * 3,
		v = p.mesh.vertices,
		x = v[at]!,
		z = v[at + 2]!,
		c = Math.cos( p.yaw ),
		s = Math.sin( p.yaw );
	return [ Math.fround( c * x - s * z + p.x ), Math.fround( v[at + 1]! + p.y ), Math.fround( s * x + c * z + p.z ) ];
}
/*
================
portalCongruent
================
*/
export function portalCongruent( a: NavPlacement, edge: number, b: NavPlacement, other: number ) {
	const a0 = endpoint( a, edge, 0 ),
		a1 = endpoint( a, edge, 1 ),
		b0 = endpoint( b, other, 0 ),
		b1 = endpoint( b, other, 1 );
	// 45d180: strict distance < 5; try reversed endpoints only if the first pair fails.
	const near = ( x: number[], y: number[] ) => {
		const d = x.map( ( n, i ) => Math.fround( n - y[i]! ) );
		return Math.fround( Math.sqrt( Math.fround( d[0]! ** 2 + d[1]! ** 2 + d[2]! ** 2 ) ) ) < 5;
	};
	return near( a0, b0 ) ? near( a1, b1 ) : near( a1, b0 ) && near( a0, b1 );
}
/*
================
dungeonLinks
================
*/
export function dungeonLinks(
	objects: readonly NavPlacement[],
	neighbors: readonly (readonly number[])[]
): NavPlacement[] {
	const groups = neighbors.map( () => [] as number[] );
	objects.forEach( ( p, i ) => {
		if ( p.block === undefined || !groups[p.block] ) {
			throw new Error( "Invalid dungeon block" );
		}
		groups[p.block]!.push( i );
	} );
	return objects.map( p => {
		const links: NavLink[] = [];
		for ( let e = 0; e < p.mesh.edges.length / 6; e++ ) {
			if ( p.mesh.edges[e * 6 + 5] !== 0 || !(p.mesh.edges[e * 6 + 4]! & 8) ) {
				continue;
			}
			search: for ( const block of neighbors[p.block!]! ) {
				if ( !groups[block] ) {
					throw new Error( "Invalid dungeon neighbor" );
				}
				for ( const target of groups[block]! ) {
					const q = objects[target]!;
					for ( let k = 0; k < q.mesh.edges.length / 6; k++ ) {
						if (
							q.mesh.edges[k * 6 + 5] === 0 && (q.mesh.edges[k * 6 + 4]! & 8) &&
							portalCongruent( p, e, q, k )
						) {
							links.push( { edge: e, target, targetEdge: k } );
							break search;
						}
					}
				}
			}
		}
		return { ...p, links };
	} );
}
/*
================
validateLinks
================
*/
export function validateLinks( objects: readonly NavPlacement[] ) {
	for ( const p of objects ) {
		const seen = new Set<number>();
		for ( const link of p.links ?? [] ) {
			const q = objects[link.target];
			if (
				!Number.isInteger( link.edge ) || !Number.isInteger( link.target ) ||
				!Number.isInteger( link.targetEdge ) || link.edge < 0 || link.targetEdge < 0 || !q ||
				p.mesh.edges[link.edge * 6 + 5] !== 0 || q.mesh.edges[link.targetEdge * 6 + 5] !== 0 ||
				!(p.mesh.edges[link.edge * 6 + 4]! & 8) || seen.has( link.edge )
			) {
				throw new Error( "Invalid navigation link" );
			}
			seen.add( link.edge );
		}
	}
}
// Padding on the chord and edge boxes of the passage broad-phase: an edge whose
// box misses the chord's by more than this cannot meet it, and the pad keeps
// float rounding at a touching box on the side of testing the edge.
const PASSAGE_BOX_PAD = 1e-6;

/*
================
LinkEdge

One link edge in world space: its two ends (endpoint's float values), the
entry cell's centroid as crossing computes it, and the edge's box.
================
*/
interface LinkEdge {
	readonly a: readonly number[];
	readonly b: readonly number[];
	readonly cx: number;
	readonly cz: number;
	readonly minX: number;
	readonly maxX: number;
	readonly minZ: number;
	readonly maxZ: number;
}

/*
================
linkEdge
================
*/
function linkEdge( p: NavPlacement, edge: number ): LinkEdge {
	const a = endpoint( p, edge, 0 ), b = endpoint( p, edge, 1 ), cell = p.mesh.edges[edge * 6 + 2]!;
	let cx = 0, cz = 0;
	for ( let k = 0; k < 3; k++ ) {
		const at = p.mesh.cells[cell * 3 + k]! * 3, x = p.mesh.vertices[at]!, z = p.mesh.vertices[at + 2]!;
		cx += (Math.cos( p.yaw ) * x - Math.sin( p.yaw ) * z + p.x) / 3;
		cz += (Math.sin( p.yaw ) * x + Math.cos( p.yaw ) * z + p.z) / 3;
	}
	return {
		a,
		b,
		cx,
		cz,
		minX: Math.min( a[0]!, b[0]! ),
		maxX: Math.max( a[0]!, b[0]! ),
		minZ: Math.min( a[2]!, b[2]! ),
		maxZ: Math.max( a[2]!, b[2]! )
	};
}

/*
================
RegionLink

One authored link of a region with both edges and the box around them, so a
chord can pass over a link whose edges lie nowhere near it in one test.
================
*/
interface RegionLink {
	readonly source: number;
	readonly link: NavLink;
	readonly sourceEdge: LinkEdge;
	readonly targetEdge: LinkEdge;
	readonly minX: number;
	readonly maxX: number;
	readonly minZ: number;
	readonly maxZ: number;
}

/*
================
PassageLinks

Every authored link of one placement list in world space. The navigation
index owns one per installed region (spatial-index.ts), so the edges are
transformed once per install instead of on every chord: the Donwhang cave
ran every link of the region per walking entity per step.
================
*/
export interface PassageLinks {
	readonly objects: readonly NavPlacement[];
	readonly links: readonly RegionLink[];
}

/*
================
createPassageLinks
================
*/
export function createPassageLinks( objects: readonly NavPlacement[] ): PassageLinks {
	const links: RegionLink[] = [];
	for ( let source = 0; source < objects.length; source++ ) {
		const p = objects[source]!;
		for ( const link of p.links ?? [] ) {
			const sourceEdge = linkEdge( p, link.edge ),
				targetEdge = linkEdge( objects[link.target]!, link.targetEdge );
			links.push( {
				source,
				link,
				sourceEdge,
				targetEdge,
				minX: Math.min( sourceEdge.minX, targetEdge.minX ),
				maxX: Math.max( sourceEdge.maxX, targetEdge.maxX ),
				minZ: Math.min( sourceEdge.minZ, targetEdge.minZ ),
				maxZ: Math.max( sourceEdge.maxZ, targetEdge.maxZ )
			} );
		}
	}
	return { objects, links };
}

export interface NavPassage {
	readonly source: number;
	readonly target: number;
	readonly edge: number;
	readonly targetEdge: number;
	readonly from: number;
	readonly to: number;
}
/*
================
linkPassages
================
*/
// A native link enters the destination edge's cell, including a small authored
// seam gap. 428cb0 -> 45c1b0 additionally nudges entry toward the target cell
// centroid by at most 0.199999988 native units; retain that bounded seam window.
export function linkPassages(
	objects: readonly NavPlacement[],
	from: readonly number[],
	to: readonly number[],
	ownedSurface = false,
	prepared?: PassageLinks
): NavPassage[] {
	const dx = to[0]! - from[0]!, dz = to[2]! - from[2]!, out: NavPassage[] = [];
	// Links prepared for another placement list (a stale index) are rebuilt.
	const links = prepared?.objects === objects ? prepared.links : createPassageLinks( objects ).links;
	const minX = Math.min( from[0]!, to[0]! ) - PASSAGE_BOX_PAD,
		maxX = Math.max( from[0]!, to[0]! ) + PASSAGE_BOX_PAD,
		minZ = Math.min( from[2]!, to[2]! ) - PASSAGE_BOX_PAD,
		maxZ = Math.max( from[2]!, to[2]! ) + PASSAGE_BOX_PAD;
	/*
	================
	misses

	The chord cannot cross an edge whose box lies apart from the chord's:
	crossing would find t or u outside [0, 1].
	================
	*/
	function misses( e: { minX: number; maxX: number; minZ: number; maxZ: number; } ) {
		return e.maxX < minX || e.minX > maxX || e.maxZ < minZ || e.minZ > maxZ;
	}
	/*
	================
	crossing
	================
	*/
	function crossing( e: LinkEdge, leaving: boolean ) {
		if ( misses( e ) ) {
			return null;
		}
		const a = e.a,
			b = e.b,
			sx = b[0]! - a[0]!,
			sz = b[2]! - a[2]!,
			den = dx * sz - dz * sx;
		if ( Math.abs( den ) < 1e-12 ) {
			return null;
		}
		const t = ((a[0]! - from[0]!) * sz - (a[2]! - from[2]!) * sx) / den,
			u = ((a[0]! - from[0]!) * dz - (a[2]! - from[2]!) * dx) / den;
		if ( t < 0 || t > 1 || u < 0 || u > 1 ) {
			return null;
		}
		if ( !ownedSurface && Math.abs( from[1]! + (to[1]! - from[1]!) * t - (a[1]! + (b[1]! - a[1]!) * u) ) > 2 ) {
			return null;
		}
		const cx = e.cx, cz = e.cz;
		const side = sx * (cz - a[2]!) - sz * (cx - a[0]!),
			approach = sx * (from[2]! - a[2]!) - sz * (from[0]! - a[0]!);
		if ( (approach * side > 0) !== leaving ) {
			return null;
		}
		return t;
	}
	for ( const region of links ) {
		// Neither edge can be crossed: no passage, and no inside() rescue.
		if ( misses( region ) ) {
			continue;
		}
		const source = region.source, link = region.link, p = objects[source]!, q = objects[link.target]!;
		let a = crossing( region.sourceEdge, true ), b = crossing( region.targetEdge, false );
		const inside = ( r: NavPlacement, edge: number, t: number ) => {
			const v = navLocal( r, from[0]! + dx * t, from[1]! + (to[1]! - from[1]!) * t, from[2]! + dz * t ),
				h = navHeight( r.mesh, v[0], v[2], v[1], r.mesh.edges[edge * 6 + 2] );
			return h !== null && Math.abs( h - v[1] ) <= 2;
		};
		if ( a !== null && b === null && inside( q, link.targetEdge, a ) ) {
			b = a;
		} else if ( b !== null && a === null && inside( p, link.edge, b ) ) {
			a = b;
		}
		if ( a === null || b === null ) {
			continue;
		}
		if ( Math.abs( a - b ) * Math.hypot( dx, dz, to[1]! - from[1]! ) >= 5 ) {
			continue;
		}
		out.push( {
			source,
			target: link.target,
			edge: link.edge,
			targetEdge: link.targetEdge,
			from: Math.max( 0, Math.min( a, b ) - 0.19999998807907104 / Math.max( Math.hypot( dx, dz ), 1e-12 ) ),
			to: Math.min( 1, Math.max( a, b ) + 0.19999998807907104 / Math.max( Math.hypot( dx, dz ), 1e-12 ) )
		} );
	}
	return out;
}
