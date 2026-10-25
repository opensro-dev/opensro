/*
===========================================================================

pose-presentation.ts - the rendered pose of every character, camera included

The simulation worker owns logical poses; this module owns what is drawn.

A character whose samples carry the simulation time they were taken at (the
local player, and every entity walking a path) is drawn on the frame clock:
the pose is extrapolated along the velocity of its last two samples to the
frame's own time, never past the end of the leg. Busy frames that deliver
samples late or in bursts therefore no longer slow a walk down and then
fast-forward it. Characters without sample times bridge the observed
delivery interval between samples instead.

The worker's admitted leg duration supplies horizontal velocity until the
second sample arrives. Coalescing a click and its receipt must not invent a
stationary frame while waiting to rediscover velocity from two positions.

Small jumps between consecutive models of a sampled character (a server
correction when a cast or pickup stops the player, a leg turn, a monster's
halt) become a bounded critically damped correction, so the
body and the camera glide instead of snapping. This smoothing is
presentation only and a deliberate deviation from the original client,
which snaps; logical poses stay authoritative.

===========================================================================
*/
import type { Pose, MovementTransition } from "@/engine/contracts/gameplay";
import type { SampleInput } from "@/engine/contracts/pose-presentation";
export type { SampleInput } from "@/engine/contracts/pose-presentation";
import { SIMULATION_STEP_MS } from "@/engine/contracts/simulation";
import { interpolateMovement, poseDistance } from "@/engine/foundation/gameplay/native-movement";
import { hypot2, hypot3 } from "@/engine/foundation/math/hypot";
import { displaceRegionXZ, sameCoordinateSpace, worldDelta } from "@/engine/foundation/world/world-space";
import { createOwnWalkPresenter } from "./own-walk-interpolation";

const TICK_SECONDS = SIMULATION_STEP_MS / 1000;
const DISCONTINUITY_DISTANCE = 192;
const MAX_SAMPLE_GAP_SECONDS = 0.25;
// Past this frame gap the interval was never drawn (a hidden or frozen
// tab, not a visible hitch): snap to the authoritative pose on return
// instead of gliding the unseen travel. Visible stalls stay below it and
// keep the tested recovery above.
const MAX_UNSEEN_GAP_SECONDS = 2;
const MAX_EXTRAPOLATION_SECONDS = 0.1;
const CORRECTION_FREQUENCY = 30;
const MAX_RECOVERY_STEP_SECONDS = 0.033;
const MIN_CORRECTION_DISTANCE = 0.01;
const MAX_CORRECTION_DISTANCE = 96;
const MAX_RECOVERY_PATHS = 4;
const MAX_WALK_RECOVERY_SPEED_FACTOR = 1.5;

/*
================
Track

A character without sample times: delivery-interval interpolation.
================
*/
interface Track {
	target: Pose;
	from: Pose;
	at: number;
	duration: number;
	last: number;
	angle: number;
	moving: boolean;
	settled?: Pose;
	displayed?: Pose;
}

/*
================
Sample

A logical pose, the frame-clock time (seconds) it was sampled at, and the
movement revision of the walk it was sampled from.
================
*/
interface Sample {
	readonly pose: Pose;
	readonly at: number;
	readonly workerAtMs: number;
	readonly revision: number;
}

/*
================
SampleTrack

A character drawn on the frame clock from its timed samples.
================
*/
interface SampleTrack {
	// A drawn pose handed back by its owner (carry): recovered toward the
	// model at walking speed instead of in one frame.
	carried?: boolean;
	paths: { from: Pose; to: Pose; }[];
	walkingPath?: readonly Pose[];
	previous?: Sample;
	latest: Sample;
	clockOffset: number;
	moving: boolean;
	to?: Pose;
	pathVelocity?: readonly [number, number];
	recoverySpeed: number;
	durationMs?: number;
	displacementAtMs?: number;
	offset: [number, number, number];
	velocity: [number, number, number];
	displayed: Pose;
	relocation: number;
	last: number;
	angle: number;
}

/*
================
recover

Exact critically damped integration. Project velocity into the remaining
correction so a new opposite receipt cannot send the body past either end.
================
*/
function recover( row: SampleTrack, seconds: number ) {
	const dt = Math.min( MAX_RECOVERY_STEP_SECONDS, seconds );
	const decay = Math.exp( -CORRECTION_FREQUENCY * dt );
	for ( let axis = 0; axis < 3; axis++ ) {
		const x = row.offset[axis]!;
		const v = x === 0 ?
			0 :
			Math.sign( x ) *
			Math.max( -CORRECTION_FREQUENCY * Math.abs( x ), Math.min( 0, Math.sign( x ) * row.velocity[axis]! ) );
		const c = v + CORRECTION_FREQUENCY * x;
		row.offset[axis] = (x + c * dt) * decay;
		row.velocity[axis] = (v - CORRECTION_FREQUENCY * c * dt) * decay;
	}
	if ( hypot3( ...row.offset ) < MIN_CORRECTION_DISTANCE ) {
		row.offset = [ 0, 0, 0 ];
		row.velocity = [ 0, 0, 0 ];
	}
}

