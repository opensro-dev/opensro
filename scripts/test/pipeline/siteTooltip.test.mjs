/*
===========================================================================

siteTooltip.test.mjs - the website's item tooltip entry refuses what it cannot read

itemTooltipRows takes an item instance from the public API: a plus, the
native variance u64 and the native encoded magic options, all as given.
Anything else is refused rather than drawn as a different item. The rows
it draws are pinned against the shipped bundle by the catalogue build's
golden vectors (tooltipBundle.mjs).

===========================================================================
*/
import "../../../apps/client-next/tests/helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
const { createMagicTable, itemTooltipRows, TOOLTIP_FORMAT } = await import( "../../build/catalogue/siteTooltip.mjs" );

// A minimal item: a name and a zero durability range, no stats.
const ITEM = { name: "Test item", typeFlags: 0, fields: { varianceIntMin1c0: 0, varianceIntMax1c4: 0 } };
const text = () => "";

test("a plain instance draws its name row", () => {
	const rows = itemTooltipRows( { refObjId: 1 }, ITEM, createMagicTable( [] ), text );
	assert.equal( rows[0]?.text, "Test item" );
	assert.equal( TOOLTIP_FORMAT, 1 );
});

test("an instance outside the native forms is refused", () => {
	const table = createMagicTable( [] );
	for (
		const instance of [
			{ refObjId: 1, plus: -1 },
			{ refObjId: 1, plus: 256 },
			{ refObjId: 1, variance: 31 },
			{ refObjId: 1, variance: "-1" },
			{ refObjId: 1, magic: "1" },
			{ refObjId: 1, magic: [ 1 ] },
			{ refObjId: 1, magic: [ "0x10" ] }
		]
	) {
		assert.throws(
			() => itemTooltipRows( instance, ITEM, table, text ),
			/Invalid item/,
			JSON.stringify( instance )
		);
	}
});
