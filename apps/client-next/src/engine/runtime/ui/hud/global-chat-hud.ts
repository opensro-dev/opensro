/*
===========================================================================

global-chat-hud.ts - the CIFWholeChat window's state

Owns the open window: the Global Chatting item's inventory slot
(CIFWholeChat_SetItemSlot 6D1D70) and the line being typed. The window
stays open after each send with its line cleared (6D1EE0), so one window
sends as many lines as the stack holds. The remaining count is read from
the slot every frame (CIFGlobalChatItem_RefreshSendEnabled 6D1D90).

===========================================================================
*/
import { GLOBAL_CHAT_MAX_LENGTH } from "@/engine/foundation/gameplay/global-chat";

/*
================
GlobalChatWindow
================
*/
interface GlobalChatWindow {
	readonly slot: number;
	readonly text: string;
}

/*
================
createGlobalChatHud
================
*/
export function createGlobalChatHud() {
	let open: GlobalChatWindow | null = null;
	return {
		/*
		================
		open

		The item in slot was used: the window opens on an empty line.
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

		The edit box's text, held to its 0x64-character limit.
		================
		*/
		type( text: string ) {
			if ( open ) open = { ...open, text: text.slice( 0, GLOBAL_CHAT_MAX_LENGTH ) };
		},
		/*
		================
		sent

		6D1EE0 clears the line once it has been handed to the wire.
		================
		*/
		sent() {
			if ( open ) open = { ...open, text: "" };
		},
		/*
		================
		state
		================
		*/
		state(): GlobalChatWindow | null {
			return open;
		}
	};
}
