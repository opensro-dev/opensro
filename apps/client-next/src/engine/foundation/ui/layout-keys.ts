/*
===========================================================================

layout-keys.ts - a letter key as the player's keyboard layout names it

The retail client has no DirectInput or raw keyboard: its keys arrive as
WM_KEYDOWN virtual keys (user32 GetKeyState, GetKeyboardLayout and
TranslateMessage are its only keyboard imports), and Windows assigns a
letter's virtual key through the active layout. On AZERTY the key marked
A sends VK_A, so the inventory hotkey "I" is the key marked I wherever it
sits. A browser's KeyboardEvent.code names the physical (QWERTY) position
instead, which bound AZERTY and QWERTZ players to the wrong letters.

Every key event the client emits therefore carries the code of the
layout's letter: event.key when it is a single Latin letter, else the
physical code. Non-Latin layouts (Cyrillic, Greek) keep the physical code,
as Windows gives those keys their position's VK_A..VK_Z. Digits, function
keys and punctuation keep their codes too: the French layout's top row is
still VK_1..VK_0. The code a key went down with is remembered, so its key
up releases the same letter even when Shift or the layout changed between.

===========================================================================
*/

/*
================
upperLatin

The upper-case Latin letter `text` is (one character, A-Z either case),
or "".
================
*/
function upperLatin( text: string ): string {
	if ( text.length !== 1 ) return "";
	const upper = text.toUpperCase();
	return upper >= "A" && upper <= "Z" ? upper : "";
}

/*
================
layoutCode

The code the key's layout letter has (KeyA for the key marked A).
================
*/
export function layoutCode( code: string, key: string ): string {
	const letter = upperLatin( key );
	if ( code.length !== 4 || !code.startsWith( "Key" ) || !upperLatin( code[3]! ) || !letter ) return code;
	return "Key" + letter;
}

/*
================
createLayoutKeys

One event source's held keys: down resolves and remembers, up releases
what went down (or resolves afresh for a key that went down elsewhere).
================
*/
export function createLayoutKeys() {
	const held = new Map<string, string>();
	return {
		down( code: string, key: string ): string {
			const resolved = layoutCode( code, key );
			held.set( code, resolved );
			return resolved;
		},
		up( code: string, key: string ): string {
			const resolved = held.get( code ) ?? layoutCode( code, key );
			held.delete( code );
			return resolved;
		},
		clear() {
			held.clear();
		}
	};
}
