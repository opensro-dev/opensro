/*
===========================================================================

native-movement.ts - the native movement wire and the shared path model

Decoders for the v1.150 movement packets (0xB738 acknowledgement, 0x376F
speed channels) and the segment model both the local and the remote movers
sample, so predicted and received movement share one set of transitions.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import { hypot2 } from "@/engine/foundation/math/hypot";

// World units per region side; region-local x/z span [0, REGION_SIZE).
export const REGION_SIZE = 1920;
// Heading words per full turn on the movement wire.
export const HEADING_SCALE = 65535;
// CPSMission_OnSkillNavResponse0xB738 (0x776200) divides the source-block
// X/Z words by 10.0.
const SOURCE_SCALE = 10;
const DESTINATION_MODE = 1;
const ANGULAR_MODE = 0;

/*
================
validMovementSpeed

A speed channel is any finite non-negative rate. Zero is authored data: a
stationary monster (MOB_OA_DESERTTRUNKZ walks and runs at 0) keeps 0 under
every slowing status, and CPSMission_OnEntitySpeedUpdate0x376F (775E40)
stores the floats at mover +0x654/+0x658 without a check.
================
*/
export function validMovementSpeed( speed: number ) {
	return Number.isFinite( speed ) && speed >= 0;
}

/*
================
movementDuration

Milliseconds to cover distance at speed. The native mover advances by
speed times the frame delta, so at speed 0 it makes no progress: the leg
holds where it is (unbounded duration) until a later speed resumes it.
================
*/
export function movementDuration( distance: number, speed: number ) {
	if ( !validMovementSpeed( speed ) ) throw new Error( "Invalid movement speed" );
	if ( !distance ) return 0;
	return speed ? distance / speed * 1000 : Infinity;
}

/*
================
decodeMovementSpeeds

775E40 reads exactly {GID, walk, run}; these are live channels, not a new
destination and not a movement acknowledgement.
================
*/
export function decodeMovementSpeeds( p: Uint8Array ) {
	if ( p.length !== 12 ) throw Error( "Invalid movement speed channels" );
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength ),
		gid = v.getUint32( 0, true ),
		walkSpeed = v.getFloat32( 4, true ),
		runSpeed = v.getFloat32( 8, true );
	if ( !gid || ![ walkSpeed, runSpeed ].every( validMovementSpeed ) ) {
		throw Error( "Invalid movement speed channels" );
	}
	return { gid, walkSpeed, runSpeed };
}

/*
================
movementSpeedTransition

Retimes the rest of a leg from the pose reached now: the remaining distance
at the new speed. A leg held at speed 0 resumes from where it stopped.
================
*/
export function movementSpeedTransition(
	segment: MovementSegment,
	previous: number,
	next: number,
	now: number
): MovementSegment {
	if ( !validMovementSpeed( previous ) ) throw Error( "Invalid movement speed" );
	const from = sampleMovement( segment, now );
	return { ...segment, from, start: now, duration: movementDuration( poseDistance( from, segment.to ), next ) };
}

/*
================
NativeMovement

One decoded 0xB738. kind names what the mover must do:

  - "destination": walk from -> to (mode 1, StartWaypointNavigation);
  - "direction": walk heading from `from` until blocked (mode 0 with the
    source block, CNavigationController_StartDirectMove 0x86D150);
  - "keep": mode 0 without the source block. The handler only enters action
    state 9 (and, for the local player, runs UpdateMovementHeading); the
    path in progress is left as it is.

The client never reads the angular flag byte: StartDirectMove runs for any
mode-0 acknowledgement that carries a source.
================
*/
export interface NativeMovement {
	readonly gid: number;
	readonly kind: "destination" | "direction" | "keep";
	readonly from: Pose;
	readonly to: Pose;
	readonly heading?: number;
}

/*
================
decodeNativeMovement

v1.150 0xB738, shared by remote motion and local server-driven movement.
================
*/
export function decodeNativeMovement( p: Uint8Array, current: Pose ): NativeMovement {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	if ( p.length < 9 || p[4]! > DESTINATION_MODE ) throw new Error( "Invalid movement broadcast" );
	const angular = p[4] === ANGULAR_MODE, offset = angular ? 8 : 13;
	if ( p.length <= offset || p[offset]! > 1 || p.length !== offset + 1 + (p[offset] ? 10 : 0) ) {
		throw new Error( "Invalid movement source block" );
	}
	const sourced = p[offset] === 1;
	const from = sourced ?
		{
			...current,
			regionId: v.getUint16( offset + 1, true ),
			x: v.getInt16( offset + 3, true ) / SOURCE_SCALE,
			y: v.getFloat32( offset + 5, true ),
			z: v.getInt16( offset + 9, true ) / SOURCE_SCALE
		} :
		current;
	if ( !Number.isFinite( from.y ) ) throw new Error( "Invalid movement height" );
	const gid = v.getUint32( 0, true );
	if ( angular ) {
		const heading = v.getUint16( 6, true );
		return sourced ?
			{ gid, kind: "direction", from, to: { ...from, angle: heading }, heading } :
			{ gid, kind: "keep", from, to: from };
	}
	const to = {
		...from,
		regionId: v.getUint16( 5, true ),
		x: v.getInt16( 7, true ),
		y: v.getInt16( 9, true ),
		z: v.getInt16( 11, true )
	};
	return { gid, kind: "destination", from, to: { ...to, angle: movementHeading( from, to ) } };
}

