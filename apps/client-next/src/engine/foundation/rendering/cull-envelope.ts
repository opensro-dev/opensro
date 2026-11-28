/*
===========================================================================

cull-envelope.ts - a conservative cull frustum a moving camera stays inside

Port-only, not native (owner 2026-11-28: "dragging camera should have no
cost"). The native client culls terrain ranges against the exact frustum
every frame (A2D1B0). Drawing a superset of what is on screen changes no
pixel - the rasterizer clips what lies outside - so the port culls against
a frustum widened by CULL_MARGIN_RAD on every side, from an eye that may
drift CULL_EYE_SLACK units, and keeps that choice while the live camera
stays inside: any view whose orientation is within the margin of the
envelope's and whose eye is within the slack sees only ranges the
envelope kept. A camera drag then re-culls once every few degrees instead
of every frame.

Why it holds: a rotation by at most m moves every view ray by at most m,
so each side plane of the frustum turns by at most m; widening each
half-angle by m contains it. A point inside the live frustum lies within
|eye - envelope eye| of a point inside the same frustum placed at the
envelope eye, so a sphere grown by the slack covers the drift. Depth: a
point of the live frustum lies at most far * |corner ray| from its eye,
so the envelope's far plane is that, plus the slack.

Only terrain uses it: placed objects advance their fades for what the walk
visits (object-fades.ts), so a wider object set would fade objects in
off screen, which the native client does not do.

===========================================================================
*/
import type { WorldCamera } from "@/engine/contracts/scene";
import { cameraBasis, projectionTangents, viewProjection } from "./world-math";

// The widening of each frustum half-angle: a drag re-culls about once per
// this much turn (35 pixels at the native 0.005 rad per pixel).
export const CULL_MARGIN_RAD = 0.17453292519943295; // 10 degrees
// How far the eye may drift from the envelope's eye, in world units: an
// orbit at the default 80-unit distance moves the eye 14 units per 10
// degrees.
export const CULL_EYE_SLACK = 32;
// The envelope's near plane: closer than any live near plane, so a rotated
// view's nearest points stay inside.
const ENVELOPE_NEAR_SCALE = 1e-3;
// The widest half-angle a widened projection may take (just under 90
// degrees: tan stays finite).
const MAX_HALF_ANGLE = 1.5607963267948965; // pi / 2 - 0.01

export interface CullEnvelope {
	// The widened view-projection to cull with, and the slack every tested
	// sphere grows by.
	readonly matrix: Float32Array;
	readonly slack: number;
	// Changes exactly when the envelope is rebuilt: a cached choice made
	// against an older stamp must be made again.
	readonly stamp: number;
}

interface Held {
	eye: readonly number[];
	basis: ReturnType<typeof cameraBasis>;
	fov: number;
	aspect: number;
	near: number;
	far: number;
	lock: boolean;
}

/*
================
rotationBetween

The angle of the rotation taking one camera basis to another: for two
orthonormal frames, cos = (trace(R0^T R) - 1) / 2.
================
*/
function rotationBetween( a: ReturnType<typeof cameraBasis>, b: ReturnType<typeof cameraBasis> ) {
	const dot = ( u: readonly number[], v: readonly number[] ) => u[0]! * v[0]! + u[1]! * v[1]! + u[2]! * v[2]!;
	const trace = dot( a.right, b.right ) + dot( a.up, b.up ) + dot( a.forward, b.forward );
	return Math.acos( Math.max( -1, Math.min( 1, (trace - 1) / 2 ) ) );
}

/*
================
widenedProjection

The envelope's view-projection from the live camera: each half-angle
grown by the margin, the far plane past every rotated corner.
================
*/
function widenedProjection( camera: WorldCamera, aspect: number, margin: number, slack: number ) {
	const { tanX, tanY } = projectionTangents( camera, aspect );
	const halfX = Math.min( MAX_HALF_ANGLE, Math.atan( tanX ) + margin ),
		halfY = Math.min( MAX_HALF_ANGLE, Math.atan( tanY ) + margin );
	const wideX = Math.tan( halfX ), wideY = Math.tan( halfY );
	const corner = Math.sqrt( 1 + tanX * tanX + tanY * tanY );
	return viewProjection( {
		...camera,
		fov: 2 * halfY,
		lockHorizontalFov: false,
		near: camera.near * ENVELOPE_NEAR_SCALE,
		far: camera.far * corner + slack
	}, wideX / wideY );
}

/*
================
createCullEnvelope

envelope( camera, aspect ) returns the envelope covering this view,
rebuilding it around the live camera when the view left the last one.
invalidate() forces the next call to rebuild.
================
*/
export function createCullEnvelope( margin = CULL_MARGIN_RAD, slack = CULL_EYE_SLACK ) {
	let held: Held | null = null, current: CullEnvelope | null = null, stamp = 0;
	const inside = ( camera: WorldCamera, aspect: number, basis: ReturnType<typeof cameraBasis> ) => {
		if ( !held ) return false;
		if (
			held.fov !== camera.fov || held.aspect !== aspect || held.near !== camera.near || held.far !== camera.far ||
			held.lock !== !!camera.lockHorizontalFov
		) return false;
		const dx = camera.eye[0] - held.eye[0]!, dy = camera.eye[1] - held.eye[1]!, dz = camera.eye[2] - held.eye[2]!;
		if ( dx * dx + dy * dy + dz * dz > slack * slack ) return false;
		return rotationBetween( held.basis, basis ) <= margin;
	};
	return {
		envelope( camera: WorldCamera, aspect: number ): CullEnvelope {
			const basis = cameraBasis( camera );
			if ( current && inside( camera, aspect, basis ) ) return current;
			held = {
				eye: [ ...camera.eye ],
				basis,
				fov: camera.fov,
				aspect,
				near: camera.near,
				far: camera.far,
				lock: !!camera.lockHorizontalFov
			};
			current = { matrix: widenedProjection( camera, aspect, margin, slack ), slack, stamp: ++stamp };
			return current;
		},
		invalidate() {
			held = null;
			current = null;
		}
	};
}