/*
================
onCorridor

Navigation owns the chord; presentation can only move along it. A walk's
navigation proof is planar; corrections require the complete 3D chord.
================
*/
function onCorridor( pose: Pose, from: Pose, to: Pose, walking = false ) {
	const span = worldVector( to, from ), point = worldVector( pose, from );
	if ( !span || !point ) return false;
	if ( walking ) span[1] = point[1] = 0;
	const length2 = span[0] ** 2 + span[1] ** 2 + span[2] ** 2;
	const t = length2 ?
		Math.max( 0, Math.min( 1, (point[0] * span[0] + point[1] * span[1] + point[2] * span[2]) / length2 ) ) :
		0;
	return hypot3( point[0] - span[0] * t, point[1] - span[1] * t, point[2] - span[2] * t ) <= MIN_CORRECTION_DISTANCE;
}

/*
================
walkingLocation

Locate a pose on the navigation owner's connected sample chain.
================
*/
function walkingLocation( pose: Pose, path: readonly Pose[], latest = false ) {
	let distance = 0;
	let best: { distance: number; error: number; pose: Pose; edge: number; fraction: number; } | null = null;
	for ( let i = 1; i < path.length; i++ ) {
		const from = path[i - 1]!, to = path[i]!;
		const span = worldVector( to, from ), point = worldVector( pose, from );
		if ( !span || !point ) return null;
		const planar2 = span[0] ** 2 + span[2] ** 2;
		const fraction = planar2 ?
			Math.max( 0, Math.min( 1, (point[0] * span[0] + point[2] * span[2]) / planar2 ) ) :
			span[1] ?
			Math.max( 0, Math.min( 1, point[1] / span[1] ) ) :
			0;
		const length = hypot3( ...span );
		const miss = hypot2( point[0] - span[0] * fraction, point[2] - span[2] * fraction );
		if ( miss <= MIN_CORRECTION_DISTANCE ) {
			const error = miss + Math.abs( point[1] - span[1] * fraction );
			if (
				!best || (latest && error <= MIN_CORRECTION_DISTANCE && best.error <= MIN_CORRECTION_DISTANCE) ||
				error < best.error
			) {
				best = {
					edge: i,
					fraction,
					distance: distance + length * fraction,
					error,
					pose: interpolateMovement( from, to, fraction )
				};
			}
		}
		distance += length;
	}
	return best;
}

/*
================
nearestWalkingPose

The certified path's point nearest a pose in the plane.
================
*/
function nearestWalkingPose( pose: Pose, path: readonly Pose[] ): Pose | null {
	let best: { miss: number; pose: Pose; } | null = null;
	for ( let i = 1; i < path.length; i++ ) {
		const from = path[i - 1]!, to = path[i]!;
		const span = worldVector( to, from ), point = worldVector( pose, from );
		if ( !span || !point ) return null;
		const planar2 = span[0] ** 2 + span[2] ** 2;
		const fraction = planar2 ?
			Math.max( 0, Math.min( 1, (point[0] * span[0] + point[2] * span[2]) / planar2 ) ) :
			0;
		const miss = hypot2( point[0] - span[0] * fraction, point[2] - span[2] * fraction );
		if ( !best || miss < best.miss ) best = { miss, pose: interpolateMovement( from, to, fraction ) };
	}
	return best?.pose ?? null;
}

/*
================
walkingPose

Spend distance along certified edges.
================
*/
function walkingPose( path: readonly Pose[], distance: number ): Pose {
	for ( let i = 1; i < path.length; i++ ) {
		const from = path[i - 1]!, to = path[i]!;
		const length = hypot3( ...worldVector( to, from )! );
		if ( distance <= length ) return interpolateMovement( from, to, length ? distance / length : 0 );
		distance -= length;
	}
	return path[path.length - 1]!;
}

/*
================
recoverWalking

Spend damped recovery along the worker's certified sample chain.
================
*/
function recoverWalking(
	before: Pose,
	model: Pose,
	candidate: Pose,
	walk: { path: readonly Pose[]; budget: number; }
) {
	const { path } = walk;
	const start = walkingLocation( before, path, true ), end = walkingLocation( model, path, true );
	if ( !start || !end || start.error > MIN_CORRECTION_DISTANCE || end.error > MIN_CORRECTION_DISTANCE ) return null;
	const delta = worldVector( candidate, before );
	if ( !delta ) return null;
	const remaining = end.distance - start.distance;
	const direction = Math.sign( remaining );
	let travel = Math.min( Math.abs( remaining ), hypot3( ...delta ) );
	let distance = start.distance, budget = walk.budget, fraction = start.fraction;
	if ( travel === 0 ) return before;
	for ( let edge = start.edge; edge > 0 && edge < path.length && travel > 0; edge += direction ) {
		const span = worldVector( path[edge]!, path[edge - 1]! )!;
		const length = hypot3( ...span ), planar = hypot2( span[0], span[2] );
		const available = length * (direction > 0 ? 1 - fraction : fraction);
		const step = Math.min( available, travel, planar ? budget * length / planar : Infinity );
		distance += direction * step;
		travel -= step;
		if ( length ) budget = Math.max( 0, budget - step * planar / length );
		if ( step < available ) break;
		fraction = direction > 0 ? 0 : 1;
	}
	return walkingPose( path, distance );
}

/*
================
turn

Native 86CBA0: shortest arc, 3*pi radians/second.
================
*/
function turn( from: number, to: number, seconds: number ) {
	const delta = ((to - from + 98304) % 65536) - 32768, limit = seconds * 98304;
	return (from + Math.max( -limit, Math.min( limit, delta ) ) + 65536) % 65536;
}

