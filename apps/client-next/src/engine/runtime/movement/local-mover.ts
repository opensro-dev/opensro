/*
===========================================================================

local-mover.ts - the local player's walk, stepped on the main thread

Render-thread movement (epic #1437, parts #1440 and #1441b). Unreal's
locally controlled character moves in its own frame tick from the input
frame on; ours was simulated in the worker and drawn one or two
publications late. This owner walks a ground click's leg on the main
thread from the click frame, every frame, by that frame's elapsed time,
with the same step (walk-step.ts) against the same navigation
(navigation.ts view()) the worker's movement owner uses. The worker stays
the authority for the wire.

Every predicted input enters through one seam, predict: an aim (a goal,
a heading walked until blocked, or an absolute timed segment), the speed
it walks at and the input's sequence number. A ground click is its first
caller: it resolves to a walk-to, or for a miss to a direction walk
(direction-movement.ts, #1563). Each caller predicts only what the worker
would walk at once (walkPredictable for the ground click) while the main
view holds the product the worker holds (navigationRequestId). Mounted
walks, selects, pickups and run-ups join through the same seam (#1566 to
#1571). A click a gate holds - a cast, the ground-skill suppress window -
cannot predict at once: the mover records its aim, as the worker records
the click, and adopts the walk from the first publication after the
worker stops holding it that walks an aim, so the release steps on the
frame it lands (#1570).

Reconciliation (#1441b, Unreal's ClientAdjustPosition then
ClientUpdatePosition): every newer worker publication rebuilds the
prediction from the worker's own pose at that publication's time, walking
the worker's own aim, then replays each input the worker has not taken
yet from that input's time at the speed it was predicted at. An input the
worker took but refused leaves no trace, because the rebuild starts from
the worker's pose and aim, not from the inputs. Whatever the rebuild
moved becomes a visual offset given back within the step cap both local
presenters share (drawn-step.ts); a teleport-sized one snaps. That is a
port-only presentation choice where Unreal differs: its autonomous proxy
does not smooth the residual after a replay (SmoothCorrection runs only
for simulated and remote autonomous proxies), so the body jumps by it;
here it drains within 1.04x walking speed and the camera never jumps. A worker
walking something it publishes no aim for ends the prediction through the
presenter's carry.
Port-only, not native: off unless the Experimental row is on.

Clocks: the mover steps on the main frame clock. The worker's clock enters
only through the simulation origin the runtime receives with every
snapshot (originMs), to place a publication's pose on that clock; the
input sequence (#1441a) ties each click to the worker's answer.

===========================================================================
*/
import type { MovementAim, Pose } from "@/engine/contracts/gameplay";
import type { GroundPickQuery, SurfaceCursor } from "@/engine/contracts/navigation";
import { SIMULATION_STEP_MS } from "@/engine/contracts/simulation";
import type { NavOwner } from "@/engine/foundation/navigation/dungeon-ownership";
import type { NavigationView } from "@/engine/foundation/navigation/clipper";
import { directionLegEnd, worldPointAction } from "@/engine/foundation/gameplay/direction-movement";
import { HEADING_SCALE } from "@/engine/foundation/gameplay/native-movement";
import { movementDuration, poseDistance } from "@/engine/foundation/gameplay/native-movement";
import { startWalking, stepWalk, walkLeg, type WalkStepSegment } from "@/engine/foundation/gameplay/walk-step";
import { boundDrawnStep, MAX_SMOOTHED_CORRECTION, poseDelta } from "@/engine/foundation/gameplay/drawn-step";
import { hypot3 } from "@/engine/foundation/math/hypot";

// Replay advances in the worker's own fixed steps, so a replayed leg clips
// against walls as the worker's ticks do.
const REPLAY_STEP_MS = SIMULATION_STEP_MS;
// At most this many unacknowledged clicks are kept for replay; one more ends
// the prediction (a worker that stopped answering).
const MAX_SAVED_INPUTS = 32;
// A residual offset below this is settled.
const SETTLED_OFFSET = 1e-3;

