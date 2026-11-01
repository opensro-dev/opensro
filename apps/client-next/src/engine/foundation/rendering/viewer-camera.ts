/*
===========================================================================

viewer-camera.ts - the 3D viewer's fixed three-quarter camera

Port-only, not native (docs/guides/VIEWER.md). Frames one model from its bounds:
the camera looks at the bounds' centre from VIEWER_YAW off the model's
front and VIEWER_PITCH above it, at the nearest distance where all eight
corners of the box fit the field of view with a margin, for the output's
aspect (width / height). Pure.

===========================================================================
*/
import type { WorldCamera } from "@/engine/contracts/scene";
import { cameraBasis } from "./world-math";

// The camera's turn off the model's front (35 degrees), and its downward
// look (12 degrees), in radians.
export const VIEWER_YAW = 0.6108652381980153;
export const VIEWER_PITCH = 0.20943951023931956;
// The vertical field of view, and the margin around the box.
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

The camera for bounds, given the model's front as a unit [x, z] direction
and the output's aspect (width / height; 1 is square). Throws on empty or
non-finite bounds or a non-positive aspect.
================
*/
export function viewerCamera( bounds: ViewerBounds, front: readonly [number, number], aspect = 1 ): WorldCamera {
	const { min, max } = bounds;
	if (
		![ ...min, ...max, ...front, aspect ].every( Number.isFinite ) || min.some( ( v, i ) => v > max[i]! ) ||
		aspect <= 0
	) {
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
	// view while |p.up| is at most slope * (d - p.back), and |p.right| at most
	// slope * aspect times that (the field of view is vertical).
	const slope = Math.tan( VIEWER_FOV / 2 ) / VIEWER_MARGIN;
	let distance = MIN_NEAR;
	for ( let corner = 0; corner < 8; corner++ ) {
		const p = [ 0, 1, 2 ].map( axis => ((corner >> axis) & 1 ? max[axis]! : min[axis]!) - target[axis]! );
		const along = ( v: readonly number[] ) => p[0]! * v[0]! + p[1]! * v[1]! + p[2]! * v[2]!;
		distance = Math.max(
			distance,
			along( back ) + Math.max( Math.abs( along( right ) ) / aspect, Math.abs( along( up ) ) ) / slope
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

/*
================
ViewerFill

How refitCamera fills the frame: "height" puts the drawn model at that
share of the frame's height (a long weapon may leave the sides), "contain"
puts it at that share of whichever axis it fills first.
================
*/
export type ViewerFill = { readonly kind: "height" | "contain"; readonly share: number; };

/*
================
refitCamera

The camera that centres the drawn picture's opaque box and scales it to
the fill, from the camera that drew it. The box is in pixels of a width x
height picture. The model's depth makes perspective scale it slightly
unevenly; one refit lands within a few percent of the fill.
================
*/
export function refitCamera(
	camera: WorldCamera,
	box: readonly [number, number, number, number],
	width: number,
	height: number,
	fill: ViewerFill
): WorldCamera {
	const fov = camera.fov ?? VIEWER_FOV;
	const back = camera.eye.map( ( v, i ) => v - camera.target[i]! ), distance = Math.hypot( ...back );
	const unit = back.map( v => v / distance );
	// The picture's right and up axes, from the basis the projection draws
	// with (world-math.ts, left-handed): a right-handed cross here mirrored
	// every horizontal correction, so an off-centre model moved further off.
	const { right, up } = cameraBasis( camera );
	// World units per pixel at the target's depth.
	const perPixel = 2 * distance * Math.tan( fov / 2 ) / height;
	// The box centre's offset from the picture's centre: x right, y up.
	const dx = ((box[0] + box[2]) / 2 - width / 2) * perPixel, dy = (height / 2 - (box[1] + box[3]) / 2) * perPixel;
	const target = camera.target.map( ( v, i ) => v + right[i]! * dx + up[i]! * dy );
	const tall = (box[3] - box[1]) / height, wide = (box[2] - box[0]) / width;
	const scale = (fill.kind === "height" ? tall : Math.max( tall, wide )) / fill.share;
	const next = distance * scale;
	return {
		...camera,
		target: [ target[0]!, target[1]!, target[2]! ],
		eye: [ target[0]! + unit[0]! * next, target[1]! + unit[1]! * next, target[2]! + unit[2]! * next ],
		near: Math.max( MIN_NEAR, (camera.near ?? MIN_NEAR) * scale ),
		far: (camera.far ?? next * 2) * scale
	};
}

/*
================
touchesEdge

Whether a drawn picture's opaque box reaches the picture's border: the
model may run past the frame, so the box measures only what fitted. With
a "height" fill only the top and bottom count, since that fill lets a long
weapon leave the sides.
================
*/
export function touchesEdge(
	box: readonly [number, number, number, number],
	width: number,
	height: number,
	fill?: ViewerFill
): boolean {
	const sides = fill?.kind !== "height" && (box[0] <= 0 || box[2] >= width);
	return sides || box[1] <= 0 || box[3] >= height;
}

/*
================
widenCamera

The same view from factor times as far, the near and far planes moved
with it, so a model the first framing clipped can be measured whole.
================
*/
export function widenCamera( camera: WorldCamera, factor: number ): WorldCamera {
	const eye = camera.eye.map( ( v, i ) => camera.target[i]! + (v - camera.target[i]!) * factor );
	return {
		...camera,
		eye: [ eye[0]!, eye[1]!, eye[2]! ],
		near: Math.max( MIN_NEAR, (camera.near ?? MIN_NEAR) * factor ),
		far: (camera.far ?? Math.hypot( ...camera.eye.map( ( v, i ) => v - camera.target[i]! ) ) * 2) * factor
	};
}

// How far a refit picture may miss its fill and the centre, as a share of
// the picture, and still count as framed.
const FRAMED_TOLERANCE = 0.02;

/*
================
framed

Whether a drawn picture's opaque box already meets the fill: centred and
at the fill's share within FRAMED_TOLERANCE, and clear of the border. A
"height" fill may leave the sides (a long weapon), so it judges only the
height and the vertical centre.
================
*/
export function framed(
	box: readonly [number, number, number, number],
	width: number,
	height: number,
	fill: ViewerFill
): boolean {
	const tall = (box[3] - box[1]) / height, wide = (box[2] - box[0]) / width;
	const dy = Math.abs( (box[1] + box[3]) / 2 - height / 2 ) / height,
		dx = Math.abs( (box[0] + box[2]) / 2 - width / 2 ) / width;
	if ( fill.kind === "height" ) {
		return Math.abs( tall - fill.share ) <= FRAMED_TOLERANCE && dy <= FRAMED_TOLERANCE && box[1] > 0 &&
			box[3] < height;
	}
	return Math.abs( Math.max( tall, wide ) - fill.share ) <= FRAMED_TOLERANCE && dx <= FRAMED_TOLERANCE &&
		dy <= FRAMED_TOLERANCE && !touchesEdge( box, width, height );
}

// The fitting loop's limits: a clipped measuring picture is drawn again
// from WIDEN_FACTOR times as far (REFIT_WIDEN_FACTOR once a refit has run,
// a gentle back-off), at most MAX_WIDENS times; a picture off the fill is
// refit at most MAX_REFITS times.
const WIDEN_FACTOR = 2;
const REFIT_WIDEN_FACTOR = 1.25;
const MAX_WIDENS = 8;
const MAX_REFITS = 6;

/*
================
ViewerFitting

How often the fitting loop has widened and refit one camera.
================
*/
export interface ViewerFitting {
	readonly widened: number;
	readonly refits: number;
}

/*
================
nextFraming

One step of fitting the camera to what it drew: the camera to draw the
next measuring picture with, and the counts, or null once the picture is
framed (or the limits are spent). A refit can only frame what was drawn,
so a clipped picture is widened first. One refit measures at the target's
depth, and a model that reaches far off it (a ride, a wing) lands off the
fill, so the refit picture is measured and refit again.
================
*/
export function nextFraming(
	camera: WorldCamera,
	box: readonly [number, number, number, number],
	width: number,
	height: number,
	fill: ViewerFill,
	fitting: ViewerFitting
): { readonly camera: WorldCamera; readonly fitting: ViewerFitting; } | null {
	if ( fitting.widened < MAX_WIDENS && touchesEdge( box, width, height, fill ) ) {
		return {
			camera: widenCamera( camera, fitting.refits === 0 ? WIDEN_FACTOR : REFIT_WIDEN_FACTOR ),
			fitting: { ...fitting, widened: fitting.widened + 1 }
		};
	}
	if ( fitting.refits < MAX_REFITS && !framed( box, width, height, fill ) ) {
		return {
			camera: refitCamera( camera, box, width, height, fill ),
			fitting: { ...fitting, refits: fitting.refits + 1 }
		};
	}
	return null;
}

/*
================
dollBounds

An actor's bind-pose bounds ([minX, minY, minZ, maxX, maxY, maxZ], the
renderer's characterBounds) in the doll's space: the doll turns the model
by pi about y (portrait.ts previewYaw of yaw 0), so x and z change sign.
================
*/
export function dollBounds( bounds: readonly number[] ): ViewerBounds {
	return { min: [ -bounds[3]!, bounds[1]!, -bounds[5]! ], max: [ -bounds[0]!, bounds[4]!, -bounds[2]! ] };
}

// Models face -z.
export const DOLL_FRONT = [ 0, -1 ] as const;
