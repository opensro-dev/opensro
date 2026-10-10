/*
===========================================================================

viewer-camera.ts - the 3D viewer's fixed three-quarter camera

Port-only, not native (docs/VIEWER.md). Frames one model from its bounds:
the camera looks at the bounds' centre from VIEWER_YAW off the model's
front and VIEWER_PITCH above it, far enough that the bounding sphere fits
the field of view with a margin. Pure.

===========================================================================
*/
import type { WorldCamera } from "@/engine/contracts/scene";

// The camera's turn off the model's front (35 degrees), and its downward
// look (12 degrees), in radians.
export const VIEWER_YAW = 0.6108652381980153;
export const VIEWER_PITCH = 0.20943951023931956;
// The vertical field of view, and the margin around the bounding sphere.
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
	const distance = radius * VIEWER_MARGIN / Math.sin( VIEWER_FOV / 2 );
	// Turn the front by the yaw about y, then lift by the pitch.
	const cos = Math.cos( VIEWER_YAW ), sin = Math.sin( VIEWER_YAW );
	const x = front[0] * cos - front[1] * sin, z = front[0] * sin + front[1] * cos;
	const flat = Math.cos( VIEWER_PITCH );
	const eye: [number, number, number] = [
		target[0] + x * flat * distance,
		target[1] + Math.sin( VIEWER_PITCH ) * distance,
		target[2] + z * flat * distance
	];
	return {
		eye,
		target,
		fov: VIEWER_FOV,
		near: Math.max( MIN_NEAR, distance - radius * 2 ),
		far: distance + radius * 2
	};
}
