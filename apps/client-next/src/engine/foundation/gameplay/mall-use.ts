/*
===========================================================================

mall-use.ts - the mall item-use confirmation gate

CIFMessageBox_ConfigureByType (52F460) kind 0x1F asks
UIIT_MSG_QUESTION_SILKMALL_ITEM_USE ("Will you use the selected item?")
under a UIIT_STT_CONFIRM_BOX title before the use runs - body pushed at
53252D in the kind-0x1F case at 5324FF. The bag activation (561D50)
opens it at 562177 (push 0x1F, opened through 5C82D0 at 562197) only
when every native gate on the pressed row holds:

- the type is 3/3/13: helper 5501D0, called at 562133, reads TID1 bits
  2-4, TID2 bits 5-6 and TID3 bits 7-10 of the packed type word;
- the item's CanUse (ItemData+0xAC, itemdata token 24) carries both
  low bits: word & 0x81 == 0x81 (56211D-562126) - across the client's
  itemdata, 129 on a 3/3/13 row is CanUse's only such authored value;
- TID4 (word >> 11) is not 8, 12 or 15 (562140-562153): those rows go
  to cursor mode 0xA6, the target picker, instead.

So the ask lands on the time tickets, the recall and repair rows and a
few quest items - not on the whole ITEM_MALL_ family (potions, scrolls
and chat items carry CanUse 1 and use at once), and not only on it
(some ITEM_QNO_ rows ask too).

The later client's storage expansion items (3/3/13/17, #3904, staged
from the v1.696 data by #4329) ask too, each with its own question.
The v1.696 text carries UIIT_STT_STORAGE_EXPANSION_INVENTORY_ADDITION_
ITEM_USE ("Inventory will be expended to 32 rooms permanently. Do you
want to use it now?") and its _WAREHOUSE_ twin, one per kind: Param2 0
is the bag and 1 the warehouse, as the row's own Param2 comment says.
No v1.696 executable is read here, so the rule is inferred: the later
client authors a "use it now?" question for exactly these two items,
and an irreversible, account-limited use asks first whatever the
row's CanUse.

===========================================================================
*/

import type { InventoryItem } from "@/engine/contracts/gameplay";

// The packed type word, as 5501D0 tests it: bit 1 marks a non-item,
// TID1 3 sits in bits 2-4, TID2 3 in bits 5-6, TID3 13 in bits 7-10.
const TYPE_NOT_ITEM_BIT = 0x2;
const TYPE_TID1_MASK = 0x1c;
const TYPE_TID1_ETC = 0xc;
const TYPE_TID2_MASK = 0x60;
const TYPE_TID2_ETC = 0x60;
const TYPE_TID3_MASK = 0x780;
const TYPE_TID3_MALL = 0x680;
const TYPE_TID4_SHIFT = 11;
const TYPE_TID4_MASK = 31;
// TID4 kinds that open the target picker (cursor mode 0xA6) instead.
const PICKER_TID4: readonly number[] = [ 8, 12, 15 ] as const;
// CanUse (ItemData+0xAC) must carry both bits for the ask.
const CAN_USE_ASK_BITS = 0x81;
// The later client's storage expansion kind (3/3/13/17); its Param2
// names what grows: 0 the bag, 1 the warehouse.
const STORAGE_EXPANSION_TID4 = 17;
const STORAGE_EXPANSION_INVENTORY = 0;
const STORAGE_EXPANSION_WAREHOUSE = 1;
// The question the native box asks for every other gated item.
export const MALL_USE_QUESTION = "UIIT_MSG_QUESTION_SILKMALL_ITEM_USE";
// Keyed by Param2: 0 (STORAGE_EXPANSION_INVENTORY), 1 (_WAREHOUSE).
const STORAGE_EXPANSION_QUESTIONS = {
	0: "UIIT_STT_STORAGE_EXPANSION_INVENTORY_ADDITION_ITEM_USE",
	1: "UIIT_STT_STORAGE_EXPANSION_WAREHOUSE_ADDITION_ITEM_USE"
} as const;

/*
================
mallTid4

A 3/3/13 item's TID4, or -1 for any other type.
================
*/
function mallTid4( word: number ): number {
	if ( (word & TYPE_NOT_ITEM_BIT) !== 0 ) return -1;
	if ( (word & TYPE_TID1_MASK) !== TYPE_TID1_ETC ) return -1;
	if ( (word & TYPE_TID2_MASK) !== TYPE_TID2_ETC ) return -1;
	if ( (word & TYPE_TID3_MASK) !== TYPE_TID3_MALL ) return -1;
	return (word >>> TYPE_TID4_SHIFT) & TYPE_TID4_MASK;
}

/*
================
storageExpansionQuestion

The later client's own question for a storage expansion item, or null
for any other item and for a Param2 neither kind names.
================
*/
function storageExpansionQuestion( item: InventoryItem ): string | null {
	if ( mallTid4( item.typeFlags ) !== STORAGE_EXPANSION_TID4 ) return null;
	const kind = item.nativeFields?.itemParam2_2a0;
	if ( kind === STORAGE_EXPANSION_INVENTORY || kind === STORAGE_EXPANSION_WAREHOUSE ) {
		return STORAGE_EXPANSION_QUESTIONS[kind];
	}
	return null;
}

/*
================
isMallUseConfirmItem

True when a bag item's plain use asks first: type 3/3/13, CanUse with
both low bits set, and not a target-picker kind (TID4 8/12/15); or a
later storage expansion item, whatever its CanUse.
================
*/
export function isMallUseConfirmItem( item: InventoryItem ): boolean {
	if ( storageExpansionQuestion( item ) !== null ) return true;
	const tid4 = mallTid4( item.typeFlags );
	if ( tid4 < 0 || PICKER_TID4.includes( tid4 ) ) return false;
	return ((item.nativeFields?.canUse ?? 0) & CAN_USE_ASK_BITS) === CAN_USE_ASK_BITS;
}

/*
================
mallUseQuestion

The text key the confirmation asks for a gated item: a storage
expansion item's own question, else the native mall-use question.
================
*/
export function mallUseQuestion( item: InventoryItem ): string {
	return storageExpansionQuestion( item ) ?? MALL_USE_QUESTION;
}
