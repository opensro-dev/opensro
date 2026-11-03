/*
===========================================================================
board-view.test.mjs - the Work board's words, tones and links
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	ago,
	agentTone,
	ciTone,
	freshness,
	issueURL,
	priorityTone,
	prTone,
	pullURL,
	since,
	sortThreads,
	splitPrs,
	stepTone,
	summary,
	threadAction
} from "../public/board-view.js";

test("threads sort fixed first, then close, then reply, most urgent first in each", () => {
	const threads = [
		{ thread: "1", action: "reply", priority: "Low" },
		{ thread: "2", action: "reply", priority: "High" },
		{ thread: "3", action: "fixed", priority: "Medium" },
		{ thread: "4", action: "close", priority: "Low" },
		{ thread: "5", action: "fixed", priority: "High" },
		{ thread: "6", action: "reply", priority: "High" }
	];
	assert.deepEqual( sortThreads( threads ).map( t => t.thread ), [ "5", "3", "4", "2", "6", "1" ] );
	assert.equal( threads[0].thread, "1", "the snapshot's own list is left as it was" );
});

test("durations read as just now, minutes, hours or days", () => {
	assert.equal( ago( 0 ), "just now" );
	assert.equal( ago( 59 ), "just now" );
	assert.equal( ago( 60 ), "1 min" );
	assert.equal( ago( 3599 ), "59 min" );
	assert.equal( ago( 7200 ), "2 h" );
	assert.equal( ago( 86400 * 3 ), "3 d" );
	assert.equal( since( null, 0 ), "" );
	assert.equal( since( 0, 120000 ), "2 min" );
	assert.equal( since( 5000, 0 ), "just now", "a time ahead of the clock is not negative" );
});

test("freshness keeps counting after the answer and turns stale past twice the interval", () => {
	const body = { ageSeconds: 100, intervalSeconds: 120 };
	assert.deepEqual( freshness( body, 0, 0 ), { stale: false, tone: "ok", text: "Collected 1 min ago" } );
	assert.deepEqual( freshness( body, 0, 141000 ), { stale: true, tone: "warn", text: "Stale: collected 4 min ago" } );
	assert.equal( freshness( { ageSeconds: 5, intervalSeconds: 120 }, 0, 0 ).text, "Collected just now" );
});

test("links are built from the repository and a number only", () => {
	assert.equal( pullURL( "opensro-dev/opensro", 2433 ), "https://github.com/opensro-dev/opensro/pull/2433" );
	assert.equal( issueURL( "opensro-dev/opensro", "12" ), "https://github.com/opensro-dev/opensro/issues/12" );
	assert.equal(
		pullURL( "a/b", "1/../../x" ),
		"https://github.com/a/b/pull/NaN",
		"a non-number never becomes a path"
	);
});

test("tones: CI, PR blockers, agents, steps, priorities and thread actions", () => {
	assert.deepEqual( [ "green", "red", "running", "none" ].map( ciTone ), [ "ok", "bad", "warn", "idle" ] );
	assert.equal( prTone( { ready: true, ci: "green", blocker: "" } ), "ok" );
	assert.equal( prTone( { ready: false, ci: "red", blocker: "CI red" } ), "bad" );
	assert.equal( prTone( { ready: false, ci: "green", blocker: "changes requested by x" } ), "bad" );
	assert.equal( prTone( { ready: false, ci: "green", blocker: "needs 1 more approval" } ), "warn" );
	assert.equal( agentTone( "idle" ), "idle" );
	assert.equal( agentTone( "" ), "idle" );
	assert.equal( agentTone( "handed off uncommitted" ), "warn" );
	assert.equal( agentTone( "staging" ), "ok" );
	assert.deepEqual( [ "done", "running", "failed", "waiting" ].map( stepTone ), [ "ok", "warn", "bad", "idle" ] );
	assert.deepEqual( [ "High", "critical", "Medium", "Low", "" ].map( priorityTone ), [
		"bad",
		"bad",
		"warn",
		"idle",
		"idle"
	] );
	assert.equal( threadAction( "fixed" ).text, "Fixed by this release" );
	assert.equal( threadAction( "anything" ).text, "Reply only" );
});

test("PRs split into agent and fork, by number, and the plaques count them", () => {
	const prs = [
		{ number: 9, fork: true, ready: false },
		{ number: 3, fork: false, ready: true },
		{ number: 5, fork: false, ready: false }
	];
	const { agent, fork } = splitPrs( prs );
	assert.deepEqual( agent.map( p => p.number ), [ 3, 5 ] );
	assert.deepEqual( fork.map( p => p.number ), [ 9 ] );
	const tiles = summary( { prs, needs: [ {} ], threads: [], issues: { open: 7 } } );
	assert.deepEqual( tiles.map( t => t.value ), [ 1, "1 / 2", 1, 0, 7 ] );
	assert.equal( tiles[0].tone, "warn", "anything waiting on the owner is amber" );
});
