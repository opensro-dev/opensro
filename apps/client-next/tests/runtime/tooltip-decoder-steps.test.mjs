/*
===========================================================================

tooltip-decoder-steps.test.mjs - the skill catalogue decodes in bounded steps

The HUD decodes the 27,835-row skill catalogue a slice per frame. A stepped
decode must publish nothing until every row is admitted, then exactly the
catalogue a whole decode produces; a malformed row late in the catalogue
still rejects it. The character owner's appearance references must equal
the msch blocks of the full decode.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { createTooltipSkillDecoder, decodeTooltipSkills, tooltipAppearanceReferences } = await import(
	"../../src/engine/foundation/ui/skill-tooltip-catalog.ts"
);
const source = JSON.parse( readFileSync( "../../.generated/client-public/assets/data/skillData.json", "utf8" ) );
const whole = decodeTooltipSkills( source );

test("a stepped decode publishes nothing early, then the whole decode's catalogue", () => {
	const decoder = createTooltipSkillDecoder( source ), slice = 1024;
	let steps = 0, catalog = null;
	while ( !catalog ) {
		catalog = decoder.step( slice );
		steps++;
		if ( steps * slice < source.rows.length ) assert.equal( catalog, null );
	}
	assert.equal( steps, Math.ceil( source.rows.length / slice ) );
	assert.deepStrictEqual( [ ...catalog.entries() ], [ ...whole.entries() ] );
	assert.deepStrictEqual( [ ...catalog.groups.entries() ], [ ...whole.groups.entries() ] );
	// A finished decoder keeps returning the same catalogue.
	assert.equal( decoder.step( slice ), catalog );
});

test("a malformed row late in the catalogue rejects it on the step that reaches it", () => {
	const rows = source.rows.slice( 0, 3000 );
	const cells = rows[2500].split( "\t" );
	cells[3] = "1.5";
	rows[2500] = cells.join( "\t" );
	const decoder = createTooltipSkillDecoder( { ...source, rows } );
	assert.equal( decoder.step( 2048 ), null );
	assert.throws( () => decoder.step( 2048 ), /scalar/ );
});

test("appearance references equal the msch blocks of the full decode and share its admission", () => {
	const expected = new Map();
	for ( const row of whole.values() ) {
		const block = row.directTooltipParams.nativeParamBlocks.slice().reverse().find( b => b.offset === 0x268 );
		if ( block ) expected.set( row.id, { type: block.values[0], cap: block.values[1] } );
	}
	assert.ok( expected.size > 0 );
	assert.deepStrictEqual( [ ...tooltipAppearanceReferences( source ).entries() ], [ ...expected.entries() ] );
	const duplicate = { ...source, rows: [ source.rows[0], source.rows[0] ] };
	assert.throws( () => decodeTooltipSkills( duplicate ), /Duplicate/ );
	assert.throws( () => tooltipAppearanceReferences( duplicate ), /Duplicate/ );
	const cells = source.rows[0].split( "\t" );
	cells[20] = "987654321";
	const dangling = { ...source, rows: [ cells.join( "\t" ) ] };
	assert.throws( () => decodeTooltipSkills( dangling ), /chain/ );
	assert.throws( () => tooltipAppearanceReferences( dangling ), /chain/ );
});

test("each skill data owner gets its own published file; the references come from this decoder", () => {
	// buildSkillDataAsset.mjs: one 15 MB file used to be parsed by the HUD, audio
	// and character owners alike. Each now reads only its own plane.
	const read = name => JSON.parse( readFileSync( "../../.generated/client-public/assets/data/" + name, "utf8" ) );
	const audio = read( "skillAudioData.json" ), action = read( "characterActionData.json" );
	for (
		const moved of [
			"skillAudioRows",
			"characterActionEffectRows",
			"characterShadowSizes",
			"effectAppearanceStores"
		]
	) {
		assert.equal( moved in source, false, moved + " left the catalogue" );
	}
	assert.equal( audio.format, "sro-skill-audio" );
	assert.equal( audio.skillAudioRows.length, source.rows.length );
	assert.equal( action.format, "sro-character-action" );
	assert.ok( action.characterActionEffectRows.length > 0 && action.effectAppearanceStores.length === 2 );
	assert.deepStrictEqual(
		action.effectAppearanceReferences,
		[ ...tooltipAppearanceReferences( source ) ].map( ( [id, { type, cap }] ) => [ id, type, cap ] )
	);
});
