/*
===========================================================================

local-mover.ts - the local player's walk, stepped on the main thread

Render-thread movement (epic #1437, part #1440). Unreal's locally
controlled character moves in its own frame tick from the input frame
on; ours was simulated in the worker and drawn one or two publications
late. This owner walks a ground click's leg on the main thread from the
click frame, every frame, by that frame's elapsed time, with the same
step (walk-step.ts) against the same navigation (navigation.ts view())
the worker's movement owner uses. The worker stays the authority for the
wire.

It predicts only what the worker would walk at once: a ground click that
resolves to a walk-to while the worker's latest publication says a click
would walk (walkPredictable) and the main view holds the product the
worker holds (navigationRequestId). Direction walks, mounted walks,
selects, pickups and run-ups stay worker-driven for now.

Until reconciliation lands (#1441b) a predicted leg runs to its end and
then hands the drawn pose back to the presenter (carry). It ends at once,
through the same handback, when the worker refused the click (it took the
input and is not walking), when the worker took any later command (a
monster click's run-up, a stop, or anything else the input sequence
numbers), or when a later click is not predicted. Not yet handled: a
correction or a speed change in the middle of a predicted leg is not
replayed; the leg runs at the speed it started with until it ends.
Port-only, not native: off unless the Experimental row is on.

The mover runs on the main frame clock alone; the worker's clock never
enters it, and the input sequence (#1441a) is what ties a prediction to
the worker's answer.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { GroundPickQuery, SurfaceCursor } from "@/engine/contracts/navigation";
import type { NavOwner } from "@/engine/foundation/navigation/dungeon-ownership";
import type { NavigationView } from "@/engine/foundation/navigation/clipper";
import { worldPointAction } from "@/engine/foundation/gameplay/direction-movement";
import { startWalking, stepWalk, walkLeg, type WalkStepSegment } from "@/engine/foundation/gameplay/walk-step";

/*
================
LocalMoverWorld

What the mover reads of the worker's latest publication and the main
thread's navigation view.
================
*/
export interface LocalMoverWorld {
	readonly pose: Pose | null;
	readonly moving: boolean;
	readonly walkPredictable: boolean;
	readonly speed: number | undefined;
	readonly inputSeq: number;
	readonly navigationRequestId: number | undefined;
	readonly navigationOwner: NavOwner | undefined;
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

The predicted pose this frame, or the pose to hand back to the presenter
(ended) on the frame a prediction stops.
================
*/
export type LocalMoverFrame =
	| { readonly kind: "idle"; }
	| { readonly kind: "walking"; readonly pose: Pose; }
	| { readonly kind: "ended"; readonly pose: Pose; };

/*
================
createLocalMover
================
*/
export function createLocalMover() {
	let walk: {
		seq: number;
		segment: WalkStepSegment | null;
		pose: Pose;
		poseAtMs: number;
		owner: NavOwner | undefined;
		cursor: SurfaceCursor;
		requestId: number;
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
	return {
		/*
		================
		click

		Starts a predicted walk for a ground click, from the drawn pose on the
		click frame. Returns whether it predicts; the click is sent to the
		worker either way.
		================
		*/
		click( input: LocalMoverClick, world: LocalMoverWorld ): boolean {
			const navigation = viewFor( world );
			if ( !navigation || !world.walkPredictable || !world.speed || !world.pose ) {
				if ( walk ) cut = walk.pose;
				walk = null;
				return false;
			}
			const from = walk?.pose ?? input.drawn;
			const action = worldPointAction( from, navigation.pick( input.query ), input.query, false );
			if ( action.kind !== "walk-to" ) {
				if ( walk ) cut = walk.pose;
				walk = null;
				return false;
			}
			cut = null;
			walk = {
				seq: input.seq,
				segment: startWalking( walkLeg( from, action.destination, input.now, world.speed ) ),
				pose: from,
				poseAtMs: input.now,
				owner: walk?.owner ?? world.navigationOwner,
				cursor: walk?.cursor ?? {},
				requestId: world.view!.requestId
			};
			return true;
		},
		/*
		================
		frame

		Steps the predicted walk to now.
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
			// The worker answered this click and is not walking it (refused,
			// held, or stopped at once), or the navigation it walks has changed:
			// the prediction ends where it is drawn.
			const refused = world.inputSeq >= walk.seq && !world.moving && walk.segment !== null &&
				now > walk.segment.start;
			// The worker took a later command than this click: whatever it does
			// (a run-up, a stop) is no longer the predicted leg.
			const superseded = world.inputSeq > walk.seq;
			if ( !navigation || world.navigationRequestId !== walk.requestId || refused || superseded ) {
				const pose = walk.pose;
				walk = null;
				return { kind: "ended", pose };
			}
			if ( walk.segment ) {
				const result = stepWalk( {
					pose: walk.pose,
					poseAtMs: walk.poseAtMs,
					segment: walk.segment,
					owner: walk.owner,
					cursor: walk.cursor,
					now,
					clip: navigation.clip,
					surface: navigation.surface
				} );
				if ( result.kind === "stepped" ) {
					walk.pose = result.pose;
					walk.owner = result.owner;
					walk.segment = result.segment;
				} else if ( result.kind === "uncovered" ) {
					walk.segment = result.segment;
				} else if ( result.kind === "rejected" ) {
					walk.segment = null;
				}
				if ( result.kind !== "idle" ) walk.poseAtMs = now;
			}
			if ( walk.segment ) return { kind: "walking", pose: walk.pose };
			// The leg has ended. Hold the drawn pose until the worker has
			// answered the click and finished its own leg, then hand it back.
			if ( world.inputSeq < walk.seq || world.moving ) return { kind: "walking", pose: walk.pose };
			const pose = walk.pose;
			walk = null;
			return { kind: "ended", pose };
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
