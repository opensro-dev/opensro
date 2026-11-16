/*
===========================================================================

reverse-return-hud.ts - the bag scroll's pending destination choice

Owns the slot and character bound to message box 0x1E. Reconcile before
input and painting so a delayed answer cannot use a replacement item or
survive a world transition. A busy bag is one item round trip, not an
invalidation: the box keeps the choice and the answer waits for it.

Port-only, not native: when the server publishes a map table
(SRO_REVERSE_RETURN_MAP), the box has a third row. It hands the same
pending scroll to the world map, where a published point is the answer
(reverse-return-map.ts).

===========================================================================
*/
import type { GameplayCommand, InventoryItem } from "@/engine/contracts/gameplay";
import type { UiView } from "@/engine/contracts/ui";
import { isReverseReturnScroll } from "@/engine/foundation/gameplay/cos-item-use";
import { REVERSE_RETURN_LAST_DEATH, REVERSE_RETURN_LAST_RECALL } from "@/engine/foundation/gameplay/count-job";
import { REVERSE_RETURN_MAP } from "@/engine/foundation/gameplay/reverse-return-map";

/*
================
createReverseReturnHud
================
*/
export function createReverseReturnHud() {
	let pending: { slot: number; refObjId: number; gid: number; } | null = null;
	// mapping: the box handed the pending scroll to the world map.
	let mapping = false;
	/*
	================
	reconcile

	The choice stands while the same character holds the same reverse scroll
	in the same slot, in the world and not travelling. A busy bag is only a
	round trip in flight, so it never drops the choice.

	INFERENCE: nothing in the native box's flow (6971B0 case 0x1E) ties it
	to an unrelated item's answer, so a busy bag does not close it either.
	================
	*/
	function reconcile( view: UiView | null ): boolean {
		if ( !pending ) return false;
		const game = view?.gameplay;
		const row = game?.inventory.find( row => row.slot === pending?.slot );
		if (
			view?.session?.phase === "world" && !view.travel && game?.localGid === pending.gid &&
			!game.reverseReturnChoice && row?.refObjId === pending.refObjId &&
			isReverseReturnScroll( row.typeFlags ) && row.quantity > 0
		) return false;
		pending = null;
		mapping = false;
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
			mapping = false;
		},
		/*
		================
		active

		The box is up: a scroll waits and the map has not taken it.
		================
		*/
		active() {
			return pending !== null && !mapping;
		},
		/*
		================
		mapping

		The world map holds the pending scroll (port-only).
		================
		*/
		mapping() {
			return pending !== null && mapping;
		},
		/*
		================
		openMap

		The box's map row: the scroll waits for a point on the world map.
		================
		*/
		openMap( view: UiView | null ): boolean {
			reconcile( view );
			if ( !pending || !view?.gameplay?.reverseMapPoints?.length ) return false;
			mapping = true;
			return true;
		},
		/*
		================
		choosePoint

		A published map point answers the pending scroll (choice 7).
		================
		*/
		choosePoint( id: number, view: UiView | null ): GameplayCommand | null {
			reconcile( view );
			if ( !pending || !mapping || !view?.gameplay?.reverseMapPoints?.some( point => point.id === id ) ) {
				return null;
			}
			// A busy bag is a round trip in flight (the worker refuses item
			// commands while one is pending); hold the choice so the answer
			// cannot race it, and let the player click again when it clears.
			if ( view.gameplay.inventoryPending ) return null;
			const slot = pending.slot;
			pending = null;
			mapping = false;
			return { kind: "item-use", slot, reverseChoice: REVERSE_RETURN_MAP, reverseMapPoint: id };
		},
		/*
		================
		close
		================
		*/
		close() {
			pending = null;
			mapping = false;
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
			// A busy bag is a round trip in flight (the worker refuses item
			// commands while one is pending); hold the choice so the answer
			// cannot race it, and let the player click again when it clears.
			if ( view?.gameplay?.inventoryPending ) return null;
			const slot = pending.slot;
			pending = null;
			mapping = false;
			return { kind: "item-use", slot, reverseChoice: choice };
		}
	};
}
