/*
===========================================================================

auto-potion-input.ts - publish changes to native automatic item input admission

The UI owns dragged controls and the Item Mall window. The simulation needs
their admission state before executing automatic quickslots, without owning
or inspecting the controls themselves.

===========================================================================
*/

/*
================
createAutoPotionInput
================
*/
export function createAutoPotionInput() {
	let blocked = false, itemMallOpen = false;
	return {
		/*
================
sync
================
		*/
		sync( next: boolean, mall: boolean ) {
			if ( next === blocked && mall === itemMallOpen ) return false;
			blocked = next;
			itemMallOpen = mall;
			return true;
		},
		/*
================
reset
================
		*/
		reset() {
			blocked = false;
			itemMallOpen = false;
		}
	};
}
