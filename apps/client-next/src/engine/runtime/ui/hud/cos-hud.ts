/*
===========================================================================

cos-hud.ts - the COS HUD's state: selected companion, bar and Clean prompt

CIFCOSManager shares one status icon across guild soldiers; other companions
have individual status icons. The worker publishes their bindings and selected
GID. The HUD owns one command panel, shown for the selected companion (CIFCOSManager_SelectCompanion
6F21F0). This module mirrors selection and owns the panel's open/close toggle
(CIFCOSCommand_OnToggleOpen 6A3750), the transport Clean confirmation
the executor raises (6A2350 case 5), and the item-target cursor A6 that
Grass of Life and the clock arm on right-button use. The UI draws from it
every frame.

===========================================================================
*/
import type { GameplayCommand, InventoryItem, CosRecord } from "@/engine/contracts/gameplay";
import { cosClass } from "@/engine/foundation/ui/cos-command";
import { companionItemTargetCommand } from "@/engine/foundation/gameplay/cos-item-use";

const ITEM_TARGET_CURSOR = 0xa6;

/*
================
createCosHud
================
*/
export function createCosHud() {
	let selected = 0, open = true, cleanConfirm: number | null = null;
	let source: InventoryItem | null = null, targetUse: GameplayCommand | null = null;
	return {
		/*
		================
		armItemTarget

		561D50 stores the selected inventory slot (596B50), then sets cursor A6
		at 5621D4, for Grass of Life and the clock alike (armsItemTargetCursor).
		Arming never sends item use or chooses a pet automatically.
		================
		*/
		armItemTarget( item: InventoryItem ) {
			source = item;
			targetUse = null;
		},
		/*
		================
		itemTargetCursor
		================
		*/
		itemTargetCursor(): 0xa6 | null {
			return source && !targetUse ? ITEM_TARGET_CURSOR : null;
		},
		/*
		================
		chooseItemTarget

		567372 checks cursor A6: any occupied inventory or equipment slot opens
		the type-1F confirmation (5673A7) and clears the cursor. Its Yes runs
		CIFInventory_ExecuteItemAction(source, target, slot) (697B79), as a
		drop on that slot does; the worker applies 6968BA's target checks.
		================
		*/
		chooseItemTarget( item: InventoryItem | undefined ) {
			if ( !source || !item || targetUse ) return false;
			targetUse = companionItemTargetCommand( source, item );
			return targetUse !== null;
		},
		/*
		================
		targetUse
		================
		*/
		targetUse() {
			return targetUse;
		},
		/*
		================
		targetUseBox

		The confirmation's control prefix: a revival and a lease renewal each
		own their ids, so no two boxes ever publish the same control.
		================
		*/
		targetUseBox(): "cos-revive" | "cos-renew" | null {
			if ( !targetUse || targetUse.kind !== "item-use" ) return null;
			return targetUse.revivalSlot !== undefined ? "cos-revive" : "cos-renew";
		},
		/*
		================
		takeTargetUse
		================
		*/
		takeTargetUse() {
			const command = targetUse;
			source = null;
			targetUse = null;
			return command;
		},
		/*
		================
		reconcileItemTarget

		The native cursor is global: hotbar use can arm it with inventory closed.
		A changed source slot or departure from the world cancels the selection.
		================
		*/
		reconcileItemTarget( items: readonly InventoryItem[], visible: boolean ) {
			const armed = source;
			if (
				!armed ||
				visible && items.some( item =>
						item.slot === armed.slot && item.refObjId === armed.refObjId &&
						item.typeFlags === armed.typeFlags && item.quantity > 0
					)
			) return false;
			source = null;
			targetUse = null;
			return true;
		},

		/*
		================
		reconcile

		The manager selects a newly added companion (CIFCOSManager_AddCompanion
		6F2710 calls SelectCompanion) and falls back to the first remaining one
		when the selected companion is removed (6F2900). Returns whether the
		state changed.
		================
		*/
		reconcile( records: readonly CosRecord[], selectedGid?: number ) {
			const shown = records.filter( r => cosClass( r.band ) !== null );
			let changed = selectedGid !== undefined && selected !== selectedGid;
			if ( selectedGid !== undefined ) selected = selectedGid;
			if ( selectedGid === undefined && !shown.some( r => r.gid === selected ) ) {
				const next = shown.at( -1 )?.gid ?? 0;
				changed = next !== selected;
				selected = next;
			}
			if ( cleanConfirm !== null && !shown.some( r => r.gid === cleanConfirm ) ) {
				cleanConfirm = null;
				changed = true;
			}
			return changed;
		},
		/*
================
reset
================
		*/
		reset() {
			selected = 0;
			open = true;
			cleanConfirm = null;
			source = null;
			targetUse = null;
		},
		/*
================
select
================
		*/
		select( gid: number ) {
			selected = gid;
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
toggle
================
		*/
		toggle() {
			open = !open;
		},
		/*
================
open
================
		*/
		open() {
			return open;
		},
		/*
================
askClean
================
		*/
		askClean( gid: number ) {
			cleanConfirm = gid;
		},
		/*
================
cleanConfirm
================
		*/
		cleanConfirm() {
			return cleanConfirm;
		},
		/*
================
takeClean
================
		*/
		takeClean() {
			const gid = cleanConfirm;
			cleanConfirm = null;
			return gid;
		}
	};
}