/*
================
poseDistance
================
*/
export function poseDistance( a: Pose, b: Pose ) {
	if ( (a.regionId | b.regionId) & 0x8000 ) {
		if ( a.regionId !== b.regionId ) throw new Error( "Dungeon transition requires teleport" );
		return hypot2( b.x - a.x, b.z - a.z );
	}
	return hypot2(
		b.x - a.x + ((b.regionId & 255) - (a.regionId & 255)) * REGION_SIZE,
		b.z - a.z + ((b.regionId >>> 8) - (a.regionId >>> 8)) * REGION_SIZE
	);
}

/*
================
movementGait

Native movement mode 2 is walking; other modes use running speed.
================
*/
export function movementGait( mode: number | undefined ): "walk" | "run" {
	return mode === 2 ? "walk" : "run";
}

/*
================
MovementSegment

Both local and remote motion use the same sampling and state transitions.
================
*/
export interface MovementSegment {
	from: Pose;
	to: Pose;
	start: number;
	duration: number;
}

/*
================
interpolateMovement
================
*/
export function interpolateMovement( from: Pose, to: Pose, t: number ): Pose {
	if ( (from.regionId & 0x8000) !== (to.regionId & 0x8000) ) return from;
	if ( from.regionId & 0x8000 ) {
		if ( from.regionId !== to.regionId ) return from;
		return {
			...to,
			x: from.x + (to.x - from.x) * t,
			y: from.y + (to.y - from.y) * t,
			z: from.z + (to.z - from.z) * t
		};
	}
	const wx = from.x + (from.regionId & 255) * REGION_SIZE, wz = from.z + (from.regionId >>> 8) * REGION_SIZE;
	const x = wx + (to.x + (to.regionId & 255) * REGION_SIZE - wx) * t,
		z = wz + (to.z + (to.regionId >>> 8) * REGION_SIZE - wz) * t;
	return {
		regionId: Math.floor( x / REGION_SIZE ) | (Math.floor( z / REGION_SIZE ) << 8),
		x: x % REGION_SIZE,
		y: from.y + (to.y - from.y) * t,
		z: z % REGION_SIZE,
		angle: to.angle
	};
}

/*
================
sampleMovement
================
*/
export function sampleMovement( segment: MovementSegment, now: number ): Pose {
	return interpolateMovement(
		segment.from,
		segment.to,
		segment.duration ? Math.max( 0, Math.min( 1, (now - segment.start) / segment.duration ) ) : 1
	);
}

/*
================
movementModeTransition
================
*/
export function movementModeTransition(
	segment: MovementSegment,
	mode: number,
	speed: number,
	now: number,
	serverTimed = false
): { pose: Pose; segment: MovementSegment | null; } {
	const pose = sampleMovement( segment, now );
	if ( mode === 0 || mode === 4 ) return { pose, segment: null };
	// A receipt's authoritative arrival time is not replaced by client speed.
	if ( serverTimed ) return { pose, segment };
	return {
		pose,
		segment: {
			from: pose,
			to: segment.to,
			start: now,
			duration: movementDuration( poseDistance( pose, segment.to ), speed )
		}
	};
}

/*
================
movementHeading

8791A0 inverts 8788C0's {sin(yaw), 0, -cos(yaw)}. Keep the coordinate-sector
conversion shared by predicted and received movement.
================
*/
export function movementHeading( from: Pose, to: Pose ): number {
	const dungeon = !!((from.regionId | to.regionId) & 0x8000);
	if ( dungeon && from.regionId !== to.regionId ) return from.angle;
	const dx = to.x - from.x + (dungeon ? 0 : ((to.regionId & 255) - (from.regionId & 255)) * REGION_SIZE);
	const dz = to.z - from.z + (dungeon ? 0 : ((to.regionId >>> 8) - (from.regionId >>> 8)) * REGION_SIZE);
	if ( dx === 0 && dz === 0 ) return to.angle;
	// 853550 converts model yaw back to wire bearing by subtracting pi/2.
	const yaw = (Math.atan2( dz, dx ) + Math.PI * 2) % (Math.PI * 2);
	return Math.trunc( yaw / (Math.PI * 2) * HEADING_SCALE );
}
