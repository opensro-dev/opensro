/*
===========================================================================

text-typos.ts - corrected English for misspelled retail text keys

Port-only, not native (Experimental "Correct English typos", on by
default, off is the original): the shipped textuisystem.txt misspells a
few strings the player reads. This table names each key and its corrected
text; the text lookup (ui.ts hudCopy) uses it only while the row is on, so
the retail strings and the published text data stay untouched.

===========================================================================
*/

/*
================
TEXT_TYPOS
================
*/
export const TEXT_TYPOS = {
	// The guild window's empty hostile-guild row: retail "<No hopstile Guild>".
	UIIT_MSG_GUILDWAR_NOTENEMYGUILD: "<No hostile Guild>"
} as const;

/*
================
correctedText

The corrected text for key when the row is on and key is listed, else
value, the retail text.
================
*/
export function correctedText( key: string, value: string, enabled: boolean ): string {
	const corrected: Readonly<Record<string, string>> = TEXT_TYPOS;
	return enabled ? corrected[key] ?? value : value;
}
