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
import { REVERSE_RETURN_LAST_DEATH, REVERSE_RETURN_LAST_RECALL } from "./count-job";

/*
================
CosItemUseContext

For live-pet recovery, the selected companion wins and a sole eligible pet
is unambiguous. Grass and Clock always require an explicit inventory target.
================
*/
export interface CosItemUseContext {
	readonly records: readonly CosRecord[];
	readonly selectedGid?: number;
	readonly targetGid?: number;
	readonly revivalSlot?: number;
	readonly summonerSlot?: number;
	// Resolved character reference of the explicitly targeted summoner.
	readonly summonedCharacterTypeFlags?: number;
	readonly skin?: SkinChoice;
	// The bag item an armour gender change tool was dropped on.
	readonly targetSlot?: number;
	// The reverse return scroll's chosen point (2 recall, 3 death).
	readonly reverseChoice?: number;
}

/*
================
companionItemUseNotice

696490 and 6968BA..696913 validate Grass/Clock targets before composing the
slot byte. Expected refusals are category-5 notices, not protocol failures.
Character reference classifies the pet; the item subtype is not that proof.
================
*/
export function companionItemUseNotice(
	flags: number,
	items: readonly InventoryItem[],
	context?: CosItemUseContext
): SystemNotice | null {
	if ( (flags & 0x7c) !== 0x6c ) return null;
	const group = flags >>> 7 & 15, subtype = flags >>> 11 & 31;
	const grass = group === 1 && subtype === 6;
	if ( !grass && !isCompanionLeaseItem( flags ) ) return null;
	const slot = grass ? context?.revivalSlot : context?.summonerSlot;
	const unavailable: SystemNotice = { key: "UIIT_MSG_COSPETERR_CANT_USEITEM", value: 0, nativeType: 5 };
	const wrongObject: SystemNotice = { key: "UIIT_MSG_COSPETERR_CANT_USE_WRONGOBJECT", value: 0, nativeType: 5 };
	if ( slot === undefined ) return wrongObject;
	if ( !Number.isInteger( slot ) || slot < 0 || slot > 255 ) throw Error( "Invalid companion target slot" );
	const matches = items.filter( item => item.slot === slot );
	if ( matches.length > 1 ) throw Error( "Duplicate companion target slot" );
	const target = matches[0], character = context?.summonedCharacterTypeFlags;
	if ( !target || (target.typeFlags & 0x7fe) !== 0xcc || !target.summon ) return wrongObject;
	// Grass's missing-character branch reaches 696311; Clock instead refuses
	// it. 5500B0/F0 inspect the character's 16-bit family and subtype masks.
	if ( character === undefined ) return grass ? null : wrongObject;
	if ( (character & 0x7fe) !== 0x1c6 || (character & 0xf800) !== (grass ? 0x1800 : 0x2000) ) return wrongObject;
	if ( grass && target.summon.state !== 4 ) return unavailable;
	return null;
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
	if ( isReverseReturnScroll( flags ) ) {
		// 6971B0 case 0x1E: the choice box's row is the one byte after the type.
		const choice = context?.reverseChoice;
		if ( choice !== REVERSE_RETURN_LAST_RECALL && choice !== REVERSE_RETURN_LAST_DEATH ) {
			throw Error( "Choose where the reverse return scroll goes" );
		}
		return Uint8Array.of( choice );
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
	if ( group === 1 && subtype === 6 || group === 13 && subtype === 12 ) {
		// The worker publishes expected refusals before reaching this strict
		// encoder. Never infer a target from a sole inventory candidate.
		if ( companionItemUseNotice( flags, items, context ) ) throw Error( "Companion target was not admitted" );
		return Uint8Array.of( (group === 1 ? context?.revivalSlot : context?.summonerSlot)! );
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
isReverseReturnScroll

3/3/3/3, ITEM_MALL_REVERSE_RETURN_SCROLL: a right click asks for its point
(6971B0 case 0x1E) before the use is sent.
================
*/
export function isReverseReturnScroll( flags: number ): boolean {
	return (flags & 0x7c) === 0x6c && (flags >>> 7 & 15) === 3 && (flags >>> 11 & 31) === 3;
}

/*
================
isCompanionLeaseItem

561D50 checks the inventory-targetable mall family; subtype 12 is the clock.
================
*/
export function isCompanionLeaseItem( flags: number ): boolean {
	return (flags & 0x7c) === 0x6c && (flags >>> 7 & 15) === 13 && (flags >>> 11 & 31) === 12;
}

/*
================
companionItemTargetCommand
================
*/
export function companionItemTargetCommand(
	source: InventoryItem,
	target: InventoryItem
): GameplayCommand | null {
	if ( source.slot < 13 ) return null;
	const flags = source.typeFlags;
	if ( (flags & 0x7c) !== 0x6c ) return null;
	const group = flags >>> 7 & 15, subtype = flags >>> 11 & 31;
	// The gender change tool dropped on a bag item uses itself on it.
	if ( group === 13 && subtype === 8 && target.slot >= 13 ) {
		return { kind: "item-use", slot: source.slot, targetSlot: target.slot };
	}
	// A wrong occupied target is still item use: the worker owns its notice.
	// Returning null here would turn the attempted use into an inventory swap.
	if ( group === 1 && subtype === 6 ) {
		return { kind: "item-use", slot: source.slot, revivalSlot: target.slot };
	}
	if ( group === 13 && subtype === 12 ) {
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
