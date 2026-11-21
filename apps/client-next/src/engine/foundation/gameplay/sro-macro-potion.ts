/*
===========================================================================

sro-macro-potion.ts - the SRO Macro's character auto-potion loop (port-only)

Port-only, not native (#3907): the v1.696 macro's auto-potion, read from
the v1.696 client (research note sro-macro.md, "Auto-potion engine").
Each tick the executor (006931b0) walks its rows in order and the first
row whose reader fires uses its item; that row then sits out a 500 ms
re-arm (table 0x17b3688) and rejoins at the back of the order.

This module is pure: the rows' conditions and the ready/waiting order.
The worker supplies the vitals and performs the use through the same
inventory gates as the v1.150 auto-potion.

Row 3 (UIIT "European Status Anomaly") keeps its settings but never fires
here: its reader (006930a0) is a buff-item gate (item data +0x4d0 -> buff
id, fire only while that buff is absent), and that link is not read yet.
The pet rows (4..8) are not automated.

===========================================================================
*/
import type { AutoPotionFacts } from "./auto-potion";
import {
	MACRO_POTION_SLOTS,
	MACRO_ROW_CURE,
	MACRO_ROW_HP,
	MACRO_ROW_MP,
	type MacroSettings,
	potionRowOn,
	potionSlot,
	withPotionRowItem
} from "./sro-macro";

// UIIT "European Status Anomaly" — reader 006930a0 / drop 0x690c70.
const MACRO_ROW_BUFF_ITEM = 3;

// 00692ba0 seeds every row's re-arm with 0x1f4.
export const MACRO_POTION_REARM_MS = 500;
// 00692e00 refuses HP while this abnormal bit stands, as 70C550 does in v1.150.
const HP_BLOCKING_ABNORMAL = 0x20;
// 00692d20 fires while any of these abnormal bits stands.
export const MACRO_CURE_ABNORMAL = 0x17fafff;
// 00692e00 / 00692f60 scale the percent byte by this double (0x12befb8).
const PERCENT_SCALE = 0.01;
// Slots below this are the equipment sockets, never a potion source.
export const BAG_FIRST_SLOT = 13;
// Expendable class bits, same mask autoPotionItemSlot reads.
const CONSUMABLE_MASK = 0x1c;
const CONSUMABLE = 0x0c;
const CONSUMABLE_BAND = 0x60;
/*
================
macroPotionAdmits

CNIFMacroAutoPotionSlot drop (0xdafff0): kind 0x46 then per-row TypeId
checks (0x690b70 HP, 0x690bf0 MP, 0x690b00 cure, 0x690c70 buff-item).
The port's typeFlags pack TID3 at bits 7..10 and TID4 at bits 11+
(item-cooldowns / mall-use).

Every row requires the expendable band (typeFlags & 0x60) === 0x60 —
equipment shares class bits 0x0c but sits in band 0x20. Alchemy stones
and elixirs (TID3 10/11) never bind on any row.
================
*/
export function macroPotionAdmits( row: number, typeFlags: number ): boolean {
	if ( !Number.isInteger( row ) || row < 0 || row >= 9 ) return false;
	if (
		(typeFlags & 2) !== 0 || (typeFlags & CONSUMABLE_MASK) !== CONSUMABLE ||
		(typeFlags & CONSUMABLE_BAND) !== CONSUMABLE_BAND
	) {
		return false;
	}
	const tid3 = typeFlags >>> 7 & 15;
	const tid4 = typeFlags >>> 11 & 31;
	// Elixirs / lucky powder (10) and stones / tablets (11).
	if ( tid3 === 10 || tid3 === 11 ) return false;
	if ( row === MACRO_ROW_HP ) return tid3 === 1 || tid3 === 3;
	if ( row === MACRO_ROW_MP ) return tid3 === 2 || tid3 === 3;
	// 690ec0 (pill mid-byte) + high byte in {1,6,8,9}: port maps to the
	// 0x16c cure family (item-cooldowns' universal pill lane) with those TID4s.
	if ( row === MACRO_ROW_CURE ) {
		return (typeFlags & 0x7fe) === 0x16c && (tid4 === 1 || tid4 === 6 || tid4 === 8 || tid4 === 9);
	}
	// 690c70 is a CodeName whitelist of speed scrolls. Without CodeName on
	// the bag row, admit only scroll-like TID3s (3 etc / 13 mall) — never
	// potions, pills, or alchemy elements.
	if ( row === MACRO_ROW_BUFF_ITEM ) return tid3 === 3 || tid3 === 13;
	return true;
}

