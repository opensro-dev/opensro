/*
===========================================================================

layout-keys.test.mjs - hotkeys follow the keyboard layout, as native VKs do

The retail client binds WM_KEYDOWN virtual keys, which Windows assigns
through the active layout; these cases are worked from that rule, not read
back from the code.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createLayoutKeys, layoutCode } = await import( "../../src/engine/foundation/ui/layout-keys.ts" );
const { virtualKey } = await import( "../../src/engine/foundation/ui/input-options.ts" );

test("an AZERTY letter is the letter printed on the key", () => {
	// The key in QWERTY's Q position types "a" on AZERTY: native sends VK_A.
	assert.equal( virtualKey( layoutCode( "KeyQ", "a" ) ), 0x41 );
	assert.equal( virtualKey( layoutCode( "KeyA", "q" ) ), 0x51 );
	assert.equal( virtualKey( layoutCode( "KeyW", "z" ) ), 0x5a );
	// QWERTZ swaps Y and Z.
	assert.equal( virtualKey( layoutCode( "KeyY", "z" ) ), 0x5a );
	// Shift (or Caps Lock) gives an upper-case key: the same letter.
	assert.equal( virtualKey( layoutCode( "KeyQ", "A" ) ), 0x41 );
});

test("QWERTY, digits, function keys and non-Latin layouts keep their codes", () => {
	assert.equal( layoutCode( "KeyI", "i" ), "KeyI" );
	// AZERTY's top row types & é " ' but is still VK_1..VK_0.
	assert.equal( layoutCode( "Digit1", "&" ), "Digit1" );
	assert.equal( layoutCode( "F1", "F1" ), "F1" );
	assert.equal( layoutCode( "Semicolon", "m" ), "Semicolon" );
	// A Cyrillic layout types "ф" on KeyA: Windows still sends VK_A.
	assert.equal( layoutCode( "KeyA", "ф" ), "KeyA" );
	// A dead key reports "Dead".
	assert.equal( layoutCode( "KeyQ", "Dead" ), "KeyQ" );
});

test("a key goes up as the letter it went down with", () => {
	const keys = createLayoutKeys();
	assert.equal( keys.down( "KeyQ", "a" ), "KeyA" );
	// Shift released or the layout switched before the key came up.
	assert.equal( keys.up( "KeyQ", "q" ), "KeyA" );
	// Released again without a down: resolved afresh.
	assert.equal( keys.up( "KeyQ", "q" ), "KeyQ" );
	keys.down( "KeyW", "z" );
	keys.clear();
	assert.equal( keys.up( "KeyW", "w" ), "KeyW" );
});
