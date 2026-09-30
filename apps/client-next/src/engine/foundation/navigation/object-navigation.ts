/*
===========================================================================

object-navigation.ts - placed-object navmeshes: decode, height and contact

Decodes the BMS offset-7 navigation payload and answers the per-object
questions the movement clipper asks: local frame, cell height, the first
blocking edge on a chord, and the native visit order of a terrain walker
(CRTNavMeshTerrain_Move 404510 -> CRTNavMeshObj_EnterFromOutside 428300).

===========================================================================
*/
import { outsideEdgeStart } from "./vertex-direction";
import type { NavigationIndex } from "./spatial-index";
import { cellEntry, edgeResponse, slideNormal } from "./contact-response";
import type { NavPassage } from "./topology";
import type { NavMesh, NavPlacement, ObjectNavWire } from "@/engine/contracts/navigation";
// Native BMS offset 7: sub_4265b0, sub_426160. Header word 11 is flags, not an offset.
/*
================
objectNavigation
================
*/
export function objectNavigation( row: ObjectNavWire ): NavMesh[] {
	const offsets = row.headerOffsets as number[];
	if ( !Array.isArray( offsets ) || !offsets[7] ) return [];
	const flags = offsets[11] ?? 0;
	if ( flags & ~7 ) throw new Error( "Unsupported object navigation flags" );
	const payload = row.nativePayloads?.find( p =>
		p.kind === "bms-offset7-post-payload-tail" && p.byteOffset === offsets[7]
	);
	if ( !payload ) throw new Error( "Missing object navigation payload" );
	if ( typeof payload.rawBase64 !== "string" || payload.rawBase64.length > 32 * 1024 * 1024 ) {
		throw new Error( "Object navigation payload budget" );
	}
	const raw = Uint8Array.from( atob( payload.rawBase64 ), c => c.charCodeAt( 0 ) );
	if ( raw.length !== payload.byteLength ) throw new Error( "Object navigation byte length" );
	const end = Math.min( row.byteLength, ...offsets.filter( ( n, i ) => i !== 11 && n > offsets[7]! ) ),
		size = end - offsets[7]!;
	if ( !Number.isInteger( size ) || size < 0 || size > raw.length ) {
		throw new Error( "Object navigation section bounds" );
	}
	const v = new DataView( raw.buffer, 0, size );
	let o = 0;
	function take( n: number ) {
		if ( !Number.isSafeInteger( n ) || n < 0 || o + n > size ) throw new Error( "Truncated object navigation" );
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true ),
		f32 = () => {
			const n = v.getFloat32( take( 4 ), true );
			if ( !Number.isFinite( n ) ) throw new Error( "Invalid nav coordinate" );
			return n;
		};
	const count = ( limit: number ) => {
		const n = u32();
		if ( n > limit ) throw new Error( "Navigation count budget" );
		return n;
	};
	const n = count( 65536 );
	const vertices = new Float32Array( n * 3 ),
		vertexDirections = new Uint8Array( n ),
		bounds = [ Infinity, Infinity, Infinity, -Infinity, -Infinity, -Infinity ];
	for ( let i = 0; i < n; i++ ) {
		for ( let k = 0; k < 3; k++ ) {
			const x = f32();
			vertices[i * 3 + k] = x;
			bounds[k] = Math.min( bounds[k]!, x );
			bounds[k + 3] = Math.max( bounds[k + 3]!, x );
		}
		vertexDirections[i] = u8();
	}
	const nc = count( 65536 ),
		cells = new Uint16Array( nc * 3 ),
		cellWords = new Uint16Array( nc ),
		cellEvents = new Uint8Array( nc );
	for ( let i = 0; i < nc; i++ ) {
		for ( let k = 0; k < 3; k++ ) {
			const a = u16();
			if ( a >= n ) throw new Error( "Invalid nav cell vertex" );
			cells[i * 3 + k] = a;
		}
		cellWords[i] = u16();
		if ( flags & 2 ) cellEvents[i] = u8();
	}
	const edges: number[] = [], edgeEvents: number[] = [];
	let outlines = 0;
	for ( let group = 0; group < 2; group++ ) {
		const ne = count( nc * 3 );
		if ( !group ) outlines = ne;
		for ( let i = 0; i < ne; i++ ) {
			const a = u16(), b = u16(), src = u16(), dst = u16(), f = u8();
			edgeEvents.push( flags & 1 ? u8() : 0 );
			if ( a >= n || b >= n || src >= nc || dst !== 65535 && dst >= nc ) {
				throw new Error( "Invalid nav edge reference" );
			}
			edges.push( a, b, src, dst, f, group );
		}
	}
	const names: string[] = [];
	if ( flags & 4 ) {
		const nn = count( 65536 );
		for ( let i = 0; i < nn; i++ ) {
			const len = count( 65536 ), at = take( len );
			names.push( new TextDecoder().decode( raw.subarray( at, at + len ) ) );
		}
	}
	const gridX = f32(), gridZ = f32(), nx = count( 65536 ), nz = count( 65536 ), nt = count( 1048576 );
	if ( nx * nz !== nt ) throw new Error( "Invalid navigation grid" );
	const offsetsGrid = new Uint32Array( nt + 1 ), gridEdges: number[] = [];
	for ( let i = 0; i < nt; i++ ) {
		const nr = count( outlines );
		if ( gridEdges.length + nr > 8 * 1024 * 1024 ) throw Error( "Navigation grid residency budget" );
		for ( let j = 0; j < nr; j++ ) {
			const edge = u16();
			if ( edge >= outlines ) throw new Error( "Invalid navigation grid edge" );
			gridEdges.push( edge );
		}
		offsetsGrid[i + 1] = gridEdges.length;
	}
	if ( o !== size ) throw new Error( "Object navigation trailing bytes" );
	if ( !n || !nc ) return [];
	return [ {
		outlineGrid: { x: gridX, z: gridZ, nx, nz, offsets: offsetsGrid, edgeIds: Uint16Array.from( gridEdges ) },
		vertices,
		vertexDirections,
		cells,
		cellWords,
		cellEvents,
		edgeEvents: Uint8Array.from( edgeEvents ),
		eventNames: names,
		edges: Uint32Array.from( edges ),
		bounds,
		passThrough: nc === 2 && names.length === 1 && [ "event", "EVENT" ].includes( names[0]! )
	} ];
}
/*
================
navLocal
================
*/
export function navLocal( p: NavPlacement, x: number, y: number, z: number ) {
	const c = Math.cos( p.yaw ), s = Math.sin( p.yaw ), dx = x - p.x, dz = z - p.z;
	return [ c * dx + s * dz, y - p.y, -s * dx + c * dz ] as const;
}
/*
================
navHeight
================
*/
export function navHeight( mesh: NavMesh, x: number, z: number, hint: number, cellIndex?: number ): number | null {
	const b = mesh.bounds;
	if ( x < b[0]! || x > b[3]! || z < b[2]! || z > b[5]! ) return null;
	let best: number | null = null, delta = Infinity;
	const v = mesh.vertices;
	for (
		let i = cellIndex === undefined ? 0 : cellIndex * 3;
		i < (cellIndex === undefined ? mesh.cells.length : cellIndex * 3 + 3);
		i += 3
	) {
		const a = mesh.cells[i]! * 3,
			b = mesh.cells[i + 1]! * 3,
			c = mesh.cells[i + 2]! * 3,
			ax = v[a]!,
			az = v[a + 2]!,
			bx = v[b]! - ax,
			bz = v[b + 2]! - az,
			cx = v[c]! - ax,
			cz = v[c + 2]! - az,
			den = bx * cz - bz * cx;
		if ( Math.abs( den ) < 1e-10 ) continue;
		const u = ((x - ax) * cz - (z - az) * cx) / den, w = (bx * (z - az) - bz * (x - ax)) / den;
		if ( u < -1e-6 || w < -1e-6 || u + w > 1.000001 ) continue;
		const y = v[a + 1]! + u * (v[b + 1]! - v[a + 1]!) + w * (v[c + 1]! - v[a + 1]!);
		if ( Math.abs( y - hint ) < delta ) {
			best = y;
			delta = Math.abs( y - hint );
		}
	}
	return best;
}
// Same side-bit/contact contract as server objectnav_collision.go. Only resolved
// explicit passages bypass a linked edge; missing topology retains collision.
/*
================
navContactDetail
The first blocking edge crossing of the chord on this placement, with the
native response point. An unowned walker meets the object from its visit.
================
*/
export function navContactDetail(
	p: NavPlacement,
	from: readonly number[],
	to: readonly number[],
	objects: readonly NavPlacement[] = [],
	passages: readonly NavPassage[] = [],
	slide = false,
	exits = false,
	ownership?: readonly { cell: number; from: number; to: number; }[]
) {
	const a = navLocal( p, from[0]!, from[1]!, from[2]! ),
		b = navLocal( p, to[0]!, to[1]!, to[2]! ),
		m = p.mesh,
		v = m.vertices,
		bb = m.bounds;
	if (
		Math.max( a[0], b[0] ) < bb[0]! || Math.min( a[0], b[0] ) > bb[3]! || Math.max( a[2], b[2] ) < bb[2]! ||
		Math.min( a[2], b[2] ) > bb[5]! ||
		!ownership && !p.terrainCells && (Math.max( a[1], b[1] ) < bb[1]! - 2 || Math.min( a[1], b[1] ) > bb[4]! + 2)
	) return null;
	// An unowned walker meets this object only from its visit (terrainVisit).
	const visit = ownership ? 0 : terrainVisit( p.terrainCells, from, to );
	if ( visit === Infinity ) return null;
	const rx = b[0] - a[0], rz = b[2] - a[2];
	let best = Infinity, edge = -1, owner = -1;
	for ( let i = 0; i < m.edges.length; i += 6 ) {
		const e = m.edges,
			aa = e[i]! * 3,
			ab = e[i + 1]! * 3,
			src = e[i + 2]!,
			flags = e[i + 4]!,
			outline = e[i + 5] === 0;
		if ( !ownership && p.terrainCells && (!outline || flags & 0x10) ) continue;
		if ( outline ? flags === 0 && !exits : !(flags & 3) ) continue;
		const ax = v[aa]!, az = v[aa + 2]!, sx = v[ab]! - ax, sz = v[ab + 2]! - az, den = rx * sz - rz * sx;
		if ( Math.abs( den ) < 1e-12 ) continue;
		const t = ((ax - a[0]) * sz - (az - a[2]) * sx) / den, u = ((ax - a[0]) * rz - (az - a[2]) * rx) / den;
		if ( t <= 0 || t > 1 || t >= best || u < 0 || u > 1 ) continue;
		if ( !ownership && p.terrainCells && t < visit - 1e-9 ) continue;
		if (
			!ownership && !p.terrainCells &&
			Math.abs( a[1] + (b[1] - a[1]) * t - (v[aa + 1]! + (v[ab + 1]! - v[aa + 1]!) * u) ) > 2
		) continue;
		let cx = 0, cz = 0;
		for ( let k = 0; k < 3; k++ ) {
			const at = m.cells[src * 3 + k]! * 3;
			cx += v[at]! / 3;
			cz += v[at + 2]! / 3;
		}
		const side = sx * (cz - az) - sz * (cx - ax),
			start = sx * (a[2] - az) - sz * (a[0] - ax),
			approach = start || -(sx * (b[2] - az) - sz * (b[0] - ax));
		if (
			ownership &&
			!ownership.some( span =>
				span.cell === (approach * side > 0 ? src : e[i + 3]) && t >= span.from - 1e-8 && t <= span.to + 1e-8
			)
		) continue;
		if ( outline && exits && flags === 0 && approach * side <= 0 ) continue;
		if (
			outline && (flags & 8) && passages.some( link =>
				(objects[link.source] === p && link.edge === i / 6) ||
				(objects[link.target] === p && link.targetEdge === i / 6)
			)
		) continue;
		if (
			outline && exits && approach * side > 0 || outline && ((flags & 8) || !(flags & 3)) ||
			(approach * side > 0 ? flags & 2 : flags & 1)
		) {
			best = t;
			edge = i;
			owner = approach * side > 0 ? src : e[i + 3]!;
		}
	}
	if ( edge < 0 ) return null;
	const f = Math.fround, e = m.edges, aa = e[edge]! * 3, ab = e[edge + 1]! * 3;
	const x0 = f( a[0] ), z0 = f( a[2] ), dx = f( f( b[0] ) - x0 ), dz = f( f( b[2] ) - z0 );
	const ex = f( v[ab]! - v[aa]! ), ez = f( v[ab + 2]! - v[aa + 2]! );
	const nx = f( x0 - v[aa]! ), nz = f( z0 - v[aa + 2]! );
	// 43b980 stores numerator, denominator and parameter before the hit.
	const t = f( f( ex * nz - ez * nx ) / f( ez * dx - ex * dz ) );
	const hit: [number, number] = [ f( x0 + dx * t ), f( z0 + dz * t ) ];
	if ( owner === 65535 ) {
		if ( !m.vertexDirections || !(e[edge + 4]! & 1) ) {
			return { fraction: best, point: null, normal: null, cell: owner, edge: edge / 6, visit, status: 1 };
		}
		// 428300 nudges the walker where it stood when it stepped this object (its visit), not the leg start.
		const origin: [number, number] = visit > 0 ?
			[ f( a[0] + (b[0] - a[0]) * visit ), f( a[2] + (b[2] - a[2]) * visit ) ] :
			[ x0, z0 ];
		const q = outsideEdgeStart(
			origin,
			hit,
			[ v[aa]!, v[aa + 2]! ],
			[ v[ab]!, v[ab + 2]! ],
			m.vertexDirections[aa / 3]!,
			m.vertexDirections[ab / 3]!
		);
		const c = Math.cos( p.yaw ), s = Math.sin( p.yaw );
		return {
			fraction: best,
			point: [ f( c * q[0] - s * q[1] + p.x ), f( a[1] + p.y ), f( s * q[0] + c * q[1] + p.z ) ] as const,
			normal: null,
			cell: owner,
			edge: edge / 6,
			visit,
			status: 1
		};
	}
	let cx = 0, cz = 0;
	for ( let k = 0; k < 3; k++ ) {
		const at = m.cells[owner * 3 + k]! * 3;
		cx += v[at]!;
		cz += v[at + 2]!;
	}
	const reflection = exits && e[edge + 5] === 0 && !(e[edge + 4]! & 0x1a);
	const adjusted = reflection ?
		edgeResponse( [ f( cx / 3 ), f( cz / 3 ) ], hit, [ b[0], b[2] ], e[edge + 4]!, true, 1 ).point :
		cellEntry( [ f( cx / 3 ), f( cz / 3 ) ], hit );
	const local: [number, number, number] = [
		adjusted[0],
		f( navHeight( m, adjusted[0], adjusted[1], a[1], owner ) ?? a[1] ),
		adjusted[1]
	];
	// 454103 loads the saved original world-start pointer (453feb), not the clipped local point.
	const normal = slide && !reflection ?
		slideNormal( [ v[aa]!, v[aa + 1]!, v[aa + 2]! ], [ v[ab]!, v[ab + 1]!, v[ab + 2]! ], [
			f( from[0]! ),
			f( from[1]! ),
			f( from[2]! )
		], [ f( b[0] ), f( b[1] ), f( b[2] ) ] ) :
		null;
	const c = Math.cos( p.yaw ), s = Math.sin( p.yaw );
	return {
		status: reflection ? 0x10 : 1,
		fraction: best,
		point: [
			f( c * local[0] - s * local[2] + p.x ),
			f( local[1] + p.y ),
			f( s * local[0] + c * local[2] + p.z )
		] as const,
		normal: normal ?
			[
				f( c * normal[0] - s * normal[2] + p.x ),
				f( normal[1] + p.y ),
				f( s * normal[0] + c * normal[2] + p.z )
			] as const :
			null,
		cell: owner,
		edge: edge / 6,
		visit
	};
}
/*
================
navContact
================
*/
export function navContact(
	p: NavPlacement,
	from: readonly number[],
	to: readonly number[],
	objects: readonly NavPlacement[] = [],
	passages: readonly NavPassage[] = []
): number {
	return navContactDetail( p, from, to, objects, passages )?.fraction ?? Infinity;
}

