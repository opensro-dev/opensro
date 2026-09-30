/*
===========================================================================

direction-movement.ts - the walk-in-direction lane shared by every mover

A click whose ground pick misses (the sky, the horizon) does not stop the
character: CGInterface_MoveToWorldPoint (0x6932A0) sends the camera ray's
yaw as a 0x7738 mode-0 command with angular flag GO
(CNavigationDeadreckon_SendAngleMoveCommand 0x877F30). The acknowledgement
starts CNavigationController_StartDirectMove (0x86D150): nav state 2, which
has no arrival and walks until the move test blocks it.

The port walks a direction as consecutive legs of DIRECTION_LEG_UNITS (the
server does the same, simulation/direction.go); along one heading at one
speed the leg boundaries are invisible. This file holds the pure geometry
and wire helpers; the local and remote movers own the walks.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { GroundPickQuery } from "@/engine/contracts/navigation";
import type { WireFrame } from "@/engine/contracts/network";
import { HEADING_SCALE, REGION_SIZE, movementHeading } from "./native-movement";

// One planned leg. INFERENCE (shared with the server): the ground pick's
// own ray cap (SWorld_PickGroundTerrainAndNavigation 0x88D340), the distance
// a destination click could not reach.
export const DIRECTION_LEG_UNITS = 1000;
// 0x7738 mode-0 flag bit 0: walk along the heading.
export const ANGULAR_FLAG_GO = 1;
// 0x7738 mode byte of the angular form.
export const ANGULAR_MODE = 0;
// dword 0xBF9120: a miss within 5 degrees of a direction already walked is
// not resent (0x693552..0x69355D).
export const DIRECTION_TURN_EPSILON = 0.0872664600610733;
// 0x6934E0: Vec3_NormalizeAndCalculateYaw keeps the seeded yaw below this
// horizontal length.
const PICK_YAW_MIN_LENGTH = 9.99999997e-7;
const TWO_PI = 6.283185307179586;
const QUARTER_TURN = 1.5707963267948966;
// A leg shorter than this against its full length ended on a contact.
const LEG_BLOCKED_EPSILON = .5;

// CNavigationDeadreckon_UpdateMovementHeading (0x8779B0) constants.
// 25 = 5 units squared of drift before the walk reconciles.
export const DRIFT_DEADBAND_SQ = 25;
// 500 ms: CIObject_ScheduleStateTimer( this, 7, 500 ).
export const DRIFT_PERIOD_MS = 500;
const DRIFT_FAST = 1.10000002;
const DRIFT_SLOW = 0.899999976;
// float32( cos( 0.17453292 ) ): within 10 degrees the reference counts as
// ahead (the fstp dword after CRT_cos).
const DRIFT_AHEAD_COS = 0.9848077297210693;
const DRIFT_LOOKAHEAD_MS = 500;
const DRIFT_FAR_SCALE = 1.7999999523162842;

/*
================
directionBearing

The wire bearing of a heading word: atan2( dz, dx ) = h / 65535 * 2pi.
================
*/
export function directionBearing( heading: number ): number {
	return heading / HEADING_SCALE * TWO_PI;
}

/*
================
modelYaw

Heading_AddQuarterTurnAndWrap: the model yaw the native comparisons use is
the wire bearing plus a quarter turn, wrapped into [0, 2pi).
================
*/
export function modelYaw( heading: number ): number {
	return (directionBearing( heading ) + QUARTER_TURN) % TWO_PI;
}

/*
================
foldPose

Re-expresses an outdoor pose whose local x/z left [0, 1920) in the region
it lands in. Dungeon locals are unbounded and stay in their region word.
================
*/
export function foldPose( pose: Pose ): Pose {
	if ( pose.regionId & 0x8000 ) return pose;
	const wx = pose.x + (pose.regionId & 255) * REGION_SIZE, wz = pose.z + (pose.regionId >>> 8) * REGION_SIZE;
	const sx = Math.floor( wx / REGION_SIZE ), sz = Math.floor( wz / REGION_SIZE );
	return { ...pose, regionId: sx | (sz << 8), x: wx - sx * REGION_SIZE, z: wz - sz * REGION_SIZE };
}

