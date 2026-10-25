/*
===========================================================================

approach-goal.ts - the ground point an entity click walks to first

A click on something out of reach is a walk before it is an interaction:
a select approaches an NPC, a gate or a stall keeper to the reach point
interaction-approach.ts names (698740), and a far pickup walks onto the
item while the server drives its own half of the run-up (the worker's
predictPickupRunUp). The worker computes that destination from its own
pose when the command lands, a worker round trip after the click.

#1567: the main-thread mover predicts the same leg from the click frame,
so both sides need the one destination, computed from the worker's
latest pose - the pose the worker itself will start from, near enough
that its own aim replaces the prediction on the first publication and
the residual glides inside the shared step cap (drawn-step.ts). A click
that starts no walk - in reach, a class with no approach, another
dungeon - has no goal and predicts nothing.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { EntityState } from "@/engine/contracts/world";
import { planarDistance, sameCoordinateSpace } from "@/engine/foundation/world/world-space";
import { interactionApproach } from "./interaction-approach";

// grounditem.ExecuteRange: a pickup closer than this is granted in place;
// farther away the server walks the player to the item first. The worker's
// predictPickupRunUp reads this same owner.
export const PICKUP_EXECUTE_RANGE = 10;

/*
================
approachGoal

The destination the worker walks to for a click on entity, from the
worker's pose, or null when the click starts no walk. A far ground item
is walked onto (its own pose, the player's heading); anything else goes
through interactionApproach, truncated to the walk's integer end the
movement owner requests (movement.ts request).
================
*/
export function approachGoal( pose: Pose, entity: EntityState ): Pose | null {
	if ( !sameCoordinateSpace( pose.regionId, entity.regionId ) ) return null;
	if ( entity.kind === "ground-item" ) {
		if ( planarDistance( pose, entity ) <= PICKUP_EXECUTE_RANGE ) return null;
		return { regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: pose.angle };
	}
	const destination = interactionApproach( pose, entity );
	if ( !destination ) return null;
	return {
		...destination,
		x: Math.trunc( destination.x ),
		y: Math.trunc( destination.y ),
		z: Math.trunc( destination.z )
	};
}
