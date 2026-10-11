/*
===========================================================================

pet-assist.ts - attack pets join the local player's own attacks

The v1.150 client orders its attack pets in itself. When the server's cast
broadcast (B245) opens a cast of the local player
(CPSMission_OnSkillCastBroadcast0xB245 776830), the skill's motion record
has byte +0xBD set (the basic attacks and the attack skills; the server
projects it as assistsCompanions) and the cast has a target, each owned
attack pet in aggressive mode (+0xAB08 == 1, the 0x705B command mode) that
stands in the world and holds no assist target yet (+0xAB2C == 0) is sent
the same order as the command bar's attack button: 0x769E [u32 pet][u8 2]
[u32 target]. The target is remembered at +0xAB2C, so the pet is not
re-ordered on every swing. CICCos_OnUpdate (854B35) forgets it once the
target is gone, is no character, or is dead; the command bar's attack sets
it and its follow (tag 9) clears it (CICCos_ExecuteActionCommand 6A2350).

INFERENCE: 776830 also skips a pet whose action-state word has any bit of
0x476. The port tracks a pet's life (record dead, HP 0) but no other action
state; a pet already fighting holds an assist target and is skipped by
that gate instead.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import { castPhase } from "./combat-result";

// The cast broadcast's result byte for an opened cast.
const CAST_OPENED = 1;
// The cast header: result, sub-result, skill, caster, token, then the phase.
const CAST_SKILL_OFFSET = 2;
const CAST_CASTER_OFFSET = 6;
const CAST_PHASE_OFFSET = 14;
const CAST_HEADER_BYTES = 19;
// The attack pet's COS class (TID4) and its aggressive command mode.
const ATTACK_PET_BAND = 3;
const AGGRESSIVE_MODE = 1;
// The COS command opcode and the attack order's tag (6A2350 case 2).
const COS_COMMAND = 0x769e;
const COS_ATTACK = 2;
// appearanceState[0], the life state: 2 is dead.
const LIFE_DEAD = 2;

/*
================
AssistPet

What the assist reads of an owned companion record, and whether its actor
stands in the world.
================
*/
export interface AssistPet {
	readonly gid: number;
	readonly band: number;
	readonly commandMode?: number;
	readonly dead: boolean;
	readonly hp: number;
	readonly present: boolean;
}

/*
================
ownCastTarget

The skill and target of a B245 that opens a cast of the local player, or
null for any other broadcast (a refusal, a peer's or a monster's cast).
================
*/
export function ownCastTarget( payload: Uint8Array, localGid: number ): { skill: number; target: number; } | null {
	if ( payload[0] !== CAST_OPENED || payload.length < CAST_HEADER_BYTES || !localGid ) return null;
	const v = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	if ( v.getUint32( CAST_CASTER_OFFSET, true ) !== localGid ) return null;
	const target = castPhase( payload, CAST_PHASE_OFFSET )?.target ?? 0;
	return { skill: v.getUint32( CAST_SKILL_OFFSET, true ), target };
}

/*
================
petAssistOrders

The attack orders a cast at target sends (the caller has admitted the
cast: the local player's own, of a skill that assists, at a target that
stands in the world), and the assist targets after them.
================
*/
export function petAssistOrders(
	target: number,
	pets: Iterable<AssistPet>,
	assisting: ReadonlyMap<number, number>
): {
	readonly orders: readonly { readonly opcode: number; readonly payload: Uint8Array; }[];
	readonly assisting: ReadonlyMap<number, number>;
} {
	const orders: { opcode: number; payload: Uint8Array; }[] = [];
	if ( !target ) return { orders, assisting };
	const next = new Map( assisting );
	for ( const pet of pets ) {
		if (
			pet.band !== ATTACK_PET_BAND || pet.commandMode !== AGGRESSIVE_MODE || pet.dead || pet.hp === 0 ||
			!pet.present || next.get( pet.gid )
		) continue;
		const payload = new Uint8Array( 9 ), w = new DataView( payload.buffer );
		w.setUint32( 0, pet.gid, true );
		payload[4] = COS_ATTACK;
		w.setUint32( 5, target, true );
		orders.push( { opcode: COS_COMMAND, payload } );
		next.set( pet.gid, target );
	}
	return { orders, assisting: orders.length ? next : assisting };
}

/*
================
retainedPetAssists

CICCos_OnUpdate (854B35): an assist target that is gone, is no character
(a monster or a player), or is dead is forgotten, so the pet's next owner
attack orders it again. Pets no longer owned are dropped. targets holds the
current actor of each assist target (absent when it is gone).
================
*/
export function retainedPetAssists(
	assisting: ReadonlyMap<number, number>,
	owned: ReadonlySet<number>,
	targets: ReadonlyMap<number, EntityState>
): ReadonlyMap<number, number> {
	let next: Map<number, number> | undefined;
	for ( const [pet, target] of assisting ) {
		const row = targets.get( target );
		const keep = owned.has( pet ) && row !== undefined && (row.kind === "monster" || row.kind === "player") &&
			row.appearanceState?.[0] !== LIFE_DEAD;
		if ( keep ) continue;
		next ??= new Map( assisting );
		next.delete( pet );
	}
	return next ?? assisting;
}
