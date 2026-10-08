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

// COS_TYPE_WORD is TID 1/2/3 of a COS type word (CICCos +0x648, the entity's
// tidWord); TID4 sits
// in bits 11..15: horse 1, transport 2, attack pet 3, pickup pet 4 and guild
// soldier 5 (RefObjTypeFlags_IsCos* 5500B0 / 5500F0 / 582160).
const COS_TYPE_MASK = 0x7fe, COS_TYPE_WORD = 0x1c6;
const COS_HORSE = 1, COS_TRANSPORT = 2, COS_ATTACK_PET = 3, COS_GUILD_SOLDIER = 5;
// Category-4 notices 6FCD50 raises for an offensive skill at a party member
// or a party member's pet.
export const SKILL_AT_PARTY_MEMBER_NOTICE = 0x22;
export const SKILL_AT_PARTY_PET_NOTICE = 0x23;

/*
================
CosRelations

Looks up the entities a pet's admission defers to: its owner (CICCos
+0x774) and, for a horse, its rider.
================
*/
export interface CosRelations {
	readonly entity: ( gid: number ) => EntityState | undefined;
	readonly rider: ( gid: number ) => EntityState | undefined;
}

/*
================
canAttackCos

CICCos_CanAttack (854860), vtable +0xA4 of a pet. A horse answers for its
rider and a transport for itself; an attack pet or guild soldier answers
for its owning player, or for itself when the owner is out of sight; a
pickup pet is never attacked.
================
*/
export function canAttackCos( target: EntityState, c: NameColorContext, alt: boolean, r: CosRelations ): boolean {
	const word = target.tidWord ?? 0;
	if ( (word & COS_TYPE_MASK) !== COS_TYPE_WORD ) return false;
	const tid4 = word >>> 11 & 31;
	if ( tid4 === COS_HORSE ) {
		const rider = r.rider( target.gid );
		return rider?.kind === "player" ? canAttackPlayer( rider, c, alt ) : false;
	}
	if ( tid4 !== COS_TRANSPORT && tid4 !== COS_ATTACK_PET && tid4 !== COS_GUILD_SOLDIER ) return false;
	if ( tid4 !== COS_TRANSPORT ) {
		const owner = target.ownerGid === undefined ? undefined : r.entity( target.ownerGid );
		if ( owner?.kind === "player" ) return canAttackPlayer( owner, c, alt );
	}
	return alt || hostileToLocalPlayer( target, c );
}

/*
================
SkillTargetAdmission
================
*/
export type SkillTargetAdmission =
	| { readonly kind: "cast"; }
	| { readonly kind: "none"; }
	| { readonly kind: "notice"; readonly code: number; };

/*
================
skillTargetAdmission

CGInterface_ExecuteSelectedActionAtTarget (6FCD50) before it sends an
offensive skill (CSkillData_IsOffensiveSkill 7F85A0) at the selection:

- a player must pass CICUser_CanAttack with the Alt state
  (GetKeyState(VK_MENU)); a party member then raises 4:0x22;
- a pet with Alt held is cast at unless its owner is in the party
  (4:0x23); without Alt it must pass CICCos_CanAttack.

Anything else, and every non-offensive skill, is cast. Without this a
hotbar skill attacked a white player with no Alt held.
================
*/
export function skillTargetAdmission(
	target: EntityState,
	c: NameColorContext,
	alt: boolean,
	offensive: boolean,
	r: CosRelations
): SkillTargetAdmission {
	if ( !offensive ) return { kind: "cast" };
	if ( target.kind === "player" ) {
		if ( !canAttackPlayer( target, c, alt ) ) return { kind: "none" };
		return partyName( c.social, target.name ) ?
			{ kind: "notice", code: SKILL_AT_PARTY_MEMBER_NOTICE } :
			{ kind: "cast" };
	}
	if ( target.kind !== "cos" ) return { kind: "cast" };
	if ( alt ) {
		const owner = target.ownerGid === undefined ? undefined : r.entity( target.ownerGid );
		return owner?.kind === "player" && partyName( c.social, owner.name ) ?
			{ kind: "notice", code: SKILL_AT_PARTY_PET_NOTICE } :
			{ kind: "cast" };
	}
	return canAttackCos( target, c, false, r ) ? { kind: "cast" } : { kind: "none" };
}

// HOVER_ATTACK_PLAIN and HOVER_ATTACK_ALT are EntityState.hoverAttack's
// bits: the hover target passes vtable +0xA4 with Alt up, or with Alt down.
export const HOVER_ATTACK_PLAIN = 1, HOVER_ATTACK_ALT = 2;

/*
================
hoverAttack

CGInterface_OnTimerEvent (6875F0) case 0, the hover cursor: it reads
GetKeyState(VK_MENU) every tick and asks the hovered player or pet's
vtable +0xA4 (CICUser_CanAttack / CICCos_CanAttack) with it. A party
member is never an attack target, nor is one of the local player's own
pets (CICPlayer_GetCosDataManager +0x18D4 holds it). Both Alt states are
judged here so the display thread can follow the key without a round
trip; anything else answers 0.
================
*/
export function hoverAttack( target: EntityState, c: NameColorContext, r: CosRelations ): number {
	if ( target.kind === "player" ) {
		if ( partyName( c.social, target.name ) ) return 0;
		return (canAttackPlayer( target, c, false ) ? HOVER_ATTACK_PLAIN : 0) |
			(canAttackPlayer( target, c, true ) ? HOVER_ATTACK_ALT : 0);
	}
	if ( target.kind !== "cos" || target.ownerGid === c.local.gid ) return 0;
	return (canAttackCos( target, c, false, r ) ? HOVER_ATTACK_PLAIN : 0) |
		(canAttackCos( target, c, true, r ) ? HOVER_ATTACK_ALT : 0);
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
