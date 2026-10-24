/*
===========================================================================

item-tooltip-stone.test.mjs - tests for item-tooltip-stone.ts

Two stones of the same name and degree merge only when their assimilation
values match, so the tooltip must show that value (native 557C50). Loads
the TypeScript sources through the shared native loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { itemTooltip } = await import( "../../src/engine/foundation/ui/item-tooltip.ts" );

const ATTRIBUTE_STONE_FLAGS = 0x6c | 11 << 7 | 2 << 11;
const MAGIC_STONE_FLAGS = 0x6c | 11 << 7 | 1 << 11;

/*
================
stone
================
*/
const stone = ( typeFlags, plus ) => ({
	name: "Attribute stone of spirit (Lv.9)",
	typeFlags,
	plus,
	magic: [],
	variance: "0",
	durability: 0,
	quantity: 1,
	tooltip: { fields: { itemParam1_29c: 9, maxStack: 500 } }
});

/*
================
values
================
*/
const values = item => itemTooltip( item, { masteries: [] }, key => key ).map( r => r.value );

test("an attribute stone shows its sort, target degree and assimilation value in native order", () => {
	const rows = values( stone( ATTRIBUTE_STONE_FLAGS, 30 ) );
	const sort = rows.indexOf( "UIIT_STT_ITEM_TYPE: UIIT_STT_ITEM_TYPE_ALCHEMY_ATTRSTONE" );
	assert.ok( sort > 0 );
	assert.deepEqual( rows.slice( sort, sort + 4 ), [
		"UIIT_STT_ITEM_TYPE: UIIT_STT_ITEM_TYPE_ALCHEMY_ATTRSTONE",
		"UIIT_STT_TARGET_ITEM_CLASS: 9UIIT_STT_CLASS",
		" ",
		"PARAM_ATTR_ASSIMILATION" + "UIIT_STT_PROBABILITY: 30%"
	] );
	// The stone block precedes the stack rows, as 56B2E0 orders them.
	assert.ok( rows.indexOf( "UIIT_STT_AMOUNT 1" ) > sort + 3 );
	// The name never carries the value, so the assimilation row is the only tell.
	assert.equal( rows[0], "Attribute stone of spirit (Lv.9)" );
});

test("two stones that cannot merge show different assimilation values", () => {
	const a = values( stone( ATTRIBUTE_STONE_FLAGS, 30 ) ), b = values( stone( ATTRIBUTE_STONE_FLAGS, 41 ) );
	assert.notDeepEqual( a, b );
	assert.ok( b.includes( "PARAM_ATTR_ASSIMILATION" + "UIIT_STT_PROBABILITY: 41%" ) );
});

test("a magic stone names its own sort and other etc items get no stone rows", () => {
	assert.ok(
		values( stone( MAGIC_STONE_FLAGS, 7 ) ).includes( "UIIT_STT_ITEM_TYPE: UIIT_STT_ITEM_TYPE_ALCHEMY_MAGICSTONE" )
	);
	const material = values( stone( 0x6c | 11 << 7 | 3 << 11, 7 ) );
	assert.ok( !material.some( v => v.includes( "PARAM_ATTR_ASSIMILATION" ) ) );
	const potion = values( stone( 0x6c | 1 << 7 | 1 << 11, 7 ) );
	assert.ok( !potion.some( v => v.includes( "UIIT_STT_TARGET_ITEM_CLASS" ) ) );
});
