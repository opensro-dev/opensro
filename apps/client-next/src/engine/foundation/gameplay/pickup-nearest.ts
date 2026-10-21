/*
===========================================================================

pickup-nearest.ts - the ground item the pickup shortcut takes

DropItemManager_FindNearestPickable (77DBA0) walks the drops the client
tracks and keeps the nearest one it may take: not claimed by a pickup in
flight (CIItem +0x284), not another player's (the drop's pickable flag),
and within 500 units (distance squared below 250000). The worker owns the
choice: its entity table has already applied every despawn and grant the
server sent before the press, where the UI's frame snapshot may still
show an item the last press just took.

===========================================================================
*/

import type { EntityState } from "@/engine/contracts/world";
import { planarDistanceSquared, samePlane } from "@/engine/foundation/world/world-space";

// PICKUP_REACH_SQUARED is 77DC5E's 250000.0.
const PICKUP_REACH_SQUARED = 250000;

/*
================
nearestPickable

The nearest ground item local may take, or undefined. party holds the
object ids whose drops the server may share with local.
================
*/
export function nearestPickable(
	entities: readonly EntityState[],
	local: { readonly gid: number; readonly regionId: number; readonly x: number; readonly z: number; },
	party: ReadonlySet<number>
): EntityState | undefined {
	let best: EntityState | undefined, bestDistance = Infinity;
	for ( const entity of entities ) {
		const item = entity.groundItem;
		if ( entity.kind !== "ground-item" || !item ) continue;
		if ( item.claimantGid !== undefined && item.claimantGid !== local.gid ) continue;
		if ( item.ownerJid !== undefined && item.ownerJid !== local.gid && !party.has( item.ownerJid ) ) continue;
		if ( !samePlane( entity.regionId, local.regionId ) ) continue;
		const distance = planarDistanceSquared( local, entity );
		if ( distance >= PICKUP_REACH_SQUARED || distance >= bestDistance ) continue;
		best = entity;
		bestDistance = distance;
	}
	return best;
}
