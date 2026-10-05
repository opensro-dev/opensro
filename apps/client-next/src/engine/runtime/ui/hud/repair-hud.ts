/*
===========================================================================

repair-hud.ts - the shop window's repair state: the armed cursor and the
Repair All confirmation

CIFStore_OnRepairButton (5B1C00) arms the repair cursor (mode 0x96); the
next item clicked is repaired and the shop's closing or Escape disarms it.
CIFStore_OnRepairAllButton (5B2B10) raises the cost confirmation (box
0x0C with the total, 526290). The UI draws from this owner every frame.

===========================================================================
*/

/*
================
createRepairHud
================
*/
export function createRepairHud() {
	let armed = false, confirmCost: number | null = null;
	return {
		/*
		================
		armed
		================
		*/
		armed() {
			return armed;
		},
		/*
		================
		arm
		================
		*/
		arm() {
			armed = true;
			confirmCost = null;
		},
		/*
		================
		disarm
		================
		*/
		disarm() {
			armed = false;
		},
		/*
		================
		ask

		Raises the Repair All confirmation for this total.
		================
		*/
		ask( cost: number ) {
			armed = false;
			confirmCost = cost;
		},
		/*
		================
		confirmCost
		================
		*/
		confirmCost() {
			return confirmCost;
		},
		/*
		================
		takeConfirm

		Closes the confirmation, returning its total.
		================
		*/
		takeConfirm() {
			const cost = confirmCost;
			confirmCost = null;
			return cost;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			armed = false;
			confirmCost = null;
		}
	};
}
