/*
===========================================================================

free-camera.ts - the GM free camera's fly step and its orbit view

While the interface's free-camera byte (CGInterface +0x3CE) stands, the
follow (68F830) and its clamp (68FAD0) return at once and
CGInterface_UpdatePointerTracking (68FE50, block 690146..69057E) flies
the camera instead: the look-at target (App+0x34C) is held at +0x42C,
and five held-key axes, each eased by CGInterface_StepFadeTowardsLimit
(68F5D0), move the target (W/S along the view, D/A along the yaw's
right vector, both scaled by the /cs speed at +0x7B0) and nudge the
shared yaw (App+0x374, Left/Right), pitch (App+0x370, Up/Down) and roll
(App+0x378, Q/E). The CApp camera build (A1BB00) then derives the eye
from the held target and the live angles and distance every frame:
eye = target - (0,0,dist)*Rz(roll)Rx(pitch)Ry(yaw), up = (0,1,0)*M.

This module owns only the arithmetic: the held target, the last built
eye, the roll and the five fades. The world stream (runtime/world)
owns the byte and the step's inputs, and the input owner keeps the
yaw and pitch the nudges are written back into.

The port's yaw is the native look yaw minus pi (camera-options.ts), so
the matrices below take yaw + pi; the world axes are the native ones.

===========================================================================
*/

export type FreeCameraVector = [number, number, number];

// 68F5D0: the fade moves by 2 * g_fDeltaTimeSeconds_Move per frame
// (68F5D6 fld dt; fadd st0, st0) and is held inside [-1, 1] (fld1 at
// 68F5F9, the -1.0f at C461E8).
const FADE_RATE = 2;
const FADE_LIMIT = 1;
// 69037A / 6904C9 / 69051D: each angle moves by fade * dt doubled.
const TURN_RATE = 2;
// The CGInterface constructor seeds +0x7B0 from BC9A2C (100.0f).
export const FREE_CAMERA_DEFAULT_SPEED = 100;
// Application_PumpIdleFrame (A0F533) caps g_fDeltaTimeSeconds_Move at
// 3.0f (BFC008), the delta every fade and translation reads.
export const FREE_CAMERA_MAX_DELTA_SECONDS = 3;

// The virtual keys 68FE50 polls through the input manager (A15AA0), each
// pair in its call order: the first key raises the fade, the second
// lowers it.
const VK_W = 0x57, VK_S = 0x53; // 69026A / 69025C, fade +0x3D0
const VK_LEFT = 0x25, VK_RIGHT = 0x27; // 690347 / 690339, fade +0x3D8
const VK_D = 0x44, VK_A = 0x41; // 6903A1 / 690393, fade +0x3D4
const VK_UP = 0x26, VK_DOWN = 0x28; // 6904A0 / 690492, fade +0x3DC
const VK_Q = 0x51, VK_E = 0x45; // 6904F4 / 6904E6, fade +0x3E0

/*
================
FreeCameraKeys

The held state of a native virtual key, as 68FE50 polls it per frame.
================
*/
export interface FreeCameraKeys {
	held( vk: number ): boolean;
}

/*
================
FreeCameraInput

What the world stream's fly reads from the display thread each frame:
the held keys, whether the chat edit has focus (69016F skips the fly
then), and where the nudged yaw and pitch are written back (the input
owner, which keeps them after the free camera ends, as App+0x374 and
App+0x370 keep them natively).
================
*/
export interface FreeCameraInput extends FreeCameraKeys {
	typing(): boolean;
	turn( yaw: number, pitch: number ): void;
}

/*
================
FreeCamera

The fly's state. target is +0x42C (null until seeded from the last
applied camera), eye the last built App+0x340, roll App+0x378, and the
fades +0x3D0 (move), +0x3D4 (strafe), +0x3D8 (turn), +0x3DC (pitch) and
+0x3E0 (roll). The fades are never cleared natively: /camera off only
zeroes the roll (6913DB).
================
*/
export interface FreeCamera {
	target: FreeCameraVector | null;
	eye: FreeCameraVector | null;
	roll: number;
	move: number;
	strafe: number;
	turn: number;
	pitch: number;
	rollFade: number;
}

/*
================
FreeCameraStep

One frame of the fly: the held keys, the frame delta, the /cs speed
(+0x7B0) and the live yaw (port convention) and pitch it nudges.
================
*/
export interface FreeCameraStep {
	readonly keys: FreeCameraKeys;
	readonly deltaSeconds: number;
	readonly speed: number;
	readonly yaw: number;
	readonly pitch: number;
}

/*
================
createFreeCamera
================
*/
export function createFreeCamera(): FreeCamera {
	return { target: null, eye: null, roll: 0, move: 0, strafe: 0, turn: 0, pitch: 0, rollFade: 0 };
}

/*
================
stepFreeCameraFade

CGInterface_StepFadeTowardsLimit (68F5D0), block by block in the ASM.
s = 2 * dt. Only the first key: min(v + s, 1). Only the second:
max(v - s, -1). Both: v unchanged. Neither: a positive v falls by s and
stops at 0 (68F62D..68F648); a negative v rises by s and is clamped
only at +1 (68F685 faddp, then the first key's clamp at 68F5FB), so it
can step past 0 by less than s, and the next frame's positive arm
brings it back to 0. Every store is a float (fstp dword).
================
*/
export function stepFreeCameraFade( first: boolean, second: boolean, value: number, deltaSeconds: number ): number {
	const f = Math.fround, s = f( deltaSeconds * FADE_RATE );
	if ( first ) {
		if ( second ) return value;
		const raised = f( value + s );
		return raised >= FADE_LIMIT ? FADE_LIMIT : raised;
	}
	if ( second ) {
		const lowered = f( value - s );
		return lowered <= -FADE_LIMIT ? -FADE_LIMIT : lowered;
	}
	if ( value > 0 ) {
		const fallen = f( value - s );
		return fallen > 0 ? fallen : 0;
	}
	if ( value < 0 ) {
		const risen = f( value + s );
		return risen >= FADE_LIMIT ? FADE_LIMIT : risen;
	}
	return value;
}

