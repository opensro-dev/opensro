/*
===========================================================================

npc-click-refusal.ts - a ground click next to an NPC does not walk

CNavigationDeadreckon_StartGroundMove (878600) walks the client's non-user
object list (0xCCB7D4) before it starts a walk. For every CICNPC (and its
only child class, CICGuard; monsters, players, pets and structures are
other classes) it takes the 3D distance squared from the object to the
clicked point (879250, region offsets applied). Below 400.0 (0xBCC0C0,
20 units) it returns at once (878669 -> 8786D2): no walk, no 0x7738, no
destination marker. A click on the ground at an NPC's feet therefore does
nothing, where the port used to walk into the NPC.

The port has no class objects, so the class is read from the spawn's type
id: IsNPC (808A80) names a CICNPC type, and a guard type is a CICGuard.

===========================================================================
*/

import type { Pose } from "@/engine/contracts/gameplay";
import type { EntityState } from "@/engine/contracts/world";
import { sameCoordinateSpace, worldDelta } from "@/engine/foundation/world/world-space";

// NPC_CLICK_REFUSAL_DISTANCE_SQ is 0xBCC0C0's 400.0. 87865B fcomp, test ah 5, jpo:
// only C0 (below) refuses, so a point exactly 20 units away still walks.
export const NPC_CLICK_REFUSAL_DISTANCE_SQ = 400;

/*
================
isNpcClass

IsNPC (808A80): bit 1 set, TypeID1 1, TypeID2 2 and TypeID3 2; a guard
(CICGuard) has TypeID3 4 and no bit-1 test. Both are CICNPC kinds.
================
*/
export function isNpcClass( tidWord: number ): boolean {
	if ( (tidWord & 0x1c) !== 4 || (tidWord & 0x60) !== 0x40 ) return false;
	const group = tidWord & 0x780;
	if ( group === 0x100 ) return (tidWord & 2) !== 0;
	return group === 0x200;
}

/*
================
npcRefusesWalk

Whether StartGroundMove refuses a walk to destination: some NPC or guard
stands within 20 units of it in 3D. 879250 stores each component and the
sum as float32, so the port rounds at the same stores.
================
*/
export function npcRefusesWalk( destination: Pose, entities: Iterable<EntityState> ): boolean {
	for ( const entity of entities ) {
		if ( entity.tidWord === undefined || !isNpcClass( entity.tidWord ) ) continue;
		if ( !sameCoordinateSpace( entity.regionId, destination.regionId ) ) continue;
		const [wx, wz] = worldDelta( destination, entity );
		const dx = Math.fround( wx ), dy = Math.fround( entity.y - destination.y ), dz = Math.fround( wz );
		if ( Math.fround( dx * dx + dy * dy + dz * dz ) < NPC_CLICK_REFUSAL_DISTANCE_SQ ) return true;
	}
	return false;
}
