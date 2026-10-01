/*
===========================================================================

item-mall.test.mjs - mall browsing, stale quotes and purchase confirmation

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createItemMall } = await import( "../../src/engine/runtime/ui/hud/item-mall.ts" );
const { mallCurrencyRows, mallPageLayout } = await import( "../../src/engine/foundation/ui/item-mall-layout.ts" );

/*
================
catalogue
================
*/
function catalogue() {
	const offers = Array.from( { length: 8 }, ( _, slot ) => ({
		group: 852,
		shop: 2,
		tab: 0,
		slot,
		packageId: 20000 + slot,
		name: "Package " + slot,
		description: "Description",
		icon: "item/example.ddj",
		silk: 20,
		giftSilk: 0,
		currencyMask: 22,
		allowsPoints: true,
		purchaseLimit: 5,
		itemIds: [ 100 + slot ]
	}) );
	return {
		tabs: [ { shop: 2, tab: 0, category: "MALL_CONSUME", label: "Consumables" } ],
		offers,
		silk: 100,
		giftSilk: 0,
		points: 15,
		pending: false,
		revision: 0
	};
}

test("mall category order preserves the independent wire shop address and six-row pages", () => {
	const mall = createItemMall(), state = catalogue();
	mall.open();
	mall.browse( 1 );
	assert.equal( mall.read( state ).offers.length, 6 );
	assert.equal( mall.read( state ).tabs[0]?.shop, 2 );
	mall.paginate( 1, state.offers.length );
	assert.deepEqual( mall.read( state ).offers.map( row => row.slot ), [ 6, 7 ] );
	mall.browse( 2 );
	assert.equal( mall.read( state ).offers.length, 0 );
	assert.equal( mall.read( state ).page, 0 );
	mall.close();
	assert.equal( mall.read().visible, false );
});

test("confirmation caps packages and points without changing authoritative balances", () => {
	const mall = createItemMall(), state = catalogue();
	mall.choose( state.offers[0] );
	mall.edit( 50, 100, state );
	assert.equal( mall.read().quantity, 5 );
	assert.equal( mall.read().points, 15 );
	const request = mall.purchase( state );
	assert.equal( request?.quantity, 5 );
	assert.equal( request?.points, 15 );
	assert.equal( state.silk, 100 );
	assert.equal( state.points, 15 );
	assert.equal( mall.purchase( { ...state, pending: true } ), null );
	assert.equal( mall.purchase( { ...state, silk: 84 } ), null );
	assert.equal( mall.purchase( { ...state, points: 14 } ), null );
	assert.equal( mall.purchase( { ...state, offers: [ { ...state.offers[0], silk: 21 } ] } ), null );
	assert.equal( mall.purchase( { ...state, offers: [ { ...state.offers[0], slot: 1 } ] } ), null );
	assert.equal( mall.purchase( { ...state, offers: [ { ...state.offers[0], allowsPoints: false } ] } ), null );
	assert.equal( mall.purchase( { ...state, offers: state.offers.map( row => ({ ...row }) ) } )?.points, 15 );
	mall.cancel();
	assert.equal( mall.purchase( state ), null );
});

test("completion and session reset clear confirmation without retrying it", () => {
	const mall = createItemMall(), state = catalogue();
	mall.open();
	mall.choose( state.offers[0] );
	mall.reserve( state.offers[0] );
	assert.equal( mall.observe( { ...state, revision: 1, pending: true } ), true );
	assert.ok( mall.read().selected );
	assert.equal( mall.observe( { ...state, revision: 2 } ), true );
	assert.equal( mall.read().selected, null );
	mall.browse( 7 );
	assert.equal( mall.read( state ).offers.length, 1 );
	mall.reset();
	mall.browse( 7 );
	assert.equal( mall.read( state ).offers.length, 0 );
	assert.equal( mall.read().visible, false );
});

test("native currency rows retain zero-priced currencies and their distinct window growth", () => {
	const offer = catalogue().offers[0];
	const layout = mallCurrencyRows( offer, 2, 7 );
	assert.equal( layout.height, 219 );
	assert.deepEqual( layout.rows.map( row => [ row.y, row.amount ] ), [ [ 102, 33 ], [ 124, 0 ], [ 146, 7 ] ] );
	assert.equal( mallCurrencyRows( { ...offer, currencyMask: 2 }, 1, 0 ).height, 177 );
});

