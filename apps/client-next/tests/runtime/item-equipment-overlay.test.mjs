/*
===========================================================================

item-equipment-overlay.test.mjs - tests for item-equipment-overlay.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { itemEquipmentOverlay: overlay, equipmentWarningUv, wearerSex } = await import(
	sourceFileUrl( "src/engine/foundation/ui/item-equipment-overlay.ts" ).href
);
const flags = ( group, sub = 1 ) => 0x2c | (group << 7) | (sub << 11);
const item = ( fields = {}, extra = {} ) => ({
	slot: 13,
	typeFlags: flags( 6, 2 ),
	durability: 30,
	tooltip: { fields },
	...extra
});
const progression = {
	level: 10,
	masteries: [ { id: 513, level: 5 }, { id: 518, level: 10 } ],
	stats: { strength: 20, intellect: 30 }
};
const check = ( i, worn = [] ) => overlay( i, progression, { country: 0, sex: 1 }, worn );
test("native equipment overlay distinguishes requirements from durability-only failure", () => {
	assert.equal( check( item() ), null );
	for (
		const fields of [ { requiredLevel: 11, reqLevelType1: 1 }, { reqStr: 21 }, { reqInt: 31 }, { reqGender: 0 }, {
			country: 1
		}, { reqLevelType1: 513, requiredLevel: 6 } ]
	) assert.equal( check( item( fields ) ), "icon_disable" );
	assert.equal( check( item( { maxDurability: 50 }, { durability: 0 } ) ), "icon_item_broken" );
	assert.equal( check( item( { maxDurability: 50, reqGender: 0 }, { durability: 0 } ) ), "icon_disable" );
	assert.equal( check( item( { maxDurability: 0 }, { durability: 0, typeFlags: flags( 5 ) } ) ), null );
	assert.equal( check( item( { country: 3, reqGender: 2 } ) ), null );
});
test("native quad alternatives and missing mastery records are not all-of requirements", () => {
	assert.equal(
		check( item( { reqLevelType1: 513, requiredLevel: 6, reqLevelType2: 518, requiredLevel2: 10 } ) ),
		null
	);
	assert.equal( check( item( { reqLevelType1: 999, requiredLevel: 100 } ) ), null );
	assert.equal(
		check( item( { reqLevelType1: 1, requiredLevel: 11, reqLevelType2: 1, requiredLevel2: 10 } ) ),
		null
	);
});
test("clothes incompatibility includes the replaced socket but permits light/heavy combinations", () => {
	const worn = [ item( {}, { slot: 0, typeFlags: flags( 1 ) } ) ];
	for ( const group of [ 2, 3, 10, 11 ] ) {
		assert.equal( check( item( {}, { typeFlags: flags( group ) } ), worn ), "icon_disable" );
	}
	assert.equal(
		check( item( {}, { typeFlags: flags( 2 ) } ), [ item( {}, { slot: 0, typeFlags: flags( 3 ) } ) ] ),
		null
	);
	assert.equal(
		check( item( {}, { typeFlags: flags( 1 ) } ), [ item( {}, { slot: 13, typeFlags: flags( 3 ) } ) ] ),
		null
	);
});
test("the wearer sex is the live model's, with the login roster only before it is known", () => {
	// The roster lists creation's gender (0 male, 1 female); the live
	// selector is the model value ReqGender speaks (1 male, 0 female).
	assert.equal( wearerSex( undefined, 0 ), 1 );
	assert.equal( wearerSex( undefined, 1 ), 0 );
	assert.equal( wearerSex( undefined, undefined ), undefined );
	// After a gender change the roster still lists the old gender; the
	// live model must win or the new wrong-gender items stay unmarked
	// (#4126). Created male, now female: an item cut for males is
	// refused (drawn disabled), where the roster alone allowed it.
	assert.equal( wearerSex( 0, 0 ), 0 );
	assert.equal(
		overlay( item( { reqGender: 1 } ), progression, { country: 0, sex: wearerSex( 0, 0 ) }, [] ),
		"icon_disable"
	);
	// Created female, now male: an item cut for females is refused.
	assert.equal( wearerSex( 1, 1 ), 1 );
	assert.equal(
		overlay( item( { reqGender: 0 } ), progression, { country: 0, sex: wearerSex( 1, 1 ) }, [] ),
		"icon_disable"
	);
	// An item cut for the wearer's new sex still passes, and one cut
	// for both always does.
	assert.equal( overlay( item( { reqGender: 0 } ), progression, { country: 0, sex: wearerSex( 0, 0 ) }, [] ), null );
	assert.equal( overlay( item( { reqGender: 2 } ), progression, { country: 0, sex: wearerSex( 0, 0 ) }, [] ), null );
});
test("ammunition and consumables do not acquire equipment refusal overlays", () => {
	assert.equal( check( item( { country: 1, reqGender: 0 }, { typeFlags: 0x6c | (4 << 7) | (1 << 11) } ) ), null );
});

test("low durability warning uses native threshold, exclusions and sprite cycle", () => {
	for ( let durability = 1; durability <= 6; durability++ ) {
		assert.equal( check( item( { maxDurability: 50 }, { durability } ) ), "icon_item_warning" );
	}
	assert.equal( check( item( { maxDurability: 50 }, { durability: 7 } ) ), null );
	for ( const group of [ 5, 12 ] ) {
		assert.equal( check( item( { maxDurability: 50 }, { durability: 1, typeFlags: flags( group ) } ) ), null );
	}
	assert.equal( check( item( { maxDurability: 50, reqStr: 100 }, { durability: 1 } ) ), "icon_disable" );
	assert.deepEqual( equipmentWarningUv( 0 ), [ 0, 0, 1 / 8, 1 / 4 ] );
	assert.deepEqual( equipmentWarningUv( 16 ), [ 7 / 8, 1 / 4, 1 / 8, 1 / 4 ] );
	assert.deepEqual( equipmentWarningUv( 17 ), equipmentWarningUv( 0 ) );
});
