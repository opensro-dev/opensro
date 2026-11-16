/*
===========================================================================

walking-chain.ts - a walking path's edges, measured once per publication

A walking character's state carries its walking path (motion.ts): the
connected ground samples behind it and just ahead, about 250 poses. The
presentation locates a pose on that chain every frame, often several times
(pose-presentation.ts sampledModel, recoverWalking), and each search used to
measure every edge again: two region-aware vectors, a length and a
projection per edge. With 100 monsters walking that was over half of the
main thread (226 ms/s of 436, dev profile, 2026-11-15).

A path changes only when the simulation publishes it (about 40 times a
second for a walker), so its edges are measured once per path here: each
edge's vector and length, the distance walked before it, and the planar
bounds of every block of edges. A publication slides the window: it drops a
few poses at the head and adds a few at the tail, and every other edge joins
the same two pose objects as in the path before it, so a path measured with
its predecessor takes those edges' measures from it. A pose belongs to an edge only within
MIN_CORRECTION_DISTANCE of it in the plane, so a search skips every block
whose bounds the pose is not near and measures only the edges it can be on.

Every value a search returns is computed by the same operations, on the
same operands and in the same order, as the full scan it replaces: the
measured edge vectors and lengths are those values, the distances before
each edge are the same running sum, and the edges a search measures are
taken in path order with the same comparison. Skipping a block whose
bounds the pose is far from changes nothing, because no edge in it could
have been chosen.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import { hypot2, hypot3 } from "@/engine/foundation/math/hypot";
import { sameCoordinateSpace, writeRegionOffsetXZ, writeWorldDelta } from "@/engine/foundation/world/world-space";
import { interpolateMovement } from "./native-movement";

// The farthest a pose may lie from an edge, in the plane, and still be on it.
export const MIN_CORRECTION_DISTANCE = 0.01;
// Edges per block of planar bounds.
const CHAIN_BLOCK_EDGES = 16;
// The margin added to a block's bounds before a pose is ruled out: the
// bounds and the pose are planar positions across sectors, which round
// differently from the per-edge vectors. Any margin far above that rounding
// (well under 1e-6 at world coordinates) is exact.
const CHAIN_BOUNDS_MARGIN = 1;
// How far into the path before it a slid path's first pose is looked for.
const MAX_CHAIN_SHIFT = 64;
// The outdoor region at sector (0, 0): the origin of the world frame every
// outdoor region shares.
const WORLD_ORIGIN_REGION = 0;

/*
================
writePlanarPosition

A pose's planar position in its coordinate space: outdoors across sectors,
in a dungeon its own x and z. Only poses of one space are compared.
================
*/
function writePlanarPosition( pose: Pose, out: [number, number] ) {
	// The sector offset from the world frame's origin: zero in a dungeon.
	writeRegionOffsetXZ( WORLD_ORIGIN_REGION, pose.regionId, out );
	out[0] += pose.x;
	out[1] += pose.z;
}

/*
================
writeWorldVector

a -> b's world-space vector, height included, written into out; false when
the two poses are not in one coordinate space.

Two poses of one region share its frame: no sector offset, and nothing to
ask about the coordinate space. A walking chain's samples lie 2 units apart
(walking-history.ts), so nearly every edge of one is that case, and the
chain is scanned edge by edge several times a frame for each walker. The
general path's nested calls kept this helper out of line there: with about
ten walkers among eighty characters the scan and this helper took 11.5 ms
of each main-thread second and left 23 MB of boxed numbers a second behind;
5.3 ms and none with it (2026-11-15, Chrome on an M3). The sum with zero
is the general path's own (the offset is written first, then the
difference added), so a difference of negative zero reads back as zero on
both. world-vector.test.mjs holds the two equal bit for bit.
================
*/
export function writeWorldVector( a: Pose, b: Pose, out: [number, number, number] ): boolean {
	if ( a.regionId === b.regionId ) {
		out[0] = 0 + (a.x - b.x);
		out[1] = a.y - b.y;
		out[2] = 0 + (a.z - b.z);
		return true;
	}
	if ( !sameCoordinateSpace( a.regionId, b.regionId ) ) {
		return false;
	}
	// The planar delta lands in out[0] and out[1]; z then moves to out[2].
	writeWorldDelta( b, a, out as unknown as [number, number] );
	out[2] = out[1];
	out[1] = a.y - b.y;
	return true;
}

