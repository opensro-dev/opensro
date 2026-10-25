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

It predicts only what the worker would walk at once: a ground click that
resolves to a walk-to while the worker's latest publication says a click
would walk (walkPredictable) and the main view holds the product the
worker holds (navigationRequestId). Direction walks, mounted walks,
selects, pickups and run-ups are not predicted from the click.

Reconciliation (#1441b, Unreal's ClientAdjustPosition then
ClientUpdatePosition): every newer worker publication rebuilds the
prediction from the worker's own pose at that publication's time, walking
the worker's own goal, then replays each click the worker has not taken
yet from that click's time at the speed it was predicted at. A click the
worker took but refused leaves no trace, because the rebuild starts from
the worker's pose and goal, not from the clicks. Whatever the rebuild
moved becomes a visual offset given back within the step cap both local
presenters share (drawn-step.ts); a teleport-sized one snaps. That is a
port-only presentation choice where Unreal differs: its autonomous proxy
does not smooth the residual after a replay (SmoothCorrection runs only
for simulated and remote autonomous proxies), so the body jumps by it;
here it drains within 1.04x walking speed and the camera never jumps. A worker
walking something the mover cannot rebuild (a direction walk, a skill
displacement) ends the prediction through the presenter's carry.
Port-only, not native: off unless the Experimental row is on.

Clocks: the mover steps on the main frame clock. The worker's clock enters
only through the simulation origin the runtime receives with every
snapshot (originMs), to place a publication's pose on that clock; the
input sequence (#1441a) ties each click to the worker's answer.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { GroundPickQuery, SurfaceCursor } from "@/engine/contracts/navigation";
import { SIMULATION_STEP_MS } from "@/engine/contracts/simulation";
import type { NavOwner } from "@/engine/foundation/navigation/dungeon-ownership";
import type { NavigationView } from "@/engine/foundation/navigation/clipper";
import { worldPointAction } from "@/engine/foundation/gameplay/direction-movement";
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
	// The goal walk the worker walks, if it walks one (movement.ts).
	readonly goal: Pose | undefined;
	readonly walkPredictable: boolean;
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

A predicted click the worker has not taken yet: its resolved destination
(a replay never re-picks), when it was clicked on the main clock, and the
walking speed it was predicted at.
================
*/
interface SavedInput {
	readonly seq: number;
	readonly destination: Pose;
	readonly clickMs: number;
	readonly speed: number;
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
}

/*
================
advance

Steps a walker to toMs in the worker's fixed steps.
================
*/
function advance( walker: Walker, toMs: number, navigation: NavigationView ) {
	while ( walker.segment && walker.atMs < toMs ) {
		const now = Math.min( toMs, walker.atMs + REPLAY_STEP_MS );
		const result = stepWalk( {
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
		} else if ( result.kind === "uncovered" ) {
			walker.segment = result.segment;
		} else if ( result.kind === "rejected" ) {
			walker.segment = null;
		}
		walker.atMs = now;
	}
	walker.atMs = Math.max( walker.atMs, toMs );
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
	that click's time (or the publication's, if the click came first) at the
	speed it was predicted at. Null when the worker walks something the mover
	cannot rebuild (a direction walk, a skill displacement).
	================
	*/
	function replay( world: LocalMoverWorld, now: number, navigation: NavigationView ): Walker | null {
		if ( world.moving && !world.goal ) return null;
		const atMs = world.poseAtMs! + world.originMs!;
		const walker: Walker = {
			pose: world.pose!,
			atMs,
			segment: world.moving && world.goal && world.speed ?
				startWalking( walkLeg( world.pose!, world.goal, atMs, world.speed ) ) :
				null,
			owner: world.navigationOwner,
			cursor: {}
		};
		for ( const input of walk!.inputs ) {
			const clickMs = Math.max( input.clickMs, walker.atMs );
			advance( walker, clickMs, navigation );
			walker.segment = startWalking( walkLeg( walker.pose, input.destination, clickMs, input.speed ) );
		}
		advance( walker, now, navigation );
		return walker;
	}
	return {
		/*
		================
		click

		Starts or redirects a predicted walk for a ground click, from the
		prediction's pose (or the drawn pose from rest) on the click frame.
		Returns whether it predicts; the click is sent to the worker either
		way.
		================
		*/
		click( input: LocalMoverClick, world: LocalMoverWorld ): boolean {
			const navigation = viewFor( world );
			if ( !navigation || !world.walkPredictable || !world.speed || !world.pose ) {
				if ( walk ) cut = walk.drawn;
				walk = null;
				return false;
			}
			const from = walk?.walker.pose ?? input.drawn;
			const action = worldPointAction( from, navigation.pick( input.query ), input.query, false );
			if ( action.kind !== "walk-to" || (walk?.inputs.length ?? 0) >= MAX_SAVED_INPUTS ) {
				if ( walk ) cut = walk.drawn;
				walk = null;
				return false;
			}
			cut = null;
			const saved = { seq: input.seq, destination: action.destination, clickMs: input.now, speed: world.speed };
			walk = {
				walker: {
					pose: from,
					atMs: input.now,
					segment: startWalking( walkLeg( from, action.destination, input.now, world.speed ) ),
					owner: walk?.walker.owner ?? world.navigationOwner,
					cursor: walk?.walker.cursor ?? {}
				},
				offset: walk?.offset ?? [ 0, 0, 0 ],
				drawn: walk?.drawn ?? input.drawn,
				drawnAtMs: walk?.drawnAtMs ?? input.now,
				inputs: [ ...(walk?.inputs ?? []), saved ],
				lastSeq: input.seq,
				reconciled: walk?.reconciled ?? { atMs: world.poseAtMs, revision: world.revision },
				requestId: world.view!.requestId,
				speed: world.speed
			};
			return true;
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
			if ( !walk ) return { kind: "idle" };
			const navigation = viewFor( world );
			if ( !navigation || world.navigationRequestId !== walk.requestId ) return end();
			walk.inputs = walk.inputs.filter( input => input.seq > world.inputSeq );
			if ( world.speed ) walk.speed = world.speed;
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
				world.inputSeq >= walk.lastSeq && hypot3( ...walk.offset ) < SETTLED_OFFSET;
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
		}
	};
}
