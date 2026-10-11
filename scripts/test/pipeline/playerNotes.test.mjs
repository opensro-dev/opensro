/*
===========================================================================

playerNotes.test.mjs - the "Player-note:" trailers a merge carries

Only "Player-note: Area: sentence" lines in the bug forum's areas count;
a repeated note counts once, and a line in any other form adds nothing to
the catalogue's notes.json.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { parsePlayerNotes } from "../../build/catalogue/playerNotes.mjs";

test("player-note trailers yield one entry per distinct note", () => {
	const message = [
		"fix(party): a suited member shows as its job alias (#673)",
		"",
		"* fix(party): a suited member shows as its job alias",
		"",
		"Player-note: Party: A suited member shows as their job alias in the party window.",
		"Party: a bare area line is not a note.",
		"Player-note: Server: an unknown area is skipped.",
		"",
		"* party: review fix",
		"",
		"Player-note: Party: A suited member shows as their job alias in the party window.",
		"player-note: ui: Job users' names read *name above them."
	].join( "\n" );
	assert.deepEqual( parsePlayerNotes( message ), [
		{ area: "Party", text: "A suited member shows as their job alias in the party window." },
		{ area: "UI", text: "Job users' names read *name above them." }
	] );
});

test("a message without a trailer yields nothing", () => {
	assert.deepEqual( parsePlayerNotes( "perf: faster packs (#600)\n\nNo player-visible change." ), [] );
	assert.deepEqual( parsePlayerNotes( undefined ), [] );
});
