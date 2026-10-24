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
A halt (0xB2F5) naming an owned pet that holds a target also starts its
countdown (+0xAB30 = 3000 ms, 775B50 at 0x775BE9); the per-frame COS tick
(77DFA0) runs it down and forgets the target at zero, and the pet's own
cast broadcast cancels it (776830 at 0x7769D8) - so a pet stopped short
of its target is ordered again on the owner's next swing.

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
// The cast-kind byte: 776830 lets only 0 (a normal cast) and 2 (a
// continuous one) past its entry; any other kind returns early.
const CAST_KIND_OFFSET = 1;
const CAST_KIND_NORMAL = 0;
const CAST_KIND_CONTINUOUS = 2;
// The cast header: result, cast kind, skill, caster, token, then the phase.
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
// The halt countdown (+0xAB30): 3000 ms from the halt (0x775BE9 writes
// 0xBB8), -1 (no entry here) while none runs.
export const PET_ASSIST_COUNTDOWN_MS = 3000;

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
haltedPetAssistCountdowns

775B50 (0x775BDE-0x775BE9): a 0xB2F5 naming an owned COS that holds an
assist target starts its countdown (+0xAB30 = 3000) when none is running
(+0xAB30 < 0). The map holds worker-clock deadlines; no entry is -1.

The handler returns earlier still, at 0x775B9A, when the named entity's
+0x434 is nonzero - the mounted flag (CICharactor_SetMountedActionState
8582E0) - so a mounted entity never arms a countdown. The port needs no
state for that gate: this arm only fires for gids in `assisting`, which
holds owned attack pets, and a mounted rider is a character, never one
of those keys, so both handlers arm on the same halts.
================
*/
export function haltedPetAssistCountdowns(
	countdowns: ReadonlyMap<number, number>,
	pet: number,
	assisting: ReadonlyMap<number, number>,
	now: number
): ReadonlyMap<number, number> {
	if ( !assisting.has( pet ) || countdowns.has( pet ) ) return countdowns;
	const next = new Map( countdowns );
	next.set( pet, now + PET_ASSIST_COUNTDOWN_MS );
	return next;
}

/*
================
castPetAssistCountdowns

776830 (0x7769D8): a cast broadcast whose caster is an owned COS cancels
its countdown (+0xAB30 = -1), so the pet keeps its assist target. The
store sits past the handler's two entry gates - the result byte must be
1 (0x776869; anything else takes the refusal tail) and the cast kind
must be 0 or 2 - and is itself ungated once they pass, so this reads
the same two bytes. The port's cast parser refuses every other result
shape before this runs; the gates keep the cancel honest on its own.
================
*/
export function castPetAssistCountdowns(
	countdowns: ReadonlyMap<number, number>,
	payload: Uint8Array
): ReadonlyMap<number, number> {
	if (
		payload.length < CAST_CASTER_OFFSET + 4 || payload[0] !== CAST_OPENED ||
		(payload[CAST_KIND_OFFSET] !== CAST_KIND_NORMAL && payload[CAST_KIND_OFFSET] !== CAST_KIND_CONTINUOUS)
	) return countdowns;
	const caster = new DataView( payload.buffer, payload.byteOffset, payload.byteLength )
		.getUint32( CAST_CASTER_OFFSET, true );
	if ( !countdowns.has( caster ) ) return countdowns;
	const next = new Map( countdowns );
	next.delete( caster );
	return next;
}

/*
================
advancedPetAssistCountdowns

77DFA0 (the per-frame COS tick): the countdown runs down by the frame's
elapsed time; at zero the assist target is forgotten (+0xAB2C = 0) and
the countdown resets to -1, as it also does when the target is already
gone. The port keeps worker-clock deadlines instead of a per-frame
decrement: a step at or past the deadline is the zero crossing.
================
*/
export function advancedPetAssistCountdowns(
	assisting: ReadonlyMap<number, number>,
	countdowns: ReadonlyMap<number, number>,
	now: number
): {
	readonly assisting: ReadonlyMap<number, number>;
	readonly countdowns: ReadonlyMap<number, number>;
} {
	let nextAssisting: Map<number, number> | undefined, nextCountdowns: Map<number, number> | undefined;
	for ( const [pet, deadline] of countdowns ) {
		if ( !assisting.has( pet ) ) {
			(nextCountdowns ??= new Map( countdowns )).delete( pet );
		} else if ( now >= deadline ) {
			(nextCountdowns ??= new Map( countdowns )).delete( pet );
			(nextAssisting ??= new Map( assisting )).delete( pet );
		}
	}
	return { assisting: nextAssisting ?? assisting, countdowns: nextCountdowns ?? countdowns };
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