// Split a chord at every resident triangle boundary to detect gaps between islands.
/*
================
navCrossings
================
*/
export function navCrossings(
	p: NavPlacement,
	from: readonly number[],
	to: readonly number[],
	cuts: number[],
	index?: NavigationIndex
) {
	const a = navLocal( p, from[0]!, from[1]!, from[2]! ),
		b = navLocal( p, to[0]!, to[1]!, to[2]! ),
		m = p.mesh,
		v = m.vertices,
		bb = m.bounds;
	if (
		Math.max( a[0], b[0] ) < bb[0]! || Math.min( a[0], b[0] ) > bb[3]! || Math.max( a[2], b[2] ) < bb[2]! ||
		Math.min( a[2], b[2] ) > bb[5]!
	) return;
	const rx = b[0] - a[0], rz = b[2] - a[2];
	for ( const cell of index?.cells( m, a, b ) ?? Array.from( { length: m.cells.length / 3 }, ( _, i ) => i ) ) {
		for ( let k = 0; k < 3; k++ ) {
			const i = cell * 3;
			const aa = m.cells[i + k]! * 3,
				ab = m.cells[i + (k + 1) % 3]! * 3,
				ax = v[aa]!,
				az = v[aa + 2]!,
				sx = v[ab]! - ax,
				sz = v[ab + 2]! - az,
				den = rx * sz - rz * sx;
			if ( Math.abs( den ) < 1e-12 ) {
				continue;
			}
			const t = ((ax - a[0]) * sz - (az - a[2]) * sx) / den, u = ((ax - a[0]) * rz - (az - a[2]) * rx) / den;
			if ( t > 0 && t < 1 && u >= 0 && u <= 1 ) {
				if ( cuts.length >= 65536 ) throw new Error( "Navigation chord complexity" );
				cuts.push( t );
			}
		}
	}
}