/*
================
recoverTurn

Spend correction travel along the two admitted legs of a turn.
================
*/
function recoverTurn( before: Pose, model: Pose, candidate: Pose, path: NonNullable<MovementTransition["turn"]> ) {
	if ( !onCorridor( model, path.outgoing.from, path.outgoing.to, true ) ) return null;
	const delta = worldVector( candidate, before );
	if ( !delta ) return null;
	const distance = hypot3( ...delta );
	if ( onCorridor( before, path.outgoing.from, path.outgoing.to, true ) ) {
		const remaining = worldVector( model, before );
		if ( !remaining ) return null;
		const length = hypot3( ...remaining );
		return length ? interpolateMovement( before, model, Math.min( 1, distance / length ) ) : model;
	}
	if ( !onCorridor( before, path.incoming.from, path.incoming.to, true ) ) return null;
	const corner = path.outgoing.from;
	const first = worldVector( corner, before ), second = worldVector( model, corner );
	if ( !first || !second ) return null;
	const toCorner = hypot3( ...first ), toModel = hypot3( ...second );
	if ( distance < toCorner ) return interpolateMovement( before, corner, distance / toCorner );
	if ( !toModel ) return model;
	return interpolateMovement( corner, model, Math.min( 1, (distance - toCorner) / toModel ) );
}

/*
================
position
================
*/
function position( row: Track, now: number ) {
	return interpolateMovement( row.from, row.target, Math.max( 0, Math.min( 1, (now - row.at) / row.duration ) ) );
}

/*
================
discontinuity
================
*/
function discontinuity( a: Pose, b: Pose ) {
	return !sameCoordinateSpace( a.regionId, b.regionId ) ||
		poseDistance( a, b ) > DISCONTINUITY_DISTANCE;
}

/*
================
worldVector
================
*/
function worldVector( a: Pose, b: Pose ): [number, number, number] | null {
	if ( !sameCoordinateSpace( a.regionId, b.regionId ) ) {
		return null;
	}
	const [dx, dz] = worldDelta( b, a );
	return [ dx, a.y - b.y, dz ];
}

/*
================
displace
================
*/
function displace( pose: Pose, v: readonly [number, number, number] ): Pose {
	if ( v[0] === 0 && v[1] === 0 && v[2] === 0 ) return pose;
	const next = displaceRegionXZ( pose, v[0], v[2] );
	return { ...pose, regionId: next.regionId, x: next.x, y: pose.y + v[1], z: next.z };
}

/*
================
sampledModel

The pose at frame time now: the latest sample, advanced along the velocity
of the last two samples. Walking is piecewise linear at constant speed, so
within a leg this is exact; the leg end bounds it.
================
*/
function sampledModel( row: SampleTrack, now: number, confirmedAt = row.latest.at ): Pose {
	const candidate = sampledLinearModel( row, now, confirmedAt );
	if ( !row.walkingPath || candidate === row.latest.pose ) return candidate;
	// A candidate off the certified chain (one that ran past its end) takes
	// the chain's nearest point, never the stale sample: falling back to the
	// latest sample drew a step back in X, Z and height.
	return walkingLocation( candidate, row.walkingPath, true )?.pose ??
		nearestWalkingPose( candidate, row.walkingPath ) ?? row.latest.pose;
}

/*
================
sampledLinearModel

Native time and the checked endpoint bound horizontal extrapolation. The
wrapper resolves its height on certified walking samples when available.

Every track extrapolates at most MAX_EXTRAPOLATION_SECONDS past its newest
sample: the local mover's own legs are drawn by the interpolated presenter
(own-walk-interpolation.ts), never by this dead reckoning, since #1564
deleted the old fallback that advanced an own leg without bound.
================
*/
function sampledLinearModel( row: SampleTrack, now: number, confirmedAt: number ): Pose {
	const latest = row.latest, previous = row.previous;
	const maximumAhead = MAX_EXTRAPOLATION_SECONDS + Math.max( 0, confirmedAt - latest.at );
	if ( !row.moving ) return latest.pose;
	if ( row.pathVelocity && !previous ) {
		const [vx, vz] = row.pathVelocity, speed = hypot2( vx, vz );
		if ( !speed ) return latest.pose;
		let ahead = Math.min( maximumAhead, Math.max( 0, now - latest.at ) );
		if ( row.to ) {
			const rest = worldVector( row.to, latest.pose );
			if ( !rest || rest[0] * vx + rest[2] * vz <= 0 ) return latest.pose;
			ahead = Math.min( ahead, hypot2( rest[0], rest[2] ) / speed );
		}
		// Only sampled ground heights define a vertical tangent. A distant
		// leg endpoint cannot predict the terrain between its two ends.
		return ahead ? displace( latest.pose, [ vx * ahead, 0, vz * ahead ] ) : latest.pose;
	}
	if ( !previous ) return latest.pose;
	const gap = latest.at - previous.at;
	if ( gap <= 0 || gap > MAX_SAMPLE_GAP_SECONDS ) return latest.pose;
	const span = worldVector( latest.pose, previous.pose );
	if ( !span ) return latest.pose;
	const stepped = hypot2( span[0], span[2] );
	if ( stepped === 0 ) return latest.pose;
	let ahead = Math.min( maximumAhead, Math.max( 0, now - latest.at ) ) / gap;
	if ( row.to ) {
		const rest = worldVector( row.to, latest.pose );
		if ( !rest || rest[0] * span[0] + rest[2] * span[2] <= 0 ) return latest.pose;
		ahead = Math.min( ahead, hypot2( rest[0], rest[2] ) / stepped );
	}
	if ( ahead === 0 ) return latest.pose;
	return {
		...displace( latest.pose, [ span[0] * ahead, span[1] * ahead, span[2] * ahead ] ),
		angle: latest.pose.angle
	};
}