/*
================
WalkingChain

One walking path, measured. spans holds each edge's vector (to - from), three
numbers per edge; lengths its length; before the distance walked before it.
bounds holds each block's planar [minX, maxX, minZ, maxZ] (writePlanarPosition),
widened by the distance a pose may lie off an edge. brokenAt is
the first edge that crosses coordinate spaces (the edge count when none
does): a search on such a chain finds nothing, as the full scan did, and
spending distance throws on reaching it.
================
*/
export interface WalkingChain {
	readonly path: readonly Pose[];
	readonly brokenAt: number;
	readonly spans: Float64Array;
	readonly lengths: Float64Array;
	readonly before: Float64Array;
	readonly bounds: Float64Array;
	// Search scratch: the pose's vector from an edge's first pose.
	readonly point: [number, number, number];
	readonly plane: [number, number];
}

/*
================
measureWalkingChain

previous, the chain of the path this one replaced, lends the measures of
every edge that joins the same two poses: they are the values measuring
would compute again. The distances before each edge are summed anew.
================
*/
export function measureWalkingChain( path: readonly Pose[], previous?: WalkingChain ): WalkingChain {
	const edges = Math.max( 0, path.length - 1 ), blocks = Math.ceil( edges / CHAIN_BLOCK_EDGES );
	const spans = new Float64Array( edges * 3 ),
		lengths = new Float64Array( edges ),
		before = new Float64Array( edges );
	const bounds = new Float64Array( blocks * 4 );
	const span: [number, number, number] = [ 0, 0, 0 ], plane: [number, number] = [ 0, 0 ];
	let brokenAt = edges, distance = 0, shift = -1;
	if ( previous && edges ) {
		const limit = Math.min( previous.brokenAt, MAX_CHAIN_SHIFT );
		for ( let d = 0; d < limit && shift < 0; d++ ) if ( previous.path[d] === path[0] ) shift = d;
	}
	for ( let e = 0; e < edges; e++ ) {
		const from = path[e]!, to = path[e + 1]!, k = e + shift;
		let length: number;
		if ( shift >= 0 && k < previous!.brokenAt && previous!.path[k] === from && previous!.path[k + 1] === to ) {
			spans[e * 3] = previous!.spans[k * 3]!;
			spans[e * 3 + 1] = previous!.spans[k * 3 + 1]!;
			spans[e * 3 + 2] = previous!.spans[k * 3 + 2]!;
			length = previous!.lengths[k]!;
		} else {
			if ( !writeWorldVector( to, from, span ) ) {
				brokenAt = e;
				break;
			}
			spans[e * 3] = span[0];
			spans[e * 3 + 1] = span[1];
			spans[e * 3 + 2] = span[2];
			length = hypot3( span[0], span[1], span[2] );
		}
		lengths[e] = length;
		before[e] = distance;
		distance += length;
	}
	if ( brokenAt === edges ) {
		for ( let b = 0; b < blocks; b++ ) {
			let minX = Infinity, maxX = -Infinity, minZ = Infinity, maxZ = -Infinity;
			const last = Math.min( edges, (b + 1) * CHAIN_BLOCK_EDGES );
			for ( let v = b * CHAIN_BLOCK_EDGES; v <= last; v++ ) {
				writePlanarPosition( path[v]!, plane );
				minX = Math.min( minX, plane[0] );
				maxX = Math.max( maxX, plane[0] );
				minZ = Math.min( minZ, plane[1] );
				maxZ = Math.max( maxZ, plane[1] );
			}
			bounds[b * 4] = minX - MIN_CORRECTION_DISTANCE - CHAIN_BOUNDS_MARGIN;
			bounds[b * 4 + 1] = maxX + MIN_CORRECTION_DISTANCE + CHAIN_BOUNDS_MARGIN;
			bounds[b * 4 + 2] = minZ - MIN_CORRECTION_DISTANCE - CHAIN_BOUNDS_MARGIN;
			bounds[b * 4 + 3] = maxZ + MIN_CORRECTION_DISTANCE + CHAIN_BOUNDS_MARGIN;
		}
	}
	return { path, brokenAt, spans, lengths, before, bounds, point: [ 0, 0, 0 ], plane: [ 0, 0 ] };
}

/*
================
WalkingLocation
================
*/
export interface WalkingLocation {
	readonly edge: number;
	readonly fraction: number;
	readonly distance: number;
	readonly error: number;
	readonly pose: Pose;
}

