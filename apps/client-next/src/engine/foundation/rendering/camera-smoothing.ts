/*
===========================================================================

camera-smoothing.ts - the optional smooth follow camera (port-only, not native)

The native follow camera (CGInterface_UpdateCameraOrbitTarget, 68F830)
copies the drawn body's height into its target every frame and takes the
collision segment's distance as it comes (68FAD0): a small step in either
moves the whole view at once. This owner eases both, for the Experimental
"Smooth camera" row only; with the row off nothing here runs and the
camera is the native one.

- Height: the target's Y follows the native target Y through a critically
  damped spring, so a step of a unit or two settles over about a tenth of
  a second instead of in one frame. Horizontal position stays exact, so
  the view never drifts off the character.
- Distance: an obstruction still pulls the camera in at once (it never
  clips through a wall or the ground), but the release back out eases.
- Discontinuities (a teleport, a region or dungeon change, a mount, a
  frame gap) snap, because easing across them would fly through the
  world.

Pure functions over one plain state record; the renderer owns the record.

===========================================================================
*/

// The height spring's smoothing time in seconds: a step settles to about
// 2% in this long. Short enough that walking up a slope never lags behind.
export const CAMERA_HEIGHT_SMOOTH_SECONDS = 0.12;
// The release spring's smoothing time: an obstruction that clears lets the
// camera glide back out instead of popping.
export const CAMERA_RELEASE_SMOOTH_SECONDS = 0.25;
// A height jump larger than this is a teleport, a fall or a portal, not a
// presentation step: snap.
export const CAMERA_HEIGHT_SNAP_UNITS = 40;
// A frame gap longer than this (a hidden tab, a long stall) snaps instead
// of springing across it.
export const CAMERA_SMOOTH_MAX_STEP_SECONDS = 0.25;

/*
================
CameraSmoothing

The renderer's per-world smoothing record. Null fields have no history and
take the next input as is.
================
*/
export interface CameraSmoothing {
	height: number | null;
	heightVelocity: number;
	distance: number | null;
	distanceVelocity: number;
	// The coordinate space the history belongs to (originRegion, with the
	// dungeon bit) and the mount state: a change of either snaps.
	space: number | null;
	mounted: boolean;
	seconds: number | null;
}

/*
================
createCameraSmoothing
================
*/
export function createCameraSmoothing(): CameraSmoothing {
	return {
		height: null,
		heightVelocity: 0,
		distance: null,
		distanceVelocity: 0,
		space: null,
		mounted: false,
		seconds: null
	};
}

/*
================
resetCameraSmoothing

Drops every history, so the next frame is the native camera exactly.
================
*/
export function resetCameraSmoothing( state: CameraSmoothing ) {
	state.height = null;
	state.heightVelocity = 0;
	state.distance = null;
	state.distanceVelocity = 0;
	state.space = null;
	state.seconds = null;
}

/*
================
smoothDamp

A critically damped spring toward target (Game Programming Gems 4, 1.10):
no overshoot, continuous velocity, frame-rate independent. Returns the new
value and velocity.
================
*/
export function smoothDamp(
	current: number,
	target: number,
	velocity: number,
	smoothSeconds: number,
	dt: number
): [number, number] {
	if ( dt <= 0 ) return [ current, velocity ];
	const omega = 2 / smoothSeconds, x = omega * dt;
	const decay = 1 / (1 + x + 0.48 * x * x + 0.235 * x * x * x);
	const change = current - target;
	const temp = (velocity + omega * change) * dt;
	const nextVelocity = (velocity - omega * temp) * decay;
	let next = target + (change + temp) * decay;
	// Never pass the target: a spring that crosses it would overshoot.
	if ( (target - current > 0) === (next > target) ) return [ target, 0 ];
	return [ next, nextVelocity ];
}

/*
================
beginCameraSmoothingFrame

Advances the clock and drops the history across a discontinuity. Returns
the frame's step in seconds, 0 when this frame snaps.
================
*/
export function beginCameraSmoothingFrame(
	state: CameraSmoothing,
	seconds: number,
	space: number,
	mounted: boolean
): number {
	const dt = state.seconds === null ? 0 : seconds - state.seconds;
	const snap = state.seconds === null || !(dt >= 0) || dt > CAMERA_SMOOTH_MAX_STEP_SECONDS ||
		state.space !== space || state.mounted !== mounted;
	if ( snap ) resetCameraSmoothing( state );
	state.seconds = seconds;
	state.space = space;
	state.mounted = mounted;
	return snap ? 0 : dt;
}

/*
================
smoothCameraHeight

The eased target Y for this frame's native target Y.
================
*/
export function smoothCameraHeight( state: CameraSmoothing, target: number, dt: number ): number {
	if ( !Number.isFinite( target ) ) return target;
	if ( state.height === null || dt === 0 || Math.abs( target - state.height ) > CAMERA_HEIGHT_SNAP_UNITS ) {
		state.height = target;
		state.heightVelocity = 0;
		return target;
	}
	[state.height, state.heightVelocity] = smoothDamp(
		state.height,
		target,
		state.heightVelocity,
		CAMERA_HEIGHT_SMOOTH_SECONDS,
		dt
	);
	return state.height;
}

/*
================
smoothCameraDistance

The eye distance for this frame: the native resolved distance whenever it
is closer (an obstruction pulls in at once), else an eased release toward
it.
================
*/
export function smoothCameraDistance( state: CameraSmoothing, resolved: number, dt: number ): number {
	if ( !Number.isFinite( resolved ) ) return resolved;
	if ( state.distance === null || dt === 0 || resolved <= state.distance ) {
		state.distance = resolved;
		state.distanceVelocity = 0;
		return resolved;
	}
	[state.distance, state.distanceVelocity] = smoothDamp(
		state.distance,
		resolved,
		state.distanceVelocity,
		CAMERA_RELEASE_SMOOTH_SECONDS,
		dt
	);
	return state.distance;
}
