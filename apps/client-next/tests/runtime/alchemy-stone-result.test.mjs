/*
===========================================================================

alchemy-stone-result.test.mjs - what a successful stone says in the chat

Native (owner decision #2377, Option 1): on a successful stone the
presenter (625980) adds one line per attribute it changed -
UIIT_MSG_ALCHEMY_APPEND_ATTR for a newly granted option,
UIIT_MSG_ALCHEMY_CHANGE_CATTR for the attribute the stone designates,
UIIT_MSG_ALCHEMY_ATTR_ASSIMILATION for any other. Alongside them the
port-only deviation (owner-approved 2026-10-13): a magic or attribute
stone names each stat it changed, from the old value to the new, in the
tooltip's own words. An elixir keeps the native enhancement-level line
(62B0B0, UIIT_MSG_REINFORCERR_SUCCESS).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { alchemyResultLines } = await import( "../../src/engine/foundation/ui/alchemy-result.ts" );

const TEMPLATES = {
	UIIT_MSG_REINFORCERR_SUCCESS: "level [%d]",
	UIIT_MSG_ALCHEMY_APPEND_ATTR: "[%s]attribute has been granted on %s.",
	UIIT_MSG_ALCHEMY_CHANGE_CATTR: "[%s]attribute of %s has been changed to [%s].",
	UIIT_MSG_ALCHEMY_ATTR_ASSIMILATION: "[Attribute Assimilation][%s]attribute of %s has been changed to [%s]."
};
const copy = key => TEMPLATES[key] ?? key;

/*
================
optionItem

An equipment item with one magic option; the 1..9 bracket grades it.
================
*/
function optionItem( optionName, amount ) {
	return {
		name: "Blade",
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
twoOptionItem

An item with a Str option and an HP option.
================
*/
function twoOptionItem( str, hp ) {
	return {
		name: "Blade",
		slot: 13,
		refObjId: 4161,
		typeFlags: 13100,
		quantity: 1,
		plus: 2,
		durability: 53,
		variance: "0",
		magic: [ String( BigInt( str ) << 32n | 9n ), String( BigInt( hp ) << 32n | 10n ) ],
		magicReferences: [
			{ paramId: 9, optionName: "MATTR_STR", degree: 1, paramName: "+", rangeWords: [ 65545, 0, 0 ] },
			{ paramId: 10, optionName: "MATTR_HP", degree: 1, paramName: "+", rangeWords: [ 65545, 0, 0 ] }
		]
	};
}

/*
================
weapon

A weapon whose physical attack variance is `grade` of 31 and whose
durability maximum variance is `durGrade` of 31 (30..61).
================
*/
function weapon( grade, durGrade = 0 ) {
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
		varianceIntMin1c0: 30,
		varianceIntMax1c4: 61,
		varianceIntMin240: 10,
		varianceIntMax244: 41,
		varianceIntMin248: 20,
		varianceIntMax24c: 51,
		varianceFloatPerPlus250: 0
	} );
	return {
		name: "Sword",
		slot: 13,
		refObjId: 4000,
		typeFlags: 0x132c,
		quantity: 1,
		plus: 2,
		durability: 0,
		variance: String( BigInt( durGrade ) | (BigInt( grade ) << 20n) ),
		magic: [],
		tooltip: { fields }
	};
}

/*
================
durWeapon

A weapon carrying a durability-increase option of `amount` percent.
================
*/
function durWeapon( amount ) {
	return {
		...weapon( 0, 0 ),
		magic: [ String( BigInt( amount ) << 32n | 11n ) ],
		magicReferences: [
			{ paramId: 11, optionName: "MATTR_DUR", degree: 1, paramName: "+", rangeWords: [ 65545, 0, 0 ] }
		]
	};
}

/*
================
armor

An armour piece whose defence variances (physical index 3, magical
index 4) are `grade` of 31.
================
*/
function armor( grade ) {
	const fields = { itemClass: 1 };
	for (
		const name of [
			"varianceFloatMin1c8",
			"varianceFloatMax1cc",
			"varianceFloatPerPlus1d0",
			"varianceFloatMin1f4",
			"varianceFloatMax1f8",
			"varianceFloatPerPlus1fc",
			"varianceIntMin1d4",
			"varianceIntMax1d8",
			"varianceFloatPerPlus1dc",
			"varianceFloatMin20c",
			"varianceFloatMax210",
			"varianceFloatMin214",
			"varianceFloatMax218"
		]
	) fields[name] = 0;
	Object.assign( fields, {
		varianceIntMin1c0: 30,
		varianceIntMax1c4: 61,
		varianceFloatMax1cc: 31,
		varianceFloatMax1f8: 31
	} );
	return {
		name: "Mail",
		slot: 14,
		refObjId: 4001,
		typeFlags: 0x180,
		quantity: 1,
		plus: 2,
		durability: 40,
		variance: String( (BigInt( grade ) << 15n) | (BigInt( grade ) << 20n) ),
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
		[
			"PARAM_STR changed from 5 PARAM_INCREASE (+50%) to 6 PARAM_INCREASE (+62%)",
			"[Attribute Assimilation][PARAM_STR PARAM_INCREASE]attribute of Blade has been changed to [5 -> 6]."
		]
	);
	// An option the item did not have changed from 0, and is granted.
	assert.deepEqual(
		alchemyResultLines(
			success( { before: optionItem( "MATTR_STR", 0 ), after: optionItem( "MATTR_STR", 1 ) } ),
			copy
		),
		[
			"PARAM_STR changed from 0 to 1 PARAM_INCREASE (+0%)",
			"[PARAM_STR 1 PARAM_INCREASE]attribute has been granted on Blade."
		]
	);
});

