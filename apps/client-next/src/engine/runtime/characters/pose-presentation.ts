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

Small jumps between consecutive models of a sampled character (a server
correction when a cast or pickup stops the player, a leg turn, a monster's
halt) become a bounded critically damped correction, so the
body and the camera glide instead of snapping. This smoothing is
presentation only and a deliberate deviation from the original client,
which snaps; logical poses stay authoritative.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import { SIMULATION_STEP_MS } from "@/engine/contracts/simulation";
import { REGION_SIZE, interpolateMovement, poseDistance } from "@/engine/foundation/gameplay/native-movement";
import { hypot2, hypot3 } from "@/engine/foundation/math/hypot";

const TICK_SECONDS = SIMULATION_STEP_MS / 1000;
// Legacy publishers without transition metadata retain their distance guard.
const DISCONTINUITY_DISTANCE = 192;
// Samples further apart than this do not define a velocity.
const MAX_SAMPLE_GAP_SECONDS = 0.25;
// Bounded extrapolation: a stalled worker parks a walker, never runs it on.
const MAX_EXTRAPOLATION_SECONDS = 0.1;
// Correction offsets decay with this time constant (about 95% gone in 0.2 s).
const CORRECTION_FREQUENCY = 30;
const MAX_RECOVERY_STEP_SECONDS = 0.033;
// Offsets smaller than this are spent.
const MIN_CORRECTION_DISTANCE = 0.01;
// Legacy publishers cannot validate large cosmetic offsets against navigation.
const MAX_CORRECTION_DISTANCE = 96;
const MAX_RECOVERY_PATHS = 4;

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
	readonly revision: number;
}

/*
================
SampleTrack

A character drawn on the frame clock from its timed samples.
================
*/
interface SampleTrack {
	paths: { from: Pose; to: Pose; }[];
	previous?: Sample;
	latest: Sample;
	moving: boolean;
	to?: Pose;
	offset: [number, number, number];
	velocity: [number, number, number];
	displayed: Pose;
	relocation: number;
	last: number;
	angle: number;
}

/*
================
SampleInput

What characters publishes each frame for a character with timed samples:
the simulation time of its latest pose, the movement revision it belongs to,
whether it is walking, and the end of the leg being walked.

The movement owner bumps the revision whenever it re-anchors a walk (a new
click, a receipt, a correction, a native move). Two samples define a
velocity only within one revision: across a re-anchor their difference is
a jump, not motion, and extrapolating it turned a 19-unit receipt
correction 8 ms after the previous sample into -2,275 units/s.
================
*/
export interface SampleInput {
	readonly atMs: number;
	readonly revision: number;
	readonly moving: boolean;
	readonly from?: Pose;
	readonly to?: Pose;
	readonly transition?: import("@/engine/contracts/gameplay").MovementTransition;
}

/*
================
recover

Exact critically damped integration. Project velocity into the remaining
correction so a new opposite receipt cannot send the body past either end.
Frozen wall time never consumes a transition that was not displayed.
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

A straight admitted chord includes height: smoothing cannot cut a corner,
cross a wall, or switch between overlapping floors. Navigation owns the
chord; presentation can only move along it.
================
*/
function onCorridor( pose: Pose, from: Pose, to: Pose, walking = false ) {
	const span = worldVector( to, from ), point = worldVector( pose, from );
	if ( !span || !point ) return false;
	// A walk's navigation proof follows terrain and owner spans, not the
	// straight height chord between its distant endpoints. Comparing that
	// chord to a sampled terrain height falsely rejects ordinary hills.
	// Corrections themselves still require the complete admitted 3D chord.
	if ( walking ) span[1] = point[1] = 0;
	const length2 = span[0] ** 2 + span[1] ** 2 + span[2] ** 2;
	const t = length2 ?
		Math.max( 0, Math.min( 1, (point[0] * span[0] + point[1] * span[1] + point[2] * span[2]) / length2 ) ) :
		0;
	return hypot3( point[0] - span[0] * t, point[1] - span[1] * t, point[2] - span[2] * t ) <= MIN_CORRECTION_DISTANCE;
}

