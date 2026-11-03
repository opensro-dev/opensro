/*
===========================================================================

mall-use-hud.ts - the mall item-use confirmation's state

CIFMessageBox_ConfigureByType (52F460) kind 0x1F asks
UIIT_MSG_QUESTION_SILKMALL_ITEM_USE before a gated item is used (the
bag activation 561D50 opens it at 562177; foundation/gameplay/
mall-use.ts carries the gate). The HUD owns the one pending ask: the
bag row it was raised on, kept so that Yes runs exactly that slot's
use and a row that changed or left the bag cancels the ask, as the
native box is modal.

===========================================================================
*/

import type { InventoryItem } from "@/engine/contracts/gameplay";

/*
================
createMallUseHud
================
*/
export function createMallUseHud() {
	let pending: { slot: number; refObjId: number; } | null = null;
	return {
		/*
		================
		ask

		Raise the confirmation for one bag item's plain use.
		================
		*/
		ask( item: InventoryItem ) {
			pending = { slot: item.slot, refObjId: item.refObjId };
		},
		/*
		================
		pending
		================
		*/
		pending() {
			return pending;
		},
		/*
		================
		take

		Yes: hand the asked slot to the use and close the box.
		================
		*/
		take(): number | null {
			const slot = pending?.slot ?? null;
			pending = null;
			return slot;
		},
		/*
		================
		dismiss

		No or Escape: close the box and run nothing.
		================
		*/
		dismiss() {
			pending = null;
		},
		/*
		================
		reconcile

		The box is modal on the item it was raised for: a row that changed
		or left the bag, or departure from the world, cancels the ask.
		================
		*/
		reconcile( items: readonly InventoryItem[], visible: boolean ) {
			const asked = pending;
			if (
				!asked ||
				visible && items.some( item => item.slot === asked.slot && item.refObjId === asked.refObjId )
			) return false;
			pending = null;
			return true;
		}
	};
}
