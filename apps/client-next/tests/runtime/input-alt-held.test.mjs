/*
===========================================================================

input-alt-held.test.mjs - the display thread's Alt state

6FCD50 reads GetKeyState(VK_MENU) when a skill press executes; the input
owner keeps either Alt key held until its release or a focus release.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createInput } = await import( "../../src/engine/runtime/input/input.ts" );

test("either Alt key is held until its own release or a focus release", () => {
	const input = createInput();
	assert.equal( input.altHeld(), false );
	input.accept( { kind: "key", code: "AltLeft", down: true, timeMs: 1 } );
	input.accept( { kind: "key", code: "AltRight", down: true, timeMs: 2 } );
	input.accept( { kind: "key", code: "AltLeft", down: false, timeMs: 3 } );
	assert.equal( input.altHeld(), true, "the right Alt is still down" );
	input.accept( { kind: "release", timeMs: 4 } );
	assert.equal( input.altHeld(), false );
	input.accept( { kind: "key", code: "AltLeft", down: true, timeMs: 5 } );
	input.accept( { kind: "key", code: "AltLeft", down: false, timeMs: 6 } );
	assert.equal( input.altHeld(), false );
});
