/*
===========================================================================

withdrawal.ts - native restoration requests and learned-rank receipts

The potion opens a choice; only confirmation sends a request. A receipt
changes the learned group, while inventory and SP follow their own server
updates. An old-ID receipt removes the group, as native 75BCE0 does.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { WireFrame } from "@/engine/contracts/network";
import type { SkillMetadata } from "./skill-catalog";
import type { QuickSlot } from "./quickslots";

const MAX_ID = 0xffffffff;
const MAX_RANK = 255;
const RESTORATION_POTION_TYPE = 0x06ec;
export const SKILL_WITHDRAWAL_RESPONSE = 0xb4d6;
export const MASTERY_WITHDRAWAL_RESPONSE = 0xb606;

/*
================
WithdrawalCommand

Rank is the desired final rank, not the number of potions to spend.
================
*/
export interface WithdrawalCommand {
	readonly kind: "skill-withdraw" | "mastery-withdraw";
	readonly potion: number;
	readonly id: number;
	readonly rank: number;
}

/*
================
isRestorationPotion

The v1.150 3/3/13/0 family contains the mall and Old Woman potions. Use its
wire type, because inventory names are localized presentation. The server
still validates the exact reference whitelist at confirmation (5169D0).
================
*/
export function isRestorationPotion( item: InventoryItem ): boolean {
	return item.typeFlags === RESTORATION_POTION_TYPE;
}

/*
================
withdrawalRequest
================
*/
export function withdrawalRequest( command: WithdrawalCommand ): WireFrame {
	for ( const id of [ command.potion, command.id ] ) {
		if ( !Number.isSafeInteger( id ) || id <= 0 || id > MAX_ID ) throw Error( "Invalid restoration identity" );
	}
	if ( !Number.isInteger( command.rank ) || command.rank < 0 || command.rank > MAX_RANK ) {
		throw Error( "Invalid restoration rank" );
	}
	const payload = new Uint8Array( 9 ), view = new DataView( payload.buffer );
	view.setUint32( 0, command.potion, true );
	view.setUint32( 4, command.id, true );
	payload[8] = command.rank;
	return { opcode: command.kind === "skill-withdraw" ? 0x74d6 : 0x7606, payload };
}

/*
================
withdrawalSkillBindings

Downgrade every hotbar occurrence together. Removal clears bindings instead
of leaving a retired rank selectable until the next login.
================
*/
export function withdrawalSkillBindings(
	bindings: { readonly skills: readonly number[]; readonly quickSlots: readonly QuickSlot[]; },
	catalog: readonly SkillMetadata[],
	id: number
) {
	const replacement = catalog.find( row => row.id === id );
	if ( !replacement ) throw Error( "Missing restored skill reference" );
	const old = catalog.find( row => row.group === replacement.group && bindings.skills.includes( row.id ) );
	if ( !old || replacement.level > old.level ) throw Error( "Invalid restoration rank receipt" );
	const removed = old.id === id;
	return {
		skills: removed ?
			bindings.skills.filter( learned => learned !== old.id ) :
			bindings.skills.map( learned => learned === old.id ? id : learned ),
		quickSlots: bindings.quickSlots.map( slot =>
			slot.kind === 0x49 && slot.payload === old.id ?
				(removed ? { ...slot, kind: 0, payload: 0 } : { ...slot, payload: id }) :
				slot
		)
	};
}