/*
================
locateOnWalkingChain

Locate a pose on the chain: of the edges the pose lies on (within
MIN_CORRECTION_DISTANCE in the plane), the one with the least error, height
included; with latest, the last edge the pose lies on exactly. edge is the
index of the edge's end pose.
================
*/
export function locateOnWalkingChain( pose: Pose, chain: WalkingChain, latest = false ): WalkingLocation | null {
	const { path, spans, lengths, before, bounds, point, plane } = chain;
	if ( path.length < 2 ) return null;
	// Every pose of an unbroken chain shares one coordinate space, so the
	// pose either shares it with every edge or with none.
	if ( chain.brokenAt < path.length - 1 || !sameCoordinateSpace( pose.regionId, path[0]!.regionId ) ) return null;
	writePlanarPosition( pose, plane );
	const px = plane[0], pz = plane[1], edges = path.length - 1;
	let hasBest = false, bestEdge = 0, bestFraction = 0, bestDistance = 0, bestError = 0;
	for ( let b = 0; b * CHAIN_BLOCK_EDGES < edges; b++ ) {
		if (
			px < bounds[b * 4]! || px > bounds[b * 4 + 1]! || pz < bounds[b * 4 + 2]! || pz > bounds[b * 4 + 3]!
		) continue;
		const last = Math.min( edges, (b + 1) * CHAIN_BLOCK_EDGES );
		for ( let e = b * CHAIN_BLOCK_EDGES; e < last; e++ ) {
			writeWorldVector( pose, path[e]!, point );
			const sx = spans[e * 3]!, sy = spans[e * 3 + 1]!, sz = spans[e * 3 + 2]!;
			const planar2 = sx ** 2 + sz ** 2;
			const fraction = planar2 ?
				Math.max( 0, Math.min( 1, (point[0] * sx + point[2] * sz) / planar2 ) ) :
				sy ?
				Math.max( 0, Math.min( 1, point[1] / sy ) ) :
				0;
			const miss = hypot2( point[0] - sx * fraction, point[2] - sz * fraction );
			if ( miss > MIN_CORRECTION_DISTANCE ) continue;
			const error = miss + Math.abs( point[1] - sy * fraction );
			if (
				!hasBest ||
				(latest && error <= MIN_CORRECTION_DISTANCE && bestError <= MIN_CORRECTION_DISTANCE) ||
				error < bestError
			) {
				hasBest = true;
				bestEdge = e + 1;
				bestFraction = fraction;
				bestDistance = before[e]! + lengths[e]! * fraction;
				bestError = error;
			}
		}
	}
	return hasBest ?
		{
			edge: bestEdge,
			fraction: bestFraction,
			distance: bestDistance,
			error: bestError,
			pose: interpolateMovement( path[bestEdge - 1]!, path[bestEdge]!, bestFraction )
		} :
		null;
}

/*
================
nearestOnWalkingChain

The chain's point nearest a pose in the plane, over every edge.
================
*/
export function nearestOnWalkingChain( pose: Pose, chain: WalkingChain ): Pose | null {
	const { path, spans, point } = chain;
	if ( path.length < 2 ) return null;
	if ( chain.brokenAt < path.length - 1 || !sameCoordinateSpace( pose.regionId, path[0]!.regionId ) ) return null;
	let hasBest = false, bestMiss = 0, bestEdge = 0, bestFraction = 0;
	for ( let e = 0; e < path.length - 1; e++ ) {
		writeWorldVector( pose, path[e]!, point );
		const sx = spans[e * 3]!, sz = spans[e * 3 + 2]!;
		const planar2 = sx ** 2 + sz ** 2;
		const fraction = planar2 ? Math.max( 0, Math.min( 1, (point[0] * sx + point[2] * sz) / planar2 ) ) : 0;
		const miss = hypot2( point[0] - sx * fraction, point[2] - sz * fraction );
		if ( !hasBest || miss < bestMiss ) {
			hasBest = true;
			bestMiss = miss;
			bestEdge = e + 1;
			bestFraction = fraction;
		}
	}
	return hasBest ? interpolateMovement( path[bestEdge - 1]!, path[bestEdge]!, bestFraction ) : null;
}

/*
================
walkingChainPose

Spend distance along the chain's edges from its first pose.
================
*/
export function walkingChainPose( chain: WalkingChain, distance: number ): Pose {
	const { path, lengths } = chain;
	for ( let e = 0; e < path.length - 1; e++ ) {
		// A certified chain shares one coordinate space; a mismatched edge was
		// a spread-of-null TypeError before and stays loud here.
		if ( e === chain.brokenAt ) throw Error( "Invalid walking path edge" );
		const length = lengths[e]!;
		if ( distance <= length ) return interpolateMovement( path[e]!, path[e + 1]!, length ? distance / length : 0 );
		distance -= length;
	}
	return path[path.length - 1]!;
}
