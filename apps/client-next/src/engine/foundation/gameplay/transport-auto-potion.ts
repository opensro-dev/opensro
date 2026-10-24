/*
===========================================================================

transport-auto-potion.ts - the trade transport's own automatic HP potion

Port-only, not native (owner, 2026-10-24, trader thread item 6): the
Experimental row "Transport auto-potion". v1.150's automatic potion
(70C550) watches only the player: a transport potion on its HP quickslot
is spent on the selected companion when the PLAYER's HP drops. With the
row on, the player's own summoned transport is watched instead: when its
HP falls to the auto-potion HP percentage, a transport HP potion
(ITEM_ETC_COS_HP_POTION_*, ETC 3/3/1/4) from the bag is used on it through
the ordinary item use, so the server keeps every rule (range, cooldown,
death). Off, the default, is the native client.

===========================================================================
*/
import type { CosRecord, InventoryItem } from "@/engine/contracts/gameplay";
import { itemCooldown, type ItemCooldown } from "./item-cooldowns";

// A trade transport's COS band (the reference's TID4, as cosClass reads it).
const TRANSPORT_BAND = 2;
// ETC 3/3/1/4: the transport and pet HP potion family.
const ETC_FAMILY_MASK = 0x7c;
const ETC_FAMILY = 0x6c;
const POTION_GROUP = 1;
const COS_HP_POTION_SUBTYPE = 4;
// The first bag slot after the equipment.
const FIRST_BAG_SLOT = 13;
// A sent use waits for its answer (inventoryPending); a refused one is
// retried no faster than this.
export const TRANSPORT_POTION_RETRY_MS = 1000;

/*
================
TransportPotionInput
================
*/
export interface TransportPotionInput {
	readonly records: readonly CosRecord[];
	// The transport reference's maximum HP, 0 when it is not loaded.
	readonly maxHp: ( refObjId: number ) => number;
	readonly items: readonly InventoryItem[];
	readonly cooldowns: readonly ItemCooldown[];
	// The native auto-potion HP percentage (1-100).
	readonly percent: number;
	readonly now: number;
	// When the last automatic use was sent, or undefined.
	readonly lastSentMs?: number;
	// An inventory transaction is open, or a window that refuses item use
	// (stall, shop, storage) is open.
	readonly blocked: boolean;
}

/*
================
transportPotionBlocked

The windows the native automatic use refuses beside (561D50..561E6B): an
open stall, an NPC conversation (its shop) and the warehouse; and any
inventory transaction still waiting for its answer.
================
*/
export function transportPotionBlocked( game: {
	readonly inventoryPending?: boolean;
	readonly storage?: unknown;
	readonly stall?: { readonly phase: string; };
	readonly npcConversation?: { readonly phase: string; };
} ): boolean {
	const stall = game.stall?.phase ?? "none";
	return !!game.inventoryPending || !!game.storage || stall === "owner" || stall === "visitor" ||
		(game.npcConversation?.phase ?? "closed") !== "closed";
}

/*
================
isTransportHpPotion
================
*/
export function isTransportHpPotion( typeFlags: number ): boolean {
	return (typeFlags & ETC_FAMILY_MASK) === ETC_FAMILY && (typeFlags >>> 7 & 15) === POTION_GROUP &&
		(typeFlags >>> 11 & 31) === COS_HP_POTION_SUBTYPE;
}

/*
================
transportAutoPotion

The use to send now, or null. The threshold is the native auto-potion
arithmetic (70C28F): percent / 100 as float32, times the maximum,
truncated; at or below it the transport is healed.
================
*/
export function transportAutoPotion(
	input: TransportPotionInput
): { readonly slot: number; readonly companionGid: number; } | null {
	if ( input.blocked ) return null;
	if ( input.lastSentMs !== undefined && input.now - input.lastSentMs < TRANSPORT_POTION_RETRY_MS ) return null;
	const transport = input.records.find( record => record.band === TRANSPORT_BAND && !record.dead && record.hp > 0 );
	if ( !transport ) return null;
	const maxHp = input.maxHp( transport.refObjId );
	if ( !(maxHp > 0) ) return null;
	const threshold = Math.trunc( maxHp * Math.fround( input.percent / 100 ) ) >>> 0;
	if ( transport.hp > threshold ) return null;
	const potion = input.items
		.filter( item =>
			item.slot >= FIRST_BAG_SLOT && item.quantity > 0 && isTransportHpPotion( item.typeFlags ) &&
			!itemCooldown( input.cooldowns, item, input.now )
		)
		.sort( ( a, b ) => a.slot - b.slot )[0];
	if ( !potion ) return null;
	return { slot: potion.slot, companionGid: transport.gid };
}
