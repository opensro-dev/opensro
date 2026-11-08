/*
===========================================================================

mall-silk-history.test.mjs - the Buying list's inquiry lane (#2269)

The worker asks for one period's recorded silk uses on the port's control
transport (MALL_SILK_HISTORY_CONTROL), accepts only the outstanding
period's answer, and never lets a lost answer expire the session. The HUD
owns only the page, the period selection and the combo's open state.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createMall } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/inventory/mall/mall.ts" ).href
);
const { MALL_SILK_HISTORY_CONTROL, isWorldControl } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/commerce-controls.ts" ).href
);
const { createItemMall } = await import( "../../src/engine/runtime/ui/hud/item-mall.ts" );
const { mallSilkHistoryDate, mallSilkPeriodTexts } = await import(
	"../../src/engine/foundation/ui/item-mall-layout.ts"
);

/*
================
json
================
*/
function json( value ) {
	return new TextEncoder().encode( JSON.stringify( value ) );
}

test("an inquiry frames the period and pends until its answer lands", () => {
	assert.equal( isWorldControl( MALL_SILK_HISTORY_CONTROL ), true );
	const mall = createMall();
	const frame = mall.inquireSilkHistory( 1, 1000 );
	assert.equal( frame.opcode, MALL_SILK_HISTORY_CONTROL );
	assert.deepEqual( JSON.parse( new TextDecoder().decode( frame.payload ) ), { period: 1 } );
	assert.deepEqual( mall.silkHistory(), { period: 1, total: 0, rows: [], pending: true } );
	mall.silkHistoryAnswer( json( {
		version: 1,
		period: 1,
		total: 37,
		rows: [ { at: 1700000100, charged: 32 }, { at: 1699999000, charged: 5 } ]
	} ) );
	assert.deepEqual( mall.silkHistory(), {
		period: 1,
		total: 37,
		rows: [ { at: 1700000100, charged: 32 }, { at: 1699999000, charged: 5 } ],
		pending: false
	} );
});

test("a superseded period's answer is dropped, not applied", () => {
	const mall = createMall();
	mall.inquireSilkHistory( 0, 0 );
	mall.inquireSilkHistory( 2, 10 );
	mall.silkHistoryAnswer( json( { version: 1, period: 0, total: 9, rows: [ { at: 1, charged: 9 } ] } ) );
	assert.deepEqual( mall.silkHistory(), { period: 2, total: 0, rows: [], pending: true } );
	mall.silkHistoryAnswer( json( { version: 1, period: 2, total: 0, rows: [] } ) );
	assert.deepEqual( mall.silkHistory(), { period: 2, total: 0, rows: [], pending: false } );
	// With nothing outstanding, a stray answer is dropped as well.
	mall.silkHistoryAnswer( json( { version: 1, period: 2, total: 5, rows: [] } ) );
	assert.deepEqual( mall.silkHistory(), { period: 2, total: 0, rows: [], pending: false } );
});

test("re-asking the same period keeps its rows while it pends", () => {
	const mall = createMall();
	mall.inquireSilkHistory( 3, 0 );
	mall.silkHistoryAnswer( json( { version: 1, period: 3, total: 5, rows: [ { at: 7, charged: 5 } ] } ) );
	mall.inquireSilkHistory( 3, 100 );
	assert.deepEqual( mall.silkHistory(), { period: 3, total: 5, rows: [ { at: 7, charged: 5 } ], pending: true } );
});

test("a lost history answer unpends in step and never expires the session", () => {
	const mall = createMall();
	mall.inquireSilkHistory( 1, 0 );
	mall.step( 9999 );
	assert.equal( mall.silkHistory().pending, true );
	mall.step( 10000 );
	assert.deepEqual( mall.silkHistory(), { period: 1, total: 0, rows: [], pending: false } );
});

test("invalid periods and malformed answers are refused", () => {
	const mall = createMall();
	for ( const period of [ -1, 4, 1.5, Number.NaN ] ) {
		assert.throws( () => mall.inquireSilkHistory( period, 0 ), /Invalid silk history period/ );
	}
	mall.inquireSilkHistory( 1, 0 );
	for (
		const bad of [
			{ version: 2, period: 1, total: 0, rows: [] },
			{ version: 1, period: 4, total: 0, rows: [] },
			{ version: 1, period: 1, total: -1, rows: [] },
			{ version: 1, period: 1, total: 0, rows: [ { at: 1, charged: 2, extra: 3 } ] },
			{ version: 1, period: 1, total: 0, rows: [ { at: 1 } ] },
			{ version: 1, period: 1, total: 0, rows: [], account: "victim" },
			{
				version: 1,
				period: 1,
				total: 0,
				rows: Array.from( { length: 101 }, () => ({ at: 1, charged: 1 }) )
			}
		]
	) {
		assert.throws( () => mall.silkHistoryAnswer( json( bad ) ), /Invalid silk history/ );
	}
});

test("the HUD owns the page, the period and the combo, and browsing closes them", () => {
	const hud = createItemMall();
	assert.deepEqual( hud.read().silk, { open: false, period: 0, comboOpen: false } );
	hud.openSilk();
	hud.toggleSilkCombo();
	assert.deepEqual( hud.read().silk, { open: true, period: 0, comboOpen: true } );
	hud.selectSilkPeriod( 2 );
	assert.deepEqual( hud.read().silk, { open: true, period: 2, comboOpen: false } );
	hud.selectSilkPeriod( 4 );
	assert.equal( hud.read().silk.period, 2 );
	hud.browse( 0 );
	assert.deepEqual( hud.read().silk, { open: false, period: 2, comboOpen: false } );
	hud.openSilk();
	hud.close();
	assert.equal( hud.read().silk.open, false );
	hud.openSilk();
	hud.reset();
	assert.deepEqual( hud.read().silk, { open: false, period: 0, comboOpen: false } );
});

test("period texts and row dates are the native combo's", () => {
	assert.deepEqual( [ ...mallSilkPeriodTexts ], [
		"UIIT_CTL_SILK_INQUIRY_ONEDAY",
		"UIIT_CTL_SILK_INQUIRY_WEEK",
		"UIIT_CTL_SILK_INQUIRY_MONTH",
		"UIIT_CTL_SILK_INQUIRY_THREE_MONTH"
	] );
	const local = new Date( 2026, 0, 2, 3, 4 );
	assert.equal( mallSilkHistoryDate( Math.floor( local.getTime() / 1000 ) ), "2026-01-02 03:04" );
});
