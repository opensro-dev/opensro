/*
===========================================================================

walk-step.ts - one advance of the local walk, as a pure function

The local mover's walk advanced by elapsed time against a navigation view,
with no owner state: given the accepted pose, the segment in progress, its
navigation owner and the time, it returns what the step did. The worker's
movement owner (movement.ts advanceTo) applies the result to its state on
its fixed tick; the render-thread mover (epic #1437) will call the same
step every frame, so both sides walk the same way.

The rules are the native client's walking step: the leg's direction is
stored when it starts (86D6D0 stores it, re-normalizing rounded positions
would turn the leg every frame), the scalar step is uncapped and consumes
elapsed time exactly once, destinations are float32 (a receipt's
higher-precision envelope included), and a rejected move retires without
accepting the collision candidate.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { SurfaceCursor } from "@/engine/contracts/navigation";
import type { NavOwner } from "@/engine/foundation/navigation/dungeon-ownership";
import { worldDelta } from "@/engine/foundation/world/world-space";
import { modelYaw } from "./direction-movement";
import {
	clientPlanarDistance,
	clientWalkingDirection,
	clientWalkingStep,
	clientWalkingVector,
	interpolateMovement,
	type MovementSegment,
	poseDistance,
	sampleMovement
} from "./native-movement";

// The navigation step's status bits (86D6D0): the walk stopped against an
// obstacle, and the move was rejected outright.
export const WALK_NAVIGATION_STOP = 1;
export const WALK_NAVIGATION_REJECT = 0x10000000;

/*
================
WalkStepSegment

The parts of a walk segment the step reads and writes. The owner's own
fields ride along untouched: the step returns the same object shape.
================
*/
export interface WalkStepSegment extends MovementSegment {
	fixedTiming?: boolean;
	walking?: { direction: readonly [number, number]; speed: number; };
	direction?: { heading: number; blocked: boolean; };
	admitted?: boolean;
	owners?: unknown;
}

/*
================
WalkStepQuery

The navigation clip's query and answer (navigation.ts clip).
================
*/
export interface WalkStepQuery {
	slide: boolean;
	sourceOwner?: NavOwner;
	owner?: NavOwner;
	status: number;
}

/*
================
WalkStepInput
================
*/
export interface WalkStepInput<S extends WalkStepSegment> {
	readonly pose: Pose;
	// When pose was accepted; elapsed time is counted from the later of
	// this and the segment's start.
	readonly poseAtMs: number;
	readonly segment: S;
	readonly owner: NavOwner | undefined;
	readonly cursor: SurfaceCursor;
	readonly now: number;
	// The navigation view: the clip of one move and the surface of a sampled
	// pose (navigation.ts). The worker's navigation owner provides them; so
	// will the render thread's.
	readonly clip: ( from: Pose, to: Pose, query: WalkStepQuery ) => Pose | null;
	readonly surface: ( pose: Pose, reference: Pose, owner: NavOwner | undefined, cursor: SurfaceCursor ) => Pose;
}

/*
================
WalkStepResult

What the step did:

	idle       no time has elapsed on the segment
	sampled    a fixed-timing segment (a skill displacement) sampled its
	           own clock and surfaced the point
	rejected   navigation rejected the move: the walk retires and the pose
	           stays (no collision candidate is accepted)
	uncovered  no navigation coverage yet: the pose stays, the elapsed time
	           is consumed (a late product cannot release a backlog)
	stepped    the walk moved from `from` to `pose`; `segment` is the rest
	           of the leg, or null when it ended (blocked, arrived, or past
	           the goal's plane)
================
*/
export type WalkStepResult<S extends WalkStepSegment> =
	| { readonly kind: "idle"; }
	| { readonly kind: "sampled"; readonly pose: Pose; readonly owner: NavOwner | undefined; }
	| { readonly kind: "rejected"; }
	| { readonly kind: "uncovered"; readonly segment: S; }
	| {
		readonly kind: "stepped";
		readonly from: Pose;
		readonly pose: Pose;
		readonly owner: NavOwner | undefined;
		readonly segment: S | null;
	};

