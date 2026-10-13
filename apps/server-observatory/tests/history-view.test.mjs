/*
===========================================================================
history-view.test.mjs - the degraded groups read as an operator asks them
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { categoryLabel, degradedCells } from "../public/history-view.js";

test("a degraded group names what the client skipped, keeps its code and shows the newest frame", () => {
	const cells = degradedCells( {
		code: "item-use-visual-unknown-ref",
		count: 3,
		players: 2,
		first: 1000,
		last: 2000,
		builds: "b1",
		example: { message: "Missing external item reference 3787", opcode: "0x3449", payload: "cb860100cb0e0000" }
	} );
	assert.equal( cells.what, "an item-use sparkle named an item the client did not know" );
	assert.equal( cells.code, "item-use-visual-unknown-ref" );
	assert.equal( cells.example, "Missing external item reference 3787 · 0x3449 cb860100cb0e0000" );
	assert.equal( categoryLabel( "degraded" ), "Degraded" );
	// An unknown site still shows its raw code.
	assert.equal( degradedCells( { code: "new-site", example: {} } ).what, "new-site" );
});
