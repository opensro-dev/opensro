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

for ( const action of [ "drop", "blind" ] ) {
	test(`Alt can hold the configured ${action} action without consuming sequenced commands`, () => {
		const input = createInput();
		if ( action === "drop" ) input.dropNameBinding( 18 );
		else input.blindBinding( 18 );
		const held = action === "drop" ? input.dropNamesHeld : input.blindHeld;
		const events = [
			{ kind: "key", code: "AltLeft", down: true, timeMs: 1 },
			{ kind: "key", code: "AltRight", down: true, timeMs: 2 },
			{ kind: "key", code: "AltLeft", down: false, timeMs: 3 },
			{ kind: "key", code: "Digit1", down: true, timeMs: 4 },
			{ kind: "key", code: "Digit1", down: false, timeMs: 5 },
			{ kind: "key", code: "AltRight", down: false, timeMs: 6 }
		];
		for ( const event of events ) {
			input.accept( { ...event, kind: "key" } );
			assert.equal( held(), event.timeMs < 6 );
			assert.equal( input.altHeld(), event.timeMs < 6 );
		}
		assert.deepEqual( input.drain(), {
			first: 1,
			last: events.length,
			commands: events.map( ( event, index ) => ({ ...event, sequence: index + 1 }) )
		} );
		input.accept( { kind: "key", code: "AltRight", down: true, timeMs: 7 } );
		assert.equal( held(), true );
		input.accept( { kind: "release", timeMs: 8 } );
		assert.equal( held(), false );
		assert.equal( input.altHeld(), false );
		if ( action === "drop" ) input.dropNameBinding( 66 );
		else input.blindBinding( 66 );
		input.accept( { kind: "key", code: "AltLeft", down: true, timeMs: 9 } );
		assert.equal( held(), false, "rebinding removes Alt from the hold action" );
		assert.equal( input.altHeld(), true );
		input.accept( { kind: "key", code: "KeyB", down: true, timeMs: 10 } );
		assert.equal( held(), true );
		input.accept( { kind: "key", code: "AltLeft", down: false, timeMs: 11 } );
		assert.equal( held(), true, "releasing Alt preserves a held replacement binding" );
		input.accept( { kind: "key", code: "KeyB", down: false, timeMs: 12 } );
		assert.equal( held(), false );
	});
}
