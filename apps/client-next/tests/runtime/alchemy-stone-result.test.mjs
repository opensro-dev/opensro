/*
===========================================================================

alchemy-stone-result.test.mjs - what a successful stone says in the chat

Port-only, not native; owner-approved deviation (2026-10-13): a magic or
attribute stone names each stat it changed, from the old value to the new,
in the tooltip's own words. An elixir keeps the native enhancement-level
line (62B0B0, UIIT_MSG_REINFORCERR_SUCCESS).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { alchemyResultLines } = await import( "../../src/engine/foundation/ui/alchemy-result.ts" );

const copy = key => (key === "UIIT_MSG_REINFORCERR_SUCCESS" ? "level [%d]" : key);

/*
================
optionItem

An equipment item with one magic option; the 1..9 bracket grades it.
================
*/
function optionItem( optionName, amount ) {
	return {
		slot: 13,
		refObjId: 4161,
		typeFlags: 13100,
		quantity: 1,
		plus: 2,
		durability: 53,
		variance: "0",
		magic: amount ? [ String( BigInt( amount ) << 32n | 9n ) ] : [],
		magicReferences: [ { paramId: 9, optionName, degree: 1, paramName: "+", rangeWords: [ 65545, 0, 0 ] } ]
	};
}

/*
================
weapon

A weapon whose physical attack variance is `grade` of 31.
================
*/
function weapon( grade ) {
	const fields = { itemClass: 1 };
	for (
		const name of [
			"varianceIntMin254",
			"varianceIntMax258",
			"varianceIntMin25c",
			"varianceIntMax260",
			"varianceFloatPerPlus264",
			"varianceIntMin288",
			"varianceIntMax28c",
			"varianceFloatPerPlus290",
			"varianceIntMin294",
			"varianceIntMax298",
			"varianceFloatMin268",
			"varianceFloatMax26c",
			"varianceFloatMin270",
			"varianceFloatMax274",
			"varianceFloatMin278",
			"varianceFloatMax27c",
			"varianceFloatMin280",
			"varianceFloatMax284"
		]
	) fields[name] = 0;
	Object.assign( fields, {
		varianceIntMin1c0: 0,
		varianceIntMax1c4: 0,
		varianceIntMin240: 10,
		varianceIntMax244: 41,
		varianceIntMin248: 20,
		varianceIntMax24c: 51,
		varianceFloatPerPlus250: 0
	} );
	return {
		slot: 13,
		refObjId: 4000,
		typeFlags: 0x132c,
		quantity: 1,
		plus: 2,
		durability: 0,
		variance: String( BigInt( grade ) << 20n ),
		magic: [],
		tooltip: { fields }
	};
}

const success = stone => ({
	sequence: 1,
	flags: 0x10,
	plus: 2,
	previousPlus: 2,
	durability: 53,
	previousDurability: 53,
	...(stone ? { stone } : {})
});

test("a magic stone names the option it raised, old value to new", () => {
	assert.deepEqual(
		alchemyResultLines(
			success( { before: optionItem( "MATTR_STR", 5 ), after: optionItem( "MATTR_STR", 6 ) } ),
			copy
		),
		[ "PARAM_STR changed from 5 PARAM_INCREASE (+50%) to 6 PARAM_INCREASE (+62%)" ]
	);
	// An option the item did not have changed from 0.
	assert.deepEqual(
		alchemyResultLines(
			success( { before: optionItem( "MATTR_STR", 0 ), after: optionItem( "MATTR_STR", 1 ) } ),
			copy
		),
		[ "PARAM_STR changed from 0 to 1 PARAM_INCREASE (+0%)" ]
	);
});

test("an attribute stone names the white stat it raised, old value to new", () => {
	assert.deepEqual( alchemyResultLines( success( { before: weapon( 0 ), after: weapon( 31 ) } ), copy ), [
		"PARAM_PA changed from 10 ~ 20 (+0%) to 41 ~ 51 (+100%)"
	] );
});

test("an elixir keeps the native enhancement-level line", () => {
	assert.deepEqual( alchemyResultLines( success(), copy ), [ "level [2]" ] );
	// A stone that changed nothing visible falls back to the native line.
	assert.deepEqual(
		alchemyResultLines( success( { before: weapon( 5 ), after: weapon( 5 ) } ), copy ),
		[ "level [2]" ]
	);
});
