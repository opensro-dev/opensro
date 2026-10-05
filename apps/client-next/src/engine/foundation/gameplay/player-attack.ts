/*
===========================================================================

player-attack.ts - what a click on another player does, and when a pet
is sent at one

CGInterface_HandlePlayerInteraction (693E50, reached from
CGInterface_OnWorldClick) attacks a player only when it is already the
selected target or Alt is held, and CICUser_CanAttack (868F40) admits it.
A party member is never attacked. Below level 20 an attacker without a
PvP cape (CICPlayer_GetEquippedPvpCape 868F00) is refused with notice
4:0x16 unless its guild has war enemies (819FC0). The fortress-war
branches need a fortress war owner the port does not have; outside a war
they decide nothing.

===========================================================================
*/

import type { EntityState } from "@/engine/contracts/world";
import { entityHoldType, jobItemType, type NameColorContext, partyName } from "./name-color";

// PLAYER_ATTACK_MIN_LEVEL is 693EF9's local level (+0x820) < 0x14.
const PLAYER_ATTACK_MIN_LEVEL = 20;
// PVP_CAPE_ITEM is the job item type of TID 3/1/7/5 (RefObj_IsPvpCape 593310).
const PVP_CAPE_ITEM = 5;
// FREE_BATTLE_CAPE_TEAM is the cape group that opposes every group (868FF3).
const FREE_BATTLE_CAPE_TEAM = 5;
// NO_EVENT_TEAM is CICUser +0x7E1 outside an event match.
const NO_EVENT_TEAM = 0xff;
// TRADER, THIEF and HUNTER are the dressed job types (vtable +0xA8).
const TRADER = 1, THIEF = 2, HUNTER = 3;

/*
================
PlayerInteraction
================
*/
export type PlayerInteraction =
	| { readonly kind: "none"; }
	| { readonly kind: "attack"; }
	| { readonly kind: "low-level"; };

/*
================
PlayerInteractionInput

selected: the clicked player was the selection before this click. alt:
Alt was held. guildWar: the local guild has war enemies.
================
*/
export interface PlayerInteractionInput {
	readonly selected: boolean;
	readonly alt: boolean;
	readonly guildWar: boolean;
}

/*
================
hostileToLocalPlayer

CICharactor_IsHostileToLocalPlayer (858FC0) for a player: the local
player's last attacker, an opposing job (a trader or hunter facing a
thief, either way round), or a player in PvP state 1 or 2.
================
*/
export function hostileToLocalPlayer( target: EntityState, c: NameColorContext ): boolean {
	if ( c.attackedName !== undefined && target.name === c.attackedName ) return true;
	const local = entityHoldType( c.local ), other = entityHoldType( target );
	if ( (local === TRADER || local === HUNTER) && other === THIEF ) return true;
	if ( (other === TRADER || other === HUNTER) && local === THIEF ) return true;
	const pvp = target.pvpState ?? 0;
	return pvp === 1 || pvp === 2;
}

/*
================
canAttackPlayer

CICUser_CanAttack (868F40). An event match decides by team alone; then a
hostile player or Alt, a guild at war, or opposing PvP capes (different
groups, or both the free-battle group) admit.
================
*/
export function canAttackPlayer( target: EntityState, c: NameColorContext, alt: boolean ): boolean {
	const team = c.local.arenaTeam ?? NO_EVENT_TEAM;
	if ( team !== NO_EVENT_TEAM ) return team !== (target.arenaTeam ?? NO_EVENT_TEAM);
	if ( hostileToLocalPlayer( target, c ) || alt ) return true;
	if ( target.guildName !== undefined && c.social.wars?.some( row => row.name === target.guildName ) ) return true;
	const targetCape = target.equipment?.find( row => row.slot === 8 );
	if ( !targetCape || !c.localItem ) return false;
	if (
		jobItemType( targetCape.typeFlags ) !== PVP_CAPE_ITEM || jobItemType( c.localItem.typeFlags ) !== PVP_CAPE_ITEM
	) {
		return false;
	}
	const theirs = c.capeTeam( targetCape.refObjId ), ours = c.capeTeam( c.localItem.refObjId );
	if ( theirs === undefined || ours === undefined ) throw Error( "Missing native PvP cape team parameter" );
	return theirs !== ours || ours === FREE_BATTLE_CAPE_TEAM;
}

/*
================
playerInteraction

693E50's decision for a click on another player.
================
*/
export function playerInteraction(
	target: EntityState,
	c: NameColorContext,
	input: PlayerInteractionInput
): PlayerInteraction {
	if ( target.kind !== "player" || (!input.selected && !input.alt) ) return { kind: "none" };
	if ( !canAttackPlayer( target, c, input.alt ) ) return { kind: "none" };
	if ( partyName( c.social, target.name ) ) return { kind: "none" };
	const caped = c.localItem !== undefined && jobItemType( c.localItem.typeFlags ) === PVP_CAPE_ITEM;
	if ( !caped && (c.local.level ?? 0) < PLAYER_ATTACK_MIN_LEVEL && !input.guildWar ) return { kind: "low-level" };
	return { kind: "attack" };
}

/*
================
petPlayerAttack

CICCos_ExecuteActionCommand (6A2350) case 2 for a player target: an attack
pet is sent at a player who is no party member, whom CICUser_CanAttack
admits, and only by an owner of level 20 or above (else notice 4:0x16).
================
*/
export function petPlayerAttack( target: EntityState, c: NameColorContext, alt: boolean ): PlayerInteraction {
	if ( target.kind !== "player" || partyName( c.social, target.name ) ) return { kind: "none" };
	if ( !canAttackPlayer( target, c, alt ) ) return { kind: "none" };
	return (c.local.level ?? 0) < PLAYER_ATTACK_MIN_LEVEL ? { kind: "low-level" } : { kind: "attack" };
}
