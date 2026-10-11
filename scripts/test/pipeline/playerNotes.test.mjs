/*
===========================================================================

playerNotes.test.mjs - the "## Player note" section a pull request carries

Only "Area: sentence" lines of the section count, in the bug forum's
areas; anything after the next heading, an unknown area, or "None" adds
nothing to the catalogue's notes.json.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { parsePlayerNote } from "../../build/catalogue/playerNotes.mjs";

test("a player note section yields one entry per area line", () => {
	const body = [
		"## Summary",
		"Party: not this one, it is outside the section.",
		"",
		"## Player note",
		"Party: A suited member shows as their job alias in the party window.",
		"- ui: Job users' names read *name above them.",
		"Server: an unknown area is skipped.",
		"None",
		"## Native claims",
		"World: after the section, so ignored."
	].join( "\n" );
	assert.deepEqual( parsePlayerNote( body ), [
		{ area: "Party", text: "A suited member shows as their job alias in the party window." },
		{ area: "UI", text: "Job users' names read *name above them." }
	] );
});

test("a description without the section or with None yields nothing", () => {
	assert.deepEqual( parsePlayerNote( "## Summary\nFixes a race." ), [] );
	assert.deepEqual( parsePlayerNote( "## Player note\nNone\n" ), [] );
	assert.deepEqual( parsePlayerNote( undefined ), [] );
});