/*
================
MoverAim

What a predicted input walks. The mover builds the segment from the pose
the input starts at (leg), because a replay re-bases every input the
worker has not taken on the worker's newer pose; only a fixed segment is
absolute and replays as it is.
================
*/
export type MoverAim =
	| MovementAim
	| { readonly kind: "fixed"; readonly segment: WalkStepSegment; }
	// Ends the walk where the walker is when the input is replayed (the Up
	// arrow's release, #1574).
	| { readonly kind: "stop"; }
	// Turns the facing at rate heading words a millisecond from the input on
	// (Left and Right, #1574); 0 ends the turn. The walk keeps its segment.
	| { readonly kind: "turn"; readonly rate: number; };

/*
================
LocalMoverWorld

What the mover reads of the worker's latest publication and the main
thread's navigation view. originMs is simulation time zero on the main
frame clock, re-derived from every snapshot, so a drifting origin is
followed.
================
*/
export interface LocalMoverWorld {
	readonly pose: Pose | null;
	readonly poseAtMs: number | undefined;
	readonly revision: number;
	readonly moving: boolean;
	// What the worker walks, if it walks something the mover can rebuild
	// (movement.ts movementAim), and the speed factor of a direction walk's
	// 500 ms drift (driftWalk; 1 otherwise).
	readonly aim: MoverAim | undefined;
	readonly speedFactor: number;
	// The arrow keys' turn the worker is making, heading words a millisecond
	// (gameplay arrow-keys.ts rate); 0 when it is not turning.
	readonly turnRate: number;
	readonly walkPredictable: boolean;
	// A click now would be held by a gate that releases into the walk
	// (gameplay.ts walkHeld): the mover records its aim and adopts the
	// walk when the worker publishes it (#1570).
	readonly walkHeld: boolean;
	// The worker still holds a click in its move reservation
	// (gameplay.ts moveHeld): a recorded click is consumed only once
	// this clears, because the worker notes an input's sequence before
	// any gate, so its own publication is taken-but-not-walking.
	readonly holding: boolean;
	readonly speed: number | undefined;
	readonly inputSeq: number;
	readonly navigationRequestId: number | undefined;
	readonly navigationOwner: NavOwner | undefined;
	readonly originMs: number | undefined;
	readonly view: { readonly requestId: number; readonly navigation: NavigationView; } | null;
}

/*
================
LocalMoverClick
================
*/
export interface LocalMoverClick {
	// The input sequence the simulation host stamped on the click (#1441a).
	readonly seq: number;
	readonly query: GroundPickQuery;
	// The local player's pose drawn this frame: the walk starts where the
	// player sees the body, so the click frame never jumps.
	readonly drawn: Pose;
	readonly now: number;
}

/*
================
MoverInput

One predicted input, whatever command made it. mount names the ridden
COS that moves (#1566); undefined is the player.
================
*/
export interface MoverInput {
	readonly seq: number;
	readonly aim: MoverAim;
	// Units a second this input walks at.
	readonly speed: number;
	// The body as drawn this frame: a prediction from rest starts here.
	readonly drawn: Pose;
	readonly now: number;
	readonly mount?: number;
}

/*
================
LocalMoverFrame

The pose to draw this frame, or the pose to hand back to the presenter
(ended) on the frame a prediction stops.
================
*/
export type LocalMoverFrame =
	| { readonly kind: "idle"; }
	| { readonly kind: "walking"; readonly pose: Pose; }
	| { readonly kind: "ended"; readonly pose: Pose; };

/*
================
SavedInput

A predicted input the worker has not taken yet: its resolved aim (a
replay never re-picks), when it was made on the main clock, and the speed
it was predicted at.
================
*/
interface SavedInput {
	readonly seq: number;
	readonly aim: MoverAim;
	readonly atMs: number;
	readonly speed: number;
}

