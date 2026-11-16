/*
===========================================================================

magic-option-hud.ts - the CIFGrantMagicAttributeWnd window's row choice

Owns what the player picked in the smith's grant window: the selected
option row (CIFGrantMagicAttributeWnd_OnRowSelected 6EBA60) and the list's
scroll offset, which the arrows, the wheel and the thumb all move (the
list is a CIFScrollManager, 6EB3E0). Both start over when the window opens or another item is
dropped on it. The session and the dropped item are the worker's
(inventory/magic-option/magic-option.ts).

===========================================================================
*/

// The list's visible rows (6EB3E0 sets the scroll manager to 5).
export const MAGIC_OPTION_LIST_ROWS = 5;

/*
================
MagicOptionChoice
================
*/
interface MagicOptionChoice {
	readonly item: number | null;
	readonly codename: string | null;
	readonly top: number;
}

/*
================
createMagicOptionHud
================
*/
export function createMagicOptionHud() {
	let choice: MagicOptionChoice = { item: null, codename: null, top: 0 }, shown = false, dragRest = 0;
	/*
	================
	scroll

	Moves the first visible row, clamped to the option count.
	================
	*/
	function scroll( delta: number, count: number ) {
		const top = Math.max( 0, Math.min( Math.max( 0, count - MAGIC_OPTION_LIST_ROWS ), choice.top + delta ) );
		choice = { ...choice, top };
	}
	return {
		/*
		================
		sync

		The worker's window and item. Returns true on the frame the window
		turns on, when the UI shows it beside the inventory. ClearRows
		(6EAE60) runs on every drop, so a new item drops the row choice.
		================
		*/
		sync( visible: boolean, item: number | null ): boolean {
			const opened = visible && !shown;
			shown = visible;
			if ( opened || item !== choice.item ) {
				choice = { item, codename: null, top: 0 };
				dragRest = 0;
			}
			return opened;
		},
		/*
		================
		choose
		================
		*/
		choose( codename: string ) {
			if ( choice.item !== null ) choice = { ...choice, codename };
		},
		scroll,
		/*
		================
		drag

		The thumb moved dy pixels along a travel of the given height: whole
		rows move the list, the remainder waits for the next move.
		================
		*/
		drag( dy: number, travel: number, count: number ) {
			const range = Math.max( 0, count - MAGIC_OPTION_LIST_ROWS );
			if ( range === 0 || travel <= 0 ) return;
			dragRest += dy * range / travel;
			const rows = Math.trunc( dragRest );
			if ( rows === 0 ) return;
			dragRest -= rows;
			scroll( rows, count );
		},
		/*
		================
		endDrag
		================
		*/
		endDrag() {
			dragRest = 0;
		},
		/*
		================
		state
		================
		*/
		state: () => choice
	};
}
