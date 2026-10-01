/*
===========================================================================

cos-item-use.ts - native companion item targets and request tails

Every inventory activation path composes the same target-dependent bytes.
Only admitted owner records can nominate a live pet or a dead summoner item.

===========================================================================
*/
import type { CosRecord, GameplayCommand, InventoryItem } from "@/engine/contracts/gameplay";

/*
================
CosItemUseContext

The selected companion wins. With no selection, a sole eligible companion is
unambiguous; several candidates require an explicit selection.
================
*/
export interface CosItemUseContext {
	readonly records: readonly CosRecord[];
	readonly selectedGid?: number;
	readonly revivalSlot?: number;
	readonly summonerSlot?: number;
}

/*
================
cosItemUseTail

696336 and 6963CF append a GID for cures and recovery; 6963F9 includes HGP
food in that same path. 696490 uses the dead summoner's inventory slot.
================
*/
export function cosItemUseTail(
	flags: number,
	items: readonly InventoryItem[],
	context?: CosItemUseContext
): Uint8Array {
	const band = flags >>> 5 & 3, group = flags >>> 7 & 15, subtype = flags >>> 11 & 31;
	if ( (flags & 0x1c) !== 0x0c || band !== 3 ) return new Uint8Array();
	if ( group === 1 && subtype === 6 ) {
		const candidates = items.filter( row =>
			row.slot >= 13 && row.summon?.state === 4 &&
			(context?.revivalSlot === undefined || row.slot === context.revivalSlot)
		);
		if ( candidates.length !== 1 ) throw Error( "Select a dead companion's summoner item" );
		return Uint8Array.of( candidates[0]!.slot );
	}
	if ( group === 13 && subtype === 12 ) {
		// 6961B0's extension arm targets the retained pickup summoner, including
		// expired items. A fresh item has no companion record to extend.
		const candidates = items.filter( row =>
			row.slot >= 13 && row.summon !== undefined && row.summon.state !== 1 &&
			(row.typeFlags >>> 5 & 3) === 2 && (row.typeFlags >>> 7 & 15) === 1 &&
			(row.typeFlags >>> 11 & 31) === 2 &&
			(context?.summonerSlot === undefined || row.slot === context.summonerSlot)
		);
		if ( candidates.length !== 1 ) throw Error( "Select a pickup companion's summoner item" );
		return Uint8Array.of( candidates[0]!.slot );
	}
	const targeted = group === 1 && [ 4, 5, 7, 9 ].includes( subtype ) || group === 2 && subtype === 7;
	if ( !targeted ) return new Uint8Array();
	const candidates = context?.records.filter( record =>
		!record.dead && record.hp > 0 &&
		(group !== 1 || subtype !== 9 || record.band === 3) &&
		(context.selectedGid === undefined || record.gid === context.selectedGid)
	) ?? [];
	if ( candidates.length !== 1 ) throw Error( "Select an available owned companion" );
	const gid = candidates[0]!.gid;
	if ( !Number.isInteger( gid ) || gid <= 0 || gid > 0xffffffff ) throw Error( "Invalid companion identity" );
	const tail = new Uint8Array( 4 );
	new DataView( tail.buffer ).setUint32( 0, gid, true );
	return tail;
}

/*
================
companionItemTargetCommand

Native 6961B0 accepts a dragged revival/extension item on a summoner slot.
Click-carry and drag-drop enter the same inventory intent route.
================
*/
export function companionItemTargetCommand(
	source: InventoryItem,
	target: InventoryItem
): GameplayCommand | null {
	if ( source.slot < 13 || target.slot < 13 || !target.summon ) return null;
	const flags = source.typeFlags;
	if ( (flags & 0x7c) !== 0x6c ) return null;
	const group = flags >>> 7 & 15, subtype = flags >>> 11 & 31;
	if ( group === 1 && subtype === 6 && target.summon.state === 4 ) {
		return { kind: "item-use", slot: source.slot, revivalSlot: target.slot };
	}
	if (
		group === 13 && subtype === 12 && target.summon.state !== 1 &&
		(target.typeFlags & 0x7fc) === 0xcc && (target.typeFlags >>> 11 & 31) === 2
	) {
		return { kind: "item-use", slot: source.slot, summonerSlot: target.slot };
	}
	return null;
}
