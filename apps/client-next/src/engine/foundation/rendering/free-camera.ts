/*
===========================================================================

free-camera.ts - the free-fly arm of the native free camera (#1787)

While the interface's free-camera byte (CGInterface +0x3CE) stands and
its waypoint play byte (+0x3CD) is clear, CGInterface_UpdatePointerTracking
(68FE50, free-fly arm at 690146) flies the app camera from the keyboard:
W/S move the orbit target along the view, A/D strafe it, the arrows turn
yaw and pitch, Q/E roll. The camera composer (A1BB00, run each frame from
CGame_OnRenderSceneStart) rebuilds the eye from that target, the angles
and the distance, so moving the target carries the whole camera. The
console's /cs row (690C40 case 136, +0x7B0, seeded 100) is the move
speed; only the two translations read it (6902AD, 69040B).

The port keeps the camera in its own frame: client yaw is native yaw
minus pi (camera-options.ts initialCameraYaw), so the eye sits at
target + distance * (sin yaw cos pitch, sin pitch, cos yaw cos pitch),
the follow camera's own formula.

===========================================================================
*/

// +0x7B0 as CGInterface_Constructor seeds it (68EF8B).
export const FREE_CAMERA_DEFAULT_SPEED = 100;
// 68F5D0 doubles the frame delta: every fade moves 2 units per second.
const FADE_RATE_PER_SECOND = 2;
// 68F5D0 rails, the -1 read from C461E8.
const FADE_MAX = 1;
const FADE_MIN = -1;
// 690146 turns each angle by twice its fade per second.
const TURN_RADIANS_PER_SECOND = 2;

export type Vec3 = readonly [number, number, number];

/*
================
FreeCameraKeys

The keys 690146 reads through CCursorManager_IsKeyDown, by native virtual
key: W 0x57, S 0x53, A 0x41, D 0x44, the arrows 0x25-0x28, Q 0x51, E 0x45.
================
*/
export interface FreeCameraKeys {
	readonly forward: boolean;
	readonly back: boolean;
	readonly strafeRight: boolean;
	readonly strafeLeft: boolean;
	readonly turnLeft: boolean;
	readonly turnRight: boolean;
	readonly pitchUp: boolean;
	readonly pitchDown: boolean;
	readonly rollPositive: boolean;
	readonly rollNegative: boolean;
}

/*
================
FreeCameraFades

The interface's five fade channels: +0x3D0 forward, +0x3D4 strafe,
+0x3D8 yaw, +0x3DC pitch, +0x3E0 roll. They live on the interface, not
the camera, so they persist across free-camera toggles.
================
*/
export interface FreeCameraFades {
	forward: number;
	strafe: number;
	yaw: number;
	pitch: number;
	roll: number;
}

/*
================
FreeCameraRig

The app camera's orbit target (+0x34C) and angles (+0x370 pitch,
+0x374 yaw, +0x378 roll, +0x37C distance), yaw in the client frame.
moved records whether any step changed the pose since the seed.
================
*/
export interface FreeCameraRig {
	target: [number, number, number];
	yaw: number;
	pitch: number;
	roll: number;
	distance: number;
	moved: boolean;
}

/*
================
createFreeCameraFades
================
*/
export function createFreeCameraFades(): FreeCameraFades {
	return { forward: 0, strafe: 0, yaw: 0, pitch: 0, roll: 0 };
}

/*
================
stepFreeCameraFade

CGInterface_StepFadeTowardsLimit (68F5D0), checked in its assembly: with
step = 2 * dt, the positive key alone raises the fade to at most 1, the
negative key alone lowers it to at least -1, neither key decays it toward
0 without crossing, and both keys leave it as it was.
================
*/
export function stepFreeCameraFade( positive: boolean, negative: boolean, fade: number, dtSeconds: number ): number {
	const step = Math.fround( dtSeconds * FADE_RATE_PER_SECOND );
	if ( positive && negative ) return fade;
	if ( positive ) return Math.fround( Math.min( fade + step, FADE_MAX ) );
	if ( negative ) return Math.fround( Math.max( fade - step, FADE_MIN ) );
	if ( fade > 0 ) return Math.fround( Math.max( fade - step, 0 ) );
	if ( fade < 0 ) return Math.fround( Math.min( fade + step, 0 ) );
	return fade;
}

