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
