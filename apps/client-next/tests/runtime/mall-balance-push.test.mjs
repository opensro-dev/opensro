/*
===========================================================================

mall-balance-push.test.mjs - the beta's earned-silk balance push

A MALL_BALANCE_CONTROL frame (server action/betasilk.go) updates an open
mall's balance in place, does nothing before a catalogue has arrived, and
rejects anything but exactly silk, giftSilk and points.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createMall } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/inventory/mall/mall.ts" ).href
);
const { MALL_BALANCE_CONTROL, isWorldControl } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/commerce-controls.ts" ).href
);

const json = value => new TextEncoder().encode( JSON.stringify( value ) );

test("a balance push updates an open mall and routes as a world control", () => {
	assert.equal( isWorldControl( MALL_BALANCE_CONTROL ), true );
	const mall = createMall();
	// Before any catalogue there is no mall to update.
	mall.balance( json( { silk: 350, giftSilk: 0, points: 0 } ) );
	mall.open( 0 );
	mall.projection( json( { version: 1, silk: 300, giftSilk: 0, points: 0, tabs: [], offers: [] } ) );
	const opened = mall.state();
	assert.equal( opened.silk, 300 );
	mall.balance( json( { silk: 350, giftSilk: 2, points: 1 } ) );
	const pushed = mall.state();
	assert.deepEqual( [ pushed.silk, pushed.giftSilk, pushed.points ], [ 350, 2, 1 ] );
	assert.ok( pushed.revision > opened.revision );
});

test("a malformed balance push is refused", () => {
	const mall = createMall();
	for (
		const bad of [
			{ silk: 1, giftSilk: 0 },
			{ silk: -1, giftSilk: 0, points: 0 },
			{ silk: 1.5, giftSilk: 0, points: 0 },
			{ silk: 1, giftSilk: 0, points: 0, extra: 1 }
		]
	) {
		assert.throws( () => mall.balance( json( bad ) ), /Invalid mall balance/ );
	}
});
