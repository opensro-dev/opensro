/*
===========================================================================

walking-history.ts - bounded navigation samples for cosmetic ground recovery

Movement owners call this after accepting a native step. Extra queries only
prove displayed recovery through terrain; they never change the native step.
The caller owns immutable history and reuses it until accepted motion changes.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { NavOwner } from "@/engine/foundation/navigation/dungeon-ownership";
import { interpolateMovement, poseDistance, REGION_SIZE } from "./native-movement";

const MAX_HISTORY_POINTS = 256;
const SAMPLE_DISTANCE = 2;
const ENDPOINT_EPSILON = .01;
const NAVIGATION_STOP = 1;
const NAVIGATION_REJECT = 0x10000000;

/*
================
WalkingHistoryQuery
================
*/
export interface WalkingHistoryQuery {
	slide: boolean;
	sourceOwner?: NavOwner;
	owner?: NavOwner;
	status?: number;
}

/*
================
WalkingHistoryInput
================
*/
interface WalkingHistoryInput {
	points?: readonly Pose[];
	from: Pose;
	to: Pose;
	sourceOwner?: NavOwner;
	clip: ( from: Pose, to: Pose, query: WalkingHistoryQuery ) => Pose | null;
}

/*
================
historyPose

Never retain a spread actor record, inventory or account identity in history.
================
*/
function historyPose( pose: Pose ): Pose {
	return { regionId: pose.regionId, x: pose.x, y: pose.y, z: pose.z, angle: pose.angle };
}

/*
================
planarDistance
================
*/
function planarDistance( from: Pose, to: Pose ) {
	const outdoor = !(from.regionId & 0x8000);
	return Math.hypot(
		to.x - from.x + (outdoor ? ((to.regionId & 255) - (from.regionId & 255)) * REGION_SIZE : 0),
		to.z - from.z + (outdoor ? ((to.regionId >>> 8) - (from.regionId >>> 8)) * REGION_SIZE : 0)
	);
}

/*
================
surfaceDistance
================
*/
function surfaceDistance( from: Pose, to: Pose ) {
	return Math.hypot( poseDistance( from, to ), to.y - from.y );
}

/*
================
sameSpace
================
*/
function sameSpace( from: Pose, to: Pose ) {
	return !((from.regionId | to.regionId) & 0x8000) || from.regionId === to.regionId;
}

/*
================
rewindWalkingHistory

An input can turn before a displayed extrapolation endpoint. Return along
the already admitted sampled edges to the actual corner before appending
the new leg; a direct chord could cut a hill or an obstacle corner.
================
*/
export function rewindWalkingHistory( points: readonly Pose[] | undefined, corner: Pose ): readonly Pose[] | undefined {
	if ( !points?.length ) return undefined;
	if ( !sameSpace( points.at( -1 )!, corner ) ) return undefined;
	if ( surfaceDistance( points.at( -1 )!, corner ) <= ENDPOINT_EPSILON ) return points;
	for ( let index = points.length - 2; index >= 0; index-- ) {
		const from = points[index]!, to = points[index + 1]!;
		if ( !sameSpace( from, corner ) || !sameSpace( from, to ) ) return undefined;
		const span = surfaceDistance( from, to );
		if ( !span ) continue;
		const first = surfaceDistance( from, corner ), second = surfaceDistance( to, corner );
		const fraction = (first * first + span * span - second * second) / (2 * span * span);
		if (
			fraction < 0 || fraction > 1 ||
			surfaceDistance( interpolateMovement( from, to, fraction ), corner ) > ENDPOINT_EPSILON
		) continue;
		const joined = [ ...points, ...points.slice( index + 1, -1 ).reverse(), historyPose( corner ) ];
		return joined.length > MAX_HISTORY_POINTS ? joined.slice( -MAX_HISTORY_POINTS ) : joined;
	}
	return undefined;
}

/*
================
extendWalkingHistory

Small accepted steps already carry their endpoint surface. A long native
step additionally samples connected cosmetic queries, so recovery cannot
replace a hill or stair with the chord between its two endpoint heights.
Missing coverage invalidates this proof rather than authorizing a shortcut.
================
*/
export function extendWalkingHistory( input: WalkingHistoryInput ): readonly Pose[] {
	const { from, to } = input;
	const distance = planarDistance( from, to );
	const count = Math.max( 1, Math.ceil( distance / SAMPLE_DISTANCE ) );
	// The history needs one initial point and one point per sampled edge.
	// Its representation budget also bounds queries; a second smaller cap
	// would discard ordinary 150-unit mounted steps despite fitting in it.
	if ( !sameSpace( from, to ) || count >= MAX_HISTORY_POINTS ) {
		return [ historyPose( to ) ];
	}
	const last = input.points?.at( -1 );
	if ( last && sameSpace( last, to ) && surfaceDistance( last, to ) === 0 ) return input.points!;
	const points = last && sameSpace( last, from ) && surfaceDistance( last, from ) <= ENDPOINT_EPSILON ?
		[ ...input.points! ] :
		[ historyPose( from ) ];
	let accepted = from, owner = input.sourceOwner;
	for ( let sample = 1; sample <= count; sample++ ) {
		let resolved = to;
		if ( count > 1 ) {
			const candidate = interpolateMovement( from, to, sample / count );
			const query: WalkingHistoryQuery = { slide: false, sourceOwner: owner, status: 0 };
			const checked = input.clip( accepted, candidate, query );
			if (
				!checked || (query.status! & NAVIGATION_REJECT) ||
				(sample < count && (query.status! & NAVIGATION_STOP)) ||
				planarDistance( checked, candidate ) > ENDPOINT_EPSILON ||
				(sample === count && surfaceDistance( checked, to ) > ENDPOINT_EPSILON)
			) {
				return [ historyPose( to ) ];
			}
			resolved = checked;
			owner = query.owner;
		}
		points.push( historyPose( sample === count ? to : resolved ) );
		accepted = resolved;
	}
	return points.length > MAX_HISTORY_POINTS ? points.slice( -MAX_HISTORY_POINTS ) : points;
}