/*
================
pathVelocity

A coalesced click and receipt may be the first published sample of a walk.
Its admitted leg already defines horizontal velocity; waiting for a second
publication invents a pause and a later catch-up. Infinite duration is the
native zero-speed hold, so its velocity is zero.
================
*/
function pathVelocity( input: SampleInput ): readonly [number, number] | undefined {
	if ( !input.from || !input.to || !(input.durationMs! > 0) ) return undefined;
	const span = worldVector( input.to, input.from );
	if ( !span ) return undefined;
	return [ span[0] * 1000 / input.durationMs!, span[2] * 1000 / input.durationMs! ];
}

/*
================
displacementAnchor

The fixed skill clock survives coalesced worker publications. Only the XZ
result enters the timing correction; sampled terrain still owns height.
================
*/
function displacementAnchor( input: SampleInput, atMs: number ): Pose | undefined {
	if (
		!input.displacement || !input.from || !input.to || !Number.isFinite( input.startedAtMs ) ||
		!(input.durationMs! > 0) || !Number.isFinite( input.durationMs )
	) return undefined;
	const phase = Math.max( 0, Math.min( 1, (atMs - input.startedAtMs!) / input.durationMs! ) );
	return interpolateMovement( input.from, input.to, phase );
}

/*
================
OwnMoverFrame

The main-thread mover's state for a frame (runtime/movement/local-mover.ts).
================
*/
export type OwnMoverFrame =
	| { readonly kind: "idle"; }
	| { readonly kind: "walking" | "ended"; readonly pose: Pose; };