/*
================
turn

Native 86CBA0: shortest arc, 3*pi radians/second. Logical headings remain
authoritative; only the visual body turns. Heading words span one full turn.
================
*/
function turn( from: number, to: number, seconds: number ) {
	const delta = ((to - from + 98304) % 65536) - 32768, limit = seconds * 98304;
	return (from + Math.max( -limit, Math.min( limit, delta ) ) + 65536) % 65536;
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

Check coordinate spaces before distance: cross-dungeon distance is undefined.
================
*/
function discontinuity( a: Pose, b: Pose ) {
	return !!((a.regionId | b.regionId) & 0x8000) && a.regionId !== b.regionId ||
		poseDistance( a, b ) > DISCONTINUITY_DISTANCE;
}

/*
================
worldVector

a - b in world units, or null when the two poses share no coordinate space.
================
*/
function worldVector( a: Pose, b: Pose ): [number, number, number] | null {
	if ( (a.regionId | b.regionId) & 0x8000 ) {
		return a.regionId === b.regionId ? [ a.x - b.x, a.y - b.y, a.z - b.z ] : null;
	}
	return [
		a.x - b.x + ((a.regionId & 255) - (b.regionId & 255)) * REGION_SIZE,
		a.y - b.y,
		a.z - b.z + ((a.regionId >>> 8) - (b.regionId >>> 8)) * REGION_SIZE
	];
}

/*
================
displace

The pose moved by a world vector, renormalized into its region. A zero
vector returns the pose untouched, preserving its exact coordinates.
================
*/
function displace( pose: Pose, v: readonly [number, number, number] ): Pose {
	if ( v[0] === 0 && v[1] === 0 && v[2] === 0 ) return pose;
	if ( pose.regionId & 0x8000 ) return { ...pose, x: pose.x + v[0], y: pose.y + v[1], z: pose.z + v[2] };
	const wx = pose.x + v[0] + (pose.regionId & 255) * REGION_SIZE,
		wz = pose.z + v[2] + (pose.regionId >>> 8) * REGION_SIZE,
		rx = Math.floor( wx / REGION_SIZE ),
		rz = Math.floor( wz / REGION_SIZE );
	return { ...pose, regionId: rx | (rz << 8), x: wx - rx * REGION_SIZE, y: pose.y + v[1], z: wz - rz * REGION_SIZE };
}

/*
================
sampledModel

The pose at frame time now: the latest sample, advanced along the velocity
of the last two samples. Walking is piecewise linear at constant speed, so
within a leg this is exact; the leg end bounds it.
================
*/
function sampledModel( row: SampleTrack, now: number ): Pose {
	const latest = row.latest, previous = row.previous;
	if ( !row.moving || !previous ) return latest.pose;
	const gap = latest.at - previous.at;
	if ( gap <= 0 || gap > MAX_SAMPLE_GAP_SECONDS ) return latest.pose;
	const span = worldVector( latest.pose, previous.pose );
	if ( !span ) return latest.pose;
	const stepped = hypot2( span[0], span[2] );
	if ( stepped === 0 ) return latest.pose;
	let ahead = Math.min( MAX_EXTRAPOLATION_SECONDS, Math.max( 0, now - latest.at ) ) / gap;
	if ( row.to ) {
		const rest = worldVector( row.to, latest.pose );
		if ( rest ) ahead = Math.min( ahead, hypot2( rest[0], rest[2] ) / stepped );
	}
	if ( ahead === 0 ) return latest.pose;
	return {
		...displace( latest.pose, [ span[0] * ahead, span[1] * ahead, span[2] * ahead ] ),
		angle: latest.pose.angle
	};
}

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

	/*
	================
	sampledPose
	================
	*/
	function sampledPose( gid: number, input: SampleInput, target: Pose, now: number ): Pose {
		const at = (originMs! + input.atMs) / 1000;
		let row = tracks.get( gid );
		const relocation = input.transition?.relocation ?? 0;
		const revisionChanged = row && input.revision !== row.latest.revision;
		const stalled = row && now - row.last > MAX_SAMPLE_GAP_SECONDS;
		const recoverySeconds = row ? Math.max( 0, now - row.last ) : 0;
		const recovering = row && hypot3( ...row.offset ) > 0;
		if (
			!row || now < row.last || row.relocation !== relocation ||
			(revisionChanged && input.transition?.eligible === false) ||
			(input.transition ? !worldVector( row.latest.pose, target ) : discontinuity( row.latest.pose, target ))
		) {
			row = {
				paths: [],
				latest: { pose: { ...target }, at, revision: input.revision },
				moving: input.moving,
				to: input.to,
				offset: [ 0, 0, 0 ],
				velocity: [ 0, 0, 0 ],
				displayed: { ...target },
				relocation,
				last: now,
				angle: target.angle
			};
			tracks.set( gid, row );
		} else if ( now !== row.last ) {
			if ( !revisionChanged ) recover( row, now - row.last );
			if ( stalled ) row.previous = undefined;
			row.angle = turn( row.angle, target.angle, now - row.last );
			row.last = now;
		}
		const latest = row.latest.pose;
		const changed = at !== row.latest.at || input.revision !== row.latest.revision ||
			latest.regionId !== target.regionId || latest.x !== target.x || latest.y !== target.y ||
			latest.z !== target.z;
		if ( changed || stalled || row.moving !== input.moving || row.to !== input.to ) {
			const preserveDisplay = stalled || revisionChanged && input.transition?.reason !== "input";
			const before = preserveDisplay ? row.displayed : displace( sampledModel( row, now ), row.offset );
			if ( changed ) {
				// Keep the previous sample only when this one is strictly newer
				// and on the same walk; a correction at the same time, or any
				// re-anchor, replaces the model outright and its jump becomes
				// the decaying offset below.
				row.previous = !stalled && at > row.latest.at && input.revision === row.latest.revision ?
					row.latest :
					undefined;
				row.latest = { pose: { ...target }, at, revision: input.revision };
			}
			row.moving = input.moving;
			row.to = input.to;
			const jump = worldVector( before, sampledModel( row, now ) );
			if ( jump ) row.offset = jump;
			if ( !jump || !input.transition && hypot3( ...row.offset ) > MAX_CORRECTION_DISTANCE ) {
				row.offset = [ 0, 0, 0 ];
				row.velocity = [ 0, 0, 0 ];
			}
		}
		// Retarget the existing trajectory rather than parking it on every
		// receipt. A first correction starts at the displayed pose; subsequent
		// receipts still spend this frame's bounded recovery step and velocity.
		if ( revisionChanged && recovering && !stalled ) recover( row, recoverySeconds );
		const model = sampledModel( row, now );
		if ( input.from && input.to ) {
			const path = row.paths[row.paths.length - 1];
			if (
				!path || poseDistance( path.from, input.from ) > MIN_CORRECTION_DISTANCE ||
				poseDistance( path.to, input.to ) > MIN_CORRECTION_DISTANCE
			) {
				row.paths.push( { from: input.from, to: input.to } );
				if ( row.paths.length > MAX_RECOVERY_PATHS ) row.paths.shift();
			}
		}
		let drawn = displace( model, row.offset );
		const corridor = input.transition?.corridor;
		if (
			input.transition && hypot3( ...row.offset ) > 0 &&
			!(corridor && onCorridor( drawn, corridor.from, corridor.to ) &&
				onCorridor( model, corridor.from, corridor.to )) &&
			!row.paths.some( path =>
				onCorridor( drawn, path.from, path.to, true ) &&
				onCorridor( model, path.from, path.to, true )
			)
		) {
			row.offset = [ 0, 0, 0 ];
			row.velocity = [ 0, 0, 0 ];
			drawn = model;
		}
		// Retain the path behind a rebased receipt only while it still carries
		// visible recovery. This cannot grow with a long session or cut a turn.
		if ( hypot3( ...row.offset ) === 0 && row.paths.length > 1 ) row.paths.splice( 0, row.paths.length - 1 );
		// Pose carries a native heading word. Keep sub-word precision internally,
		// but do not pass fractional words to the model's strict angle decoder.
		row.displayed = { ...drawn, angle: Math.round( row.angle ) % 65536 };
		return { ...row.displayed };
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
		},
		/*
		================
		pose
		================
		*/
		pose( gid: number, target: Pose, now: number, settledTranslation = false ): Pose {
			const input = inputs.get( gid );
			if ( input && originMs !== null && !settledTranslation ) {
				rows.delete( gid );
				return sampledPose( gid, input, target, now );
			}
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
		},
		/*
		================
		moving

		A decaying correction is a glide, not a walk: a sampled character's
		animation follows its logical movement flag.
		================
		*/
		moving( gid: number ) {
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
		},
		/*
		================
		reset
		================
		*/
		reset() {
			rows.clear();
			tracks.clear();
			inputs = new Map();
		}
	};
}
