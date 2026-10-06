/*
===========================================================================

water-reflection.ts - native water capture camera and texture projection

===========================================================================
*/
export const WATER_REFLECTION_SIZE = 512;

/*
================
waterReflectionMatrix

8B9EC7 mirrors the orbit target's Y and negates pitch. A1BB00 rebuilds the
camera's up vector from that pitch: this also reverses the capture's clip Y
relative to a world-space reflection. Underwater (8B9F2C), the camera stays put.
================
*/
export function waterReflectionMatrix( view: Float32Array, height: number, above = true ): Float32Array {
	const result = new Float32Array( view );
	if ( !above ) return result;
	for ( let row = 0; row < 4; row++ ) {
		result[4 + row] = -view[4 + row]!;
		result[12 + row] = view[12 + row]! + 2 * height * view[4 + row]!;
	}
	for ( let col = 0; col < 4; col++ ) result[col * 4 + 1]! *= -1;
	return result;
}

/*
================
waterTextureProjection

8BA79B/8BAA28/8BAA49, and the underwater branch at 8BA7D0/8BA7F1.
D3DTSS_TCI_CAMERASPACEPOSITION with PROJECTED|COUNT3: scale X/Y, add
Z-weighted offsets, then divide by camera Z. These are not viewport UVs.
================
*/
export function waterTextureProjection( above: boolean ): Float32Array {
	return new Float32Array( [ .65, above ? .8 : -.8, .5, above ? -.52 : .52 ] );
}
