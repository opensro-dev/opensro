/*
===========================================================================

name-change-hud.ts - the name-change scroll's entry window state

Port-only, not native (#1689): using the scroll opens a name entry on
the CIFWholeChat frame (its edit box, count and Use/Cancel buttons; the
window's title says what it is). The window owns the scroll's inventory
slot and the name being typed; a confirmed Use hands the name to the
item-use command and closes, unlike the chat window it borrows, which
stays open for more lines.

===========================================================================
*/
import { NAME_CHANGE_MAX_LENGTH } from "@/engine/foundation/gameplay/name-change";

/*
================
NameChangeWindow
================
*/
interface NameChangeWindow {
	readonly slot: number;
	readonly text: string;
}

/*
================
createNameChangeHud
================
*/
export function createNameChangeHud() {
	let open: NameChangeWindow | null = null;
	return {
		/*
		================
		open

		The scroll in slot was used: the window opens on an empty name.
		================
		*/
		open( slot: number ) {
			open = { slot, text: "" };
		},
		/*
		================
		close
		================
		*/
		close() {
			open = null;
		},
		/*
		================
		type

		The edit box's text, held to the native name bound.
		================
		*/
		type( text: string ) {
			if ( open ) open = { ...open, text: text.slice( 0, NAME_CHANGE_MAX_LENGTH ) };
		},
		/*
		================
		state
		================
		*/
		state(): NameChangeWindow | null {
			return open;
		}
	};
}
