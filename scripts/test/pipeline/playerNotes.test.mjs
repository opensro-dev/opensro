/*
===========================================================================

playerNotes.test.mjs - the "Player-note:" trailers a merge carries

"Player-note: Area: sentence" lines in the bug forum's areas keep their
area; a note under any other label, or none, is filed under Other rather
than dropped, with its text whole; a label with no sentence is no note.
A repeated note counts once, and a line that is not a Player-note trailer
adds nothing to the catalogue's notes.json. A later merge's
"Player-note-retract: #<pr>" withdraws every note of that pull request.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { notesOfMerges, parsePlayerNoteRetractions, parsePlayerNotes } from "../../build/catalogue/playerNotes.mjs";

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

test("a retract line names the pull requests it withdraws and is no note itself", () => {
	const message = [
		"Player-note-retract: #1060",
		"player-note-retract: #12, #1060 and #7",
		"Player-note-retract: none"
	].join( "\n" );
	assert.deepEqual( parsePlayerNoteRetractions( message ), [ 7, 12, 1060 ] );
	assert.deepEqual( parsePlayerNotes( message ), [] );
	assert.deepEqual( parsePlayerNoteRetractions( undefined ), [] );
});

test("a later merge's retraction leaves the earlier note out and keeps its own", () => {
	// F9's shape: #1060's test-pacing note was made false by #1073.
	const records = [
		{
			date: "2026-10-15",
			subject: "feat(job): Most Wanted test pacing (#1060)",
			message: "Player-note: Other: Most Wanted is in a test mode."
		},
		{
			date: "2026-10-15",
			subject: "feat(chat): notice links (#1068)",
			message: "Player-note: UI: Drop and reinforcement notices link the player and the item."
		},
		{
			date: "2026-10-15",
			subject: "fix(ops): Most Wanted test pacing ships off (#1073)",
			message: "Player-note-retract: #1060\nPlayer-note: Other: Most Wanted keeps its ordinary pace."
		},
		// Not a pull request merge: its retraction counts for nothing.
		{ date: "2026-10-15", subject: "Merge branch 'x'", message: "Player-note-retract: #1068" }
	];
	assert.deepEqual( notesOfMerges( records ), [
		{
			date: "2026-10-15",
			area: "UI",
			text: "Drop and reinforcement notices link the player and the item.",
			pr: 1068
		},
		{ date: "2026-10-15", area: "Other", text: "Most Wanted keeps its ordinary pace.", pr: 1073 }
	] );
});

test("a retraction never reaches a pull request merged after it", () => {
	const records = [
		{ date: "2026-10-16", subject: "fix: an early withdrawal (#20)", message: "Player-note-retract: #21" },
		{ date: "2026-10-16", subject: "feat: a later note (#21)", message: "Player-note: UI: The later note stays." }
	];
	assert.deepEqual( notesOfMerges( records ), [
		{ date: "2026-10-16", area: "UI", text: "The later note stays.", pr: 21 }
	] );
});
