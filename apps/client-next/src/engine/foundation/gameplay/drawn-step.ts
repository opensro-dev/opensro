/*
===========================================================================

drawn-step.ts - how far the local player's drawn body may move in a frame

Port-only, not native. Both presenters of the local walk, the interpolated
own walk (own-walk-interpolation.ts, #1444) and the main-thread mover
(runtime/movement/local-mover.ts, #1440), hold every drawn frame to the
same cap: at most DRAWN_SPEED_MARGIN times walking speed times the frame
time, whatever moved the body - a decaying correction, a reconciliation, a
late publication or a frame clock that jumps. What does not fit carries to
the next frame as a visual offset. Unreal lets a mesh offset glide faster
than the walk; here the camera, which follows the drawn body, never surges.
Only a deliberate snap is exempt: a correction farther than
MAX_SMOOTHED_CORRECTION is a teleport, not a glide.

The browser acceptance test (#1443, movement-cadence.test.mjs) measures
the result against a 1.05 bar.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import { hypot3 } from "@/engine/foundation/math/hypot";
import { displaceRegionXZ, sameCoordinateSpace, worldDelta } from "@/engine/foundation/world/world-space";

// The cap on a drawn frame's step, in multiples of walking speed times the
// frame time.
export const DRAWN_SPEED_MARGIN = 1.04;
// A correction farther than this snaps (a teleport, a relocation).
export const MAX_SMOOTHED_CORRECTION = 96;

/*
================
poseDelta

World XZ (and Y) from a to b, or null across coordinate spaces.
================
*/
export function poseDelta( a: Pose, b: Pose ): [number, number, number] | null {
	if ( !sameCoordinateSpace( a.regionId, b.regionId ) ) return null;
	const [dx, dz] = worldDelta( a, b );
	return [ dx, b.y - a.y, dz ];
}

/*
================
offsetPose

pose moved by v (world XZ and Y), across a region border if it falls there.
================
*/
export function offsetPose( pose: Pose, v: readonly [number, number, number] ): Pose {
	if ( v[0] === 0 && v[1] === 0 && v[2] === 0 ) return pose;
	const next = displaceRegionXZ( pose, v[0], v[2] );
	return { ...pose, regionId: next.regionId, x: next.x, y: pose.y + v[1], z: next.z };
}

/*
================
boundDrawnStep

The pose to draw this frame: drawn, or the point toward it from last that
the frame's budget reaches (unitsPerMs * DRAWN_SPEED_MARGIN * frameMs).
frameMs is real frame time on the frame clock, so a jump of a worker-time
origin cannot widen its own budget. Without a last pose, a speed or a frame
time, or across coordinate spaces, drawn is returned as it is.
================
*/
export function boundDrawnStep(
	last: Pose | undefined,
	drawn: Pose,
	unitsPerMs: number | undefined,
	frameMs: number
): Pose {
	if ( !last || !unitsPerMs || !(frameMs > 0) ) return drawn;
	const step = poseDelta( last, drawn );
	if ( !step ) return drawn;
	const length = hypot3( ...step ), budget = unitsPerMs * DRAWN_SPEED_MARGIN * frameMs;
	if ( length <= budget ) return drawn;
	const k = budget / length;
	return { ...offsetPose( last, [ step[0] * k, step[1] * k, step[2] * k ] ), angle: drawn.angle };
}
