/*
===========================================================================

beginnerQuests.test.mjs - optional quest display data joins the server manifest

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import "../../../apps/client-next/tests/helpers/native-source-loader.mjs";
import {
	appendBeginnerQuestProjection,
	beginnerQuestProjection,
	readBeginnerQuestContent
} from "../../build/shared/beginnerQuests.mjs";

const { decodeQuestPresentation, questObjectivePresentation } = await import(
	"../../../apps/client-next/src/engine/foundation/ui/quest-presentation.ts"
);

/*
================
emptyCatalog
================
*/
function emptyCatalog() {
	return { rows: [], giveupWarnBytes: [], countryBytes: [], progressClearBytes: [], textEntries: {} };
}

test("beginner projection names all milestones and binding rules", () => {
	const content = readBeginnerQuestContent();
	assert.deepEqual( content.milestones.map( m => m.level ), [ 8, 10, 16, 20, 24, 30, 32, 40, 42, 50, 52, 60, 64 ] );
	const catalog = emptyCatalog();
	appendBeginnerQuestProjection( catalog );
	assert.equal( catalog.rows.length, 26 );
	assert.deepEqual( catalog.countryBytes, [ ...Array( 13 ).fill( 0 ), ...Array( 13 ).fill( 1 ) ] );
	for ( const row of catalog.rows ) {
		const fields = row.split( "\t" );
		for ( const symbol of fields.slice( 2 ) ) assert.ok( catalog.textEntries[symbol], symbol );
		assert.match( catalog.textEntries[fields[4]], /permanently character-bound/ );
		assert.match( catalog.textEntries[fields[4]], /cannot be traded, sold, dropped/ );
	}
	assert.equal( new Set( catalog.rows.map( row => row.split( "\t" )[0] ) ).size, 26 );
	assert.throws( () => appendBeginnerQuestProjection( catalog ), /collision/ );
});

test("the shipped client decodes the journal and formats beginner progress", () => {
	const catalog = { version: 5, ...emptyCatalog() };
	appendBeginnerQuestProjection( catalog );
	const presentation = decodeQuestPresentation( catalog );
	assert.equal( presentation.records[60008].title, "Beginner: Degree 2 Moon Weapon (Lv. 8)" );
	assert.match( presentation.records[60164].rewardBody, /degree 8 Seal of Moon weapon/ );
	const objective = questObjectivePresentation( {
		tag: 1,
		kind: 1,
		description: "SN_CON_QPORT_BEGINNER_CH_08",
		objectiveSentinel: false,
		objectiveValues: [ 7 ]
	}, presentation.text );
	assert.equal( objective.description, "Water Ghost: 7 / 30" );
});

test("both routes expose readable names for every Moon weapon choice", () => {
	const content = readBeginnerQuestContent();
	const projection = beginnerQuestProjection( content );
	assert.equal( content.weapons.length, 14 );
	for ( const milestone of content.milestones.filter( m => m.degree > 0 ) ) {
		for ( const family of content.weapons ) {
			const label = projection.textEntries[`SN_PORT_BEGINNER_${family}_${milestone.degree}`];
			assert.match( label, /Seal of Moon/ );
			assert.match( label, /character-bound/ );
			const prompt = projection.textEntries[`SN_PORT_BEGINNER_CONFIRM_${family}_${milestone.degree}`];
			assert.match( prompt, /Claim one degree/ );
			assert.match( prompt, /Your choice is final/ );
		}
	}
});