/*
================
HeldClick

A click a gate held (#1570): its aim resolved at click time, as the
worker resolves it, waiting for the publication that walks it. The aim
is bookkeeping only - a direction pick re-resolves at release in the
worker, so adoption always walks the worker's published aim. Dropped
with no trace when the worker took the input but does not walk.
================
*/
interface HeldClick {
	readonly seq: number;
	readonly aim: MoverAim;
	readonly speed: number;
	readonly drawn: Pose;
	readonly now: number;
}

/*
================
Walker

A pose walking a segment through the navigation view, as stepWalk advances
it.
================
*/
interface Walker {
	pose: Pose;
	atMs: number;
	segment: WalkStepSegment | null;
	owner: NavOwner | undefined;
	cursor: SurfaceCursor;
	// The arrow keys' turn: words a millisecond, and the unrounded facing it
	// has reached (the worker rounds only what it applies, as here).
	turn: number;
	heading: number;
}

/*
================
advance

Steps a walker to toMs in the worker's fixed steps. A turn under way is
applied after each step's walk, as the worker's tick steers after its
move (gameplay arrow-keys.ts, movement.ts steer): the facing turns and a
direction walk is re-aimed from where it stands.
================
*/
function advance( walker: Walker, toMs: number, navigation: NavigationView ) {
	while ( (walker.segment || walker.turn) && walker.atMs < toMs ) {
		const now = Math.min( toMs, walker.atMs + REPLAY_STEP_MS );
		const result = !walker.segment ? { kind: "idle" as const } : stepWalk( {
			pose: walker.pose,
			poseAtMs: walker.atMs,
			segment: walker.segment,
			owner: walker.owner,
			cursor: walker.cursor,
			now,
			clip: navigation.clip,
			surface: navigation.surface
		} );
		if ( result.kind === "stepped" ) {
			walker.pose = result.pose;
			walker.owner = result.owner;
			walker.segment = result.segment;
		} else if ( result.kind === "sampled" ) {
			// A fixed segment is sampled on its own clock; it retires at
			// its authored end, where the sampled pose is its destination.
			walker.pose = result.pose;
			walker.owner = result.owner;
			// Only a segment samples (a turn alone steps "idle"), so this holds one.
			if ( walker.segment && now >= walker.segment.start + walker.segment.duration ) walker.segment = null;
		} else if ( result.kind === "uncovered" ) {
			walker.segment = result.segment;
		} else if ( result.kind === "rejected" ) {
			walker.segment = null;
		}
		if ( walker.turn ) turnWalker( walker, now - walker.atMs );
		walker.atMs = now;
	}
	walker.atMs = Math.max( walker.atMs, toMs );
}

/*
================
turnWalker

Turns the walker's facing by elapsed milliseconds of its turn and re-aims a
direction walk on the new heading at its own speed.
================
*/
function turnWalker( walker: Walker, elapsed: number ) {
	walker.heading = ((walker.heading + walker.turn * elapsed) % HEADING_SCALE + HEADING_SCALE) % HEADING_SCALE;
	const angle = Math.round( walker.heading ) % HEADING_SCALE;
	walker.pose = { ...walker.pose, angle };
	const segment = walker.segment;
	if ( segment?.direction && !segment.direction.blocked && segment.walking ) {
		walker.segment = leg(
			walker.pose,
			{ kind: "direction", heading: angle },
			walker.atMs + elapsed,
			segment.walking.speed
		);
	}
}

/*
================
leg

The segment of an aim walked from from, as the worker's movement owner
starts it: a goal leg (walk-step.ts walkLeg); one DIRECTION_LEG_UNITS leg
along the heading (movement.ts directionSegment), which stepWalk walks
past its end until the navigation stops it; the fixed segment itself; or
none for a stop.
================
*/
function leg(
	from: Pose,
	aim: Exclude<MoverAim, { kind: "turn"; }>,
	start: number,
	speed: number
): WalkStepSegment | null {
	if ( aim.kind === "stop" ) return null;
	if ( aim.kind === "fixed" ) return aim.segment;
	if ( aim.kind === "goal" ) return startWalking( walkLeg( from, aim.destination, start, speed ) );
	const to = directionLegEnd( from, aim.heading );
	return startWalking( {
		from,
		to: { ...to, angle: aim.heading },
		start,
		duration: movementDuration( poseDistance( from, to ), speed ),
		direction: { heading: aim.heading, blocked: false }
	} );
}

