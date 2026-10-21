/*
===========================================================================

spatial-index.ts - the navigation broad phase of one installed placement list

Box trees over placements, mesh cells and mesh edges, plus the region's
passage links in world space (topology.ts). Movement asks it for candidates
and runs the exact narrow phase on them; the index never decides a contact.
Immutable once built; the navigation owner rebuilds it on every install.

===========================================================================
*/
import type { NavMesh, NavPlacement } from "@/engine/contracts/navigation";
import { createPassageLinks } from "./topology";
type Bounds = readonly [number, number, number, number];
/*
================
tree

Immutable broad phase. Candidate IDs are returned in authored order so exact
contact/tie decisions remain in the existing narrow phase.
================
*/
function tree( boxes: readonly Bounds[] ) {
	const order = Int32Array.from( boxes, ( _, i ) => i ), nodes: number[] = [];
	function build( start: number, end: number ): number {
		const at = nodes.length;
		let x = Infinity, z = Infinity, X = -Infinity, Z = -Infinity;
		for ( let i = start; i < end; i++ ) {
			const b = boxes[order[i]!]!;
			x = Math.min( x, b[0] );
			z = Math.min( z, b[1] );
			X = Math.max( X, b[2] );
			Z = Math.max( Z, b[3] );
		}
		nodes.push( x, z, X, Z, -1, -1, start, end - start );
		if ( end - start > 8 ) {
			const axis = X - x >= Z - z ? 0 : 1, farAxis = axis === 0 ? 2 : 3;
			order.subarray( start, end ).sort( ( a, b ) =>
				(boxes[a]![axis] + boxes[a]![farAxis]) - (boxes[b]![axis] + boxes[b]![farAxis]) || a - b
			);
			const middle = (start + end) >>> 1;
			nodes[at + 4] = build( start, middle );
			nodes[at + 5] = build( middle, end );
		}
		return at;
	}
	if ( boxes.length ) build( 0, boxes.length );
	const data = Float64Array.from( nodes );
	return {
		bytes: data.byteLength + order.byteLength,
		query( bounds: Bounds ) {
			const found: number[] = [], pending = data.length ? [ 0 ] : [];
			let visited = 0;
			while ( pending.length ) {
				const at = pending.pop()!;
				visited++;
				if (
					bounds[2] < data[at]! || bounds[0] > data[at + 2]! || bounds[3] < data[at + 1]! ||
					bounds[1] > data[at + 3]!
				) continue;
				if ( data[at + 4]! >= 0 ) {
					pending.push( data[at + 4]!, data[at + 5]! );
					continue;
				}
				for ( let i = data[at + 6]!; i < data[at + 6]! + data[at + 7]!; i++ ) {
					const id = order[i]!, b = boxes[id]!;
					if ( bounds[2] >= b[0] && bounds[0] <= b[2] && bounds[3] >= b[1] && bounds[1] <= b[3] ) {
						found.push(
							id
						);
					}
				}
			}
			return { ids: found.sort( ( a, b ) => a - b ), visited };
		}
	};
}
/*
================
rectangle
================
*/
function rectangle( from: readonly number[], to = from, pad = 1e-5 ): Bounds {
	return [
		Math.min( from[0]!, to[0]! ) - pad,
		Math.min( from[2]!, to[2]! ) - pad,
		Math.max( from[0]!, to[0]! ) + pad,
		Math.max( from[2]!, to[2]! ) + pad
	];
}
/*
================
createNavigationIndex
================
*/
export function createNavigationIndex( objects: readonly NavPlacement[] ) {
	const meshes = new Map<
			NavMesh,
			{ cells: ReturnType<typeof tree>; edges: ReturnType<typeof tree>; bounds: Bounds; }
		>(),
		transforms = new Map<NavPlacement, readonly [number, number]>();
	let bytes = objects.length * 48, queries = 0, visited = 0, candidates = 0;
	for ( const p of objects ) {
		transforms.set( p, [ Math.cos( p.yaw ), Math.sin( p.yaw ) ] );
		if ( meshes.has( p.mesh ) ) continue;
		const m = p.mesh, v = m.vertices;
		function bounds( ids: readonly number[] ): Bounds {
			let x = Infinity, z = Infinity, X = -Infinity, Z = -Infinity;
			for ( const id of ids ) {
				x = Math.min( x, v[id * 3]! );
				z = Math.min( z, v[id * 3 + 2]! );
				X = Math.max( X, v[id * 3]! );
				Z = Math.max( Z, v[id * 3 + 2]! );
			}
			return [ x, z, X, Z ];
		}
		const cellBoxes: Bounds[] = [], edgeBoxes: Bounds[] = [];
		for ( let i = 0; i < m.cells.length; i += 3 ) {
			const b = bounds( [ m.cells[i]!, m.cells[i + 1]!, m.cells[i + 2]! ] );
			const pad = Math.max( 1, b[2] - b[0], b[3] - b[1] ) * 2e-6;
			cellBoxes.push( [ b[0] - pad, b[1] - pad, b[2] + pad, b[3] + pad ] );
		}
		for ( let i = 0; i < m.edges.length; i += 6 ) edgeBoxes.push( bounds( [ m.edges[i]!, m.edges[i + 1]! ] ) );
		const cells = tree( cellBoxes ),
			edges = tree( edgeBoxes ),
			b: Bounds = [ m.bounds[0]!, m.bounds[2]!, m.bounds[3]!, m.bounds[5]! ];
		// Include actual edge/cell extents as well as authored bounds: a loose or
		// inconsistent authored box must not make this accelerator omit a contact.
		const actual: [number, number, number, number] = [ ...b ];
		for ( const box of [ ...cellBoxes, ...edgeBoxes ] ) {
			actual[0] = Math.min( actual[0], box[0] );
			actual[1] = Math.min( actual[1], box[1] );
			actual[2] = Math.max( actual[2], box[2] );
			actual[3] = Math.max( actual[3], box[3] );
		}
		bytes += cells.bytes + edges.bytes + (cellBoxes.length + edgeBoxes.length) * 32;
		if ( bytes > 64 * 1024 * 1024 ) throw Error( "Navigation acceleration budget" );
		meshes.set( m, { cells, edges, bounds: actual } );
	}
	const placements = tree( objects.map( p => {
		const [c, s] = transforms.get( p )!, b = meshes.get( p.mesh )!.bounds;
		const xx = [
				c * b[0] - s * b[1] + p.x,
				c * b[0] - s * b[3] + p.x,
				c * b[2] - s * b[1] + p.x,
				c * b[2] - s * b[3] + p.x
			],
			zz = [
				s * b[0] + c * b[1] + p.z,
				s * b[0] + c * b[3] + p.z,
				s * b[2] + c * b[1] + p.z,
				s * b[2] + c * b[3] + p.z
			];
		return [ Math.min( ...xx ), Math.min( ...zz ), Math.max( ...xx ), Math.max( ...zz ) ] as Bounds;
	} ) );
	bytes += placements.bytes;
	function query( index: ReturnType<typeof tree>, bounds: Bounds ) {
		const result = index.query( bounds );
		queries++;
		visited += result.visited;
		candidates += result.ids.length;
		return result.ids;
	}
	// The region's link edges in world space, read by every passage query (topology.ts).
	const passageLinks = createPassageLinks( objects );
	bytes += passageLinks.links.length * 160;
	return {
		bytes,
		passageLinks,
		local( p: NavPlacement, point: readonly number[] ) {
			const [c, s] = transforms.get( p )!, dx = point[0]! - p.x, dz = point[2]! - p.z;
			return [ c * dx + s * dz, point[1]! - p.y, -s * dx + c * dz ] as const;
		},
		placements( from: readonly number[], to = from ) {
			return query( placements, rectangle( from, to ) );
		},
		cells( mesh: NavMesh, from: readonly number[], to = from ) {
			return query( meshes.get( mesh )!.cells, rectangle( from, to ) );
		},
		edges( mesh: NavMesh, from: readonly number[], to = from ) {
			return query( meshes.get( mesh )!.edges, rectangle( from, to ) );
		},
		stats() {
			return { queries, visited, candidates, bytes };
		}
	};
}
export type NavigationIndex = ReturnType<typeof createNavigationIndex>;
