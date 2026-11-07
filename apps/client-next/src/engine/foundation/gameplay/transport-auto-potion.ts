/*
===========================================================================

transport-auto-potion.ts - shared companion HP potions and abnormal cures

Port-only, not native: the Experimental companion auto-potion option.
The T dialog binds one HP potion and one pet universal pill for owned riding
transports, trade transports and attack pets, with separate HP thresholds.
Item use remains the native request: the server validates every consumption.
Off is the native player-only automatic recovery.

===========================================================================
*/
import type { CosRecord, InventoryItem } from "@/engine/contracts/gameplay";
import { itemCooldown, type ItemCooldown } from "./item-cooldowns";
import { autoPotionEntry, autoPotionItemSlot } from "./auto-potion";
import type { QuickSlot } from "./quickslots";

// ETC 3/3/1/4: the transport and pet HP potion family.
const ETC_FAMILY_MASK = 0x7c;
const ETC_FAMILY = 0x6c;
const POTION_GROUP = 1;
const COS_HP_POTION_SUBTYPE = 4;
// A sent use waits for its answer (inventoryPending); a refused one is
// retried after the server's 1100 ms lane plus one 100 ms scheduling margin.
export const TRANSPORT_POTION_RETRY_MS = 1200;

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
CompanionPotionInput

Port-only, not native. Maxima and abnormal masks are authoritative snapshots;
never infer missing HP from a selected actor or cure an unrelated companion.
================
*/
export interface CompanionPotionInput {
	readonly records: readonly CosRecord[];
	readonly items: readonly InventoryItem[];
	readonly cooldowns: readonly ItemCooldown[];
	readonly now: number;
	readonly lastSentMs?: number;
	readonly blocked: boolean;
	readonly settings: {
		readonly transport: number;
		readonly attack: number;
		readonly bindings: { readonly hp: number; readonly cure: number; };
	};
	readonly quickSlots: readonly QuickSlot[];
	readonly maxima: readonly { readonly gid: number; readonly maxHp: number; }[];
	readonly vitals: readonly { readonly gid: number; readonly abnormal?: number; }[];
}
/*
================
companionAutoPotion

One request per retry interval across both companions. Shared item cooldowns
remain server-owned. Cures precede HP only on an actually afflicted companion.
================
*/
export function companionAutoPotion(
	input: CompanionPotionInput
): { readonly slot: number; readonly companionGid: number; } | null {
	if (
		input.blocked ||
		input.lastSentMs !== undefined && input.now >= input.lastSentMs &&
			input.now - input.lastSentMs < TRANSPORT_POTION_RETRY_MS
	) return null;
	const bindings = input.settings.bindings;
	for ( const kind of [ "cure", "hp" ] as const ) {
		const entry = autoPotionEntry( bindings[kind] );
		if ( kind === "cure" && !entry.enabled ) continue;
		const binding = input.quickSlots.find( row => row.slot === entry.slot );
		const slot = binding ? autoPotionItemSlot( binding, input.items ) : null;
		const item = slot === null ? undefined : input.items.find( row => row.slot === slot );
		if ( !item || item.quantity <= 0 || itemCooldown( input.cooldowns, item, input.now ) ) continue;
		const flags = item.typeFlags;
		if (
			kind === "hp" ?
				!isTransportHpPotion( flags ) :
				!((flags & ETC_FAMILY_MASK) === ETC_FAMILY && (flags >>> 7 & 15) === 2 && (flags >>> 11 & 31) === 7)
		) continue;
		for ( const record of input.records ) {
			if ( record.dead || record.hp <= 0 || record.band !== 1 && record.band !== 2 && record.band !== 3 ) {
				continue;
			}
			const channel = autoPotionEntry( input.settings[record.band === 3 ? "attack" : "transport"] );
			if ( kind === "hp" && !channel.enabled ) continue;
			if ( kind === "cure" ) {
				// Port-only: attempt the pill for any negative status; the server owns cure eligibility.
				if ( (input.vitals.find( vital => vital.gid === record.gid )?.abnormal ?? 0) === 0 ) continue;
			} else {
				const maxHp = input.maxima.find( row => row.gid === record.gid )?.maxHp ?? 0;
				if (
					!(maxHp > 0) || record.hp >= maxHp ||
					record.hp > Math.trunc( maxHp * Math.fround( channel.percent / 100 ) )
				) continue;
			}
			return { slot: item.slot, companionGid: record.gid };
		}
	}
	return null;
}

/*
================
PlayerVigorInput

Port-only recovery channels; the native player settings packet stays unchanged.
================
*/
export interface PlayerVigorInput
	extends Pick<CompanionPotionInput, "items" | "quickSlots" | "cooldowns" | "now" | "lastSentMs" | "blocked"> {
	readonly settings: { readonly vigorHp: number; readonly vigorMp: number; };
	readonly vital?: {
		readonly hp?: number;
		readonly mp?: number;
		readonly maxHp?: number;
		readonly maxMp?: number;
		readonly abnormal?: number;
	};
}
/*
================
playerVigorAutoPotion

Both thresholds may bind one vigor stack; admit only one request per retry.
================
*/
export function playerVigorAutoPotion( input: PlayerVigorInput ): { readonly slot: number; } | null {
	const v = input.vital;
	if (
		input.blocked || !(v?.hp && v.hp > 0) ||
		input.lastSentMs !== undefined && input.now >= input.lastSentMs &&
			input.now - input.lastSentMs < TRANSPORT_POTION_RETRY_MS
	) return null;
	for ( const key of [ "vigorHp", "vigorMp" ] as const ) {
		const entry = autoPotionEntry( input.settings[key] ), hp = key === "vigorHp";
		const current = hp ? v.hp : v.mp, max = hp ? v.maxHp : v.maxMp;
		if (
			!entry.enabled || current === undefined || !max || current >= max ||
			current > Math.trunc( max * entry.percent / 100 ) || hp && ((v.abnormal ?? 0) & 0x20) !== 0
		) continue;
		const binding = input.quickSlots.find( row => row.slot === entry.slot );
		const slot = binding ? autoPotionItemSlot( binding, input.items ) : null;
		const item = input.items.find( row => row.slot === slot );
		if (
			!item || item.quantity <= 0 || (item.typeFlags & 0xffff) !== 0x18ec ||
			itemCooldown( input.cooldowns, item, input.now )
		) continue;
		return { slot: item.slot };
	}
	return null;
}
