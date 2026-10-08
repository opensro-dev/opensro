/*
===========================================================================

camera-options.ts - sight mode and heading-locked camera orientation

The input camera stores the target-to-eye azimuth used by follow-camera.

===========================================================================
*/
export type SightMode = 0 | 1 | 2;
/*
================
sightMode
================
*/
export function sightMode( value: unknown ): SightMode {
	if ( value !== 0 && value !== 1 && value !== 2 ) throw Error( "Invalid sight mode" );
	return value;
}
// CApp_ResetCameraDefaults (818790), run by CGInterface_OnCreate, stores
// orbit pitch +0x370 = 0.19f and look yaw +0x374 = 3.14f.
const NATIVE_RESET_PITCH = 0.19;
const NATIVE_RESET_LOOK_YAW = 3.14;

/*
================
initialCameraPitch
================
*/
export function initialCameraPitch(): number {
	return Math.fround( NATIVE_RESET_PITCH );
}

/*
================
initialCameraYaw

The port's yaw is the native look yaw minus pi (thirdPersonYaw).
================
*/
export function initialCameraYaw(): number {
	return Math.fround( NATIVE_RESET_LOOK_YAW ) - Math.PI;
}

/*
================
thirdPersonYaw

68F8E2..68F8FB computes the native look azimuth. The port adds its offset to
place the eye, so reverse that direction to put the eye behind the actor.
================
*/
export function thirdPersonYaw( playerYaw: number ): number {
	const nativeLookYaw = Math.fround( 1.5700000524520874 - playerYaw + 1.5707963705062866 );
	return nativeLookYaw - Math.PI;
}