/*
================
createPosePresentation

The worker journal is backpressured and can deliver several fixed steps in
one batch. Never extrapolate beyond admitted navigation; the camera shares
this sample.
================
*/
export function createPosePresentation() {
	const rows = new Map<number, Track>(), tracks = new Map<number, SampleTrack>();
	let inputs: ReadonlyMap<number, SampleInput> = new Map();
	// Frame-clock milliseconds of simulation time zero (ClockSample.originMs).
	let originMs: number | null = null;
	// Frame-clock seconds of the last pose served; the return frame after a
	// long unseen gap snaps instead of recovering (MAX_UNSEEN_GAP_SECONDS).
	let lastPoseAt: number | undefined;
	// Port-only (own-walk-interpolation.ts): the local mover's walk drawn
	// between its worker ticks instead of extrapolated past the newest one.
	const ownWalks = createOwnWalkPresenter( turn );
	// Port-only (#1440): the main-thread mover's frame, set each frame before
	// poses are asked, for the local player and, while it rides, its mount
	// (0x85E000: the mount is the mover, the rider sits on it; #1566); the
	// characters it already handed back this frame; and the pose last shown
	// per character with the frame clock it was shown at.
	let ownMover: { gids: readonly number[]; frame: OwnMoverFrame; carried: Set<number>; } | null = null;
	const shown = new Map<number, { pose: Pose; at: number; }>();

	/*
	================
	sampledPose
	================
	*/
	function sampledPose( gid: number, input: SampleInput, target: Pose, now: number ): Pose {
		const walkingPath = input.displacement ? undefined : input.walkingPath ?? input.transition?.walkingPath;
		const rawAt = (originMs! + input.atMs) / 1000;
		let row = tracks.get( gid );
		const previousTrack = row;
		const untimed = !row ? rows.get( gid ) : undefined;
		const relocation = input.transition?.relocation ?? 0;
		const revisionChanged = row && input.revision !== row.latest.revision;
		const stalled = row && now - row.last > MAX_SAMPLE_GAP_SECONDS;
		const previousFrameAt = row?.last;
		const freshWorkerSample = !row || input.atMs !== row.latest.workerAtMs ||
			input.revision !== row.latest.revision;
		let clockOffset = !row || stalled || revisionChanged && input.transition?.eligible === false ?
			0 :
			row.clockOffset;
		if ( freshWorkerSample && input.moving && !stalled ) {
			if ( now - (rawAt + clockOffset) > 0.095 ) {
				const nextOffset = Math.max( 0, now - rawAt - 0.016 );
				const deltaOffset = nextOffset - clockOffset;
				clockOffset = nextOffset;
				if ( row && deltaOffset ) {
					row.latest = { ...row.latest, at: row.latest.at + deltaOffset };
					if ( row.previous ) {
						row.previous = { ...row.previous, at: row.previous.at + deltaOffset };
					}
				}
			} else if ( clockOffset > 0 && rawAt + clockOffset > now ) {
				const nextOffset = Math.max( 0, now - rawAt );
				const deltaOffset = nextOffset - clockOffset;
				clockOffset = nextOffset;
				if ( row && deltaOffset ) {
					row.latest = { ...row.latest, at: row.latest.at + deltaOffset };
					if ( row.previous ) {
						row.previous = { ...row.previous, at: row.previous.at + deltaOffset };
					}
				}
			}
		} else if ( !input.moving ) {
			clockOffset = 0;
		}
		const at = rawAt + clockOffset;
		let carriedDisplay: Pose | undefined;
		if (
			!row || now < row.last || row.relocation !== relocation ||
			(revisionChanged && input.transition?.eligible === false) ||
			(input.transition || walkingPath ?
				!worldVector( row.latest.pose, target ) :
				discontinuity( row.latest.pose, target ))
		) {
			row = {
				paths: [],
				walkingPath: input.displacement ? undefined : input.walkingPath ?? input.transition?.walkingPath,
				latest: { pose: { ...target }, at, workerAtMs: input.atMs, revision: input.revision },
				clockOffset,
				moving: input.moving,
				to: input.to,
				pathVelocity: pathVelocity( input ),
				recoverySpeed: 0,
				durationMs: input.durationMs,
				displacementAtMs: input.displacement ? input.startedAtMs : undefined,
				offset: [ 0, 0, 0 ],
				velocity: [ 0, 0, 0 ],
				displayed: { ...target },
				relocation,
				last: now,
				angle: target.angle
			};
			row.recoverySpeed = !input.displacement && row.pathVelocity ? hypot2( ...row.pathVelocity ) : 0;
			if (
				untimed && input.moving && input.transition?.eligible !== false &&
				!discontinuity( untimed.target, target )
			) {
				// An actor can be idle without a simulation timestamp. Its first
				// dash must still respect the last frame already shown, rather
				// than spending unseen worker time as an instantaneous step.
				const switchAt = Math.max(
					untimed.last,
					Math.min( now, (originMs! + (input.startedAtMs ?? input.atMs)) / 1000 )
				);
				const anchor = displacementAnchor( input, switchAt * 1000 - originMs! );
				if ( anchor ) {
					const before = position( untimed, switchAt );
					const offset = worldVector( before, anchor );
					if ( offset && hypot3( ...offset ) <= MAX_CORRECTION_DISTANCE ) {
						row.offset = [ offset[0], before.y - target.y, offset[2] ];
					}
				}
			}
			tracks.set( gid, row );
			rows.delete( gid );
		} else if ( now !== row.last ) {
			if ( !revisionChanged ) recover( row, now - row.last );
			else if ( !stalled ) {
				// Elapsed display time belongs to the old correction. Advancing
				// after retargeting would apply the new receipt's force before
				// it arrived, especially after a small correction and a long lag.
				const [x, y, z] = row.offset;
				recover( row, now - row.last );
				carriedDisplay = row.moving ?
					displace( row.displayed, [ row.offset[0] - x, row.offset[1] - y, row.offset[2] - z ] ) :
					displace( sampledModel( row, now ), row.offset );
				const corridor = input.transition?.corridor;
				// A new corridor can exclude the old trajectory. Keep its valid
				// displayed origin rather than turn rejected carry into a snap.
				if (
					!input.displacement && !walkingPath && corridor &&
					onCorridor( row.displayed, corridor.from, corridor.to ) &&
					!onCorridor( carriedDisplay, corridor.from, corridor.to )
				) {
					carriedDisplay = row.displayed;
				}
			}
			if ( stalled ) row.previous = undefined;
			row.angle = turn( row.angle, target.angle, now - row.last );
			row.last = now;
		}
		const latest = row.latest.pose;
		const changed = at !== row.latest.at || input.revision !== row.latest.revision ||
			latest.regionId !== target.regionId || latest.x !== target.x || latest.y !== target.y ||
			latest.z !== target.z;
		const timingChanged = row.durationMs !== input.durationMs;
		const displacementAtMs = input.displacement ? input.startedAtMs : undefined;
		const displacementChanged = row.displacementAtMs !== displacementAtMs;
		if (
			changed || stalled || timingChanged || displacementChanged || row.moving !== input.moving ||
			row.to !== input.to
		) {
			// A receipt which kept the logical walk did not move its anchor.
			// Advance the old model to this frame before replacing it; parking at
			// the preceding display creates a correction that never happened.
			const retainedWalk = row.moving && input.moving && input.transition?.reason === "receipt" &&
				input.transition.logicalDistance === 0 && input.transition.pathEligible === true;
			// A stalled frame holds the display: the local mover's own legs
			// are the interpolated presenter's business (own-walk-
			// interpolation.ts), and every other track parks at its bound.
			const preserveDisplay = stalled ||
				revisionChanged && input.transition?.reason !== "input" && !retainedWalk;
			// A fresh publication after a main-frame gap confirms the intervening
			// leg. Do not manufacture a parked interval inside that unseen gap.
			// If an earlier displayed frame already reached the prediction bound,
			// keep normal recovery: a worker stall really was visible to the user.
			const continuing = !stalled && !revisionChanged && at > row.latest.at &&
				previousFrameAt !== undefined && previousFrameAt - row.latest.at <= MAX_EXTRAPOLATION_SECONDS;
			const before = preserveDisplay ?
				carriedDisplay ?? row.displayed :
				displace( sampledModel( row, now, continuing ? at : undefined ), row.offset );
			// Native 8DD550 advances at skill speed from the skill's own start.
			// Retiming the old walk at render time invents lag and a later burst.
			// Reconcile at the actual switch, bounded by the last frame already
			// shown: late publications cannot retroactively redraw that frame.
			let anchor: Pose | undefined, beforeSwitch: Pose | undefined;
			if ( displacementChanged && !stalled && input.moving ) {
				const switchAt = Math.max(
					previousFrameAt ?? now,
					Math.min( now, (originMs! + (displacementAtMs ?? input.atMs)) / 1000 )
				);
				anchor = displacementAnchor( input, switchAt * 1000 - originMs! );
				if ( anchor ) {
					beforeSwitch = displace( sampledModel( row, switchAt, continuing ? at : undefined ), row.offset );
				}
			}
			if ( changed ) {
				// Keep the previous sample only when this one is strictly newer
				// and on the same walk; a correction at the same time, or any
				// re-anchor, replaces the model outright and its jump becomes
				// the decaying offset below.
				row.previous = !stalled && at > row.latest.at && input.revision === row.latest.revision ?
					row.latest :
					undefined;
				row.latest = { pose: { ...target }, at, workerAtMs: input.atMs, revision: input.revision };
			}
			row.clockOffset = clockOffset;
			row.moving = input.moving;
			row.to = input.to;
			row.walkingPath = input.displacement ? undefined : input.walkingPath ?? input.transition?.walkingPath;
			// Samples from distinct skill legs cannot define one velocity, even
			// when their duration and entity revision happen to match.
			if ( timingChanged || displacementChanged ) row.previous = undefined;
			row.durationMs = input.durationMs;
			row.displacementAtMs = displacementAtMs;
			row.pathVelocity = pathVelocity( input );
			// A stop may omit its leg, but outstanding display recovery still
			// belongs to the speed of the admitted walk that reached it.
			const walkSpeed = !input.displacement && row.pathVelocity ? hypot2( ...row.pathVelocity ) : 0;
			if ( input.displacement ) row.recoverySpeed = 0;
			else if ( walkSpeed > 0 ) row.recoverySpeed = walkSpeed;
			const jump = worldVector( before, sampledModel( row, now ) );
			if ( jump && beforeSwitch && anchor ) {
				const clockJump = worldVector( beforeSwitch, anchor );
				if ( clockJump ) {
					jump[0] = clockJump[0];
					jump[2] = clockJump[2];
				}
			}
			if ( jump ) row.offset = jump;
			if ( !jump || !input.transition && !walkingPath && hypot3( ...row.offset ) > MAX_CORRECTION_DISTANCE ) {
				row.offset = [ 0, 0, 0 ];
				row.velocity = [ 0, 0, 0 ];
			}
		}
		const model = sampledModel( row, now );
		if ( revisionChanged && input.transition?.previousPath ) {
			row.paths.push( input.transition.previousPath );
		}
		if ( input.from && input.to ) {
			const path = row.paths[row.paths.length - 1];
			if (
				!path || poseDistance( path.from, input.from ) > MIN_CORRECTION_DISTANCE ||
				poseDistance( path.to, input.to ) > MIN_CORRECTION_DISTANCE
			) {
				row.paths.push( { from: input.from, to: input.to } );
			}
		}
		if ( row.paths.length > MAX_RECOVERY_PATHS ) row.paths.splice( 0, row.paths.length - MAX_RECOVERY_PATHS );
		let drawn = displace( model, row.offset );
		const recoveryBudget = row === previousTrack && !input.displacement && walkingPath && row.recoverySpeed > 0 &&
				previousFrameAt !== undefined ?
			MAX_WALK_RECOVERY_SPEED_FACTOR * row.recoverySpeed * Math.max( 0, now - previousFrameAt ) :
			Infinity;
		const corridor = input.transition?.corridor;
		// Receipt adoption also happens twice at one display timestamp. The
		// old velocity must fit the new corridor even when no carry was spent.
		if (
			revisionChanged && !input.displacement && !walkingPath && corridor &&
			onCorridor( row.displayed, corridor.from, corridor.to ) &&
			onCorridor( model, corridor.from, corridor.to )
		) {
			const span = worldVector( corridor.to, corridor.from )!;
			const length2 = span[0] ** 2 + span[1] ** 2 + span[2] ** 2;
			const along = length2 ?
				(row.velocity[0] * span[0] + row.velocity[1] * span[1] + row.velocity[2] * span[2]) / length2 :
				0;
			row.velocity = [ span[0] * along, span[1] * along, span[2] * along ];
		}
		const walked = walkingPath && hypot3( ...row.offset ) > 0 ?
			recoverWalking( row.displayed, model, drawn, { path: walkingPath, budget: recoveryBudget } ) :
			null;
		if ( walked ) {
			drawn = walked;
			row.offset = worldVector( drawn, model )!;
		}
		if (
			(input.transition || walkingPath) && !walked && hypot3( ...row.offset ) > 0 &&
			!(corridor && onCorridor( drawn, corridor.from, corridor.to ) &&
				onCorridor( model, corridor.from, corridor.to )) &&
			!(!walkingPath && row.paths.some( path =>
				onCorridor( drawn, path.from, path.to, true ) &&
				onCorridor( model, path.from, path.to, true )
			))
		) {
			const recovered = !walkingPath && input.transition?.turn ?
				recoverTurn( row.displayed, model, drawn, input.transition.turn ) :
				null;
			if ( recovered ) {
				drawn = recovered;
				row.offset = worldVector( drawn, model )!;
			} else {
				row.offset = [ 0, 0, 0 ];
				row.velocity = [ 0, 0, 0 ];
				drawn = model;
			}
		}
		if (
			input.displacement && row === previousTrack && !displacementChanged && !revisionChanged && !stalled &&
			previousFrameAt !== undefined && now > previousFrameAt && row.pathVelocity && input.from && input.to &&
			onCorridor( row.displayed, input.from, input.to, true ) && onCorridor( model, input.from, input.to, true )
		) {
			// 8DD550 spends skill speed times frame delta. A late first sample
			// cannot repay its phase lag on top of that speed. Carry the lag
			// along this admitted leg, then finish at the same speed at arrival.
			const delta = worldVector( model, row.displayed )!;
			const distance = hypot2( delta[0], delta[2] );
			if ( distance <= MAX_CORRECTION_DISTANCE ) {
				const budget = hypot2( ...row.pathVelocity ) * (now - previousFrameAt);
				const fraction = distance ? Math.min( 1, budget / distance ) : 0;
				const advanced = displace( row.displayed, [ delta[0] * fraction, 0, delta[2] * fraction ] );
				drawn = { ...drawn, regionId: advanced.regionId, x: advanced.x, z: advanced.z };
				row.offset = worldVector( drawn, model )!;
			}
		}
		if ( row.carried ) {
			// A carried pose (a released WAIT hold, carry) walks to the model at
			// MAX_WALK_RECOVERY_SPEED_FACTOR times the walk's speed, height
			// included. Released while standing (a correction or a receipt, no
			// walk to pace it), it decays at CORRECTION_FREQUENCY as any
			// correction does. A teleport-sized gap is not recovered.
			const delta = worldVector( model, row.displayed );
			const distance = delta ? hypot3( ...delta ) : 0;
			// Only a walk paces it: a standing track keeps its last walk's
			// recovery speed, which would glide a stop at walking pace.
			const speed = row.moving ?
				row.recoverySpeed || (row.pathVelocity ? hypot2( ...row.pathVelocity ) : 0) :
				0;
			if ( !delta || distance > MAX_CORRECTION_DISTANCE ) {
				row.carried = undefined;
			} else {
				const elapsed = previousFrameAt === undefined ? 0 : Math.max( 0, now - previousFrameAt );
				const fraction = distance <= MIN_CORRECTION_DISTANCE ?
					1 :
					speed ?
					Math.min( 1, MAX_WALK_RECOVERY_SPEED_FACTOR * speed * elapsed / distance ) :
					1 - Math.exp( -CORRECTION_FREQUENCY * Math.min( MAX_RECOVERY_STEP_SECONDS, elapsed ) );
				drawn = {
					...displace( row.displayed, [ delta[0] * fraction, delta[1] * fraction, delta[2] * fraction ] ),
					angle: drawn.angle
				};
				row.offset = worldVector( drawn, model ) ?? [ 0, 0, 0 ];
				if ( fraction >= 1 ) {
					row.carried = undefined;
					row.offset = [ 0, 0, 0 ];
				}
			}
		}
		// Retain the path behind a rebased receipt only while it still carries
		// visible recovery. This cannot grow with a long session or cut a turn.
		if ( hypot3( ...row.offset ) === 0 && row.paths.length > 1 ) row.paths.splice( 0, row.paths.length - 1 );
		// Pose carries a native heading word. Keep sub-word precision internally,
		// but do not pass fractional words to the model's strict angle decoder.
		row.displayed = { ...drawn, angle: Math.round( row.angle ) % 65536 };
		return { ...row.displayed };
	}

	/*
	================
	presentedPose

	The presenter's own pose for a character this frame.
	================
	*/
	function presentedPose( gid: number, target: Pose, now: number, settledTranslation: boolean ): Pose {
		if ( lastPoseAt !== undefined && now - lastPoseAt > MAX_UNSEEN_GAP_SECONDS ) {
			tracks.clear();
			ownWalks.clear();
			rows.clear();
		}
		if ( lastPoseAt === undefined || now > lastPoseAt ) lastPoseAt = now;
		const input = inputs.get( gid );
		if ( input && originMs !== null && !settledTranslation ) {
			if ( ownWalks.handles( input ) ) {
				// A generic track still on file was drawn last frame: hand it over.
				const handoff = tracks.get( gid )?.displayed;
				tracks.delete( gid );
				return ownWalks.pose( gid, input, target, { now, originMs, handoff } );
			}
			// Leaving interpolation (a skill displacement): this frame keeps
			// the last interpolated pose and the generic track recovers from
			// it on the next (carry).
			const last = ownWalks.take( gid );
			const pose = sampledPose( gid, input, target, now );
			const track = tracks.get( gid );
			if ( !last || !track ) return pose;
			track.displayed = { ...last };
			track.carried = true;
			return { ...last };
		}
		ownWalks.take( gid );
		tracks.delete( gid );
		let row = rows.get( gid );
		// Death navigation is already settled by the world owner. Do not spend an
		// additional delivery interpolation interval sliding a falling corpse.
		// Keep accepting authoritative corrections; this is not a cached death pose.
		if ( settledTranslation && row ) {
			row.from = { ...target };
			row.target = { ...target };
			row.at = now - TICK_SECONDS;
			row.duration = TICK_SECONDS;
			row.settled = undefined;
		}

		// Settled world transforms have no remaining interpolation or body turn.
		// Retain the already-normalized result, not the raw region-local target;
		// this preserves the original floating-point conversion at sector edges.
		if (
			row?.settled && now >= row.last && now - row.last <= .25 && row.target.regionId === target.regionId &&
			row.target.x === target.x && row.target.y === target.y && row.target.z === target.z &&
			row.target.angle === target.angle
		) {
			row.last = now;
			return { ...row.settled };
		}
		if ( row ) row.settled = undefined;
		if ( !row || now < row.last || discontinuity( row.target, target ) ) {
			row = {
				from: { ...target },
				target: { ...target },
				at: now,
				duration: TICK_SECONDS,
				last: now,
				angle: target.angle,
				moving: false
			};
			rows.set( gid, row );
		} else {
			if ( now - row.last > MAX_SAMPLE_GAP_SECONDS && row.displayed && !settledTranslation ) {
				row.from = row.displayed;
				row.at = now;
			}
			row.angle = turn( row.angle, target.angle, now - row.last );
			row.last = now;
			if (
				row.target.regionId !== target.regionId || row.target.x !== target.x || row.target.y !== target.y ||
				row.target.z !== target.z || row.target.angle !== target.angle
			) {
				const from = position( row, now ), interval = now - row.at;
				row.duration = interval > .25 ?
					TICK_SECONDS :
					Math.min( .1, Math.max( TICK_SECONDS, interval, row.duration * .75 ) );
				row.from = from;
				row.target = { ...target };
				row.at = now;
			}
		}
		const result = position( row, now );
		row.moving = poseDistance( result, row.target ) > 1e-5;
		// Pose carries a native heading word. Keep sub-word precision internally,
		// but do not pass fractional words to the model's strict angle decoder.
		const output = { ...result, angle: Math.round( row.angle ) % 65536 };
		row.displayed = { ...output };
		if ( now - row.at >= row.duration && row.angle === target.angle ) row.settled = { ...output };
		return output;
	}
	/*
	================
	carryPose
	================
	*/
	function carryPose( gid: number, displayed: Pose ) {
		if ( ownWalks.carry( gid, displayed ) ) return;
		const track = tracks.get( gid );
		if ( !track ) return;
		track.displayed = { ...displayed };
		track.carried = true;
	}

	return {
		/*
		================
		origin
		================
		*/
		origin( ms: number ) {
			originMs = ms;
		},
		/*
		================
		samples

		This frame's timed samples by character. A character missing from the
		map is drawn by delivery interpolation and its sampled track retires.
		================
		*/
		samples( next: ReadonlyMap<number, SampleInput> ) {
			inputs = next;
			for ( const gid of tracks.keys() ) if ( !next.has( gid ) ) tracks.delete( gid );
			ownWalks.retain( next );
		},
		/*
		================
		pose

		The presented pose, unless the main-thread mover (#1440, port-only)
		predicts the local player's walk this frame: then its pose, turned
		toward its heading at the native rate (86CBA0). The presenter keeps
		advancing underneath, and the frame the prediction ends hands the
		drawn pose back to it (carry), so neither handover jumps.
		================
		*/
		pose( gid: number, target: Pose, now: number, settledTranslation = false ): Pose {
			let mover = ownMover?.gids.includes( gid ) ? ownMover.frame : undefined;
			if ( mover?.kind === "ended" ) {
				// Each mover character hands its drawn pose back once.
				if ( !ownMover!.carried.has( gid ) ) carryPose( gid, mover.pose );
				ownMover!.carried.add( gid );
				mover = undefined;
			}
			const presented = presentedPose( gid, target, now, settledTranslation );
			const last = shown.get( gid );
			let result = presented;
			if ( mover?.kind === "walking" ) {
				const angle = last ?
					turn( last.pose.angle, mover.pose.angle, Math.max( 0, now - last.at ) ) :
					mover.pose.angle;
				result = { ...mover.pose, angle: Math.round( angle ) % 65536 };
			}
			shown.set( gid, { pose: result, at: now } );
			return { ...result };
		},
		/*
		================
		ownMover

		This frame's main-thread mover state for the local player (#1440) and
		the mount it rides, if it rides (#1566).
		================
		*/
		ownMover( gid: number, frame: OwnMoverFrame, mount?: number ) {
			ownMover = frame.kind === "idle" ?
				null :
				{ gids: mount === undefined ? [ gid ] : [ gid, mount ], frame, carried: new Set() };
		},
		/*
		================
		drawn

		The pose last shown for a character: where a ground click's predicted
		walk starts.
		================
		*/
		drawn( gid: number ): Pose | undefined {
			const last = shown.get( gid );
			return last && { ...last.pose };
		},
		/*
		================
		carry

		Hand a drawn pose back to the sampled track: its owner showed this pose
		instead of the presented one (actor-motion's WAIT hold). The track then
		recovers from it at walking speed rather than jumping to the model.
		================
		*/
		carry( gid: number, displayed: Pose ) {
			carryPose( gid, displayed );
		},
		/*
		================
		moving

		A decaying correction is a glide, not a walk: a sampled character's
		animation follows its logical movement flag.
		================
		*/
		moving( gid: number ) {
			const ownMoving = ownWalks.moving( gid );
			if ( ownMoving !== undefined ) return ownMoving;
			const track = tracks.get( gid );
			if ( track ) return track.moving;
			return rows.get( gid )?.moving ?? false;
		},
		/*
		================
		retain
		================
		*/
		retain( gids: ReadonlySet<number> ) {
			for ( const gid of rows.keys() ) if ( !gids.has( gid ) ) rows.delete( gid );
			for ( const gid of tracks.keys() ) if ( !gids.has( gid ) ) tracks.delete( gid );
			for ( const gid of shown.keys() ) if ( !gids.has( gid ) ) shown.delete( gid );
			ownWalks.retain( gids );
		},
		/*
		================
		reset
		================
		*/
		reset() {
			rows.clear();
			tracks.clear();
			ownWalks.clear();
			inputs = new Map();
			lastPoseAt = undefined;
			ownMover = null;
			shown.clear();
		}
	};
}
