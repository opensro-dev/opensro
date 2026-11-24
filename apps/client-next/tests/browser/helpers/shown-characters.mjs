/*
===========================================================================

shown-characters.mjs - the diagnostics' count of characters on screen

The runtime's "Characters:" line (runtime.ts) prints the renderer's actor
total and, in brackets, the characters the frame presents. Only the second
is the roster at the dock: the total also holds item glows and scenery
emitters. Browser tests read the shown count through this one helper.

===========================================================================
*/

/*
================
waitShownCharacters

Wait until the diagnostics say exactly count characters are shown.
================
*/
export async function waitShownCharacters( page, count, timeout = 30000 ) {
	await page.waitForFunction(
		wanted =>
			/Characters: \d+ actors \((\d+) shown\)/.exec( document.querySelector( "output" )?.textContent ?? "" )
				?.[1] ===
				String( wanted ),
		count,
		{ timeout }
	);
}
