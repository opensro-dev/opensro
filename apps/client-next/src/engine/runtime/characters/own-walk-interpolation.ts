/*
===========================================================================

own-walk-interpolation.ts - the local mover's walk drawn between its ticks

Port-only, not native (owner, 2026-10-24: the "start of a walk lurches"
report; any smoother solution allowed): always on — the Experimental row
that once switched it was deleted (#1564). The worker simulates the local
mover on a fixed 16 ms tick and publishes to the main thread a frame or two later.
Drawing that walk at frame time means extrapolating past the newest tick,
which overshoots every redirect and then has to catch up.

This is fixed-tick smoothing as Unreal's Network Prediction plugin does
it (TFixedSmoothingService::FinalizeSmoothingFrame: the presented state is
the previous and the newest fixed-tick state blended by the unspent time,
never a state past the newest tick; Mover applies the result to the visual
component only). Here the tick states are buffered with their worker time,
each leg's start is inserted as an exact corner, and the walk is drawn at
render time = worker now - delay, where the delay covers the measured
publication latency. The drawn body therefore walks the worker's own path
at the worker's own speed: a start leaves rest at walking speed, a
redirect turns its exact corner, and nothing ever runs ahead to come back.

The delay only grows while walking (a brief slowdown, never a surge) and
relaxes while standing. A real correction (the server moved the walk
somewhere its legs do not lead) restarts the buffer and leaves a visual
offset that every frame scales by 1 - dt / 0.1 s: a discrete exponential
decay compounding per frame, exactly as Unreal's SmoothClientPosition
does for a simulated proxy (MeshTranslationOffset scaled by
1 - DeltaSeconds / SmoothNetUpdateTime, default 0.1 s), not a linear
ramp over 0.1 s.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { SampleInput } from "@/engine/contracts/pose-presentation";
import { SIMULATION_STEP_MS } from "@/engine/contracts/simulation";
import { interpolateMovement } from "@/engine/foundation/gameplay/native-movement";
import { hypot2, hypot3 } from "@/engine/foundation/math/hypot";
import {
	boundDrawnStep,
	MAX_SMOOTHED_CORRECTION,
	offsetPose,
	poseDelta as delta
} from "@/engine/foundation/gameplay/drawn-step";

// The delay never drops below one fixed step: the fixed-tick blend sits at
// most one step behind the newest tick.
const MIN_DELAY_MS = SIMULATION_STEP_MS;
// A publication later than this is a stall, not latency; the delay stops
// growing and the stall path below carries the walk.
const MAX_DELAY_MS = 100;
// Standing, the delay relaxes toward the latency it has seen at this rate
// (milliseconds per second of standing).
const DELAY_RELAX_MS_PER_SECOND = 50;
// The latency the delay must cover is the spread of the last this many
// fresh publications. Standing publishes no fresh ticks (the recording's
// worker time freezes until the click), so the window is counted in
// publications, never expired by time: a click after a long rest still
// meets the delay its walk measured.
const LATENCY_WINDOW = 32;
// Buffered tick states; a few hundred milliseconds of walk.
const MAX_HISTORY = 48;
// A sample farther than this from where the buffered legs put it is a
// correction, not the same walk (float noise on exact legs is far below).
const CONSISTENT_DISTANCE = .25;
// Unreal's default NetworkSimulatedSmoothLocationTime; halved when stopped.
const CORRECTION_SMOOTH_SECONDS = .1;

/*
================
OwnWalkSample

One worker publication of the local mover.
================
*/
export interface OwnWalkSample {
	readonly atMs: number;
	readonly pose: Pose;
	readonly moving: boolean;
	readonly from?: Pose;
	readonly to?: Pose;
	readonly durationMs?: number;
	// The worker time the main thread received it at.
	readonly arrivedAtMs: number;
}

/*
================
TickState
================
*/
interface TickState {
	readonly atMs: number;
	readonly pose: Pose;
}

/*
================
Leg

An admitted straight walk: where it starts, at what worker time, and its
speed in units per millisecond.
================
*/
interface Leg {
	readonly from: Pose;
	readonly to: Pose;
	readonly startMs: number;
	readonly speed: number;
	readonly length: number;
}

/*
================
planar
================
*/
function planar( a: Pose, b: Pose ): number {
	const d = delta( a, b );
	return d ? hypot2( d[0], d[2] ) : Infinity;
}