test("point editor cancels without applying and bounds applied contribution", () => {
	const mall = createItemMall(), state = catalogue();
	mall.choose( state.offers[0] );
	mall.showPoints();
	mall.editPointDraft( 100, state );
	assert.equal( mall.read().pointDraft, 15 );
	mall.closePoints();
	assert.equal( mall.read().points, 0 );
	mall.showPoints();
	assert.equal( mall.read().pointDraft, 0 );
	mall.editPointDraft( 12, state );
	mall.edit( mall.read().quantity, mall.read().pointDraft, state );
	mall.closePoints();
	assert.equal( mall.purchase( state )?.points, 12 );
});

test("reserved list clamps its page after removing the last item", () => {
	const mall = createItemMall(), state = catalogue();
	for ( const offer of state.offers ) mall.reserve( offer );
	mall.browse( 7 );
	mall.paginate( 1, state.offers.length );
	mall.reserve( state.offers[6] );
	mall.reserve( state.offers[7] );
	assert.equal( mall.read( state ).page, 0 );
	assert.equal( mall.read( state ).offers.length, 6 );
	assert.deepEqual( mallPageLayout( [ 15, 522, 385, 26 ], 4, 30 ), {
		first: 4,
		count: 1,
		pages: 5,
		x: 197,
		y: 528,
		width: 20,
		height: 14,
		left: 180,
		right: 234,
		previous: 0,
		next: 8
	} );
});

test("reservation requires confirmation and batch purchases advance once per receipt", () => {
	const mall = createItemMall(), state = catalogue();
	mall.open();
	mall.askReserve( state.offers[0] );
	mall.cancelQuestion();
	mall.browse( 7 );
	assert.equal( mall.read( state ).count, 0 );
	for ( const offer of state.offers.slice( 0, 2 ) ) {
		mall.askReserve( offer );
		mall.confirmQuestion( state );
	}
	mall.askBatch( "basket", state );
	mall.showPoints();
	mall.editPointDraft( 15, state );
	mall.edit( 1, mall.read().pointDraft, state );
	mall.closePoints();
	mall.confirmQuestion( state );
	const first = mall.takeNextPurchase( state );
	assert.equal( first?.slot, 0 );
	assert.equal( first?.points, 15 );
	assert.equal( mall.takeNextPurchase( state ), null );
	mall.askBatch( "basket", state );
	assert.equal( mall.read().question, null );
	mall.choose( state.offers[4] );
	assert.equal( mall.purchase( state ), null );
	mall.observe( { ...state, revision: 1, pending: true } );
	assert.equal( mall.takeNextPurchase( state ), null );
	const receipt = { ...state, revision: 2, silk: 95, points: 0 };
	mall.observe( receipt );
	assert.equal( mall.read( receipt ).count, 1 );
	const second = mall.takeNextPurchase( receipt );
	assert.equal( second?.slot, 1 );
	assert.equal( second?.points, 0 );
	mall.observe( receipt );
	assert.equal( mall.takeNextPurchase( receipt ), null );
	mall.observe( { ...receipt, revision: 3, silk: 75 } );
	assert.equal( mall.read( state ).count, 0 );
	assert.equal( mall.read().batchPending, false );
	assert.equal( mall.takeNextPurchase( state ), null );
});

test("batch preflight rejects insufficient funds and stale quotes before any request", () => {
	const mall = createItemMall(), state = catalogue();
	mall.open();
	for ( const offer of state.offers ) mall.reserve( offer );
	mall.askBatch( "basket", state );
	assert.equal( mall.questionReady( state ), false );
	mall.confirmQuestion( state );
	assert.equal( mall.takeNextPurchase( state ), null );
	mall.cancelQuestion();
	for ( const offer of state.offers.slice( 2 ) ) mall.reserve( offer );
	mall.askBatch( "basket", state );
	assert.equal( mall.questionReady( state ), true );
	const changed = { ...state, offers: state.offers.map( offer => ({ ...offer, silk: 21 }) ) };
	assert.equal( mall.questionReady( changed ), false );
	mall.confirmQuestion( changed );
	assert.equal( mall.takeNextPurchase( changed ), null );
});

test("rejection and closing cancel unsent purchases without retrying the in-flight item", () => {
	for ( const close of [ false, true ] ) {
		const mall = createItemMall(), state = catalogue();
		mall.open();
		for ( const offer of state.offers.slice( 0, 2 ) ) mall.reserve( offer );
		mall.askBatch( "basket", state );
		mall.confirmQuestion( state );
		assert.ok( mall.takeNextPurchase( state ) );
		if ( close ) {
			mall.close();
			mall.open();
		}
		assert.equal( mall.takeNextPurchase( state ), null );
		mall.observe( { ...state, revision: 1, error: close ? undefined : "Rejected" } );
		assert.equal( mall.takeNextPurchase( state ), null );
		assert.equal( mall.read().batchPending, false );
	}
});
