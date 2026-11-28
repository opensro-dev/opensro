/*
===========================================================================

input-orbiting.test.mjs - when the display thread counts an orbit in progress

67CCA0 orbits while the mouse mode's camera-drag button is held (mode 0
the right button, mode 1 the wheel button); its press drops the hover
cursor, so the runtime picks no hover target while orbiting() holds.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createInput } = await import( "../../src/engine/runtime/input/input.ts" );

const RIGHT = 2, WHEEL = 4, LEFT = 1;

function pointer( input, buttons, timeMs ) {
	input.accept( { kind: "pointer", x: 100 + timeMs, y: 100, buttons, timeMs } );
}

test("an orbit lasts while the mode's drag button is held, and ends on its release or a focus release", () => {
	const input = createInput();
	assert.equal( input.orbiting(), false, "no pointer yet" );
	pointer( input, 0, 1 );
	assert.equal( input.orbiting(), false, "a hover is not an orbit" );
	pointer( input, LEFT, 2 );
	assert.equal( input.orbiting(), false, "the left button never orbits" );
	pointer( input, RIGHT, 3 );
	assert.equal( input.orbiting(), true );
	pointer( input, 0, 4 );
	assert.equal( input.orbiting(), false, "released" );
	pointer( input, RIGHT | LEFT, 5 );
	assert.equal( input.orbiting(), true );
	input.accept( { kind: "release", timeMs: 6 } );
	assert.equal( input.orbiting(), false, "a focus release ends it" );
});

test("mouse mode 1 orbits on the wheel button, not the right one", () => {
	const input = createInput();
	input.mouseMode( 1 );
	pointer( input, RIGHT, 1 );
	assert.equal( input.orbiting(), false );
	pointer( input, WHEEL, 2 );
	assert.equal( input.orbiting(), true );
	input.mouseMode( 0 );
	assert.equal( input.orbiting(), false, "a mode change drops the held drag" );
});
