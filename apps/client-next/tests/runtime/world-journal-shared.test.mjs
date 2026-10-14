/*
===========================================================================

world-journal-shared.test.mjs - a walking path shared by queued state events
counts once per batch (journal-cost.ts, entities.ts sharedArrays)

The 2026-10-14 backlog failure: every HP or LIFE state of a walking monster
carried the same walking path, and the budget charged it to each event
(a 62 KB state, 33 MB with 616 queued) although the batch's structured
clone sends it once.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createEntities } = await load( "src/engine/runtime/simulation/worker/session/world/entities/entities.ts" );
const { journalCost } = await load( "src/engine/foundation/gameplay/journal-cost.ts" );

// About 400 terrain samples: a long chase, as the failing monster held.
const SAMPLES = 400;
// Enough states that full costing passes the 32 MiB budget (~ 64 MB).
const STATES = 1000;

/*
================
walkingPath
================
*/
function walkingPath() {
	return Array.from( { length: SAMPLES }, ( _, i ) => ({
		regionId: 24230,
		x: 1000.123456789 + i,
		y: 12.123456789,
		z: 900.123456789,
		angle: 1234
	}) );
}

/*
================
walker

A walking monster's state, as entities.ts spreads it: the paths stay the
same references from one state to the next.
================
*/
function walker( shared, hp ) {
	const movementPath = { from: shared[0], to: shared[SAMPLES - 1], walkingPath: shared };
	return {
		gid: 471430,
		kind: "monster",
		refObjId: 1933,
		regionId: 24230,
		x: 1000,
		y: 12,
		z: 900,
		heading: 0,
		name: "",
		hp,
		movementPath,
		movementTransition: { relocation: 0, reason: "receipt", eligible: true, walkingPath: shared }
	};
}

test("a walking path shared by many queued states counts once per batch", () => {
	const shared = walkingPath();
	assert.ok(
		journalCost( { kind: "state", entity: walker( shared, 1 ) } ) > 60000,
		"the fixture is the failing size"
	);
	const owner = createEntities();
	for ( let i = 0; i < STATES; i++ ) owner.publish( { kind: "state", entity: walker( shared, i ) } );
	const batch = owner.take();
	assert.equal( batch.events.length, STATES );
	owner.ack( batch.sequence );
	// The next batch is another clone: the path counts again, once.
	for ( let i = 0; i < STATES; i++ ) owner.publish( { kind: "state", entity: walker( shared, i ) } );
	assert.equal( owner.take().events.length, STATES );
});

test("distinct paths still count in full and still hit the budget", () => {
	const owner = createEntities();
	assert.throws( () => {
		for ( let i = 0; i < STATES; i++ ) owner.publish( { kind: "state", entity: walker( walkingPath(), i ) } );
	}, /journal backlog exceeded/ );
});

test("journalCost charges a repeated array once only when given the batch's set", () => {
	const shared = walkingPath(), event = { kind: "state", entity: walker( shared, 1 ) };
	const full = journalCost( event ), set = new WeakSet();
	const first = journalCost( event, set ), second = journalCost( event, set );
	assert.ok( first < full, "the event's own second reference is already shared" );
	assert.ok( second < first / 10, `a repeat costs references only: ${second} vs ${first}` );
	assert.equal( journalCost( event ), full, "without a set nothing is memoised" );
});