/*
================
scrubMacroPotion

Drop any bound RefItem whose bag stack fails macroPotionAdmits (equipment
left on the anomaly rows from an earlier looser check). Unknown refs that
are not in the bag are left alone.
================
*/
export function scrubMacroPotion(
	settings: MacroSettings,
	inventory: readonly { readonly slot: number; readonly refObjId: number; readonly typeFlags: number; }[]
): MacroSettings {
	let potion = settings.potion;
	for ( let row = 0; row < MACRO_POTION_SLOTS; row++ ) {
		const ref = potionSlot( potion, row ).item;
		if ( !ref ) continue;
		const item = inventory.find( entry => entry.slot >= BAG_FIRST_SLOT && entry.refObjId === ref );
		if ( item && !macroPotionAdmits( row, item.typeFlags ) ) {
			potion = withPotionRowItem( potion, row, 0 );
		}
	}
	return potion === settings.potion ? settings : { ...settings, potion };
}

export type MacroPotionRow = typeof MACRO_ROW_HP | typeof MACRO_ROW_MP | typeof MACRO_ROW_CURE;
// MACRO_ROW_HP, MACRO_ROW_MP, MACRO_ROW_CURE: the executor's initial order.
export const MACRO_POTION_ORDER = [ 0, 1, 2 ] as const;

/*
================
MacroPotionQueue

ready is the executor's ordered list; waiting holds the rows that fired,
each with the time its re-arm runs out.
================
*/
export interface MacroPotionQueue {
	readonly ready: readonly MacroPotionRow[];
	readonly waiting: readonly { readonly row: MacroPotionRow; readonly readyAt: number; }[];
}

/*
================
emptyMacroPotionQueue
================
*/
export function emptyMacroPotionQueue(): MacroPotionQueue {
	return { ready: MACRO_POTION_ORDER, waiting: [] };
}

/*
================
macroPotionWants

Whether a row's reader would try its item now: on, an item bound, and its
vital condition met. The item's presence, cooldown and use are the
worker's.
================
*/
export function macroPotionWants( potion: readonly number[], row: MacroPotionRow, facts: AutoPotionFacts ): boolean {
	if ( !facts.alive || !potionRowOn( potion, row ) ) return false;
	const slot = potionSlot( potion, row );
	if ( slot.item === 0 ) return false;
	if ( row === MACRO_ROW_CURE ) return (facts.abnormal & MACRO_CURE_ABNORMAL) !== 0;
	if ( row === MACRO_ROW_HP && (facts.abnormal & HP_BLOCKING_ABNORMAL) !== 0 ) return false;
	const current = row === MACRO_ROW_HP ? facts.hp : facts.mp;
	const max = row === MACRO_ROW_HP ? facts.maxHp : facts.maxMp;
	return max * (slot.a * PERCENT_SCALE) >= current;
}

/*
================
macroPotionBagSlot

The bag slot a row uses: the first stack of its RefItem ID. The v1.696 UI
resolves the ID to one slot when it loads the row (00db29e0); taking the
first live stack each tick is the inferred equivalent once that stack is
spent.
================
*/
export function macroPotionBagSlot(
	potion: readonly number[],
	row: MacroPotionRow,
	inventory: readonly { readonly slot: number; readonly refObjId: number; }[]
): number | undefined {
	const item = potionSlot( potion, row ).item;
	if ( item === 0 ) return undefined;
	let best: number | undefined;
	for ( const entry of inventory ) {
		if ( entry.slot >= BAG_FIRST_SLOT && entry.refObjId === item && (best === undefined || entry.slot < best) ) {
			best = entry.slot;
		}
	}
	return best;
}

/*
================
rearmMacroPotion

The start of an executor tick: the first waiting row whose re-arm ran out
returns to the back of the order, one per tick, as 006931b0 does. The
worker then walks ready in order and hands the first row whose use went
out to macroPotionFired.
================
*/
export function rearmMacroPotion( queue: MacroPotionQueue, now: number ): MacroPotionQueue {
	const back = queue.waiting.findIndex( entry => now >= entry.readyAt );
	if ( back < 0 ) return queue;
	return {
		ready: [ ...queue.ready, queue.waiting[back]!.row ],
		waiting: queue.waiting.filter( ( _, i ) => i !== back )
	};
}

/*
================
macroPotionFired

A row whose use went out leaves the order for its 500 ms re-arm.
================
*/
export function macroPotionFired( queue: MacroPotionQueue, row: MacroPotionRow, now: number ): MacroPotionQueue {
	return {
		ready: queue.ready.filter( r => r !== row ),
		waiting: [ ...queue.waiting, { row, readyAt: now + MACRO_POTION_REARM_MS } ]
	};
}
