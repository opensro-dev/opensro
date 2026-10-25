/*
===========================================================================

displacement-queue.test.mjs - combat's displacement outbox drains once

The queue is the one hand-off between combat (which validates a cast
phase) and movement (which moves the bodies). A take that left its
entries behind would re-apply a dash or knockback on every drain, and a
clear that kept them would leak one session's displacements into the
next; both are pinned here at the queue itself.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createDisplacementQueue } = await import(
	"../../src/engine/foundation/gameplay/displacement-queue.ts"
);

const DESTINATION = Object.freeze( { regionId: 25000, x: 1200, y: 1000, z: 1000 } );

/*
================
queueEntries

The entries a queue is holding, read by taking them.
================
*/
function queueEntries( queue ) {
	return queue.take();
}

test("take hands the pending displacements over once, then nothing", () => {
	const queue = createDisplacementQueue();
	queue.impact( 7, 1, 2, 4, DESTINATION );
	queue.own( 7, 1, 8, DESTINATION );
	const taken = queueEntries( queue );
	assert.equal( taken.length, 2 );
	assert.deepEqual(
		queueEntries( queue ),
		[],
		"a second take returns nothing - a displacement is never re-applied"
	);
});

test("clear drops what is pending, and later pushes still land", () => {
	const queue = createDisplacementQueue();
	queue.own( 7, 1, 8, DESTINATION );
	queue.clear();
	assert.deepEqual( queueEntries( queue ), [], "clear empties the queue" );
	queue.impact( 8, 1, 2, 5, DESTINATION );
	assert.equal( queueEntries( queue ).length, 1, "the queue still fills after a clear" );
});

test("an impact is the caster's own only when it struck itself; own travel always is", () => {
	const queue = createDisplacementQueue();
	queue.impact( 7, 1, 2, 4, DESTINATION );
	queue.impact( 7, 1, 1, 5, DESTINATION );
	queue.own( 7, 1, 2, DESTINATION );
	const [struckOther, struckSelf, travel] = queueEntries( queue );
	assert.equal( struckOther.self, false );
	assert.equal( struckOther.gid, 2 );
	assert.equal( struckSelf.self, true );
	assert.equal( travel.self, true );
	assert.equal( travel.gid, 1 );
	assert.equal( travel.kind, 2 );
});
