/*
===========================================================================

runtime-errors.test.mjs - tests for runtime-errors.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createRuntimeErrors } = await import( sourceFileUrl( "src/engine/runtime/runtime-errors.ts" ).href );
test("runtime failures report once per transition, independently by subsystem, and rearm after recovery", () => {
	const messages = [], errors = createRuntimeErrors( ( source, error ) => messages.push( `${source}: ${error}` ) );
	for ( let i = 0; i < 100; i++ ) errors.update( "Characters", "Missing height" );
	errors.update( "Audio", "Missing sound" );
	errors.update( "Characters", "Invalid model" );
	errors.update( "Characters", null );
	errors.update( "Characters", "Missing height" );
	assert.deepEqual( messages, [
		"Characters: Missing height",
		"Audio: Missing sound",
		"Characters: Invalid model",
		"Characters: Missing height"
	] );
});

test("a held failure reports only once it has stood the whole hold, and recovery before that files nothing", () => {
	const messages = [], errors = createRuntimeErrors( ( source, error ) => messages.push( `${source}: ${error}` ) );
	// A retrying failure appears at t=1000 and recovers at t=20000, inside
	// the 30000 hold: it never reports.
	errors.updateHeld( "Interface", "UI image unavailable; retrying: a.png", 1000, 30000 );
	errors.updateHeld( "Interface", "UI image unavailable; retrying: a.png", 20000, 30000 );
	errors.updateHeld( "Interface", null, 20000, 30000 );
	assert.deepEqual( messages, [] );
	// The same failure returns and stands: it reports once, when the hold
	// completes, and not again while it keeps standing.
	errors.updateHeld( "Interface", "UI image unavailable; retrying: a.png", 40000, 30000 );
	errors.updateHeld( "Interface", "UI image unavailable; retrying: a.png", 69999, 30000 );
	assert.deepEqual( messages, [] );
	errors.updateHeld( "Interface", "UI image unavailable; retrying: a.png", 70000, 30000 );
	errors.updateHeld( "Interface", "UI image unavailable; retrying: a.png", 90000, 30000 );
	assert.deepEqual( messages, [ "Interface: UI image unavailable; retrying: a.png" ] );
	// A different failure restarts the hold; recovery rearms both.
	errors.updateHeld( "Interface", "UI image unavailable; retrying: b.png", 95000, 30000 );
	errors.updateHeld( "Interface", null, 96000, 30000 );
	errors.updateHeld( "Interface", "UI image unavailable; retrying: a.png", 100000, 30000 );
	errors.updateHeld( "Interface", "UI image unavailable; retrying: a.png", 130000, 30000 );
	assert.deepEqual( messages, [
		"Interface: UI image unavailable; retrying: a.png",
		"Interface: UI image unavailable; retrying: a.png"
	] );
});