/*
================
createLocalMover
================
*/
export function createLocalMover() {
	let walk: {
		// The prediction, and how far the drawn body still is from it: what a
		// rebuild moved that the cap has not given back yet.
		walker: Walker;
		offset: [number, number, number];
		drawn: Pose;
		drawnAtMs: number;
		inputs: SavedInput[];
		// The last click predicted, and the publication last rebuilt from.
		lastSeq: number;
		reconciled: { atMs: number | undefined; revision: number; };
		requestId: number;
		// Walking speed (units a second) the cap holds the drawn body to.
		speed: number;
	} | null = null;
	// A prediction a later unpredicted click cut short: its drawn pose is
	// handed back on the next frame instead of being dropped.
	let cut: Pose | null = null;
	// The newest click a gate held, waiting for its walk (#1570).
	let heldClick: HeldClick | null = null;
	/*
	================
	viewFor

	The navigation view when it holds what the worker holds, else null.
	================
	*/
	function viewFor( world: LocalMoverWorld ): NavigationView | null {
		const view = world.view;
		return view && view.requestId === world.navigationRequestId ? view.navigation : null;
	}
	/*
	================
	end

	Hands the drawn pose back to the presenter.
	================
	*/
	function end(): LocalMoverFrame {
		const pose = walk!.drawn;
		walk = null;
		return { kind: "ended", pose };
	}
	/*
	================
	replay

	The worker's answer rebuilt to now: its pose at the publication's own
	time, walking its own goal, then every click it has not taken yet from
	that input's time (or the publication's, if the input came first) at the
	speed it was predicted at. Null when the worker walks something it
	publishes no aim for.
	================
	*/
	function replay( world: LocalMoverWorld, now: number, navigation: NavigationView ): Walker | null {
		if ( world.moving && !world.aim ) return null;
		const atMs = world.poseAtMs! + world.originMs!;
		const walker: Walker = {
			pose: world.pose!,
			atMs,
			segment: world.moving && world.aim && world.aim.kind !== "turn" && world.speed ?
				leg( world.pose!, world.aim, atMs, world.speed * world.speedFactor ) :
				null,
			owner: world.navigationOwner,
			cursor: {},
			// The worker's own turn carries on past its publication.
			turn: world.turnRate,
			heading: world.pose!.angle
		};
		for ( const input of walk!.inputs ) {
			const inputMs = Math.max( input.atMs, walker.atMs );
			advance( walker, inputMs, navigation );
			if ( input.aim.kind === "turn" ) {
				walker.turn = input.aim.rate;
				walker.heading = walker.pose.angle;
			} else {
				walker.segment = leg( walker.pose, input.aim, inputMs, input.speed );
			}
		}
		advance( walker, now, navigation );
		return walker;
	}
	/*
	================
	predict

	The one entry every predicted input takes: starts or redirects the
	prediction from the prediction's pose (or the drawn pose from rest)
	on the input's frame and saves the input for replay. Returns whether
	it predicts; the command goes to the worker either way, and a caller
	predicts only what the worker's own gate admits now.
	================
	*/
	function predict( input: MoverInput, world: LocalMoverWorld ): boolean {
		// An admitted newer input supersedes a held click (#1570).
		if ( heldClick && input.seq > heldClick.seq ) heldClick = null;
		if ( !viewFor( world ) || !world.pose || !input.speed || (walk?.inputs.length ?? 0) >= MAX_SAVED_INPUTS ) {
			if ( walk ) cut = walk.drawn;
			walk = null;
			return false;
		}
		cut = null;
		const from = walk?.walker.pose ?? input.drawn;
		const saved = { seq: input.seq, aim: input.aim, atMs: input.now, speed: input.speed };
		const aim = input.aim;
		walk = {
			walker: {
				pose: from,
				atMs: input.now,
				// A turn keeps the walk it turns; any other aim starts its own.
				segment: aim.kind === "turn" ? walk?.walker.segment ?? null : leg( from, aim, input.now, input.speed ),
				owner: walk?.walker.owner ?? world.navigationOwner,
				cursor: walk?.walker.cursor ?? {},
				turn: aim.kind === "turn" ? aim.rate : walk?.walker.turn ?? 0,
				heading: aim.kind === "turn" || !walk ? from.angle : walk.walker.heading
			},
			offset: walk?.offset ?? [ 0, 0, 0 ],
			drawn: walk?.drawn ?? input.drawn,
			drawnAtMs: walk?.drawnAtMs ?? input.now,
			inputs: [ ...(walk?.inputs ?? []), saved ],
			lastSeq: input.seq,
			reconciled: walk?.reconciled ?? { atMs: world.poseAtMs, revision: world.revision },
			requestId: world.view!.requestId,
			speed: input.speed
		};
		return true;
	}
	return {
		predict,
		/*
		================
		arrow

		The Up arrow (#1574, arrow-keys.ts): its press walks the facing unless
		a direction walk is already predicted (0x877F30 with bFlag 0 does not
		resend); its release ends a predicted direction walk where the walker
		is. Left and Right turn on the worker; each publication rebuilds the
		walk on the heading the turn has reached.
		================
		*/
		arrow( seq: number, down: boolean, drawn: Pose, now: number, world: LocalMoverWorld ): boolean {
			if ( down ) {
				if ( walk?.walker.segment?.direction ) return true;
				if ( !world.walkPredictable || !world.speed || !world.pose ) return false;
				const heading = (walk?.walker.pose ?? world.pose).angle;
				return predict( { seq, aim: { kind: "direction", heading }, speed: world.speed, drawn, now }, world );
			}
			if ( !walk?.walker.segment?.direction ) return false;
			return predict( { seq, aim: { kind: "stop" }, speed: walk.speed, drawn, now }, world );
		},
		/*
		================
		turn

		Left and Right (#1574): the facing turns at rate heading words a
		millisecond from this frame (0 ends it), walking or standing; a
		direction walk under way follows the turn as the worker's does.
		================
		*/
		turn( seq: number, rate: number, drawn: Pose, now: number, world: LocalMoverWorld ): boolean {
			if ( !walk && (!rate || !world.walkPredictable) ) return false;
			if ( !world.speed || !world.pose ) return false;
			return predict(
				{ seq, aim: { kind: "turn", rate }, speed: walk?.speed ?? world.speed, drawn, now },
				world
			);
		},
		/*
		================
		click

		A ground click (CGInterface_MoveToWorldPoint 0x6932A0): a hit walks to
		the point, a miss walks the ray's heading until blocked, and a miss
		within 5 degrees of the direction already walked changes nothing, as
		the worker drops it (0x693519).
		================
		*/
		click( input: LocalMoverClick, world: LocalMoverWorld ): boolean {
			const navigation = viewFor( world );
			if ( !navigation || !world.speed || !world.pose ) {
				if ( walk ) cut = walk.drawn;
				walk = null;
				return false;
			}
			const from = walk?.walker.pose ?? input.drawn;
			// The worker tests the walk it walks: a direction walk in progress.
			const walkingDirection = walk ?
				!!walk.walker.segment?.direction :
				world.moving && world.aim?.kind === "direction";
			const action = worldPointAction( from, navigation.pick( input.query ), input.query, walkingDirection );
			if ( action.kind === "none" ) return walk !== null;
			const aim: MoverAim = action.kind === "walk-to" ?
				{ kind: "goal", destination: action.destination } :
				{ kind: "direction", heading: action.heading };
			if ( !world.walkPredictable ) {
				// A holding gate keeps the click for its release (#1570):
				// record the aim and leave any prediction walking; an
				// outright refusal cuts the prediction as before.
				if ( world.walkHeld ) {
					heldClick = {
						seq: input.seq,
						aim,
						speed: world.speed,
						drawn: input.drawn,
						now: input.now
					};
					return false;
				}
				if ( walk ) cut = walk.drawn;
				walk = null;
				return false;
			}
			heldClick = null;
			return predict( { seq: input.seq, aim, speed: world.speed, drawn: input.drawn, now: input.now }, world );
		},
		/*
		================
		frame

		Steps the prediction to now, rebuilds it from every newer worker
		publication, and draws it within the shared step cap.
		================
		*/
		frame( now: number, world: LocalMoverWorld ): LocalMoverFrame {
			if ( cut ) {
				const pose = cut;
				cut = null;
				return { kind: "ended", pose };
			}
			// A held click whose gate released: the worker notes the
			// input's sequence before any gate, so the click's own
			// publication is taken-but-not-walking and must not consume
			// the record; the first publication after the reservation
			// clears adopts it when it walks an aim, predicted from where
			// the body was drawn at click time and reconciled like any
			// prediction (#1570). Taken but not walking: no trace.
			if ( heldClick && world.inputSeq >= heldClick.seq && !world.holding ) {
				const held = heldClick;
				heldClick = null;
				if ( world.moving && world.aim ) {
					predict(
						{ seq: held.seq, aim: world.aim, speed: world.speed ?? held.speed, drawn: held.drawn, now },
						world
					);
				}
			}
			if ( !walk ) return { kind: "idle" };
			const navigation = viewFor( world );
			if ( !navigation || world.navigationRequestId !== walk.requestId ) return end();
			walk.inputs = walk.inputs.filter( input => input.seq > world.inputSeq );
			// The cap follows the speed the worker walks at, a direction walk's
			// drift factor included, or the body falls behind a faster drift.
			// A fixed segment is exempt: it is drawn at its authored rate (a
			// dash is not a correction to throttle to walking speed), so the
			// speed the prediction started with stands while it runs.
			if ( world.speed && !walk.walker.segment?.fixedTiming ) walk.speed = world.speed * world.speedFactor;
			advance( walk.walker, now, navigation );
			let snapped = false;
			const fresh = world.poseAtMs !== walk.reconciled.atMs || world.revision !== walk.reconciled.revision;
			if ( fresh && world.pose && world.poseAtMs !== undefined && world.originMs !== undefined ) {
				walk.reconciled = { atMs: world.poseAtMs, revision: world.revision };
				let rebuilt = replay( world, now, navigation );
				if ( !rebuilt ) return end();
				// A teleport-sized rebuild is a jump on purpose. The clicks still in
				// flight were aimed from the old place: drop them, so the body does
				// not walk a click's leg from the new place before the worker
				// answers it (the worker decides what they mean there).
				const gap = poseDelta( walk.drawn, rebuilt.pose );
				snapped = !gap || hypot3( ...gap ) > MAX_SMOOTHED_CORRECTION;
				if ( snapped && walk.inputs.length ) {
					walk.inputs = [];
					rebuilt = replay( world, now, navigation )!;
				}
				walk.walker = rebuilt;
			}
			// The drawn body heads for the prediction; the shared cap
			// (drawn-step.ts) limits how far it moves this frame, so what a
			// rebuild moved is given back within the margin, never in a jump.
			const drawn = snapped ?
				walk.walker.pose :
				boundDrawnStep( walk.drawn, walk.walker.pose, walk.speed / 1000, now - walk.drawnAtMs );
			walk.offset = poseDelta( walk.walker.pose, drawn ) ?? [ 0, 0, 0 ];
			walk.drawn = drawn;
			walk.drawnAtMs = now;
			// Settled: nothing left to walk, no click in flight, the worker is
			// standing and the offset is given back.
			const settled = !walk.walker.segment && !walk.inputs.length && !world.moving &&
				!world.turnRate && world.inputSeq >= walk.lastSeq && hypot3( ...walk.offset ) < SETTLED_OFFSET;
			if ( settled ) return end();
			return { kind: "walking", pose: drawn };
		},
		/*
		================
		active
		================
		*/
		active(): boolean {
			return walk !== null || cut !== null;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			walk = null;
			cut = null;
			heldClick = null;
		}
	};
}
