/*
===========================================================================

stale-draw-runs.test.mjs - retained static runs pass the stale-draw guard

The static object runs are recorded once a world revision and drawn by
the frame, the sun cascade and the mirror. A run draw released while the
world revision stands must still leave every list those passes draw, the
frame its release moved the draw revision.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createStaleDrawGuard } = await import( "../../src/engine/runtime/renderer/frame/stale-draws.ts" );

/*
================
guardOver

A guard over a release record the test writes: released.add( draw ) is
the geometry owner releasing it.
================
*/
function guardOver() {
	/** @type {Set<object>} */
	const released = new Set(), reports = [];
	const guard = createStaleDrawGuard(
		draw => released.has( draw ) ? { atMs: 0, stack: "release" } : undefined,
		/** @param {any} r */ r => reports.push( r )
	);
	return { guard, released, reports };
}

test("a run draw released under a standing world revision leaves the runs at the next draw revision", () => {
	const { guard, released, reports } = guardOver();
	const a = /** @type {any} */ ({}), b = /** @type {any} */ ({}), c = /** @type {any} */ ({});
	const blocks = [ [ a, b ], [ c ] ];
	const first = guard.runs( "world-static", blocks, 1 );
	assert.deepEqual( first, [ [ a, b ], [ c ] ] );
	assert.equal( guard.runs( "world-static", blocks, 1 ), first, "an unchanged revision reuses the checked runs" );
	released.add( b );
	const next = guard.runs( "world-static", blocks, 2 );
	assert.deepEqual( next, [ [ a ], [ c ] ], "the released draw is gone from what the passes draw" );
	assert.equal( reports.length, 1 );
	assert.equal( reports[0].list, "world-static" );
});

test("new blocks are checked again at the same draw revision", () => {
	const { guard, released } = guardOver();
	const a = /** @type {any} */ ({}), b = /** @type {any} */ ({});
	guard.runs( "world-static", [ [ a ] ], 5 );
	released.add( b );
	assert.deepEqual( guard.runs( "world-static", [ [ a, b ] ], 5 ), [ [ a ] ] );
});
