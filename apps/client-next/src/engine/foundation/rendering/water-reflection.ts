/*
===========================================================================

water-reflection.ts - native planar mirror and water bump conversion

===========================================================================
*/
export const WATER_REFLECTION_SIZE = 512;

/*
================
waterReflectionMatrix

8B9DE0 mirrors the camera about one water height and flips winding. Multiplying
the existing view-projection by that world-space reflection preserves its
projection, including aspect and near/far planes.
================
*/
export function waterReflectionMatrix( view: Float32Array, height: number ): Float32Array {
	const result = new Float32Array( view );
	for ( let row = 0; row < 4; row++ ) {
		result[4 + row] = -view[4 + row]!;
		result[12 + row] = view[12 + row]! + 2 * height * view[4 + row]!;
	}
	return result;
}
