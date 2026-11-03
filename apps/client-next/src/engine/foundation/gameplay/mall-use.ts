/*
===========================================================================

mall-use.ts - the mall item-use confirmation gate

CIFMessageBox_ConfigureByType (52F460) kind 0x1E asks
UIIT_MSG_QUESTION_SILKMALL_ITEM_USE ("Will you use the selected item?")
under a UIIT_STT_CONFIRM_BOX title before the use runs - body pushed at
53252D in the kind-0x1E case at 5324FF. Its opener (573AD0, reached from a
bag double-activation) carries no item at all: it only checks that no box
is already up (69B2E0) and opens through 5C82D0 with the local player, so
the item test the client ran on the pressed row is not on the opener's
instruction stream.

Recorded inference: the ask covers the generic mall family - codenames
under ITEM_MALL_, the rows the mall sells and the activation dispatcher
already keys on natively. A premium row (ITEM_PRE_*) keeps its own flow,
and the reverse-return scroll's timed use is raised by its own picker, not
by a plain bag press.

===========================================================================
*/

import type { InventoryItem } from "@/engine/contracts/gameplay";

/*
================
isMallUseConfirmItem

True when a bag item's plain use asks first.
================
*/
export function isMallUseConfirmItem( item: InventoryItem ): boolean {
	return item.codename?.startsWith( "ITEM_MALL_" ) ?? false;
}
