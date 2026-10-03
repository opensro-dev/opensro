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
	ownedSurface = false
): NavPassage[] {
	const dx = to[0]! - from[0]!, dz = to[2]! - from[2]!, out: NavPassage[] = [];
	/*
	================
	crossing
	================
	*/
	function crossing( p: NavPlacement, edge: number, leaving: boolean ) {
		const a = endpoint( p, edge, 0 ),
			b = endpoint( p, edge, 1 ),
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
		const cell = p.mesh.edges[edge * 6 + 2]!;
		let cx = 0, cz = 0;
		for ( let k = 0; k < 3; k++ ) {
			const at = p.mesh.cells[cell * 3 + k]! * 3, x = p.mesh.vertices[at]!, z = p.mesh.vertices[at + 2]!;
			cx += (Math.cos( p.yaw ) * x - Math.sin( p.yaw ) * z + p.x) / 3;
			cz += (Math.sin( p.yaw ) * x + Math.cos( p.yaw ) * z + p.z) / 3;
		}
		const side = sx * (cz - a[2]!) - sz * (cx - a[0]!),
			approach = sx * (from[2]! - a[2]!) - sz * (from[0]! - a[0]!);
		if ( (approach * side > 0) !== leaving ) {
			return null;
		}
		return t;
	}
	for ( let source = 0; source < objects.length; source++ ) {
		const p = objects[source]!;
		for ( const link of p.links ?? [] ) {
			const q = objects[link.target]!;
			let a = crossing( p, link.edge, true ), b = crossing( q, link.targetEdge, false );
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
	}
	return out;
}
