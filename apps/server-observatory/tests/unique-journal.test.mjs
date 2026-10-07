/*
===========================================================================

unique-journal.test.mjs - unique observations beyond the ordinary census cap

Complete unique snapshots still produce arrivals and departures when the
ordinary population is sampled. Missing unique rows never invent deaths.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createJournal } from "../public/operations.js";

/*
================
snapshot
================
*/
function snapshot( time, monsters, uniqueAlive ) {
	return {
		capturedAt: new Date( time * 1000 ).toISOString(),
		uptimeSeconds: time,
		players: [],
		population: { monsters, uniqueAlive, truncated: true }
	};
}

test("complete unique census keeps arrivals and departures through ordinary truncation", () => {
	const journal = createJournal();
	const cerberus = { gid: 50001, rarity: 3, hp: 100, name: "Cerberus" };
	journal.observe( "a", snapshot( 1, [], 0 ) );
	journal.observe( "a", snapshot( 2, [ cerberus ], 1 ) );
	assert.match( journal.rows( "a" )[0].message, /Cerberus entered/ );
	journal.observe( "a", snapshot( 3, [ { ...cerberus, hp: 0 } ], 0 ) );
	assert.match( journal.rows( "a" )[0].message, /Cerberus left.*cause unknown/ );
});

test("an incomplete unique list cannot invent departures", () => {
	const journal = createJournal();
	journal.observe( "a", snapshot( 1, [ { gid: 1, rarity: 3, hp: 100, name: "Cerberus" } ], 1 ) );
	journal.observe( "a", snapshot( 2, [], 1 ) );
	assert.deepEqual( journal.rows( "a" ), [] );
});
