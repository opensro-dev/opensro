/*
===========================================================================

target-player.ts - the player a selected target stands for

Several windows act on "the selected player" and read the selection the
same way: the target itself when it is another player, or the rider of a
selected pet or transport that has a player on it. CIFFriend_OnAddButton
(602010) takes the name this way, the guild invite
(NetClient_SendTargetCommand700B50) the gid.

===========================================================================
*/

/*
================
TargetEntity

The entity fields the rule reads.
================
*/
export interface TargetEntity {
	readonly gid: number;
	readonly kind: string;
	readonly name?: string;
	readonly mountedOn?: number;
}

/*
================
targetPlayer

The player the selected target stands for, or undefined: no target, the
local player, a target out of view, or one that is neither another player
nor ridden by one.
================
*/
export function targetPlayer<T extends TargetEntity>(
	target: number | undefined,
	entities: readonly T[],
	localGid: number | undefined
): T | undefined {
	if ( !target || target === localGid ) return undefined;
	const selected = entities.find( entity => entity.gid === target );
	if ( !selected ) return undefined;
	if ( selected.kind === "player" ) return selected;
	return entities.find( entity => entity.kind === "player" && entity.mountedOn === target );
}
