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
