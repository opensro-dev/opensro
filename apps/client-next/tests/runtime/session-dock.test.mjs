/*
===========================================================================

session-dock.test.mjs - tests for dock-retry.ts

Leaving the world for character select asks the Agent to forget the
remembered character. Until it does, a reload re-enters the world
(ops #329), so a dropped connection or a server error is repeated and
an answered refusal is not.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { dockRetryDelayMs } = await import(
	sourceFileUrl( "src/engine/foundation/session/dock-retry.ts" ).href
);

/*
================
repeatsUntilFinal

How many repeats one failure earns before it is final.
================
*/
function repeatsUntilFinal( failure ) {
	let attempt = 0;
	while ( dockRetryDelayMs( failure, attempt ) !== null ) attempt++;
	return attempt;
}

/*
================
a dropped connection and a server error are repeated
================
*/
test("a dropped connection and a server error are repeated, with growing waits", () => {
	for ( const failure of [ {}, { status: 500 }, { status: 502 }, { status: 503 } ] ) {
		assert.ok( repeatsUntilFinal( failure ) > 0, `status ${failure.status} must be repeated` );
	}
	const waits = [];
	for ( let attempt = 0; dockRetryDelayMs( {}, attempt ) !== null; attempt++ ) {
		waits.push( dockRetryDelayMs( {}, attempt ) );
	}
	assert.ok( waits.every( ( wait, i ) => wait > 0 && (i === 0 || wait > waits[i - 1]) ), `waits ${waits}` );
});

/*
================
an answered refusal is final
================
*/
test("an answered refusal is final, the outdated 426 included", () => {
	for ( const status of [ 200, 400, 401, 403, 415, 426 ] ) {
		assert.equal( dockRetryDelayMs( { status }, 0 ), null, `status ${status}` );
	}
});

/*
================
the repeats are bounded
================
*/
test("the repeats are bounded", () => {
	const repeats = repeatsUntilFinal( {} );
	assert.ok( repeats >= 2 && repeats <= 5, `${repeats} repeats` );
	assert.equal( dockRetryDelayMs( {}, repeats ), null );
	assert.equal( dockRetryDelayMs( {}, -1 ), null );
});
