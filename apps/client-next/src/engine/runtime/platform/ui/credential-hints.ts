/*
===========================================================================

credential-hints.ts - which mirrored text edits a password manager may use

The bridge mirrors every text edit as a real input, so password managers
and the browser's own Autofill see them all. Only the title screen's login
edits may be saved or filled; every other edit is marked for both to leave
alone, so typing in chat never drops a list of the player's saved e-mail
addresses over the game.

===========================================================================
*/

// The title screen's login edits, the only controls a password manager may
// save or fill. Value is the autocomplete token that identifies the field.
const LOGIN_AUTOCOMPLETE: Readonly<Record<string, string>> = {
	account: "username",
	password: "current-password"
};

// The autocomplete token of every other edit. Chrome overrides "off" for its
// address Autofill whenever its heuristics guess a field type (the chat edit
// was guessed as e-mail and offered the saved addresses), but it classes a
// field with an unrecognized token as unrecognized and offers no Autofill.
// The edits carry no name or id, so no browser keeps form history for them.
export const GAME_EDIT_AUTOCOMPLETE = "sro-game-edit";

/*
================
configureCredentialHints

Password managers ignore autocomplete="off", so every other edit (chat,
stat-point inputs and the like) carries the vendor opt-outs for 1Password,
LastPass, Bitwarden and Dashlane; otherwise clicking one offers to save a
login. The browser's own Autofill is kept off by GAME_EDIT_AUTOCOMPLETE.
================
*/
export function configureCredentialHints( element: HTMLInputElement, id: string ): void {
	const token = LOGIN_AUTOCOMPLETE[id];
	if ( token ) {
		element.autocomplete = token as AutoFill;
		return;
	}
	element.setAttribute( "autocomplete", GAME_EDIT_AUTOCOMPLETE );
	element.setAttribute( "data-1p-ignore", "true" );
	element.setAttribute( "data-lpignore", "true" );
	element.setAttribute( "data-bwignore", "true" );
	element.setAttribute( "data-form-type", "other" );
}
