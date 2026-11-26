/*
===========================================================================

magic-cube-hud.ts - Magic Cube use-confirm and open-window state

Port-only, not native (#3906). v1.150 has no Magic Cube window. Opening
is client-local after UIIT_MSG_MAGIC_CUBE_USE: Yes arms the cube slot
the bag will push into / pop from; No dismisses. Contents ride the
item's linkedStorage projection (bootstrap + 0xB06D 0x27/0x28 apply).

===========================================================================
*/

import type { InventoryItem } from "@/engine/contracts/gameplay";
import { isMagicCube } from "@/engine/foundation/gameplay/magic-cube";

/*
================
createMagicCubeHud
================
*/
export function createMagicCubeHud() {
	let pending: { slot: number; refObjId: number; } | null = null;
	let openSlot: number | null = null;
	let openRefObjId: number | null = null;
	return {
		/*
		================
		ask

		Raise the use confirmation for one Magic Cube bag row.
		================
		*/
		ask( item: InventoryItem ) {
			if ( !isMagicCube( item.typeFlags ) ) return;
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
		openSlot

		The bag slot whose linked storage the window is drawing, or null.
		================
		*/
		cubeSlot() {
			return openSlot;
		},
		/*
		================
		open

		Yes on the confirm, or reopen an already-known cube: show its window.
		================
		*/
		open( item: InventoryItem ) {
			if ( !isMagicCube( item.typeFlags ) ) return;
			pending = null;
			openSlot = item.slot;
			openRefObjId = item.refObjId;
		},
		/*
		================
		take

		Yes: open the asked cube and clear the confirm.
		================
		*/
		take( items: readonly InventoryItem[] ): number | null {
			const asked = pending;
			pending = null;
			if ( !asked ) return null;
			const item = items.find( row => row.slot === asked.slot && row.refObjId === asked.refObjId );
			if ( !item || !isMagicCube( item.typeFlags ) ) return null;
			openSlot = item.slot;
			openRefObjId = item.refObjId;
			return item.slot;
		},
		/*
		================
		dismiss

		No or Escape on the confirm: close the ask only.
		================
		*/
		dismiss() {
			pending = null;
		},
		/*
		================
		close

		Close the open cube window.
		================
		*/
		close() {
			openSlot = null;
			openRefObjId = null;
		},
		/*
		================
		reconcile

		A cube that left the bag, changed identity, or left the world closes
		the ask and the window. Returns whether anything changed.
		================
		*/
		reconcile( items: readonly InventoryItem[], visible: boolean ) {
			let changed = false;
			if ( pending ) {
				if (
					!visible ||
					!items.some( item => item.slot === pending!.slot && item.refObjId === pending!.refObjId )
				) {
					pending = null;
					changed = true;
				}
			}
			if ( openSlot !== null ) {
				const still = visible &&
					items.some(
						item =>
							item.slot === openSlot && item.refObjId === openRefObjId && isMagicCube( item.typeFlags )
					);
				if ( !still ) {
					openSlot = null;
					openRefObjId = null;
					changed = true;
				}
			}
			return changed;
		}
	};
}
