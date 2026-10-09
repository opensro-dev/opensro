/*
===========================================================================

pickup-notice.ts - the status line for an item picked up into the bag

CPSMission_ApplyInventoryOperation (756CF0) prints it while applying a
0xB06D pickup (types 6 and 0x1C resolve to source 0, window 0x46 at
74EF50). Gold, slot 0xFE, has its own line (UIIT_MSG_STATE_GAIN_GOLD).

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { SystemNotice } from "./system-notices";

// CGInterface_ChatSystemMessageFormatted channel 1, as the gold gain.
const GAIN_CHANNEL = 1;
// ItemTid_IsEtcTid3_13_Tid4_15 (550900): the type 3/13/15 family.
const ETC_13_15_MASK = 0xff80;
const ETC_13_15 = 0x7e80;
// Item reference +0x2B0 bit 1 marks one of that family as non-expendable.
const ETC_NONEXPENDABLE_BIT = 2;

/*
================
consumable

CItemData_IsConsumable (4FB260): an item whose type word is 3/3.
================
*/
function consumable( typeFlags: number ) {
	return (typeFlags & 2) === 0 && (typeFlags & 0x1c) === 0xc && (typeFlags & 0x60) === 0x60;
}

/*
================
pickupNotice

before is the bag slot's item ahead of the pickup and after the slot once
applied. A consumable already in the slot merges, and the line names the
gained count. Otherwise the new item prints with its whole count,
or with only its name when it does not stack (757199, 75744C).
================
*/
export function pickupNotice(
	before: InventoryItem | undefined,
	after: InventoryItem | undefined
): SystemNotice | null {
	if ( !after ) return null;
	const name = after.name ?? "";
	if ( before && consumable( before.typeFlags ) ) {
		const gained = after.quantity - before.quantity;
		return {
			key: "UIIT_MSG_STATE_GET_ITEM_EXPENDABLE",
			value: gained,
			arguments: [ name, String( gained ) ],
			nativeType: GAIN_CHANNEL
		};
	}
	const nonExpendable = !consumable( after.typeFlags ) ||
		(after.typeFlags & ETC_13_15_MASK) === ETC_13_15 &&
			((after.tooltip?.fields.itemParam6_2b0 ?? 0) & ETC_NONEXPENDABLE_BIT) !== 0;
	if ( nonExpendable ) {
		return {
			key: "UIIT_MSG_STATE_GET_ITEM_NONEXPENDABLE",
			value: 0,
			arguments: [ name ],
			nativeType: GAIN_CHANNEL
		};
	}
	return {
		key: "UIIT_MSG_STATE_GET_ITEM_EXPENDABLE",
		value: after.quantity,
		arguments: [ name, String( after.quantity ) ],
		nativeType: GAIN_CHANNEL
	};
}
