/*
===========================================================================

cos-item-use.ts - native companion selection, item targets and request tails

Every inventory activation path composes the same target-dependent bytes.
The worker owns selection; admitted records nominate pets or summoner items.

===========================================================================
*/
import { constantNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";
import type { CosRecord, GameplayCommand, InventoryItem } from "@/engine/contracts/gameplay";
import { isSkinChangeScroll, skinChangeTail, type SkinChoice } from "./skin-change";

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
	readonly targetGid?: number;
	readonly revivalSlot?: number;
	readonly summonerSlot?: number;
	readonly skin?: SkinChoice;
	// The bag item an armour gender change tool was dropped on.
	readonly targetSlot?: number;
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
	// 3/3/13/8, the armour gender change: CIFInventory_ExecuteItemAction
	// appends the bag slot the tool was dropped on (49C2B0 case 7).
	if ( group === 13 && subtype === 8 ) {
		if ( context?.targetSlot === undefined ) throw Error( "Drop the tool on the armour to change" );
		return Uint8Array.of( context.targetSlot );
	}
	if ( isSkinChangeScroll( flags ) ) {
		if ( !context?.skin ) throw Error( "Choose a skin in the change window" );
		return skinChangeTail( context.skin );
	}
	if ( group === 1 && subtype === 10 ) {
		// 697036 / 67A5B0 append the target window's GID, including zero.
		const tail = new Uint8Array( 4 );
		new DataView( tail.buffer ).setUint32( 0, context?.targetGid ?? 0, true );
		return tail;
	}
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
		record.band !== 4 && record.band !== 5 &&
		(context.selectedGid === undefined || record.gid === context.selectedGid)
	) ?? [];
	if ( candidates.length !== 1 ) throw Error( "Select an available owned companion" );
	const target = candidates[0]!;
	// 69641F: the attack pet display rounds satiety down, then adds one.
	if (
		group === 1 && subtype === 9 && target.band === 3 &&
		1 - Math.trunc( Math.fround( (target.satiety ?? 0) / 10000 ) * -100 ) >= 100
	) {
		throw Error( "Companion is already fed" );
	}
	const gid = target.gid;
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
	if ( source.slot < 13 || target.slot < 13 ) return null;
	const flags = source.typeFlags;
	if ( (flags & 0x7c) !== 0x6c ) return null;
	const group = flags >>> 7 & 15, subtype = flags >>> 11 & 31;
	// The gender change tool dropped on a bag item uses itself on it.
	if ( group === 13 && subtype === 8 ) return { kind: "item-use", slot: source.slot, targetSlot: target.slot };
	if ( !target.summon ) return null;
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

/*
================
autoPotionTarget

The automatic quickslot has no dragged summoner slot (696490). Companion
items use the COS panel selection (696365/696D22), never an arbitrary pet.
A refused target leaves the channel's retry timer running.
================
*/
export function autoPotionTarget(
	flags: number,
	records: readonly CosRecord[],
	selectedGid: number,
	targetGid = 0
): CosItemUseContext | null {
	const group = flags >>> 7 & 15, subtype = flags >>> 11 & 31;
	if ( group === 1 && subtype === 6 ) return null;
	const targeted = group === 1 && [ 4, 5, 7, 9 ].includes( subtype ) || group === 2 && subtype === 7;
	if ( !targeted ) return { records, selectedGid, targetGid };
	const target = records.find( r => r.gid === selectedGid );
	if ( !target || target.band === 4 || target.band === 5 ) return null;
	if (
		group === 1 && subtype === 9 && target.band === 3 &&
		1 - Math.trunc( Math.fround( (target.satiety ?? 0) / 10000 ) * -100 ) >= 100
	) return null;
	return { records, selectedGid, targetGid };
}

/*
================
createCosSelection

6F2710/6F2900 keep one tab for all guild soldiers. Its original GID can
outlive its record; removing another soldier must not select a different pet.
The worker owns this state and publishes the selection to the HUD.
================
*/
export function createCosSelection() {
	const GUILD_BAND = 5, LAST_COS_BAND = 6;
	let selected = 0, guildRepresentative: CosRecord | undefined;
	let tabs: number[] = [];
	return {
		/*
================
add

Called after the decoded record enters the owned COS map.
================
		*/
		add( record: CosRecord, records: ReadonlyMap<number, CosRecord> ) {
			if ( record.band === GUILD_BAND ) {
				let count = 0;
				for ( const row of records.values() ) if ( row.band === GUILD_BAND ) count++;
				if ( count > 1 ) return;
				guildRepresentative = record;
			}
			if ( record.band < 1 || record.band > LAST_COS_BAND ) return;
			tabs.push( record.gid );
			selected = record.gid;
		},
		/*
================
remove

Called before erasing the record. 6F29BD returns early while other soldiers
remain, even when this was the representative. 6F21F0 refuses missing GIDs.
================
		*/
		remove( gid: number, records: ReadonlyMap<number, CosRecord> ) {
			const record = records.get( gid );
			if ( !record ) return;
			let tab = gid;
			if ( record.band === GUILD_BAND ) {
				let count = 0;
				for ( const row of records.values() ) if ( row.band === GUILD_BAND ) count++;
				if ( count > 1 ) return;
				tab = guildRepresentative?.gid ?? gid;
			}
			const index = tabs.indexOf( tab );
			if ( index !== -1 ) tabs.splice( index, 1 );
			if ( records.size === 1 ) {
				tabs = [];
				selected = 0;
				return;
			}
			const first = tabs[0];
			if ( first !== undefined && first !== gid && records.has( first ) ) selected = first;
		},
		/*
================
select
================
		*/
		select( gid: number, records: ReadonlyMap<number, CosRecord> ) {
			if ( records.has( gid ) ) selected = gid;
		},
		/*
================
statusRecords

Only the first guild soldier creates a status control. Keep its binding until
the last soldier is removed, even after that representative's own despawn.
================
		*/
		statusRecords( records: ReadonlyMap<number, CosRecord> ) {
			const shown: CosRecord[] = [];
			for ( const gid of tabs ) {
				const record = records.get( gid ) ??
					(guildRepresentative?.gid === gid ? guildRepresentative : undefined);
				if ( record ) shown.push( record );
			}
			return shown;
		},
		/*
================
selected
================
		*/
		selected() {
			return selected;
		},
		/*
================
reset
================
		*/
		reset() {
			selected = 0;
			guildRepresentative = undefined;
			tabs = [];
		}
	};
}

/*
================
autoPotionTargetNotice

69691D reports incompatible targets, 696D22 reports a missing companion,
and 696486 routes the full-food rejection through native category 1.
================
*/
export function autoPotionTargetNotice(
	flags: number,
	records: readonly CosRecord[],
	selectedGid: number
): SystemNotice | null {
	if ( autoPotionTarget( flags, records, selectedGid ) ) return null;
	const group = flags >>> 7 & 15, subtype = flags >>> 11 & 31;
	const target = records.find( row => row.gid === selectedGid );
	if ( group === 1 && subtype === 6 || target?.band === 4 || target?.band === 5 ) {
		return { key: "UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT", value: 0, nativeType: 5 };
	}
	if ( !target ) return { key: "UIIT_MSG_COSPETERR_CANT_USEITEM", value: 0, nativeType: 5 };
	const ITEM_NOTICE_CATEGORY = 1, FULL_FOOD_REFUSAL = 0xb3;
	return constantNativeNotice( ITEM_NOTICE_CATEGORY, FULL_FOOD_REFUSAL );
}
