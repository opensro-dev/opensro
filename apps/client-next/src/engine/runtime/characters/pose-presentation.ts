/*
===========================================================================

pose-presentation.ts - the rendered pose of every character, camera included

The simulation worker owns logical poses; this module owns what is drawn.
Remote characters bridge observed delivery intervals between samples.

The local player is drawn on the frame clock instead: each sample carries
the simulation time it was taken at, so the pose is extrapolated along the
sampled velocity to the frame's own time (never past the path end). Busy
frames that deliver samples late or in bursts therefore no longer slow the
walk down and then fast-forward it.

Small jumps between consecutive local models (a server correction when a
cast or pickup stops the player, a leg turn) become a visual offset that
decays in CORRECTION_TAU_SECONDS, so the body and the camera glide instead
of snapping. This smoothing is presentation only and a deliberate deviation
from the original client, which snaps; logical poses stay authoritative.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import { SIMULATION_STEP_MS } from "@/engine/contracts/simulation";
import { interpolateMovement, poseDistance } from "@/engine/foundation/gameplay/native-movement";

const TICK_SECONDS = SIMULATION_STEP_MS / 1000;
const REGION_SIZE = 1920;
// A discontinuity is a teleport, not motion: no interpolation or smoothing.
const DISCONTINUITY_DISTANCE = 192;
// Samples further apart than this do not define a velocity.
const MAX_SAMPLE_GAP_SECONDS = 0.25;
// Bounded extrapolation: a stalled worker parks the player, never runs it on.
const MAX_EXTRAPOLATION_SECONDS = 0.1;
// Correction offsets decay with this time constant (about 95% gone in 0.2 s).
const CORRECTION_TAU_SECONDS = 0.07;
// Offsets smaller than this are spent.
const MIN_CORRECTION_DISTANCE = 0.01;
// Offsets larger than this are real relocations and snap.
const MAX_CORRECTION_DISTANCE = 96;

interface Track {
	target: Pose;
	from: Pose;
	at: number;
	duration: number;
	last: number;
	angle: number;
	moving: boolean;
	settled?: Pose;
}

/*
================
LocalSample

The local player's latest logical pose and the frame-clock time it was
sampled at (seconds), with the destination of the leg being walked.
================
*/
interface LocalSample {
	readonly pose: Pose;
	readonly at: number;
}

interface LocalTrack {
	previous?: LocalSample;
	latest: LocalSample;
	moving: boolean;
	to?: Pose;
	offset: [number, number, number];
	last: number;
	angle: number;
	drawn: boolean;
}