test("the designated option reads as a change, a foreign one as assimilation (625980)", () => {
	assert.deepEqual(
		alchemyResultLines(
			success( {
				before: twoOptionItem( 5, 1 ),
				after: twoOptionItem( 6, 2 ),
				designated: "MATTR_STR"
			} ),
			copy
		),
		[
			"PARAM_STR changed from 5 PARAM_INCREASE (+50%) to 6 PARAM_INCREASE (+62%)",
			"PARAM_HP changed from 1 PARAM_INCREASE (+0%) to 2 PARAM_INCREASE (+12%)",
			"[Attribute Assimilation][PARAM_HP PARAM_INCREASE]attribute of Blade has been changed to [1 -> 2].",
			"[PARAM_STR]attribute of Blade has been changed to [5 -> 6]."
		]
	);
	// A newly granted designated option is appended with its amount.
	assert.deepEqual(
		alchemyResultLines(
			success( {
				before: optionItem( "MATTR_STR", 0 ),
				after: optionItem( "MATTR_STR", 1 ),
				designated: "MATTR_STR"
			} ),
			copy
		),
		[
			"PARAM_STR changed from 0 to 1 PARAM_INCREASE (+0%)",
			"[PARAM_STR 1 PARAM_INCREASE]attribute has been granted on Blade."
		]
	);
});

test("an attribute stone names the white stat it raised, old value to new", () => {
	assert.deepEqual( alchemyResultLines( success( { before: weapon( 0 ), after: weapon( 31 ) } ), copy ), [
		"PARAM_PA changed from 10 ~ 20 (+0%) to 41 ~ 51 (+100%)",
		"[Attribute Assimilation][PARAM_PA]attribute of Sword has been changed to [(10 ~ 20) -> (41 ~ 51)]."
	] );
	// The stone's own stat reads as a change (625980 arm 2).
	assert.deepEqual(
		alchemyResultLines(
			success( { before: weapon( 0 ), after: weapon( 31 ), designated: "NATTR_PA" } ),
			copy
		),
		[
			"PARAM_PA changed from 10 ~ 20 (+0%) to 41 ~ 51 (+100%)",
			"[PARAM_PA]attribute of Sword has been changed to [(10 ~ 20) -> (41 ~ 51)]."
		]
	);
});

test("a durability rise the stone did not designate is assimilation (625980 arm 1)", () => {
	assert.deepEqual(
		alchemyResultLines(
			success( { before: weapon( 0, 0 ), after: weapon( 0, 31 ), designated: "NATTR_PA" } ),
			copy
		),
		[
			"PARAM_DUR changed from 0/30 (+0%) to 0/61 (+99%)",
			"[Attribute Assimilation][PARAM_DUR]attribute of Sword has been changed to [30 -> 61].",
			"[PARAM_PA]attribute of Sword has been changed to [(10 ~ 20) -> (10 ~ 20)]."
		]
	);
});

test("a durability option's own stone keeps the white durability arm silent (625980 arm 1)", () => {
	assert.deepEqual(
		alchemyResultLines(
			success( { before: durWeapon( 5 ), after: durWeapon( 10 ), designated: "MATTR_DUR" } ),
			copy
		),
		[
			"PARAM_DUR changed from 0/31 (+0%) to 0/33 (+0%)",
			"PARAM_DUR changed from 5% PARAM_INCREASE (+50%) to 10% PARAM_INCREASE (+112%)",
			"[PARAM_DUR]attribute of Sword has been changed to [5 % -> 10 %]."
		]
	);
});

test("a defence rise prints one decimal, designated or assimilated (625980 arms 8 and 10)", () => {
	assert.deepEqual(
		alchemyResultLines(
			success( { before: armor( 0 ), after: armor( 31 ), designated: "NATTR_PD" } ),
			copy
		),
		[
			"PARAM_PD changed from 0 to 31.0 (+100%)",
			"PARAM_MD changed from 0 to 31.0 (+100%)",
			"[PARAM_PD]attribute of Mail has been changed to [0.0 -> 31.0].",
			"[Attribute Assimilation][PARAM_MD]attribute of Mail has been changed to [0.0 -> 31.0]."
		]
	);
});

test("an elixir keeps the native enhancement-level line", () => {
	assert.deepEqual( alchemyResultLines( success(), copy ), [ "level [2]" ] );
	// A stone that changed nothing visible falls back to the native line.
	assert.deepEqual(
		alchemyResultLines( success( { before: weapon( 5 ), after: weapon( 5 ) } ), copy ),
		[ "level [2]" ]
	);
});