/*
================
directionPoint

The pose distance units along the heading from from, facing the heading.
================
*/
export function directionPoint( from: Pose, heading: number, distance: number ): Pose {
	const bearing = directionBearing( heading );
	return foldPose( {
		...from,
		x: from.x + Math.cos( bearing ) * distance,
		z: from.z + Math.sin( bearing ) * distance,
		angle: heading
	} );
}

/*
================
directionLegEnd
================
*/
export function directionLegEnd( from: Pose, heading: number ): Pose {
	return directionPoint( from, heading, DIRECTION_LEG_UNITS );
}

/*
================
directionLegBlocked

Whether a constrained leg of length travelled ended on a contact.
================
*/
export function directionLegBlocked( travelled: number ): boolean {
	return travelled < DIRECTION_LEG_UNITS - LEG_BLOCKED_EPSILON;
}

/*
================
pickMissHeading

0x6934D6..0x6934FF: the ray's height is dropped, and the remaining
horizontal direction becomes the heading. A vertical ray keeps the
mover's current heading (the yaw was seeded with it).
================
*/
export function pickMissHeading( query: GroundPickQuery, pose: Pose ): number {
	const dx = query.ray.delta[0]!, dz = query.ray.delta[2]!;
	if ( !(Math.hypot( dx, dz ) >= PICK_YAW_MIN_LENGTH) ) return pose.angle;
	return movementHeading( pose, { ...pose, x: pose.x + dx, z: pose.z + dz } );
}

/*
================
directionTurnSkipped

0x693519..0x69355D: only while the mover is already walking a direction
(moving, nav state not 1), a miss whose yaw differs by at most
DIRECTION_TURN_EPSILON is dropped. The difference is taken in model yaw with
no wrap (fsub, then fmul -1.0 when negative), exactly as native: a turn
across model yaw 0 always resends.
================
*/
export function directionTurnSkipped( walking: boolean, current: number, next: number ): boolean {
	return walking && Math.abs( modelYaw( current ) - modelYaw( next ) ) <= DIRECTION_TURN_EPSILON;
}

/*
================
directionMoveBody

The 4-byte 0x7738 angular body [u8 0][u8 GO][u16 heading].
================
*/
export function directionMoveBody( heading: number ): Uint8Array {
	if ( !Number.isInteger( heading ) || heading < 0 || heading > HEADING_SCALE ) throw Error( "Invalid heading" );
	const body = Uint8Array.of( ANGULAR_MODE, ANGULAR_FLAG_GO, 0, 0 );
	new DataView( body.buffer ).setUint16( 2, heading, true );
	return body;
}

/*
================
DirectionDrift
================
*/
export interface DirectionDrift {
	readonly heading: number;
	readonly factor: number;
}

