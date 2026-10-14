/*
===========================================================================

items.test.mjs - the item catalog, its artwork and the MAKEITEM command

The real-catalog test reads the server's item export under .generated. The
source gates run without game data, so it skips there; the full pipeline sets
SRO_REQUIRE_GAME_DATA=1 (run_check_pipeline.mjs), which turns the skip into a
failure, as the Go server's licensed tests do.

===========================================================================
*/
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { filterItems, itemCommand, stackable } from "../public/item-model.js";
import { createItems, ITEM_CATALOG_URL, itemIconPath } from "../server/items.mjs";

// Why the real-catalog test does not run here, or false when it must.
const NO_CATALOG = process.env.SRO_REQUIRE_GAME_DATA !== "1" && !existsSync( fileURLToPath( ITEM_CATALOG_URL ) ) &&
	"no generated item catalog (SRO_REQUIRE_GAME_DATA=1 makes this a failure)";

test("MAKEITEM cannot silently wrap quantities and preserves zero-plus equipment", () => {
	const potion = {
		id: 12,
		name: "MP recovery potion (small)",
		codename: "ITEM_ETC_MP_POTION_02",
		typeFlags: 4332,
		maxStack: 50
	};
	assert.equal( itemCommand( potion, 50 ), "/MAKEITEM ITEM_ETC_MP_POTION_02 50" );
	for ( const amount of [ 0, 51, 256, 1.5, NaN, "", null ] ) assert.equal( itemCommand( potion, amount ), null );
	const equipment = { ...potion, typeFlags: 0 };
	assert.equal( stackable( equipment ), false );
	assert.equal( itemCommand( equipment, 0 ), "/MAKEITEM ITEM_ETC_MP_POTION_02 0" );
	assert.equal( itemCommand( equipment, 13 ), null );
	assert.deepEqual( filterItems( [ potion ], "MP small", "" ), [ potion ] );
	assert.deepEqual( filterItems( [ potion ], "12", "" ), [ potion ] );
});

test("icon paths are restricted to authored item artwork", () => {
	assert.equal( itemIconPath( "item\\etc\\mp_potion_02.ddj" ), "item/etc/mp_potion_02.png" );
	for ( const path of [ "../secret", "item/../../secret.png", "https://example.com/item.png", "item/a%2fb.png" ] ) {
		assert.equal( itemIconPath( path ), null );
	}
});

test( "real exported catalog and MP artwork resolve without player state", { skip: NO_CATALOG }, async () => {
	const source = createItems();
	const catalog = await source.catalog();
	const potion = catalog.items.find( ( i ) => i.codename === "ITEM_ETC_MP_POTION_02" );
	assert.ok( catalog.items.length > 8000 );
	assert.equal( potion.recoveryMP, 220 );
	assert.equal( potion.maxStack, 50 );
	assert.equal( potion.icon, "/api/item-icon/12" );
	assert.deepEqual( [ ...(await source.icon( 12 )).subarray( 0, 8 ) ], [ 137, 80, 78, 71, 13, 10, 26, 10 ] );
	assert.equal( await source.icon( -1 ), null );
} );