/*
================
LocalInput

What characters publishes each frame for the local player.
================
*/
export interface LocalInput {
	readonly gid: number;
	readonly atMs: number;
	readonly moving: boolean;
	readonly to?: Pose;
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

The pose moved by a world vector, renormalized into its region.
================
*/
function displace( pose: Pose, v: readonly [number, number, number] ): Pose {
	if ( pose.regionId & 0x8000 ) return { ...pose, x: pose.x + v[0], y: pose.y + v[1], z: pose.z + v[2] };
	const wx = pose.x + v[0] + (pose.regionId & 255) * REGION_SIZE,
		wz = pose.z + v[2] + (pose.regionId >>> 8) * REGION_SIZE,
		rx = Math.floor( wx / REGION_SIZE ),
		rz = Math.floor( wz / REGION_SIZE );
	return { ...pose, regionId: rx | (rz << 8), x: wx - rx * REGION_SIZE, y: pose.y + v[1], z: wz - rz * REGION_SIZE };
}

/*
================
localModel

The local pose at frame time now: the latest sample, advanced along the
velocity of the last two samples. Walking is piecewise linear at constant
speed, so within a leg this is exact; the leg end bounds it.
================
*/
function localModel( row: LocalTrack, now: number ): Pose {
	const latest = row.latest, previous = row.previous;
	if ( !row.moving || !previous ) return latest.pose;
	const gap = latest.at - previous.at;
	if ( gap <= 0 || gap > MAX_SAMPLE_GAP_SECONDS ) return latest.pose;
	const span = worldVector( latest.pose, previous.pose );
	if ( !span ) return latest.pose;
	const stepped = Math.hypot( span[0], span[2] );
	if ( stepped === 0 ) return latest.pose;
	let ahead = Math.min( MAX_EXTRAPOLATION_SECONDS, Math.max( 0, now - latest.at ) ) / gap;
	if ( row.to ) {
		const rest = worldVector( row.to, latest.pose );
		if ( rest ) ahead = Math.min( ahead, Math.hypot( rest[0], rest[2] ) / stepped );
	}
	return {
		...displace( latest.pose, [ span[0] * ahead, span[1] * ahead, span[2] * ahead ] ),
		angle: latest.pose.angle
	};
}

/*
================
createPosePresentation

The worker journal is backpressured and can deliver several fixed steps in
one batch. Remote rows bridge observed delivery intervals, not a fictitious
16ms cadence. Never extrapolate beyond admitted navigation; the camera
shares this sample.
================
*/
export function createPosePresentation() {
	const rows = new Map<number, Track>();
	let local: LocalTrack | null = null, localInput: LocalInput | null = null;
	// Frame-clock milliseconds of simulation time zero (ClockSample.originMs).
	let originMs: number | null = null;

	/*
	================
	localPose
	================
	*/
	function localPose( input: LocalInput, target: Pose, now: number ): Pose {
		const at = (originMs! + input.atMs) / 1000;
		let row = local;
		if (
			!row || now < row.last || now - row.last > MAX_SAMPLE_GAP_SECONDS ||
			discontinuity( row.latest.pose, target )
		) {
			row = {
				latest: { pose: { ...target }, at },
				moving: input.moving,
				to: input.to,
				offset: [ 0, 0, 0 ],
				last: now,
				angle: target.angle,
				drawn: false
			};
			local = row;
		} else {
			const decay = Math.exp( -(now - row.last) / CORRECTION_TAU_SECONDS );
			row.offset = [ row.offset[0] * decay, row.offset[1] * decay, row.offset[2] * decay ];
			row.angle = turn( row.angle, target.angle, now - row.last );
			row.last = now;
			const latest = row.latest.pose;
			const changed = at !== row.latest.at || latest.regionId !== target.regionId || latest.x !== target.x ||
				latest.y !== target.y || latest.z !== target.z;
			if ( changed || row.moving !== input.moving || row.to !== input.to ) {
				const before = localModel( row, now );
				if ( changed ) {
					// Keep the previous sample only when this one is strictly newer;
					// a correction at the same time replaces the model outright.
					if ( at > row.latest.at ) row.previous = row.latest;
					else row.previous = undefined;
					row.latest = { pose: { ...target }, at };
				}
				row.moving = input.moving;
				row.to = input.to;
				const jump = worldVector( before, localModel( row, now ) );
				if ( jump ) {
					row.offset = [ row.offset[0] + jump[0], row.offset[1] + jump[1], row.offset[2] + jump[2] ];
				}
				if ( !jump || Math.hypot( row.offset[0], row.offset[1], row.offset[2] ) > MAX_CORRECTION_DISTANCE ) {
					row.offset = [ 0, 0, 0 ];
				}
			}
		}
		const model = localModel( row, now ), drawn = displace( model, row.offset );
		// A decaying correction is a glide, not a walk: animation follows the
		// logical movement flag. Residues below a hundredth of a unit clear.
		if ( Math.hypot( row.offset[0], row.offset[1], row.offset[2] ) < MIN_CORRECTION_DISTANCE ) {
			row.offset = [ 0, 0, 0 ];
		}
		row.drawn = input.moving;
		// Pose carries a native heading word. Keep sub-word precision internally,
		// but do not pass fractional words to the model's strict angle decoder.
		return { ...drawn, angle: Math.round( row.angle ) % 65536 };
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
		local

		The local player's sample for this frame, or null when none is known.
		================
		*/
		local( input: LocalInput | null ) {
			localInput = input;
			if ( !input ) local = null;
		},
		/*
		================
		pose
		================
		*/
		pose( gid: number, target: Pose, now: number, settledTranslation = false ): Pose {
			if ( localInput?.gid === gid && originMs !== null && !settledTranslation ) {
				return localPose( localInput, target, now );
			}
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
			if ( !row || now < row.last || now - row.last > .25 || discontinuity( row.target, target ) ) {
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
			if ( now - row.at >= row.duration && row.angle === target.angle ) row.settled = { ...output };
			return output;
		},
		/*
		================
		moving
		================
		*/
		moving( gid: number ) {
			if ( localInput?.gid === gid && local ) return local.drawn;
			return rows.get( gid )?.moving ?? false;
		},
		/*
		================
		retain
		================
		*/
		retain( gids: ReadonlySet<number> ) {
			for ( const gid of rows.keys() ) if ( !gids.has( gid ) ) rows.delete( gid );
			if ( localInput && !gids.has( localInput.gid ) ) local = null;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			rows.clear();
			local = null;
			localInput = null;
		}
	};
}
