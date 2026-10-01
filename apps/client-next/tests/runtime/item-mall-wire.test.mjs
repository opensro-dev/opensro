/*
===========================================================================

item-mall-wire.test.mjs - native purchase identity, widths and point contribution

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { mallPurchasePayload } from "../../src/engine/foundation/gameplay/item-mall-wire.ts";

test("mall purchase preserves the native shop address and point contribution", () => {
	const request = { group: 852, shop: 2, tab: 3, slot: 4, quantity: 5, points: 7, packageId: 0x11223344 };
	assert.deepEqual(
		[ ...mallPurchasePayload( request ) ],
		[ 0x18, 0x54, 0x03, 2, 3, 4, 5, 0, 7, 0, 0, 0, 0x44, 0x33, 0x22, 0x11 ]
	);
	for ( const field of Object.keys( request ) ) {
		for ( const value of [ -1, 0.5, NaN, Infinity, 0x100000000 ] ) {
			assert.throws( () => mallPurchasePayload( { ...request, [field]: value } ), field );
		}
	}
	for ( const field of [ "group", "quantity", "packageId" ] ) {
		assert.throws( () => mallPurchasePayload( { ...request, [field]: 0 } ), field );
	}
	for ( const field of [ "shop", "tab", "slot" ] ) {
		assert.throws( () => mallPurchasePayload( { ...request, [field]: 256 } ), field );
	}
	assert.equal( new DataView( mallPurchasePayload( { ...request, points: 0 } ).buffer ).getUint32( 8, true ), 0 );
});
