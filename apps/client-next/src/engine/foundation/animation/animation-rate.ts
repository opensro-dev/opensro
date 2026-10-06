/*
===========================================================================

animation-rate.ts - native character action-speed conversion and update wire

Spawn and 0x3453 share the denominator at CICharactor +4D8. Animation
installations capture its reciprocal at +4DC; movement has separate rates.

===========================================================================
*/
const NORMAL_ACTION_SPEED = 100;
const ACTION_SPEED_BYTES = 8;

/*
================
animationRate

85FBA5 and 775EF4 divide in x87, then store a float32 reciprocal.
================
*/
export function animationRate( denominator: number ): number {
	if ( !Number.isFinite( denominator ) || denominator <= 0 ) throw new Error( "Invalid animation speed" );
	const rate = Math.fround( NORMAL_ACTION_SPEED / denominator );
	if ( !Number.isFinite( rate ) || rate <= 0 ) throw new Error( "Invalid animation rate" );
	return rate;
}

/*
================
decodeAnimationSpeed

775EB0 reads GID followed by the action-speed denominator.
================
*/
export function decodeAnimationSpeed( payload: Uint8Array ) {
	if ( payload.length !== ACTION_SPEED_BYTES ) throw new Error( "Invalid animation speed packet" );
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	return { gid: view.getUint32( 0, true ), rate: animationRate( view.getFloat32( 4, true ) ) };
}