/*
================
seedFreeCamera

The free arm starts from the orbit target the follow last stored
(68F830 writes +0x42C every frame while the byte is clear) and the app
angles it left. The port reads both back from the last applied camera,
so the first free frame draws exactly what the follow drew. Roll starts
level: the follow never writes it.
================
*/
export function seedFreeCamera( camera: { readonly eye: Vec3; readonly target: Vec3; } ): FreeCameraRig {
	const dx = camera.eye[0] - camera.target[0], dy = camera.eye[1] - camera.target[1];
	const dz = camera.eye[2] - camera.target[2];
	const distance = Math.hypot( dx, dy, dz );
	return {
		target: [ camera.target[0], camera.target[1], camera.target[2] ],
		yaw: Math.atan2( dx, dz ),
		pitch: distance > 0 ? Math.asin( Math.max( -1, Math.min( 1, dy / distance ) ) ) : 0,
		roll: 0,
		distance,
		moved: false
	};
}

/*
================
freeCameraEye

A1BB00: eye = target - (0, 0, distance) * RotZ(roll) * RotX(pitch) *
RotY(native yaw). Roll turns only the up vector, (0, 1, 0) through the
same matrix.
================
*/
export function freeCameraEye( rig: FreeCameraRig ): { eye: Vec3; up: Vec3; } {
	const sy = Math.sin( rig.yaw ), cy = Math.cos( rig.yaw );
	const sp = Math.sin( rig.pitch ), cp = Math.cos( rig.pitch );
	const sr = Math.sin( rig.roll ), cr = Math.cos( rig.roll );
	return {
		eye: [
			rig.target[0] + sy * cp * rig.distance,
			rig.target[1] + sp * rig.distance,
			rig.target[2] + cy * cp * rig.distance
		],
		up: [ sr * cy - cr * sp * sy, cr * cp, -sr * sy - cr * sp * cy ]
	};
}

/*
================
stepFreeCamera

One frame of the free-fly arm, in its native order (690146-690553):
translate along the view by the W/S fade, turn yaw, strafe along the
turned yaw by the A/D fade, then pitch and roll. Both translations scale
by fade * speed * dt; the turns by 2 * fade * dt, and never the speed.
The view direction is target minus the eye the composer last built
(+0x34C - +0x340), here the rig's own eye before this frame's changes.
================
*/
export function stepFreeCamera(
	rig: FreeCameraRig,
	fades: FreeCameraFades,
	keys: FreeCameraKeys,
	speed: number,
	dtSeconds: number
): void {
	const before = freeCameraEye( rig ).eye;
	fades.forward = stepFreeCameraFade( keys.forward, keys.back, fades.forward, dtSeconds );
	let vx = rig.target[0] - before[0], vy = rig.target[1] - before[1], vz = rig.target[2] - before[2];
	const length = Math.hypot( vx, vy, vz );
	if ( length > 0 ) {
		vx /= length;
		vy /= length;
		vz /= length;
	}
	const move = fades.forward * speed * dtSeconds;
	translate( rig, vx * move, vy * move, vz * move );
	// Left (0x25) is the positive key, and the arm subtracts: yaw turns down.
	fades.yaw = stepFreeCameraFade( keys.turnLeft, keys.turnRight, fades.yaw, dtSeconds );
	turn( rig, "yaw", -TURN_RADIANS_PER_SECOND * fades.yaw * dtSeconds );
	// CGInterface_FreeCam_YawRightVector (68F7E0): (1, 0, 0) * RotY(native
	// yaw), in the client frame (-cos yaw, 0, sin yaw).
	fades.strafe = stepFreeCameraFade( keys.strafeRight, keys.strafeLeft, fades.strafe, dtSeconds );
	const slide = fades.strafe * speed * dtSeconds;
	translate( rig, -Math.cos( rig.yaw ) * slide, 0, Math.sin( rig.yaw ) * slide );
	fades.pitch = stepFreeCameraFade( keys.pitchUp, keys.pitchDown, fades.pitch, dtSeconds );
	turn( rig, "pitch", TURN_RADIANS_PER_SECOND * fades.pitch * dtSeconds );
	fades.roll = stepFreeCameraFade( keys.rollPositive, keys.rollNegative, fades.roll, dtSeconds );
	turn( rig, "roll", TURN_RADIANS_PER_SECOND * fades.roll * dtSeconds );
}

/*
================
translate
================
*/
function translate( rig: FreeCameraRig, x: number, y: number, z: number ): void {
	if ( x === 0 && y === 0 && z === 0 ) return;
	rig.target = [
		Math.fround( rig.target[0] + x ),
		Math.fround( rig.target[1] + y ),
		Math.fround( rig.target[2] + z )
	];
	rig.moved = true;
}

/*
================
turn
================
*/
function turn( rig: FreeCameraRig, angle: "yaw" | "pitch" | "roll", delta: number ): void {
	if ( delta === 0 ) return;
	rig[angle] = Math.fround( rig[angle] + delta );
	rig.moved = true;
}