// 452d10: authored flag-2 objects contribute XZ circles, storing squared radius.
/*
================
navObstacleContact
================
*/
export function navObstacleContact(
	p: NavPlacement,
	from: readonly number[],
	to: readonly number[],
	ownership?: readonly { cell: number; from: number; to: number; }[]
): number {
	const dx = to[0]! - from[0]!, dz = to[2]! - from[2]!, a = dx * dx + dz * dz;
	if ( a === 0 ) return Infinity;
	for ( const obstacle of p.obstacles ?? [] ) {
		const x = from[0]! - obstacle.x,
			z = from[2]! - obstacle.z,
			b = x * dx + z * dz,
			c = x * x + z * z - obstacle.radiusSquared;
		let t: number;
		if ( c < 0 ) {
			if ( b >= 0 ) continue;
			t = 0;
		} else {
			const d = b * b - a * c;
			if ( d < 0 ) continue;
			t = (-b - Math.sqrt( d )) / a;
			if ( t < 0 || t > 1 ) continue;
		}
		if ( ownership && !ownership.some( span => t >= span.from - 1e-8 && t <= span.to + 1e-8 ) ) continue;
		const q = navLocal( p, from[0]! + dx * t, from[1]! + (to[1]! - from[1]!) * t, from[2]! + dz * t ),
			h = navHeight( p.mesh, q[0], q[2], q[1] );
		if ( h !== null && Math.abs( h - q[1] ) <= 2 ) return t;
	}
	return Infinity;
}

