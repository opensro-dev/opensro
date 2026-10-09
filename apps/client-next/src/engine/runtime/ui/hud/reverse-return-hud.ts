/*
===========================================================================

reverse-return-hud.ts - the bag scroll's pending destination choice

Owns the slot and character bound to message box 0x1E. Reconcile before
input and painting so a delayed answer cannot use a replacement item or
survive a world transition.

===========================================================================
*/
import type { GameplayCommand, InventoryItem } from "@/engine/contracts/gameplay";
import type { UiView } from "@/engine/contracts/ui";
import { isReverseReturnScroll } from "@/engine/foundation/gameplay/cos-item-use";
import { REVERSE_RETURN_LAST_DEATH, REVERSE_RETURN_LAST_RECALL } from "@/engine/foundation/gameplay/count-job";

/*
================
createReverseReturnHud
================
*/
export function createReverseReturnHud() {
	let pending: { slot: number; refObjId: number; gid: number; } | null = null;
	/*
	================
	reconcile
	================
	*/
	function reconcile( view: UiView | null ): boolean {
		if ( !pending ) return false;
		const game = view?.gameplay;
		const row = game?.inventory.find( row => row.slot === pending?.slot );
		if (
			view?.session?.phase === "world" && !view.travel && game?.localGid === pending.gid &&
			!game.inventoryPending && !game.reverseReturnChoice && row?.refObjId === pending.refObjId &&
			isReverseReturnScroll( row.typeFlags ) && row.quantity > 0
		) return false;
		pending = null;
		return true;
	}
	return {
		reconcile,
		/*
		================
		open
		================
		*/
		open( item: InventoryItem, gid: number ) {
			pending = { slot: item.slot, refObjId: item.refObjId, gid };
		},
		/*
		================
		active
		================
		*/
		active() {
			return pending !== null;
		},
		/*
		================
		close
		================
		*/
		close() {
			pending = null;
		},
		/*
		================
		choose
		================
		*/
		choose( choice: number, view: UiView | null ): GameplayCommand | null {
			reconcile( view );
			if ( !pending || choice !== REVERSE_RETURN_LAST_RECALL && choice !== REVERSE_RETURN_LAST_DEATH ) {
				return null;
			}
			const slot = pending.slot;
			pending = null;
			return { kind: "item-use", slot, reverseChoice: choice };
		}
	};
}