/*
================
directionDrift

CNavigationDeadreckon_UpdateMovementHeading (0x8779B0), run every
DRIFT_PERIOD_MS while a direction walk lasts. Within 5 units of the
reference the walk keeps its heading at speed factor 1. Farther away:

  - the factor is 1.1 when the reference lies within 10 degrees ahead,
    0.9 when it lies behind, 1 otherwise;
  - the lookahead is factor * (speed * factor) * 500 / 1000, or 1.8 times
    the drift when the drift is longer;
  - the walker aims at the reference moved that lookahead along the walk.

INFERENCE: native aims its nav mover at a point ahead of the character and
also sends that aim as 0x72CF. The port's server walks exactly the heading
it acknowledged; steering it would move both lines together and never close
a sideways gap, so the port aims only the local walker onto the server's
line (the caller does not send the drift heading).
================
*/
export function directionDrift( local: Pose, reference: Pose, heading: number, speed: number ): DirectionDrift {
	const dx = reference.x - local.x + worldOffset( local, reference, 255, 0 ),
		dz = reference.z - local.z + worldOffset( local, reference, 0xff00, 8 ),
		distanceSq = dx * dx + dz * dz;
	if ( distanceSq < DRIFT_DEADBAND_SQ ) return { heading, factor: 1 };
	const distance = Math.sqrt( distanceSq ), bearing = directionBearing( heading );
	const ux = Math.cos( bearing ), uz = Math.sin( bearing ), ahead = (dx * ux + dz * uz) / distance;
	const factor = ahead < 0 ? DRIFT_SLOW : ahead > DRIFT_AHEAD_COS ? DRIFT_FAST : 1;
	let lookahead = factor * speed * factor * DRIFT_LOOKAHEAD_MS / 1000;
	if ( distance > lookahead ) lookahead = distance * DRIFT_FAR_SCALE;
	const aimX = dx + ux * lookahead, aimZ = dz + uz * lookahead;
	return { heading: movementHeading( local, { ...local, x: local.x + aimX, z: local.z + aimZ } ), factor };
}

/*
================
worldOffset

The sector difference between two outdoor poses along one axis, in units.
================
*/
function worldOffset( from: Pose, to: Pose, mask: number, shift: number ): number {
	if ( (from.regionId | to.regionId) & 0x8000 ) return 0;
	return (((to.regionId & mask) >>> shift) - ((from.regionId & mask) >>> shift)) * REGION_SIZE;
}

// ============================================================================

const OP_TARGET_ACTION = 0x72cd;
const TARGET_ACTION_CANCEL = 2;
const OP_LOGOUT_CANCEL = 0x731f;

/*
================
WorldPointAction

What a left click on the world that hit no entity does.
================
*/
export type WorldPointAction =
	| { readonly kind: "none"; }
	| { readonly kind: "walk-to"; readonly destination: Pose; }
	| { readonly kind: "walk-direction"; readonly heading: number; };

/*
================
worldPointAction

CGInterface_MoveToWorldPoint (0x6932A0) after its gates. The caller has
already applied the interaction, death, freeze, guided/teleport cast and
seated stand-up gates and cancelled a running action (targetActionCancel)
and a logout countdown (logoutCancelRequest). What remains:

  - a ground pick hit walks to the point (0x7738 mode 1);
  - a miss walks the ray's direction until blocked (0x7738 mode 0 with GO),
    unless the mover already walks within 5 degrees of it.

The entity-approach arm (arg3 != 0) is the select/attack path.
================
*/
export function worldPointAction(
	pose: Pose,
	picked: Omit<Pose, "angle"> | null,
	query: GroundPickQuery,
	walkingDirection: boolean
): WorldPointAction {
	if ( picked ) return { kind: "walk-to", destination: { ...picked, angle: pose.angle } };
	const heading = pickMissHeading( query, pose );
	if ( directionTurnSkipped( walkingDirection, pose.angle, heading ) ) return { kind: "none" };
	return { kind: "walk-direction", heading };
}

/*
================
targetActionCancel

0x6932D7..0x69337B: while an action is still running (the last 0xB2CD type
is 2 or more, CGInterface_CanCastSkill false) a ground click first sends
0x72CD [u8 2]. CGInterface_SetTargetMoveActive( 1 ) beside it arms a 20 s
approach timeout this client has no counterpart for.
================
*/
export function targetActionCancel(): WireFrame {
	return { opcode: OP_TARGET_ACTION, payload: Uint8Array.of( TARGET_ACTION_CANCEL ) };
}

/*
================
logoutCancelRequest

0x693380..0x69338D: while a logout/restart countdown runs (CGInterface
+0x39C) a ground click sends the empty 0x731F
(CGameApp_SendLogoutCancelRequest0x731F); 0xB31F ends the countdown.
================
*/
export function logoutCancelRequest(): WireFrame {
	return { opcode: OP_LOGOUT_CANCEL, payload: new Uint8Array( 0 ) };
}
