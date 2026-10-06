/*
===========================================================================

recovery-settlement.test.mjs - posted worker commands cannot settle early

The loaded recovery run saw its old idle publication after posting a new
anchor move. These fixtures vary publication timing without a real browser.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { waitForMovementSettlement } from "../../tools/perf/core/movement-settlement.mjs";

/*
================
Old idle publications do not finish a newly posted command
================
*/
test("settlement waits through old idle publications, admission and the receipt", async () => {
	const publications = [
		{ movementRevision: 10, moving: false, pendingMoves: 0 },
		{ movementRevision: 10, moving: false, pendingMoves: 0 },
		{ movementRevision: 11, moving: true, pendingMoves: 1 },
		{ movementRevision: 12, moving: true, pendingMoves: 0 },
		{ movementRevision: 12, moving: false, pendingMoves: 0 }
	];
	let reads = 0;
	const settled = await waitForMovementSettlement( {
		read: async () => publications[reads++],
		pause: async () => {},
		timeoutMs: 1000,
		afterRevision: 10
	} );
	assert.equal( reads, publications.length );
	assert.deepEqual( settled, publications.at( -1 ) );
});

/*
================
A completed command need not expose its transient busy publication
================
*/
test("settlement admits an already completed new revision", async () => {
	const state = { movementRevision: 12, moving: false, pendingMoves: 0 };
	const settled = await waitForMovementSettlement( {
		read: async () => state,
		pause: async () => assert.fail( "already settled" ),
		timeoutMs: 1000,
		afterRevision: 10
	} );
	assert.equal( settled, state );
});

/*
================
An ordinary observation may use the current idle publication
================
*/
test("settlement without a new command accepts current idle state", async () => {
	const state = { movementRevision: 10, moving: false, pendingMoves: 0 };
	assert.equal(
		await waitForMovementSettlement( {
			read: async () => state,
			pause: async () => assert.fail( "already settled" ),
			timeoutMs: 1000
		} ),
		state
	);
});

/*
================
A command never admitted fails instead of accepting an old idle snapshot
================
*/
test("settlement times out if only the pre-command revision exists", async () => {
	await assert.rejects(
		waitForMovementSettlement( {
			read: async () => ({ movementRevision: 10, moving: false, pendingMoves: 0 }),
			pause: async () => assert.fail( "deadline already expired" ),
			timeoutMs: 0,
			afterRevision: 10
		} ),
		/did not admit and settle/
	);
});
