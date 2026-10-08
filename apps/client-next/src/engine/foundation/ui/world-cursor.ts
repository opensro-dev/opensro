/*
===========================================================================

world-cursor.ts - which retail cursor resource the pointer shows

The ids are the RT_GROUP_CURSOR ordinals CursorManager_SetCursorByParam
(A15AF0) loads. Hovering the world picks one from the entity under the
pointer (6875F0); a UI cursor mode such as the repair hammer (0x96, set by
CIFStore_OnRepairButton 5B1C00) replaces it and is owned by the UI.

A player or a pet the local player may attack shows the PvP cursor 0xA0;
holding Alt admits a neutral one (the worker's hoverAttack verdict). The
fortress-war branches need a fortress war owner the port does not have;
outside a war they change nothing.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import { HOVER_ATTACK_ALT, HOVER_ATTACK_PLAIN } from "@/engine/foundation/gameplay/player-attack";

export type WorldCursor = 0x95 | 0x96 | 0x97 | 0x98 | 0x99 | 0x9a | 0xa0 | 0xa1 | 0xa3;

// STALL_TITLE_MODE is CICharactor +0x461 (appearanceState[6]) with a stall
// open; a player there who cannot be attacked shows the stall cursor 0xA3.
const STALL_TITLE_MODE = 4;
// NO_EVENT_TEAM is CICUser +0x7E1 outside an event match.
const NO_EVENT_TEAM = 0xff;

/*
================
playerCursor

6875F0's CICUser branch: the PvP cursor when vtable +0xA4 admits the
player under the live Alt state, else the stall or plain cursor. In an
event match (local +0x7E1 set) the team alone decides: an opponent shows
the attack cursor 0x97, a teammate the plain one.
================
*/
function playerCursor( entity: EntityState, local: EntityState, alt: boolean ): WorldCursor {
	const team = local.arenaTeam ?? NO_EVENT_TEAM;
	if ( team !== NO_EVENT_TEAM ) return team !== (entity.arenaTeam ?? NO_EVENT_TEAM) ? 0x97 : 0x95;
	if ( (entity.hoverAttack ?? 0) & (alt ? HOVER_ATTACK_ALT : HOVER_ATTACK_PLAIN) ) return 0xa0;
	return entity.appearanceState?.[6] === STALL_TITLE_MODE ? 0xa3 : 0x95;
}

/*
================
worldCursor

6875F0's ordinary world-class branches; attack flags are the constructor
value overridden by the optional name-info dword, not inferred from HP.
alt is GetKeyState(VK_MENU), read as the cursor is chosen.
================
*/
export function worldCursor(
	entity: EntityState | undefined,
	local: EntityState | undefined,
	pressed = false,
	alt = false
): WorldCursor {
	if ( !entity || entity.appearanceState?.[0] === 2 ) return 0x95;
	if ( entity.kind === "ground-item" ) return pressed ? 0x9a : 0x99;
	if ( entity.kind === "teleport" ) return 0xa1;
	if ( entity.kind === "npc" ) return 0x98;
	if ( !local || entity.appearanceState?.[0] === 4 ) return 0x95;
	if ( entity.kind === "monster" ) return ((entity.attackFlags ?? 0x10) & 0x10) ? 0x97 : 0x95;
	if ( entity.kind === "player" ) return playerCursor( entity, local, alt );
	if ( entity.kind === "cos" ) {
		return (entity.hoverAttack ?? 0) & (alt ? HOVER_ATTACK_ALT : HOVER_ATTACK_PLAIN) ? 0xa0 : 0x95;
	}
	return 0x95;
}
