/*
===========================================================================

playerNotes.test.mjs - the "Player-note:" trailers a merge carries

"Player-note: Area: sentence" lines in the bug forum's areas keep their
area; a note under any other label, or none, is filed under Other rather
than dropped, with its text whole; a label with no sentence is no note.
A repeated note counts once, and a line that is not a Player-note trailer
adds nothing to the catalogue's notes.json.

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

test("a note under an area the forum has no tag for is filed under Other, word kept", () => {
	// #751's shape: the storage gold fix named an area outside the tags.
	assert.deepEqual(
		parsePlayerNotes( "Player-note: Storage: Gold goes into and out of the warehouse again." ),
		[ { area: "Other", text: "Storage: Gold goes into and out of the warehouse again." } ]
	);
	// A sentence that opens with a word and a colon is not cut either.
	assert.deepEqual(
		parsePlayerNotes( "Player-note: Warning: the trade window no longer closes itself." ),
		[ { area: "Other", text: "Warning: the trade window no longer closes itself." } ]
	);
});

test("a label with no sentence is no note", () => {
	assert.deepEqual( parsePlayerNotes( [ "Player-note: Storage:", "Player-note: Party:  " ].join( "\n" ) ), [] );
});

test("a note that names no area is filed under Other with its whole sentence", () => {
	// #757's shape: the sentence itself, no label.
	assert.deepEqual(
		parsePlayerNotes( 'Player-note: Fixed a disconnect ("Invalid experience update") after a trap kill.' ),
		[ { area: "Other", text: 'Fixed a disconnect ("Invalid experience update") after a trap kill.' } ]
	);
});

test("a message without a trailer yields nothing", () => {
	assert.deepEqual( parsePlayerNotes( "perf: faster packs (#600)\n\nNo player-visible change." ), [] );
	assert.deepEqual( parsePlayerNotes( undefined ), [] );
});
