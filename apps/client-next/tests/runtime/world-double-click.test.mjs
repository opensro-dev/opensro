/*
===========================================================================

world-double-click.test.mjs - the drifted double-click rescue (deliberate deviation)

Native attacks only on the OS double click, which a few pixels of drift
loses. A second press on the same monster within the double-click time
promotes; the browser's dblclick after a promoted pair is absorbed so the
attack goes out once. See world-double-click.ts before changing either.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { createWorldDoubleClick, WORLD_DOUBLE_CLICK_MS } = await import(
	"../../src/engine/foundation/gameplay/world-double-click.ts"
);

test("a drifted second press on the same monster attacks once", () => {
	const clicks = createWorldDoubleClick();
	assert.equal( clicks.press( 7, 1000 ), false, "the first press only selects" );
	assert.equal( clicks.press( 7, 1117 ), true, "the recorded 117 ms, 3 px pair promotes" );
	assert.equal( clicks.double( 7, 1150 ), false, "the browser dblclick after it is absorbed" );
});

test("a native double click still attacks exactly once", () => {
	// press, press (promotes), dblclick (absorbed): one attack in total.
	const clicks = createWorldDoubleClick();
	const attacks = [ clicks.press( 7, 0 ), clicks.press( 7, 100 ), clicks.double( 7, 110 ) ].filter( Boolean );
	assert.equal( attacks.length, 1 );
	// A dblclick that no press promoted (the presses picked differently) acts.
	const fresh = createWorldDoubleClick();
	fresh.press( null, 0 );
	assert.equal( fresh.double( 7, 50 ), true );
});

test("no promotion across monsters, ground, the time limit or a third press", () => {
	const clicks = createWorldDoubleClick();
	clicks.press( 7, 0 );
	assert.equal( clicks.press( 8, 100 ), false, "another monster starts a new pair" );
	assert.equal( clicks.press( null, 150 ), false, "ground breaks the pair" );
	assert.equal( clicks.press( 8, 200 ), false );
	clicks.press( 9, 1000 );
	assert.equal( clicks.press( 9, 1000 + WORLD_DOUBLE_CLICK_MS + 1 ), false, "too slow" );
	clicks.press( 5, 3000 );
	assert.equal( clicks.press( 5, 3100 ), true );
	assert.equal( clicks.press( 5, 3200 ), false, "a third press is a new first click" );
});
