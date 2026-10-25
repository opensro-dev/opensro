/*
===========================================================================

mover-publication.ts - what the worker publishes for the main-thread mover

The fields of the gameplay snapshot that exist for the local player's
main-thread mover (runtime/movement/local-mover.ts, epic #1437): the input
sequence the worker has taken, the walk speed, the walk it is making as a
rebuildable aim and its drift factor, and whether a ground click would walk
at once. GameplayState extends this interface, so readers keep reading
them from the snapshot; the mover's contract grows here, not in
gameplay.ts.

===========================================================================
*/
import type { Pose } from "./gameplay";

/*
================
MovementAim

A rebuildable walk as the worker publishes it (#1563): to a point, or
along a heading until the navigation stops it.
================
*/
export type MovementAim =
	| { readonly kind: "goal"; readonly destination: Pose; }
	| { readonly kind: "direction"; readonly heading: number; };
/*
================
MoverPublication

The worker's publication for the main-thread mover (#1440, #1441).
================
*/
export interface MoverPublication {
	// The last gameplay input sequence the worker took (movement-input.ts).
	readonly movementInputSeq?: number;
	// The local walk speed in world units a second that a new walk leg uses
	// (movement.ts request): the main-thread mover times its legs with it (#1440).
	readonly movementSpeed?: number;
	// What the worker's movement owner is walking, when it is a walk the
	// main-thread mover can rebuild (movement.ts state): a goal walk's
	// destination or a direction walk's live heading. The mover rebuilds the
	// worker's leg from it when it reconciles (#1441b, #1563).
	readonly movementAim?: MovementAim;
	// The direction walk's 500 ms drift factor (movement.ts driftWalk); 1
	// for any other walk.
	readonly movementSpeedFactor?: number;
	// The arrow keys' turn under way, heading words a millisecond (Left
	// positive; 0 when not turning): the main-thread mover keeps turning
	// past the publication (#1574, gameplay arrow-keys.ts).
	readonly movementTurnRate?: number;
	// A ground click now would start a walk at once (gameplay.ts
	// groundWalkRefusal): the main-thread mover predicts only then (#1440).
	readonly walkPredictable?: boolean;
	// The local player's own cast displacement in progress (a dash or
	// leap), published so the main-thread mover steps the segment on its
	// own clock; absent for any other walk and for a knockback struck
	// onto the player, which stays on the interpolated presenter (#1569).
	// Times are on the simulation clock (movement.ts state).
	readonly movementDisplacement?: {
		readonly from: Pose;
		readonly to: Pose;
		readonly startedAtMs: number;
		readonly durationMs: number;
	};
	// A ground click now would be held by a gate that releases into the
	// walk (a cast, the ground-skill suppress window) rather than refused:
	// the mover records the click's aim and adopts the walk when the
	// worker publishes it (#1570).
	readonly walkHeld?: boolean;
	// A held click is still in the worker's move reservation: the mover
	// keeps its record while this holds and consumes it on the first
	// publication after it clears (#1570).
	readonly moveHeld?: boolean;
	// A ground pickup now would start the worker's own run-up at once
	// (gameplay.ts pickupPredictableNow): the main-thread mover predicts
	// the pickup leg only then (#1567).
	readonly pickupPredictable?: boolean;
}
