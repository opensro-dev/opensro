/*
===========================================================================

instance-bounds.ts - one sphere around every placement of an instanced group

The selection walk tests each placement of an instanced group against the
frustum, but a group whose sphere is outside the frustum has no visible
placement, and a turning camera leaves most resident groups outside it.
The sphere holds every placement's frustum bound (its transformed local
box, or its sphere) with headroom, so rejecting it rejects every
placement test.

===========================================================================
*/

// Placement bounds are tested with float32 headroom of a few hundredths of
// a unit at world scale; a whole unit keeps the group test conservative.
const GROUP_SPHERE_MARGIN = 1;

/*
================
instanceGroupSphere

instances: 16 floats a placement (column-major, translation at 12..14).
bounds: the local box (min xyz, max xyz) placements are tested with, or
undefined for spheres of radius. Returns centre xyz and radius.
================
*/
export function instanceGroupSphere(
	instances: Float32Array,
	bounds: readonly number[] | undefined,
	radius: number
): Float64Array {
	const low = [ Infinity, Infinity, Infinity ], high = [ -Infinity, -Infinity, -Infinity ];
	for ( let i = 0; i < instances.length; i += 16 ) {
		for ( let axis = 0; axis < 3; axis++ ) {
			low[axis] = Math.min( low[axis]!, instances[i + 12 + axis]! );
			high[axis] = Math.max( high[axis]!, instances[i + 12 + axis]! );
		}
	}
	const cx = (low[0]! + high[0]!) / 2, cy = (low[1]! + high[1]!) / 2, cz = (low[2]! + high[2]!) / 2;
	let reach = 0;
	for ( let i = 0; i < instances.length; i += 16 ) {
		const ox = instances[i + 12]!, oy = instances[i + 13]!, oz = instances[i + 14]!;
		if ( !bounds ) {
			reach = Math.max( reach, Math.hypot( ox - cx, oy - cy, oz - cz ) + radius );
			continue;
		}
		for ( let corner = 0; corner < 8; corner++ ) {
			const x = bounds[corner & 1 ? 3 : 0]!, y = bounds[corner & 2 ? 4 : 1]!, z = bounds[corner & 4 ? 5 : 2]!;
			const wx = instances[i]! * x + instances[i + 4]! * y + instances[i + 8]! * z + ox,
				wy = instances[i + 1]! * x + instances[i + 5]! * y + instances[i + 9]! * z + oy,
				wz = instances[i + 2]! * x + instances[i + 6]! * y + instances[i + 10]! * z + oz;
			reach = Math.max( reach, Math.hypot( wx - cx, wy - cy, wz - cz ) );
		}
	}
	return Float64Array.of( cx, cy, cz, reach + GROUP_SPHERE_MARGIN );
}