/*
================
legOf

The sample's admitted leg, timed by where the sample stands on it. A
sample off its own leg, or a leg without speed, has none.
================
*/
function legOf( sample: OwnWalkSample ): Leg | undefined {
	const { from, to, durationMs } = sample;
	if ( !sample.moving || !from || !to || !(durationMs! > 0) ) return undefined;
	const length = planar( from, to );
	if ( !(length > 0) || length === Infinity ) return undefined;
	const speed = length / durationMs!;
	const walked = planar( from, sample.pose );
	if ( walked === Infinity || walked > length + CONSISTENT_DISTANCE ) return undefined;
	return { from, to, speed, length, startMs: sample.atMs - walked / speed };
}

/*
================
alongLeg

Where a leg puts the walker at worker time ms: its start before it
began, its end after it arrived.
================
*/
function alongLeg( leg: Leg, ms: number ): Pose {
	const t = Math.max( 0, Math.min( 1, (ms - leg.startMs) * leg.speed / leg.length ) );
	return interpolateMovement( leg.from, leg.to, t );
}

/*
================
createOwnWalkInterpolation
================
*/
export function createOwnWalkInterpolation() {
	let history: TickState[] = [];
	let leg: Leg | undefined;
	let delayMs = MIN_DELAY_MS;
	// Recent publication latencies (arrival minus tick, worker milliseconds).
	let latencies: number[] = [];

	/*
	================
	windowLatency

	The delay the recent publications need: their largest latency, never
	less than one step past the smallest. The frame and worker clocks share
	no exact origin (the recording's offset is about 2 s and drifts), so
	only the spread is meaningful: the smallest latency stands for the
	clock offset, and the delay covers the rest, bounded.
	================
	*/
	function windowLatency(): number {
		if ( !latencies.length ) return MIN_DELAY_MS;
		const low = Math.min( ...latencies ), high = Math.max( ...latencies );
		return Math.min( low + MAX_DELAY_MS, Math.max( low + MIN_DELAY_MS, high ) );
	}
	let offset: [number, number, number] = [ 0, 0, 0 ];
	let lastDrawn: Pose | undefined;
	let lastDrawnAtMs: number | undefined;
	// The buffered walk at the last render time (no offset) and that frame's
	// worker time.
	let lastBase: Pose | undefined;
	let lastNowMs: number | undefined;
	// A leg that began before the render time already drawn: its start was
	// published later than the delay covers, so the body walked on past it.
	let lateTurn: { atMs: number; speed: number; overshoot: number; } | undefined;
	// The latest leg's walking speed (units per millisecond): what bounds a
	// frame's step while an offset decays, also after the walk stopped.
	let walkSpeed: number | undefined;
	// restart() snapped this frame (a teleport-sized correction): the next
	// drawn step is a jump on purpose and is not bounded.
	let snapped = false;
	// The frame clock of the last drawn frame (pose clockMs).
	let lastClockMs: number | undefined;

	/*
	================
	newest
	================
	*/
	const newest = () => history[history.length - 1];

	/*
	================
	append
	================
	*/
	function append( state: TickState ) {
		const last = newest();
		if ( last && state.atMs < last.atMs ) return;
		if ( last && state.atMs === last.atMs ) history[history.length - 1] = state;
		else history.push( state );
		if ( history.length > MAX_HISTORY ) history.splice( 0, history.length - MAX_HISTORY );
	}

	/*
	================
	expectedAt

	Where the buffered walk puts the walker at worker time ms: on the
	current leg once it began, else the newest state.
	================
	*/
	function expectedAt( ms: number ): Pose | undefined {
		if ( leg && ms >= leg.startMs ) return alongLeg( leg, ms );
		return newest()?.pose;
	}

	/*
	================
	restart

	A correction: the buffered walk no longer leads to the sample. Keep the
	drawn body where it is as a decaying visual offset (or snap a
	teleport), and start a fresh buffer at the sample.
	================
	*/
	function restart( sample: OwnWalkSample, nextLeg: Leg | undefined ) {
		const shown = lastDrawn;
		history = [];
		leg = nextLeg;
		if ( nextLeg ) append( { atMs: nextLeg.startMs, pose: nextLeg.from } );
		append( { atMs: sample.atMs, pose: sample.pose } );
		if ( !shown ) return;
		// The offset is measured against where the fresh buffer draws at the
		// same render time, so the first frame after it shows no jump.
		const d = delta( poseAt( lastDrawnAtMs ?? sample.atMs ), shown );
		offset = d && hypot3( ...d ) <= MAX_SMOOTHED_CORRECTION ? d : [ 0, 0, 0 ];
		snapped = !d || hypot3( ...d ) > MAX_SMOOTHED_CORRECTION;
	}

	/*
	================
	poseAt

	The buffered walk at worker time ms, without the visual offset. Past
	the newest tick only the admitted leg may carry it (a worker stall):
	never a guess beyond the leg's end.
	================
	*/
	function poseAt( ms: number ): Pose {
		const first = history[0]!, last = newest()!;
		if ( ms <= first.atMs ) return first.pose;
		if ( ms >= last.atMs ) {
			return leg && ms > leg.startMs && planar( alongLeg( leg, last.atMs ), last.pose ) <= CONSISTENT_DISTANCE ?
				alongLeg( leg, ms ) :
				last.pose;
		}
		let i = history.length - 1;
		while ( i > 0 && history[i - 1]!.atMs > ms ) i--;
		const a = history[i - 1]!, b = history[i]!;
		const span = b.atMs - a.atMs;
		return interpolateMovement( a.pose, b.pose, span > 0 ? (ms - a.atMs) / span : 1 );
	}

	return {
		/*
		================
		push

		One fresh worker publication. Inserts the new leg's start as an exact
		corner (a walk from rest holds still until it), records the tick
		state, and measures the publication latency.
		================
		*/
		push( sample: OwnWalkSample ) {
			const last = newest();
			if ( last && sample.atMs < last.atMs ) return;
			const nextLeg = legOf( sample );
			// Only walking ticks measure latency: a standing pose may be
			// republished long after its tick (a spawn, a stop), which says
			// nothing about how late the next walk's ticks will arrive.
			// A republished tick (a worker stall) is not a new arrival: counting
			// it again would read the stall itself as latency.
			if ( sample.moving && (!last || sample.atMs > last.atMs) ) {
				latencies.push( sample.arrivedAtMs - sample.atMs );
				if ( latencies.length > LATENCY_WINDOW ) latencies.splice( 0, latencies.length - LATENCY_WINDOW );
				// Growing the delay slows the drawn walk for a frame; it never
				// speeds it up.
				delayMs = Math.max( delayMs, windowLatency() );
			}
			if ( !last ) {
				leg = nextLeg;
				if ( nextLeg ) append( { atMs: nextLeg.startMs, pose: nextLeg.from } );
				append( { atMs: sample.atMs, pose: sample.pose } );
				return;
			}
			const sameLeg = !!leg && !!nextLeg && planar( leg.from, nextLeg.from ) <= CONSISTENT_DISTANCE &&
				planar( leg.to, nextLeg.to ) <= CONSISTENT_DISTANCE;
			if ( nextLeg && !sameLeg ) {
				// A new leg: its start must be where the buffered walk stood at
				// that time, or the server moved the walk.
				const corner = expectedAt( nextLeg.startMs );
				if (
					!corner || nextLeg.startMs < last.atMs - SIMULATION_STEP_MS ||
					planar( corner, nextLeg.from ) > CONSISTENT_DISTANCE
				) {
					restart( sample, nextLeg );
					return;
				}
				if ( lastDrawnAtMs !== undefined && lastDrawnAtMs > nextLeg.startMs ) {
					lateTurn = {
						atMs: nextLeg.startMs,
						speed: nextLeg.speed,
						overshoot: lastBase ? planar( lastBase, nextLeg.from ) : 0
					};
				}
				// Close the old leg at the corner time before turning.
				if ( leg && nextLeg.startMs > last.atMs ) append( { atMs: nextLeg.startMs, pose: corner } );
				leg = nextLeg;
				append( { atMs: Math.max( nextLeg.startMs, last.atMs ), pose: nextLeg.from } );
				append( { atMs: sample.atMs, pose: sample.pose } );
				return;
			}
			// On the same leg the sample must stand where the current leg's own
			// clock puts it; a leg rebuilt from the sample would always agree.
			const expected = sameLeg ? alongLeg( leg!, sample.atMs ) : expectedAt( sample.atMs );
			if ( !expected || planar( expected, sample.pose ) > CONSISTENT_DISTANCE ) {
				restart( sample, nextLeg );
				return;
			}
			if ( !sample.moving && leg ) {
				// The walk ended on its leg: it arrived at the leg's time, not at
				// the publication's.
				const arrived = leg.startMs + leg.length / leg.speed;
				if ( arrived > last.atMs && arrived < sample.atMs ) append( { atMs: arrived, pose: sample.pose } );
				leg = undefined;
			}
			append( { atMs: sample.atMs, pose: sample.pose } );
		},
		/*
		================
		pose

		The drawn pose at worker time nowMs: the buffered walk at nowMs -
		delay, plus the decaying correction offset. clockMs is the frame
		clock: the step bound measures real frame time on it, so a jump of
		the worker-time origin cannot widen its own budget.
		================
		*/
		pose( nowMs: number, clockMs = nowMs ): Pose | undefined {
			if ( !history.length ) return undefined;
			let renderMs = nowMs - delayMs;
			if ( lateTurn && lastNowMs !== undefined && renderMs > lateTurn.atMs ) {
				// Walk back to the corner and on along the new leg within this
				// frame's walking distance: by the triangle inequality the frame's
				// step is then at most walking speed times the frame time. The
				// later render time keeps the delay this late publication needed.
				const frameMs = Math.max( 0, nowMs - lastNowMs );
				const ahead = Math.max( 0, frameMs - lateTurn.overshoot / lateTurn.speed );
				const turnAt = lateTurn.atMs + ahead;
				if ( turnAt < renderMs ) {
					delayMs = nowMs - turnAt;
					renderMs = turnAt;
				}
			}
			lateTurn = undefined;
			const elapsed = lastDrawnAtMs === undefined ? 0 : Math.max( 0, renderMs - lastDrawnAtMs ) / 1000;
			const last = newest()!;
			const standing = !leg && renderMs >= last.atMs;
			if ( standing ) {
				// Relax toward the recent latency, never below it: a shorter delay
				// would render past a click the worker has not yet published.
				delayMs = Math.max( windowLatency(), delayMs - DELAY_RELAX_MS_PER_SECOND * elapsed );
			}
			if ( offset[0] || offset[1] || offset[2] ) {
				const smooth = standing ? CORRECTION_SMOOTH_SECONDS / 2 : CORRECTION_SMOOTH_SECONDS;
				const keep = elapsed < smooth ? 1 - elapsed / smooth : 0;
				offset = [ offset[0] * keep, offset[1] * keep, offset[2] * keep ];
				if ( hypot3( ...offset ) < 1e-2 ) offset = [ 0, 0, 0 ];
			}
			// Drop states the render time has passed, keeping its bracket.
			while ( history.length > 2 && history[1]!.atMs <= renderMs ) history.shift();
			const base = poseAt( renderMs );
			if ( leg ) walkSpeed = leg.speed;
			let drawn = offsetPose( base, offset );
			const frameMs = lastClockMs === undefined ? 0 : Math.max( 0, clockMs - lastClockMs );
			if ( !snapped ) {
				// Bound the frame's whole step (drawn-step.ts); keep the rest
				// as offset.
				const bounded = boundDrawnStep( lastDrawn, drawn, walkSpeed, frameMs );
				if ( bounded !== drawn ) {
					drawn = bounded;
					offset = delta( base, drawn ) ?? [ 0, 0, 0 ];
				}
			}
			snapped = false;
			lastBase = base;
			lastDrawn = drawn;
			lastDrawnAtMs = renderMs;
			lastNowMs = nowMs;
			lastClockMs = clockMs;
			return drawn;
		},
		/*
		================
		moving

		Whether the drawn body is still walking: its render time has not
		reached the end of the buffered walk.
		================
		*/
		moving( nowMs: number ): boolean {
			const last = newest();
			if ( !last ) return false;
			return !!leg || nowMs - delayMs < last.atMs;
		},
		/*
		================
		carry

		A drawn pose handed back (actor-motion's WAIT hold, or the other
		presenter's last frame at a handoff): recover from it as from a
		correction.
		================
		*/
		carry( displayed: Pose, nowMs: number ) {
			if ( !history.length ) return;
			const d = delta( poseAt( lastDrawnAtMs ?? nowMs - delayMs ), displayed );
			if ( d && hypot3( ...d ) <= MAX_SMOOTHED_CORRECTION ) offset = d;
			// The carried pose is the one on screen: the next frame's step is
			// bounded from it (drawn-step.ts), not from the last pose
			// this presenter computed but did not show.
			lastDrawn = { ...displayed };
		},
		/*
		================
		delay

		The current render delay in milliseconds (diagnostics and tests).
		================
		*/
		delay() {
			return delayMs;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			history = [];
			leg = undefined;
			delayMs = MIN_DELAY_MS;
			latencies = [];
			offset = [ 0, 0, 0 ];
			lastDrawn = undefined;
			lastDrawnAtMs = undefined;
			lastBase = undefined;
			lastNowMs = undefined;
			lateTurn = undefined;
			walkSpeed = undefined;
			snapped = false;
			lastClockMs = undefined;
		}
	};
}