/*
================
startWalking

The leg's stored walking direction and speed (86D6D0 stores them when the
leg starts), with the destination rounded to float32 as native stores it.
Returns the segment it completes; a segment already walking is returned
as is.
================
*/
export function startWalking<S extends WalkStepSegment>( segment: S ): S {
	if ( segment.walking ) return segment;
	const speed = segment.duration > 0 ? poseDistance( segment.from, segment.to ) / segment.duration * 1000 : 0;
	const to = interpolateMovement( segment.to, {
		...segment.to,
		x: Math.fround( segment.to.x ),
		y: Math.fround( segment.to.y ),
		z: Math.fround( segment.to.z )
	}, 1 );
	const direction = segment.direction ?
		clientWalkingVector( modelYaw( segment.direction.heading ) ) :
		clientWalkingDirection( worldDelta( segment.from, to ) );
	return { ...segment, to, walking: { direction, speed } };
}

/*
================
stepWalk

One advance of the walk by the time elapsed since the accepted pose.
================
*/
export function stepWalk<S extends WalkStepSegment>( input: WalkStepInput<S> ): WalkStepResult<S> {
	const { pose, now } = input;
	const elapsed = Math.max( 0, now - Math.max( input.poseAtMs, input.segment.start ) );
	if ( !elapsed ) return { kind: "idle" };
	if ( input.segment.fixedTiming ) {
		const sampled = input.surface( sampleMovement( input.segment, now ), pose, input.owner, input.cursor );
		return { kind: "sampled", pose: sampled, owner: input.cursor.owner ?? input.owner };
	}
	const segment = startWalking( input.segment );
	const walking = segment.walking!;
	const rate = walking.speed / 1000;
	const delta = worldDelta( pose, segment.to );
	const forward = delta[0] * walking.direction[0] + delta[1] * walking.direction[1];
	// A direction walk renews its leg along the heading: only a goal walk is
	// bounded by its destination.
	const goalBounded = !(segment.direction && !segment.direction.blocked);
	const { step, arrived } = clientWalkingStep(
		walking.speed,
		elapsed / 1000,
		walking.direction,
		goalBounded ? (forward <= 0 ? 0 : clientPlanarDistance( delta )) : Infinity
	);
	const desired = interpolateMovement( pose, {
		...pose,
		x: Math.fround( Math.fround( pose.x ) + step[0] ),
		y: Math.fround( pose.y ),
		z: Math.fround( Math.fround( pose.z ) + step[1] ),
		angle: segment.to.angle
	}, 1 );
	const query: WalkStepQuery = { slide: false, sourceOwner: input.owner, status: 0 };
	const resolved = input.clip( pose, desired, query );
	if ( query.status & WALK_NAVIGATION_REJECT ) return { kind: "rejected" };
	if ( !resolved ) {
		return {
			kind: "uncovered",
			segment: {
				...segment,
				from: pose,
				start: now,
				duration: rate ? poseDistance( pose, segment.to ) / rate : Infinity,
				admitted: false
			}
		};
	}
	const afterDelta = worldDelta( resolved, segment.to );
	const reachedPlane = goalBounded &&
		afterDelta[0] * walking.direction[0] + afterDelta[1] * walking.direction[1] <= 0;
	const blocked = !!(query.status & WALK_NAVIGATION_STOP);
	const ended = blocked || arrived || reachedPlane;
	return {
		kind: "stepped",
		from: pose,
		pose: resolved,
		owner: query.owner,
		segment: ended ? null : {
			...segment,
			from: resolved,
			start: now,
			duration: rate ? poseDistance( resolved, segment.to ) / rate : Infinity,
			owners: undefined
		}
	};
}
