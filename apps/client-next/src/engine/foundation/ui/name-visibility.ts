/*
===========================================================================

name-visibility.ts - which characters show their overhead name board

CICCharacter 85E2E0 decides the name board before drawing it. The whole
board (name, guild line, fortress marks, quick status bars, party mark)
shares the decision's range, so nothing overhead outlives its name.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { GameOptions } from "@/engine/foundation/gameplay/game-options";
import { REGION_SIZE } from "@/engine/foundation/gameplay/native-movement";

// Squared world distance past which a name board is hidden (300 units).
const NAME_RANGE_SQUARED = 90000;

/*
================
nameInRange

The distance half of the name board decision: strictly inside 300 world
units of the local player (its live pose when known). Hover exempts it in
the callers, as 85E2E0 tests hover first.
================
*/
export function nameInRange(
	entity: EntityState,
	local: EntityState | undefined,
	pose?: import("@/engine/contracts/gameplay").Pose | null
): boolean {
	if ( !local ) return false;
	const origin = pose ?? local, target = entity.gid === local.gid ? origin : entity;
	const dx = target.x - origin.x + ((target.regionId & 255) - (origin.regionId & 255)) * REGION_SIZE;
	const dz = target.z - origin.z + ((target.regionId >>> 8) - (origin.regionId >>> 8)) * REGION_SIZE;
	const dy = target.y - origin.y;
	return dx * dx + dy * dy + dz * dz < NAME_RANGE_SQUARED;
}

/*
================
nameVisible

CICCharacter 85E2E0: hover wins before distance/options. Guild equality is
native guild-record pointer equality, including two absent records.
================
*/
export function nameVisible(
	entity: EntityState,
	local: EntityState | undefined,
	hovered: boolean,
	options: GameOptions,
	pose?: import("@/engine/contracts/gameplay").Pose | null
): boolean {
	if ( hovered ) return true;
	if ( !local || !nameInRange( entity, local, pose ) ) return false;
	if ( entity.kind === "monster" || entity.kind === "cos" ) return options.monsterNames && !entity.ownerGid;
	if ( entity.kind === "npc" ) return options.npcNames;
	if ( entity.gid === local.gid ) return options.ownName;
	if ( entity.kind === "player" ) {
		return options.playerNames || (options.guildNames && (entity.guildId ?? 0) === (local.guildId ?? 0));
	}
	return false;
}

/*
================
blindableCharacter

8602C0/85E550 class mask, with the requested local-character exemption.
================
*/
export function blindableCharacter( entity: Pick<EntityState, "kind" | "gid">, localGid?: number ): boolean {
	return entity.gid !== localGid && [ "monster", "player", "script-object" ].includes( entity.kind );
}

/*
================
hiddenSilkCos

854680 -> 5500F0: mall pickup COS, not every pet bought with Silk.
================
*/
export function hiddenSilkCos( entity: EntityState, hide: boolean ): boolean {
	const tid = entity.tidWord ?? 0;
	return hide && entity.kind === "cos" && (tid & 2) !== 0 && (tid & 0x1c) === 4 && (tid & 0x60) === 0x40 &&
		(tid & 0x780) === 0x180 && (tid & 0xf800) === 0x2000;
}
