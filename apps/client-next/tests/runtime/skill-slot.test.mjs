/*
===========================================================================

skill-slot.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const load = async entry => {
	return import( sourceFileUrl( entry ).href );
};
const { SkillSlot_Resolve, SKILL_SLOT_PREVIEW_ALPHA } = await load( "src/engine/runtime/ui/hud/skill-slot.ts" );
const { createSkillTrainingContext } = await load( "src/engine/foundation/gameplay/skill-catalog.ts" );

// Mastery 257 at level 10; group 1 (ranks 11/12) is a prerequisite of
// group 2 (rank 21) at level 2.
const none = { ID: 0, Level: 0 };
const meta = ( id, group, level, prerequisite = none, masteryLevel = 1 ) => ({
	id,
	group,
	level,
	name: "S" + id,
	trainable: true,
	targetRequired: false,
	spCost: 0,
	cooldownMs: 0,
	masteries: [ { ID: 257, Level: masteryLevel }, none ],
	prerequisites: [ prerequisite, none, none ]
});
const catalog = [
	meta( 11, 1, 1 ),
	meta( 12, 1, 2 ),
	meta( 21, 2, 1, { ID: 1, Level: 2 } ),
	meta( 31, 3, 1, none, 50 )
];
const cell = ( id, group, level ) => ({
	id,
	group,
	level,
	mastery: 257,
	row: 0,
	column: 0,
	icon: "skill/s" + id + ".ddj",
	name: "S" + id,
	study: ""
});
const groupOne = [ cell( 11, 1, 1 ), cell( 12, 1, 2 ) ],
	groupTwo = [ cell( 21, 2, 1 ) ],
	groupThree = [ cell( 31, 3, 1 ) ];
const progression = {
	masteries: [ { id: 257, level: 10 } ],
	skillPoints: 1000,
	stats: { strength: 100, intellect: 100 }
};
const resolve = ( candidates, learned ) =>
	SkillSlot_Resolve( {
		candidates,
		training: createSkillTrainingContext( learned, catalog ),
		masteries: progression.masteries,
		progression
	} );

test("588AF0: an unlearned skill with an untrained predecessor is dimmed, not drawn active", () => {
	const slot = resolve( groupTwo, [] );
	assert.equal( slot.owned, undefined );
	assert.deepEqual( slot.icon, { kind: "skill", icon: "skill/s21.ddj" } );
	assert.equal( slot.alpha, SKILL_SLOT_PREVIEW_ALPHA );
	assert.equal( slot.button.kind, "none", "prerequisites still gate the button" );
});

test("the same preview stays dimmed once its predecessor is trained, and offers the add button", () => {
	const slot = resolve( groupTwo, [ 11, 12 ] );
	assert.equal( slot.alpha, SKILL_SLOT_PREVIEW_ALPHA );
	assert.equal( slot.button.kind, "learn" );
	assert.equal( slot.button.upgrade, false );
});

test("589050: below the mastery level the preview shows the mastery-disable icon, opaque", () => {
	const slot = resolve( groupThree, [] );
	assert.deepEqual( slot.icon, { kind: "mastery-disable" } );
	assert.equal( slot.alpha, 1 );
});

test("state 0: a learned rank is opaque, offers the next rank, then the max marker", () => {
	const first = resolve( groupOne, [ 11 ] );
	assert.equal( first.owned.id, 11 );
	assert.equal( first.alpha, 1 );
	assert.equal( first.button.kind, "learn" );
	assert.equal( first.button.skill.id, 12 );
	assert.equal( first.button.upgrade, true );
	const last = resolve( groupOne, [ 11, 12 ] );
	assert.equal( last.owned.id, 12 );
	assert.equal( last.button.kind, "max" );
});

test("an empty column binds nothing", () => {
	const slot = resolve( [], [] );
	assert.equal( slot.entry, undefined );
	assert.deepEqual( slot.icon, { kind: "empty" } );
	assert.equal( slot.button.kind, "none" );
});
