/*
===========================================================================

spine-aim.ts - a caster's Spine_Base turns toward a target above or below

CIDecoSkill_Update (8DC440) aims every skill whose animation set names a
rotation axis (skilleffect column 23, record+0xBC): each frame it sets the
caster's bone rotator for Spine_Base (CCompoundObj_SetBoneRotator, vtable
+0x7C) to the vertical bearing of the target, and CCompChar_ApplyBoneRotators
(A9ADD0) steps the bone toward it while the pose is built. The final stage's
command (flag 8, 8DDE93) and the decoration's end release it: the angles
ease back to zero and the rotator is dropped (CCompoundObj_ReleaseBoneRotator).

This module owns the native arithmetic only; the effects owner arms and
feeds it, and the pose applies its rotation.

===========================================================================
*/

export const SPINE_AIM_BONE = "Spine_Base";
// 8DC440 pushes 1.74532914 rad/s for every aim.
const SPINE_AIM_RADIANS_PER_SECOND = 1.74532914;
// Within 15 units of horizontal distance the aim is level (8DC440).
const SPINE_AIM_MIN_DISTANCE = 15;
// The bearing is clamped to +-30 degrees (0.52359879).
const SPINE_AIM_LIMIT = 0.52359879;
// A9ADD0's step uses the frame's milliseconds: speed * dt * 0.001.
const MILLISECONDS = 0.001;
// World regions are 1920 units; the sector bit marks a dungeon frame.
const REGION_SIZE = 1920;
const DUNGEON_REGION_BIT = 0x8000;

/*
================
SpineAimRotator

One bone rotator entry (+0x264 list node): current and target yaw, pitch
and roll, and the release flag that drops it once it has eased home.
================
*/
export interface SpineAimRotator {
	readonly current: [number, number, number];
	readonly target: [number, number, number];
	releasing: boolean;
}

/*
================
spineAimAngle

8DC440: level within 15 units, else atan( (height + dy) / distance )
clamped to +-30 degrees. height is the record+0xB8 stage's target binding
height less its start binding height, measured when the cast began (+0xF8).
================
*/
export function spineAimAngle( dx: number, dy: number, dz: number, height: number ): number {
	const distance = Math.fround( Math.sqrt( Math.fround( dx * dx + dz * dz ) ) );
	if ( !(distance > SPINE_AIM_MIN_DISTANCE) ) return 0;
	const angle = Math.fround( Math.atan( Math.fround( (height + dy) / distance ) ) );
	if ( !(angle >= -SPINE_AIM_LIMIT) ) return Math.fround( -SPINE_AIM_LIMIT );
	return angle >= SPINE_AIM_LIMIT ? Math.fround( SPINE_AIM_LIMIT ) : angle;
}

/*
================
spineAimBearing

spineAimAngle from caster to target positions across region frames. Two
dungeon regions share no frame, so the aim stays level between them.
================
*/
export function spineAimBearing(
	caster: { readonly regionId: number; readonly x: number; readonly y: number; readonly z: number; },
	target: { readonly regionId: number; readonly x: number; readonly y: number; readonly z: number; },
	height: number
): number {
	const dungeon = !!((caster.regionId | target.regionId) & DUNGEON_REGION_BIT);
	if ( dungeon && caster.regionId !== target.regionId ) return 0;
	const dx = target.x - caster.x +
		(dungeon ? 0 : ((target.regionId & 255) - (caster.regionId & 255)) * REGION_SIZE);
	const dz = target.z - caster.z +
		(dungeon ? 0 : ((target.regionId >>> 8) - (caster.regionId >>> 8)) * REGION_SIZE);
	return spineAimAngle( dx, target.y - caster.y, dz, height );
}

/*
================
spineAimTargets

8DC440's switch: the axis token (1 Roll, 2 Yaw, 3 Pitch, 4 RollR, 5 YawR,
6 PitchR) selects which of yaw, pitch and roll takes the angle; Roll, YawR
and PitchR take it negated.
================
*/
export function spineAimTargets( axis: number, angle: number ): [number, number, number] {
	switch ( axis ) {
		case 1:
			return [ 0, 0, -angle ];
		case 2:
			return [ angle, 0, 0 ];
		case 3:
			return [ 0, angle, 0 ];
		case 4:
			return [ 0, 0, angle ];
		case 5:
			return [ -angle, 0, 0 ];
		case 6:
			return [ 0, -angle, 0 ];
		default:
			return [ 0, 0, 0 ];
	}
}

/*
================
stepAngle

BoneRotator_StepAngle (A8FA60): move toward target by step, landing on it
when within one step. Returns whether it has arrived.
================
*/
function stepAngle( values: [number, number, number], index: number, target: number, step: number ): boolean {
	const current = values[index]!;
	if ( current === target ) return true;
	const difference = Math.fround( target - current );
	if ( !(step < Math.abs( difference )) ) {
		values[index] = target;
		return true;
	}
	values[index] = Math.fround( difference > 0 ? current + step : current - step );
	return false;
}

/*
================
stepSpineAim

One CCompChar_ApplyBoneRotators pass over the rotator. Returns false when
a released rotator has eased home and is erased.
================
*/
export function stepSpineAim( rotator: SpineAimRotator, milliseconds: number ): boolean {
	const step = Math.fround( SPINE_AIM_RADIANS_PER_SECOND * milliseconds * MILLISECONDS );
	const yaw = stepAngle( rotator.current, 0, rotator.target[0], step ),
		pitch = stepAngle( rotator.current, 1, rotator.target[1], step ),
		roll = stepAngle( rotator.current, 2, rotator.target[2], step );
	return !(rotator.releasing && yaw && pitch && roll);
}

/*
================
spineAimRotation

D3DXQuaternionRotationYawPitchRoll of the current angles, carried into the
imported model space. The exporter mirrors native Z per node, so a native
bone rotation (x, y, z, w) is (-x, -y, z, w) there; A9ADD0 multiplies it
after the bone's own local rotation, which is rotation * local in the
loader's Hamilton order.
================
*/
export function spineAimRotation( rotator: SpineAimRotator ): [number, number, number, number] {
	const [yaw, pitch, roll] = rotator.current;
	const sy = Math.sin( yaw / 2 ),
		cy = Math.cos( yaw / 2 ),
		sp = Math.sin( pitch / 2 ),
		cp = Math.cos( pitch / 2 ),
		sr = Math.sin( roll / 2 ),
		cr = Math.cos( roll / 2 );
	const x = cy * sp * cr + sy * cp * sr,
		y = sy * cp * cr - cy * sp * sr,
		z = cy * cp * sr - sy * sp * cr,
		w = cy * cp * cr + sy * sp * sr;
	return [ -x, -y, z, w ];
}
