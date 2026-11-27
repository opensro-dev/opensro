/*
===========================================================================

character-deletion-countdown.test.mjs - the select screen counts down the server's window

The countdown spans the reservation to the server's deleteMaturesAt (the
shard's SRO_QUICK_CHARACTER_DELETE day, or the native seven days), and an
older server that names no maturity keeps the native seven days.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { deletionWindowMs } = await import( "../../src/engine/runtime/ui/title/title.ts" );

const DAY_MS = 24 * 3600000;

test("the countdown spans the server's deletion window", () => {
	const reserved = Date.parse( "2026-11-27T10:00:00Z" );
	assert.equal( deletionWindowMs( reserved, Date.parse( "2026-11-28T10:00:00Z" ) ), DAY_MS );
	assert.equal( deletionWindowMs( reserved, reserved + 7 * DAY_MS ), 7 * DAY_MS );
});

test("no maturity from the server keeps the native seven days", () => {
	const reserved = Date.parse( "2026-11-27T10:00:00Z" );
	assert.equal( deletionWindowMs( reserved, Number.NaN ), 7 * DAY_MS );
	assert.equal(
		deletionWindowMs( reserved, reserved ),
		7 * DAY_MS,
		"a maturity at or before the reservation is ignored"
	);
});
