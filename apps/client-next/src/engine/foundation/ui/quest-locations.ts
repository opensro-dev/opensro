/*
===========================================================================

quest-locations.ts - map markers for the places a quest tool is used (port-only)

Some Blue Berserk quest steps send the player to a spot rather than to an NPC:
the Soul-ringing Bell is used at a cemetery and the Radiant Crystal at a dig
site. The quest's target list names NPCs, so such a spot has no marker of its
own. This table names each spot for the quest that needs it; the marker passes
draw it under the quest target markers Experimental row, as an NPC marker.

The spots and quest ids mirror the server (apps/server/internal/game/quest/
hwan_use.go: hwanBellPoint, hwanDigPoint; the quest rows 349 and 363 are
QNO_{CH,EU}_HWAN_1_3_1, 357 and 371 are QNO_{CH,EU}_HWAN_2_4).

===========================================================================
*/
import type { Pose, QuestRecord } from "@/engine/contracts/gameplay";

/*
================
QuestLocation
================
*/
export type QuestLocation = Readonly<Pick<Pose, "regionId" | "x" | "y" | "z">>;

// The spots, and which quest uses which: literal tables, as a shared module's state must be.
const SPOTS = {
	bell: { regionId: 23980, x: 1218, y: 230, z: 706 },
	dig: { regionId: 25954, x: 1672, y: 180, z: 232 }
} as const;
const SPOT_OF_QUEST = { 349: "bell", 363: "bell", 357: "dig", 371: "dig" } as const;

/*
================
questLocationTargets

The spots of the player's active quests, each once. Only while the quest target
markers row is on (allQuests): a tracked quest's own markers stay the native ones.
================
*/
export function questLocationTargets(
	quests: readonly QuestRecord[] | undefined,
	allQuests: boolean
): readonly QuestLocation[] {
	if ( !allQuests ) return [];
	const names = new Set<keyof typeof SPOTS>();
	for ( const quest of quests ?? [] ) {
		const name = (SPOT_OF_QUEST as Readonly<Record<number, keyof typeof SPOTS>>)[quest.refId];
		if ( name ) names.add( name );
	}
	return [ ...names ].map( name => SPOTS[name] );
}
