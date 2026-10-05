/*
===========================================================================

name-visibility.ts - which characters show their overhead name board

CICCharacter 85E2E0 decides the name board before drawing it. Natively the
decision gates only the name text: 85F5EE skips the name, and the icon pass
at 85F5FD (vtable+0x3C kind 4: beginner mark, party-monster mark, fortress
marks) runs for every drawn character, so a far player showed a lone icon.
Deliberate deviation (owner's call): an icon never shows without its name.
overheadBoardVisible widens the name decision to every character that shows
an overhead icon or line, and every board element draws under that one
decision.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { GameOptions } from "@/engine/foundation/gameplay/game-options";
import { REGION_SIZE } from "@/engine/foundation/gameplay/native-movement";
import { hasMonsterPartyMark } from "@/engine/foundation/ui/monster-nameplate";

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
riderBoardPosition

Where a character's name board is measured from. Movement moves a rider's
mount (85E000) and leaves the rider's own row where it mounted, so a peer
rider is placed at its ride; with the ride absent (85D870 null) the rider
falls back to its own row, as 85F58E does.
================
*/
export function riderBoardPosition(
	entity: EntityState,
	ride: EntityState | undefined
): EntityState {
	if ( entity.mountedOn === undefined || !ride || ride.gid !== entity.mountedOn ) return entity;
	return { ...entity, regionId: ride.regionId, x: ride.x, y: ride.y, z: ride.z };
}

/*
================
nameVisible

CICCharacter 85E2E0: hover wins before distance/options. Guild equality is
native guild-record pointer equality, including two absent records. A
monster or COS in a ride link (CICharactor_GetMountedHorseOrVehicle reads
the linked GID at +0x2A4, set on a ridden mount) shows no name board; an
owned pet that is not ridden shows its name like any companion.
================
*/
export function nameVisible(
	entity: EntityState,
	local: EntityState | undefined,
	hovered: boolean,
	options: GameOptions,
	pose?: import("@/engine/contracts/gameplay").Pose | null,
	rideLinked = false
): boolean {
	if ( hovered ) return true;
	if ( !local || !nameInRange( entity, local, pose ) ) return false;
	if ( entity.kind === "monster" || entity.kind === "cos" ) return options.monsterNames && !rideLinked;
	if ( entity.kind === "npc" ) return options.npcNames;
	if ( entity.gid === local.gid ) return options.ownName;
	if ( entity.kind === "player" ) {
		return options.playerNames || (options.guildNames && (entity.guildId ?? 0) === (local.guildId ?? 0));
	}
	return false;
}

/*
================
beginnerMarkShown

CICUser_RenderOverheadBoardPass (86B350) draws icon_rudiment for a player
whose +0x779 bit 0 is set, inside its option +0x0C block.
================
*/
export function beginnerMarkShown( entity: EntityState, options: GameOptions ): boolean {
	return options.ownName && (entity.kind === "local-player" || entity.kind === "player") &&
		((entity.visualFlags ?? 0) & 1) !== 0;
}

/*
================
overheadBoardVisible

The one overhead decision: the native name decision, widened so that a
character showing any overhead icon or line also shows its name (see the
file header). overlayShown is the guild/fortress/status block's own gate.
================
*/
export function overheadBoardVisible(
	entity: EntityState,
	local: EntityState | undefined,
	hovered: boolean,
	options: GameOptions,
	pose: import("@/engine/contracts/gameplay").Pose | null | undefined,
	overlayShown: boolean,
	rideLinked = false
): boolean {
	return overlayShown || beginnerMarkShown( entity, options ) || hasMonsterPartyMark( entity ) ||
		nameVisible( entity, local, hovered, options, pose, rideLinked );
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
