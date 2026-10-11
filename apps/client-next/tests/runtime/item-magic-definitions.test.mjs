/*
===========================================================================

item-magic-definitions.test.mjs - the option rows an item's tooltip grades by

An item's tooltip reads each option's own definition and the same option's
definition at the item's degree (55B608: degree = (class - 1) / 3 + 1,
toward zero), which carries the bracket words that grade it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { itemMagicDefinitions } = await import( "../../src/engine/foundation/ui/item-tooltip-magic.ts" );

const STR_1 = { paramId: 10, optionName: "MATTR_STR", paramName: "str+", degree: 1 };
const STR_3 = { paramId: 30, optionName: "MATTR_STR", paramName: "str+", degree: 3 };
const HP_3 = { paramId: 31, optionName: "MATTR_HP", paramName: "hp+", degree: 3 };
const table = new Map( [ STR_1, STR_3, HP_3 ].map( r => [ r.paramId, r ] ) );
const encode = ( paramId, value ) => ((BigInt( value ) << 32n) | BigInt( paramId )).toString();

test("an option is read with its own row and its row at the item's degree", () => {
	// Class 7..9 is degree 3.
	const ids = itemMagicDefinitions( [ encode( 10, 5 ) ], table, 8 ).map( r => r.paramId ).sort();
	assert.deepEqual( ids, [ 10, 30 ] );
});

test("class zero is degree one, and an unknown option is skipped", () => {
	assert.deepEqual( itemMagicDefinitions( [ encode( 10, 1 ), encode( 99, 1 ) ], table, 0 ).map( r => r.paramId ), [
		10
	] );
	assert.deepEqual( itemMagicDefinitions( [], table, 8 ), [] );
});
