/*
===========================================================================

quest-presentation.test.mjs - tests for quest-presentation.ts,
quest-timers.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import fc from "fast-check";

const { questObjectivePresentation } = await import(
	sourceFileUrl( "src/engine/foundation/ui/quest-presentation.ts" ).href
);
const content = ( fields = {} ) => ({
	tag: 1,
	kind: 1,
	description: "objective",
	objectiveSentinel: false,
	objectiveValues: [],
	...fields
});

test("quest objective formatting uses wire signed integers in order and preserves zero", () => {
	assert.equal(
		questObjectivePresentation( content( { objectiveValues: [ 0, 7, 0xffffffff, 0x80000000 ] } ), {
			objective: "%d / %d / %d / %d"
		} ).description,
		"0 / 7 / -1 / -2147483648"
	);
	fc.assert(
		fc.property( fc.integer( { min: 0, max: 0xffffffff } ), value => {
			const expected = value > 0x7fffffff ? value - 0x100000000 : value;
			assert.equal(
				questObjectivePresentation( content( { objectiveValues: [ value ] } ), { objective: "Count %d" } )
					.description,
				"Count " + expected
			);
		} ),
		{ seed: 15054870, numRuns: 256 }
	);
});

test("no-argument and missing-symbol branches never invent objective progress", () => {
	assert.equal(
		questObjectivePresentation( content(), { objective: "Talk to the NPC" } ).description,
		"Talk to the NPC"
	);
	assert.equal( questObjectivePresentation( content(), { objective: "Count %d" } ).description, "Count %d" );
	assert.equal(
		questObjectivePresentation( content( { objectiveSentinel: true, objectiveValues: [ 8 ] } ), {
			objective: "Count %d"
		} ).description,
		"Count %d"
	);
	assert.equal( questObjectivePresentation( content( { objectiveValues: [ 8 ] } ), {} ).description, "" );
	assert.equal(
		questObjectivePresentation( content( { objectiveValues: [ 8 ] } ), { objective: "%d / %d" } ).description,
		"8 / %d"
	);
});

test("all content-kind bytes choose native status and both-child color without mutating authority", () => {
	for ( let kind = 0; kind < 256; kind++ ) {
		const row = Object.freeze( content( { kind, objectiveValues: Object.freeze( [ 3 ] ) } ) );
		const result = questObjectivePresentation( row, { objective: "Count %d" } ),
			complete = kind === 0 || kind === 2;
		assert.equal( result.statusKey, complete ? "UIIT_STT_QUEST_END" : "UIIT_STT_QUEST_ING" );
		assert.deepEqual(
			result.color,
			complete ? [ 1, 156 / 255, 104 / 255, 1 ] : [ 239 / 255, 218 / 255, 164 / 255, 1 ]
		);
		assert.equal( row.kind, kind );
	}
});

test("every published objective format stays inside the supported native grammar", () => {
	const { textEntries } = JSON.parse(
		readFileSync( CLIENT_PUBLIC_ROOT + "/assets/data/questData.json", "utf8" )
	);
	let formatted = 0;
	for ( const [key, value] of Object.entries( textEntries ) ) {
		if ( !key.startsWith( "SN_CON_" ) ) continue;
		const specifiers = value.match( /%./g ) ?? [];
		assert.ok(
			specifiers.every( s => s === "%d" ),
			"New quest formatter grammar requires native evidence: " + key
		);
		if ( !specifiers.length ) continue;
		formatted++;
		const args = specifiers.map( ( _, i ) => i + 7 ),
			expected = value.split( "%d" ).reduce( ( s, part, i ) => s + (i ? String( args[i - 1] ) : "") + part, "" );
		assert.equal(
			questObjectivePresentation( content( { description: key, objectiveValues: args } ), textEntries )
				.description,
			expected
		);
	}
	assert.ok( formatted > 100, "Exercise the actual published quest corpus" );
});

const { createQuestTimers } = await import( sourceFileUrl( "src/engine/runtime/ui/hud/quest-timers.ts" ).href );
test("quest countdown keeps packet rearm separate from progress updates and retires with its owner", () => {
	const timers = createQuestTimers(), q = { refId: 10, flags: 4, progress: 1 << 20 }, copy = k => k;
	timers.step( [ q ], 100, 2 );
	assert.deepEqual( timers.step( [ q ], 10099, 2 ).notices, [] );
	assert.deepEqual( timers.step( [ q ], 10100, 2 ).notices, [ 50 ] );
	const update = { ...q, flags: 0 };
	timers.step( [ update ], 11000, 2 );
	assert.deepEqual( timers.step( [ update ], 20100, 2 ).notices, [ 40 ] );
	const rearm = { ...q };
	timers.step( [ rearm ], 21000, 2 );
	assert.deepEqual( timers.step( [ rearm ], 31000, 2 ).notices, [ 50 ] );
	timers.step( [], 32000, 2 );
	assert.deepEqual( timers.step( [], 999999, 2 ).notices, [] );
	timers.step( [ rearm ], 1000000, 2 );
	timers.reset();
	assert.equal( timers.text( { ...q, progress: 0xffffffff }, copy ), "UIIT_STT_QUEST_UNLIMITED" );
});
test("quest countdown advances one native tick after a delayed frame and switches to seconds", () => {
	const timers = createQuestTimers(), q = { refId: 10, flags: 0, progress: 1 << 20 };
	timers.step( [ q ], 0, 2 );
	assert.deepEqual( timers.step( [ q ], 3600000, 2 ).notices, [ 50 ] );
	for ( let i = 1; i <= 4; i++ ) {
		assert.deepEqual( timers.step( [ q ], 3600000 + i * 10000, 2 ).notices, [ 50 - i * 10 ] );
	}
	assert.deepEqual( timers.step( [ q ], 3640999, 2 ).notices, [] );
	assert.deepEqual( timers.step( [ q ], 3641000, 2 ).notices, [ 9 ] );
	for ( let i = 2; i <= 10; i++ ) timers.step( [ q ], 3640000 + i * 1000, 2 );
	assert.equal( timers.step( [ q ], 4000000, 2 ).changed, false );
});