// Native visit order (CRTNavMeshTerrain_Move 404510): a terrain walker steps a
// placed object once it stands in a terrain cell the object is registered in,
// against the rest of the chord. Returns that chord fraction (the first t
// inside any registered rectangle), 0 without registration data, Infinity
// when the chord never enters one. Server twin: terrainVisitKey.
/*
================
terrainVisit
================
*/
export function terrainVisit(
	cells: NavPlacement["terrainCells"],
	from: readonly number[],
	to: readonly number[]
): number {
	if ( !cells ) return 0;
	let best = Infinity;
	for ( const r of cells ) {
		let lo = 0, hi = 1, ok = true;
		for (
			const [p, q, a, b] of [ [ from[0]!, to[0]! - from[0]!, r[0], r[2] ], [
				from[2]!,
				to[2]! - from[2]!,
				r[1],
				r[3]
			] ] as const
		) {
			if ( Math.abs( q ) < 1e-12 ) {
				if ( p < a || p > b ) {
					ok = false;
					break;
				}
				continue;
			}
			let t0 = (a - p) / q, t1 = (b - p) / q;
			if ( t0 > t1 ) [t0, t1] = [ t1, t0 ];
			lo = Math.max( lo, t0 );
			hi = Math.min( hi, t1 );
			if ( lo > hi ) {
				ok = false;
				break;
			}
		}
		if ( ok && lo < best ) best = lo;
	}
	return best;
}
