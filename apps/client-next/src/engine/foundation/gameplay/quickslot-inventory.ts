/*
===========================================================================

quickslot-inventory.ts - repair persisted hotbar references after item changes

Native 574800 remaps move endpoints, including split sources. Native 573390
searches the current bag on depletion; stronger HP/MP replacement is the
existing product extension.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { Progression } from "./progression";
import { quickSlotItemSlot, MAX_INVENTORY_SLOT_COUNT, type QuickSlot } from "./quickslots";
import { recoveryCategory } from "./item-cooldowns";

// Published by the inventory owner only after the entire move packet commits.
/*
================
QuickslotInventoryMove
================
*/
export interface QuickslotInventoryMove {
	readonly source: number;
	readonly destination: number;
	readonly destinationMoves: boolean;
}
/*
================
PotionReplacementFacts
================
*/
export interface PotionReplacementFacts {
	readonly inventorySlotCount?: number;
	readonly country?: number;
	readonly progression: Progression;
	readonly maxHp: number;
	readonly maxMp: number;
}

/*
================
usable
================
*/
function usable( item: InventoryItem, facts: PotionReplacementFacts ): boolean {
	const f = item.tooltip?.fields;
	if ( !f ) return false;
	if ( f.country !== undefined && f.country !== 3 && f.country !== facts.country ) return false;
	if ( f.reqGender !== undefined && f.reqGender !== 2 ) return false;
	if (
		(f.reqStr ?? 0) > (facts.progression.stats?.strength ?? 0) ||
		(f.reqInt ?? 0) > (facts.progression.stats?.intellect ?? 0)
	) return false;
	for ( let i = 1; i <= 4; i++ ) {
		const type = f["reqLevelType" + i] ?? 0, required = f["requiredLevel" + (i === 1 ? "" : i)] ?? 0;
		if ( !type || !required ) continue;
		const level = type === 1 ?
			facts.progression.level :
			facts.progression.masteries.find( m => m.id === type )?.level;
		if ( level === undefined || level < required ) return false;
	}
	return true;
}

/*
================
replacement
================
*/
function replacement(
	old: InventoryItem,
	inventory: readonly InventoryItem[],
	facts: PotionReplacementFacts
): InventoryItem | undefined {
	const candidates = inventory.filter( i =>
		i.slot >= 13 && i.slot < (facts.inventorySlotCount ?? MAX_INVENTORY_SLOT_COUNT) && i.quantity > 0
	).sort( ( a, b ) => a.slot - b.slot );
	const same = candidates.find( i => i.refObjId === old.refObjId ), category = recoveryCategory( old.typeFlags );
	// 573390 refills the same reference. Strongest HP/MP on exhaustion is the
	// requested extension; never cross HP/MP/vigor/pet/cure families or rank by ID.
	if ( category !== 1 && category !== 2 ) return same;
	/*
================
amount
================
	*/
	const amount = ( i: InventoryItem ) => {
		const f = i.tooltip?.fields;
		if ( !f ) return 0;
		return category === 1 ?
			(f.itemParam1_29c ?? 0) + facts.maxHp * (f.itemParam2_2a0 ?? 0) / 100 :
			(f.itemParam3_2a4 ?? 0) + facts.maxMp * (f.itemParam4_2a8 ?? 0) / 100;
	};
	let best = same;
	for ( const item of candidates ) {
		if (
			recoveryCategory( item.typeFlags ) === category && usable( item, facts ) &&
			amount( item ) > amount( best ?? old ) && amount( item ) > 0
		) best = item;
	}
	// A weaker potion is still useful when the last stronger stack is gone.
	if ( !best ) {
		for ( const item of candidates ) {
			if (
				recoveryCategory( item.typeFlags ) === category && usable( item, facts ) && amount( item ) > 0 &&
				(!best || amount( item ) > amount( best ))
			) best = item;
		}
	}
	return best;
}

// 574800 remaps BOTH sides of a swap across all 51 slots. 573390 replaces
// exhausted bag references. UI and auto-potion consume the same repaired rows.
/*
================
reconcileQuickslotInventory
================
*/
export function reconcileQuickslotInventory(
	rows: readonly QuickSlot[],
	before: readonly InventoryItem[],
	after: readonly InventoryItem[],
	moves: readonly QuickslotInventoryMove[],
	facts: PotionReplacementFacts,
	refill = false
): QuickSlot[] {
	return rows.map( row => {
		let slot = quickSlotItemSlot( row );
		if ( slot === null ) return row;
		const old = before.find( i => i.slot === slot );
		if ( !old ) return row;
		for ( const move of moves ) {
			if ( slot === move.source ) slot = move.destination;
			else if ( slot === move.destination && move.destinationMoves ) slot = move.source;
		}
		let item = after.find( i => i.slot === slot && i.refObjId === old.refObjId );
		if ( !item && refill && row.kind === 0x46 ) item = replacement( old, after, facts );
		if ( !item || item.slot >= (facts.inventorySlotCount ?? MAX_INVENTORY_SLOT_COUNT) ) {
			return { slot: row.slot, kind: 0, payload: 0 };
		}
		const kind = item.slot < 13 ? 0x47 : 0x46, payload = item.slot < 13 ? item.slot : item.slot - 13;
		return kind === row.kind && payload === row.payload ? row : { slot: row.slot, kind, payload };
	} );
}
