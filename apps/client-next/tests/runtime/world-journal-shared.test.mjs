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

test("journalCost charges a repeated object once when given a set", () => {
	const row = catalogueRow( 1 );
	const pair = { first: row, second: row, third: [ row, row ] };
	const shared = journalCost( pair, new WeakSet() );
	assert.ok( shared < journalCost( pair ) / 2, `a repeated object costs references only: ${shared}` );
	assert.equal( journalCost( pair ), journalCost( { first: row, second: row, third: [ row, row ] } ) );
});

/*
================
catalogueRow

One skill-catalogue row at the shape skill-catalog.ts publishes: plain
data, a few hundred bytes of JSON, and nested requirement rows the
snapshot's other fields point back into.
================
*/
function catalogueRow( id ) {
	return {
		id,
		group: 7,
		level: 3,
		name: `SKILL_EU_SWORD_LIGHTNING_${id}`,
		nameSymbol: `SN_SKILL_EU_SWORD_LIGHTNING_${id}`,
		icon: "icon\\skill\\eu\\skill_eu_sword_lightning.ddj",
		masteries: [ { ID: 11, Level: 2 }, { ID: 12, Level: 0 } ],
		prerequisites: [ { ID: 101, Level: 1 }, { ID: 102, Level: 1 }, { ID: 103, Level: 0 } ],
		trainable: true,
		targetRequired: false,
		targetSelf: false,
		groundTarget: false,
		cooldownMs: 12345,
		spCost: 42,
		reqStr: 10,
		reqInt: 10
	};
}

/*
================
catalogueSnapshot

A gameplay snapshot the way core.ts publishes one after a portal
bootstrap: the catalogue array itself, the learned-skill wrappers that
each point at one catalogue row, and the quick-slot pages that each
point at the whole catalogue. One structured clone sends every row
once; JSON-style counting charges it once per reference - the
2026-10-19 cave entry (705 floor spawns behind one such snapshot)
charged 33 MB for the snapshot alone and died on the 32 MiB budget.
================
*/
function catalogueSnapshot() {
	const skillCatalog = Array.from( { length: 1200 }, ( _, i ) => catalogueRow( i + 1 ) );
	return {
		skillCatalog,
		skills: skillCatalog.map( row => ({ skill: row, cooldown: 0, queued: false }) ),
		quickSlots: Array.from( { length: 40 }, () => skillCatalog )
	};
}

/*
================
floorSpawn

One cave-floor spawn at the size entities.ts journals (region 32769,
the Donwhang cave entry of the incident).
================
*/
function floorSpawn( i ) {
	return {
		gid: 200000 + i,
		kind: "monster",
		refObjId: 1933,
		regionId: 32769,
		x: 100 + i,
		y: 12,
		z: 900,
		heading: 0,
		name: ""
	};
}

test("a portal gameplay snapshot plus 705 floor spawns fits the journal budget", () => {
	const event = { kind: "gameplay", state: catalogueSnapshot() };
	assert.ok( journalCost( event ) > (32 << 20), "the fixture is the failing size" );
	assert.ok(
		journalCost( event, new WeakSet() ) < (4 << 20),
		"one clone sends the snapshot's shared rows once"
	);
	const owner = createEntities();
	owner.publish( event );
	for ( let i = 0; i < 705; i++ ) owner.publish( { kind: "spawn", entity: floorSpawn( i ) } );
	const batch = owner.take();
	assert.equal( batch.events.length, 706 );
	owner.ack( batch.sequence );
});
