/*
===========================================================================

cos-hud.ts - the COS HUD's state: selected companion, bar and Clean prompt

CIFCOSManager keeps one status icon per owned companion and one command
panel, shown for the selected companion (CIFCOSManager_SelectCompanion
6F21F0). This module owns that selection, the panel's open/close toggle
(CIFCOSCommand_OnToggleOpen 6A3750) and the transport Clean confirmation
the executor raises (6A2350 case 5). The UI draws from it every frame.

===========================================================================
*/
import type { CosRecord } from "@/engine/contracts/gameplay";
import { cosClass } from "@/engine/foundation/ui/cos-command";

/*
================
createCosHud
================
*/
export function createCosHud() {
	let selected = 0, open = true, cleanConfirm: number | null = null;
	return {
		/*
		================
		reconcile

		The manager selects a newly added companion (CIFCOSManager_AddCompanion
		6F2710 calls SelectCompanion) and falls back to the first remaining one
		when the selected companion is removed (6F2900). Returns whether the
		state changed.
		================
		*/
		reconcile( records: readonly CosRecord[] ) {
			const shown = records.filter( r => cosClass( r.band ) !== null );
			let changed = false;
			if ( !shown.some( r => r.gid === selected ) ) {
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
		reset() {
			selected = 0;
			open = true;
			cleanConfirm = null;
		},
		select( gid: number ) {
			selected = gid;
		},
		selected() {
			return selected;
		},
		toggle() {
			open = !open;
		},
		open() {
			return open;
		},
		askClean( gid: number ) {
			cleanConfirm = gid;
		},
		cleanConfirm() {
			return cleanConfirm;
		},
		takeClean() {
			const gid = cleanConfirm;
			cleanConfirm = null;
			return gid;
		}
	};
}
