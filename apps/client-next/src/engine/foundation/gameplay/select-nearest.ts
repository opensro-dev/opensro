/*
===========================================================================

select-nearest.ts - the monster the Auto Select action takes

Action 1005 in CGInterface_ExecuteActionCommand (695420) selects the result
of World_FindClosestMonsterWithinRange70 (861580): that walks the client's
CICMonster list and keeps the nearest one inside 70 units, measured in three
dimensions from the player. Native v1.150 never reaches this code: no
actionwnddata row and no key names 1005. Port-only, not native: the Actions
window row exists only behind the Experimental "Auto Select" option. The
worker owns the choice, as it does for the pickup shortcut.

===========================================================================
*/

import type { EntityState } from "@/engine/contracts/world";
import { planarDistanceSquared, samePlane } from "@/engine/foundation/world/world-space";

// 861580 seeds its best distance with 70.0.
const AUTO_SELECT_REACH = 70;
// The appearance byte of a dead character (gameplay.ts reads the same value).
const APPEARANCE_DEAD = 2;

/*
================
nearestMonster

The nearest living monster inside the reach, or undefined. 861580 tests a
state bit on each list entry; the port's list holds spawned monsters, and
the inference recorded here is that the bit excludes the dead, which no
player could select to any purpose.
================
*/
export function nearestMonster(
	monsters: readonly EntityState[],
	local: { readonly regionId: number; readonly x: number; readonly y: number; readonly z: number; }
): EntityState | undefined {
	let best: EntityState | undefined, bestDistance = AUTO_SELECT_REACH * AUTO_SELECT_REACH;
	for ( const monster of monsters ) {
		if ( monster.appearanceState?.[0] === APPEARANCE_DEAD ) continue;
		if ( !samePlane( monster.regionId, local.regionId ) ) continue;
		const height = monster.y - local.y, distance = planarDistanceSquared( local, monster ) + height * height;
		if ( distance > bestDistance ) continue;
		best = monster;
		bestDistance = distance;
	}
	return best;
}