/*
================
rotateCameraVector

v * Rz(roll) * Rx(pitch) * Ry(yaw) with the D3DX row-vector matrices
A1BB00 multiplies (D3DXMatrixRotationZ/X/Y, then D3DXMatrixMultiply
twice): Rz first, then Rx, then Ry. yaw is the native look yaw.
================
*/
function rotateCameraVector( v: FreeCameraVector, roll: number, pitch: number, yaw: number ): FreeCameraVector {
	const cr = Math.cos( roll ), sr = Math.sin( roll );
	const x1 = v[0] * cr - v[1] * sr, y1 = v[0] * sr + v[1] * cr, z1 = v[2];
	const cp = Math.cos( pitch ), sp = Math.sin( pitch );
	const y2 = y1 * cp - z1 * sp, z2 = y1 * sp + z1 * cp;
	const cy = Math.cos( yaw ), sy = Math.sin( yaw );
	return [ x1 * cy + z2 * sy, y2, -x1 * sy + z2 * cy ];
}

/*
================
freeCameraView

The CApp camera build (A1BB00) for a held target: the eye sits the
distance behind the target along the rotated +Z, and up is the rotated
+Y. yaw is the port's (native look yaw minus pi).
================
*/
export function freeCameraView(
	target: readonly [number, number, number],
	yaw: number,
	pitch: number,
	roll: number,
	distance: number
): { eye: FreeCameraVector; up: FreeCameraVector; } {
	const nativeYaw = yaw + Math.PI;
	const back = rotateCameraVector( [ 0, 0, distance ], roll, pitch, nativeYaw );
	return {
		eye: [ target[0] - back[0], target[1] - back[1], target[2] - back[2] ],
		up: rotateCameraVector( [ 0, 1, 0 ], roll, pitch, nativeYaw )
	};
}

/*
================
moveTarget

target += direction * fade * speed * dt, each product stored as a float
in the order 68FE50 stores them (6902A9, 6902D7, 69030A, then the adds
at 690314..690327; the strafe at 69043D..690480).
================
*/
function moveTarget( target: FreeCameraVector, direction: FreeCameraVector, fade: number, step: FreeCameraStep ) {
	const f = Math.fround, speed = f( step.speed ), dt = f( step.deltaSeconds );
	for ( let i = 0; i < 3; i++ ) {
		const scaled = f( f( f( direction[i]! * fade ) * speed ) * dt );
		target[i] = f( target[i]! + scaled );
	}
}

/*
================
stepFreeCamera

The fly block of 68FE50 (690213..690525), in its native order:
1. forward = normalize(target - last eye) (690213, 410890); W/S fade
   +0x3D0 moves the target along it (the first +0x7B0 reader, 6902AD).
2. Left/Right fade +0x3D8: yaw -= 2 * fade * dt (69037C fsubp).
3. D/A fade +0x3D4 moves the target along (1,0,0)*RotY(yaw) of the
   yaw just written (68F7E0, the second +0x7B0 reader, 69040B).
4. Up/Down fade +0x3DC: pitch += 2 * fade * dt, unclamped (6904CB).
5. Q/E fade +0x3E0: roll += 2 * fade * dt (69051F).
The camera must be seeded (target and eye). Returns the nudged yaw
(port convention) and pitch; the rotations never read the speed.
================
*/
export function stepFreeCamera( camera: FreeCamera, step: FreeCameraStep ): { yaw: number; pitch: number; } {
	const target = camera.target!, eye = camera.eye!, keys = step.keys, dt = step.deltaSeconds;
	const f = Math.fround;
	const toward: FreeCameraVector = [ target[0] - eye[0], target[1] - eye[1], target[2] - eye[2] ];
	const length = Math.hypot( toward[0], toward[1], toward[2] );
	const forward: FreeCameraVector = length > 0 ?
		[ f( toward[0] / length ), f( toward[1] / length ), f( toward[2] / length ) ] :
		[ 0, 0, 0 ];
	camera.move = stepFreeCameraFade( keys.held( VK_W ), keys.held( VK_S ), camera.move, dt );
	moveTarget( target, forward, camera.move, step );
	camera.turn = stepFreeCameraFade( keys.held( VK_LEFT ), keys.held( VK_RIGHT ), camera.turn, dt );
	const yaw = step.yaw - TURN_RATE * camera.turn * dt;
	camera.strafe = stepFreeCameraFade( keys.held( VK_D ), keys.held( VK_A ), camera.strafe, dt );
	const right = rotateCameraVector( [ 1, 0, 0 ], 0, 0, yaw + Math.PI );
	moveTarget( target, [ f( right[0] ), f( right[1] ), f( right[2] ) ], camera.strafe, step );
	camera.pitch = stepFreeCameraFade( keys.held( VK_UP ), keys.held( VK_DOWN ), camera.pitch, dt );
	const pitch = step.pitch + TURN_RATE * camera.pitch * dt;
	camera.rollFade = stepFreeCameraFade( keys.held( VK_Q ), keys.held( VK_E ), camera.rollFade, dt );
	camera.roll = f( camera.roll + TURN_RATE * camera.rollFade * dt );
	return { yaw, pitch };
}
