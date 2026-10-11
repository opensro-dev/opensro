/*
===========================================================================

viewer-camera.ts - the 3D viewer's fixed three-quarter camera

Port-only, not native (docs/VIEWER.md). Frames one model from its bounds:
the camera looks at the bounds' centre from VIEWER_YAW off the model's
front and VIEWER_PITCH above it, at the nearest distance where all eight
corners of the box fit the square field of view with a margin. Pure.

===========================================================================
*/
import type { WorldCamera } from "@/engine/contracts/scene";

// The camera's turn off the model's front (35 degrees), and its downward
// look (12 degrees), in radians.
export const VIEWER_YAW = 0.6108652381980153;
export const VIEWER_PITCH = 0.20943951023931956;
// The field of view (square output), and the margin around the box.
const VIEWER_FOV = 0.6;
const VIEWER_MARGIN = 1.08;
// The nearest plane never reaches the eye.
const MIN_NEAR = 0.01;

/*
================
ViewerBounds

A model's axis-aligned bounds in world units (y up).
================
*/
export interface ViewerBounds {
	readonly min: readonly [number, number, number];
	readonly max: readonly [number, number, number];
}

/*
================
viewerCamera

The camera for bounds, given the model's front as a unit [x, z] direction.
Throws on empty or non-finite bounds.
================
*/
export function viewerCamera( bounds: ViewerBounds, front: readonly [number, number] ): WorldCamera {
	const { min, max } = bounds;
	if ( ![ ...min, ...max, ...front ].every( Number.isFinite ) || min.some( ( v, i ) => v > max[i]! ) ) {
		throw Error( "Invalid viewer bounds" );
	}
	const target: [number, number, number] = [ (min[0] + max[0]) / 2, (min[1] + max[1]) / 2, (min[2] + max[2]) / 2 ];
	const radius = Math.max( Math.hypot( max[0] - min[0], max[1] - min[1], max[2] - min[2] ) / 2, MIN_NEAR );
	// The unit vector from the target back to the eye: the front turned by
	// the yaw about y, then lifted by the pitch. right and up complete the view.
	const cos = Math.cos( VIEWER_YAW ), sin = Math.sin( VIEWER_YAW );
	const x = front[0] * cos - front[1] * sin, z = front[0] * sin + front[1] * cos;
	const flat = Math.cos( VIEWER_PITCH ), lift = Math.sin( VIEWER_PITCH );
	const back = [ x * flat, lift, z * flat ], right = [ z, 0, -x ];
	const up = [
		back[1]! * right[2]! - back[2]! * right[1]!,
		back[2]! * right[0]! - back[0]! * right[2]!,
		back[0]! * right[1]! - back[1]! * right[0]!
	];
	// A corner at offset p from the target, seen from distance d, stays in
	// view while |p.right| and |p.up| are at most slope * (d - p.back).
	const slope = Math.tan( VIEWER_FOV / 2 ) / VIEWER_MARGIN;
	let distance = MIN_NEAR;
	for ( let corner = 0; corner < 8; corner++ ) {
		const p = [ 0, 1, 2 ].map( axis => ((corner >> axis) & 1 ? max[axis]! : min[axis]!) - target[axis]! );
		const along = ( v: readonly number[] ) => p[0]! * v[0]! + p[1]! * v[1]! + p[2]! * v[2]!;
		distance = Math.max(
			distance,
			along( back ) + Math.max( Math.abs( along( right ) ), Math.abs( along( up ) ) ) / slope
		);
	}
	const eye: [number, number, number] = [
		target[0] + back[0]! * distance,
		target[1] + back[1]! * distance,
		target[2] + back[2]! * distance
	];
	return {
		eye,
		target,
		fov: VIEWER_FOV,
		near: Math.max( MIN_NEAR, distance - radius * 2 ),
		far: distance + radius * 2
	};
}
