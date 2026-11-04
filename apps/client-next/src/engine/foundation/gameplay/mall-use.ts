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
const PICKER_TID4 = [ 8, 12, 15 ];
// CanUse (ItemData+0xAC) must carry both bits for the ask.
const CAN_USE_ASK_BITS = 0x81;

/*
================
isMallUseConfirmItem

True when a bag item's plain use asks first: type 3/3/13, CanUse with
both low bits set, and not a target-picker kind (TID4 8/12/15).
================
*/
export function isMallUseConfirmItem( item: InventoryItem ): boolean {
	const word = item.typeFlags;
	if ( (word & TYPE_NOT_ITEM_BIT) !== 0 ) return false;
	if ( (word & TYPE_TID1_MASK) !== TYPE_TID1_ETC ) return false;
	if ( (word & TYPE_TID2_MASK) !== TYPE_TID2_ETC ) return false;
	if ( (word & TYPE_TID3_MASK) !== TYPE_TID3_MALL ) return false;
	if ( PICKER_TID4.includes( (word >>> TYPE_TID4_SHIFT) & TYPE_TID4_MASK ) ) return false;
	return ((item.nativeFields?.canUse ?? 0) & CAN_USE_ASK_BITS) === CAN_USE_ASK_BITS;
}