// ============================================================================

/*
================
OwnWalkTrack

One character under the interpolated presenter: its buffer, the last
publication it took, and its drawn heading.
================
*/
interface OwnWalkTrack {
	walk: ReturnType<typeof createOwnWalkInterpolation>;
	relocation: number;
	atMs?: number;
	revision?: number;
	pose?: Pose;
	last: number;
	nowMs?: number;
	angle: number;
	displayed?: Pose;
}

/*
================
OwnWalkFrame

The frame the presenter draws: frame-clock seconds, the frame-clock
milliseconds of simulation time zero, and the pose the generic presenter
drew last frame when it hands this character over.
================
*/
export interface OwnWalkFrame {
	readonly now: number;
	readonly originMs: number;
	readonly handoff?: Pose;
}

/*
================
createOwnWalkPresenter

The per-character owner of the interpolated presentation, used by
pose-presentation.ts for the local mover's own walking legs. turn is the
presenter's native heading turn (86CBA0), shared so both presenters turn
a body the same way.
================
*/
export function createOwnWalkPresenter( turn: ( from: number, to: number, seconds: number ) => number ) {
	const tracks = new Map<number, OwnWalkTrack>();
	return {
		/*
		================
		handles

		Whether a sample is drawn here: the local mover's own leg, never a
		skill displacement (which keeps its own clock).
		================
		*/
		handles( input: SampleInput ): boolean {
			return input.ownLeg === true && !input.displacement;
		},
		/*
		================
		pose

		A fresh worker publication enters the buffer; a relocation restarts
		it. A handed-over pose is recovered from instead of jumped over.
		================
		*/
		pose( gid: number, input: SampleInput, target: Pose, frame: OwnWalkFrame ): Pose {
			const { now } = frame, nowMs = now * 1000 - frame.originMs;
			const relocation = input.transition?.relocation ?? 0;
			let own = tracks.get( gid );
			if ( !own || own.relocation !== relocation || now < own.last ) {
				own = { walk: createOwnWalkInterpolation(), relocation, last: now, angle: target.angle };
				tracks.set( gid, own );
			}
			const fresh = own.atMs !== input.atMs || own.revision !== input.revision || !own.pose ||
				own.pose.regionId !== target.regionId || own.pose.x !== target.x || own.pose.y !== target.y ||
				own.pose.z !== target.z;
			if ( fresh ) {
				own.walk.push( {
					atMs: input.atMs,
					pose: target,
					moving: input.moving,
					from: input.from,
					to: input.to,
					durationMs: input.durationMs,
					arrivedAtMs: nowMs
				} );
				own.atMs = input.atMs;
				own.revision = input.revision;
				own.pose = { ...target };
			}
			if ( frame.handoff ) own.walk.carry( frame.handoff, nowMs );
			own.angle = turn( own.angle, target.angle, now - own.last );
			own.last = now;
			own.nowMs = nowMs;
			const drawn = own.walk.pose( nowMs, now * 1000 ) ?? target;
			own.displayed = { ...drawn, angle: Math.round( own.angle ) % 65536 };
			return { ...own.displayed };
		},
		/*
		================
		take

		Releases a character to the generic presenter, returning the pose it
		was last drawn at (undefined if it was not drawn here).
		================
		*/
		take( gid: number ): Pose | undefined {
			const displayed = tracks.get( gid )?.displayed;
			tracks.delete( gid );
			return displayed;
		},
		/*
		================
		carry

		A drawn pose handed back (actor-motion's WAIT hold). Reports whether
		the character is drawn here.
		================
		*/
		carry( gid: number, displayed: Pose ): boolean {
			const own = tracks.get( gid );
			if ( !own ) return false;
			own.walk.carry( displayed, own.nowMs ?? 0 );
			return true;
		},
		/*
		================
		moving

		The drawn body's walking flag, or undefined when not drawn here.
		================
		*/
		moving( gid: number ): boolean | undefined {
			const own = tracks.get( gid );
			return own ? own.walk.moving( own.nowMs ?? 0 ) : undefined;
		},
		/*
		================
		retain

		Keeps only the characters still present.
		================
		*/
		retain( gids: ReadonlySet<number> | ReadonlyMap<number, unknown> ) {
			for ( const gid of tracks.keys() ) if ( !gids.has( gid ) ) tracks.delete( gid );
		},
		/*
		================
		clear
		================
		*/
		clear() {
			tracks.clear();
		}
	};
}
