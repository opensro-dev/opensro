/*
===========================================================================

item-mall.test.mjs - mall browsing, stale quotes and purchase confirmation

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createItemMall } = await import( "../../src/engine/runtime/ui/hud/item-mall.ts" );
const {
	mallCurrencyRows,
	mallPageLayout,
	mallPageNumberPresentation,
	compactMallBagLayout,
	mallQuestionLayout,
	mallCategories,
	mallCategoriesLater,
	mallTabCaptionLater,
	mallBadgeMark,
	mallDescription
} = await import(
	"../../src/engine/foundation/ui/item-mall-layout.ts"
);
const { mallProjection } = await import( "../../src/engine/foundation/gameplay/item-mall-catalog.ts" );

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

test("isOpen answers what read().visible does through open, close and reset", () => {
	const mall = createItemMall();
	const agree = expected => {
		assert.equal( mall.isOpen(), expected );
		assert.equal( mall.read().visible, expected );
	};
	agree( false );
	mall.open();
	agree( true );
	mall.close();
	agree( false );
	mall.open();
	mall.reset();
	agree( false );
});

test("each rail is one shared table, so reading it allocates nothing", () => {
	// The frame reads a rail every step (the mall model's categories).
	assert.equal( mallCategories(), mallCategories() );
	assert.equal( mallCategoriesLater(), mallCategoriesLater() );
	const mall = createItemMall();
	assert.equal( mall.read().categories, mallCategories() );
	mall.setCategoriesLater( true );
	assert.equal( mall.read().categories, mallCategoriesLater() );
});

test("compact pages keep action indices and purchase identity aligned through rotation", () => {
	const mall = createItemMall(), state = catalogue();
	mall.open();
	mall.browse( 1 );
	mall.setPageSize( 2 );
	mall.paginate( 2, state.offers.length );
	assert.deepEqual( mall.read( state ).offers.map( row => row.slot ), [ 4, 5 ] );
	mall.choose( mall.read( state ).offers[1] );
	const purchase = mall.purchase( state );
	mall.setPageSize( 3 );
	assert.deepEqual( mall.read( state ).offers.map( row => row.slot ), [ 3, 4, 5 ] );
	assert.deepEqual( mall.purchase( state ), purchase );
	mall.setPageSize( 6 );
	assert.deepEqual( mall.read( state ).offers.map( row => row.slot ), [ 0, 1, 2, 3, 4, 5 ] );
	mall.paginate( 999, state.offers.length );
	assert.deepEqual( mall.read( state ).offers.map( row => row.slot ), [ 6, 7 ] );
	mall.setPageSize( 0 );
	assert.equal( mall.read( state ).pageSize, 6 );
	mall.setPageSize( 2 );
	mall.reset();
	assert.equal( mall.read( state ).pageSize, 6 );
});

test("compact populated pages select Buy, Wear and Basket by displayed index without early dispatch", () => {
	for ( const pageSize of [ 1, 2, 3, 4, 6 ] ) {
		const mall = createItemMall(), initial = catalogue();
		const state = {
			...initial,
			tabs: [ { shop: 2, tab: 1, category: "MALL_AVATAR", label: "Dress" } ],
			offers: initial.offers.map( offer => ({ ...offer, tab: 1 }) )
		};
		mall.open();
		mall.browse( 2 );
		mall.setPageSize( pageSize );
		mall.previewState( { gid: 99, wearable: state.offers.flatMap( offer => offer.itemIds ) } );
		const seen = [];
		for ( let page = 0; page < Math.ceil( state.offers.length / pageSize ); page++ ) {
			mall.paginate( page, state.offers.length );
			const displayed = mall.read( state ).offers;
			for ( let index = 0; index < displayed.length; index++ ) {
				const offer = mall.read( state ).offers[index];
				seen.push( offer.packageId );
				mall.choose( offer );
				assert.equal( mall.read( state ).selected?.packageId, displayed[index].packageId );
				assert.equal( mall.purchase( state )?.slot, displayed[index].slot );
				assert.equal( mall.takeNextPurchase( state ), null, "opening a quote must not queue a purchase" );
				mall.cancel();
				assert.equal( mall.purchase( state ), null );
				assert.equal( mall.takeNextPurchase( state ), null, "cancel must not dispatch" );
				mall.wear( offer, state );
				assert.deepEqual( mall.previewRequest(), displayed[index].itemIds );
				assert.equal( mall.read( state ).worn[0].packageId, displayed[index].packageId );
				mall.askReserve( offer );
				assert.equal( mall.read( state ).question?.offers[0].packageId, displayed[index].packageId );
				assert.equal( mall.takeNextPurchase( state ), null );
				mall.confirmQuestion( state );
				assert.equal( mall.takeNextPurchase( state ), null, "basket confirmation is not a purchase" );
			}
		}
		assert.deepEqual( seen, state.offers.map( offer => offer.packageId ) );
		mall.browse( 7 );
		assert.equal( mall.read( state ).count, state.offers.length );
		mall.askBatch( "worn", state );
		assert.equal( mall.takeNextPurchase( state ), null );
		mall.cancelQuestion();
		assert.equal( mall.takeNextPurchase( state ), null );
		mall.askBatch( "worn", state );
		mall.confirmQuestion( state );
		assert.equal(
			mall.takeNextPurchase( state )?.slot,
			7,
			"only explicit confirmation admits the selected worn offer"
		);
		assert.equal( mall.takeNextPurchase( state ), null, "confirmation dispatches once" );
	}
});

test("compact bag pages keep native cells inside small windows and expose every inventory slot", () => {
	for ( const [width, height] of [ [ 375, 375 ], [ 320, 568 ], [ 667, 375 ], [ 280, 375 ] ] ) {
		const seen = [];
		const options = { width, height, total: 109, equipment: 13, requestedPage: 0 };
		const first = compactMallBagLayout( options );
		for ( let page = 0; page < first.pages; page++ ) {
			const bag = compactMallBagLayout( { ...options, requestedPage: page } );
			for ( const cell of bag.slots ) {
				const [x, y, w, h] = cell.rect;
				assert.deepEqual( [ w, h ], [ 32, 32 ] );
				assert.ok( x >= 0 && x + w <= width );
				assert.ok( y >= 124 && y + h <= height - 56, `${width}x${height} ${cell.rect}` );
				if ( cell.enabled ) seen.push( cell.slot );
			}
		}
		assert.deepEqual( seen, Array.from( { length: 96 }, ( _, i ) => 13 + i ) );
		assert.equal( compactMallBagLayout( { ...options, requestedPage: 999 } ).page, first.pages - 1 );
	}
});

test("secondary compact views preserve catalogue and purchase state and reset on close", () => {
	const mall = createItemMall(), state = catalogue();
	mall.open();
	mall.browse( 1 );
	mall.setPageSize( 2 );
	mall.paginate( 2, state.offers.length );
	const offers = mall.read( state ).offers;
	assert.equal( mall.compactAction( "item-mall-view:bag" ), true );
	assert.equal( mall.read( state ).compactView, "bag" );
	mall.compactAction( "item-mall-bag-slice:2" );
	assert.equal( mall.read( state ).compactBagPage, 2 );
	mall.compactAction( "item-mall-view:preview" );
	assert.equal( mall.read( state ).compactView, "preview" );
	assert.deepEqual( mall.read( state ).offers, offers );
	assert.equal( mall.takeNextPurchase( state ), null );
	assert.equal( mall.compactAction( "item-mall-purchase" ), false );
	mall.close();
	assert.equal( mall.read( state ).compactView, "shop" );
	mall.reset();
	assert.equal( mall.read( state ).compactBagPage, 0 );
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

test("the current page number takes the highlight colour and styled face", () => {
	assert.deepEqual( mallPageNumberPresentation( 2, 2 ), {
		color: [ 1, 0x99 / 255, 0x51 / 255, 1 ],
		fontStyle: 2
	} );
	assert.deepEqual( mallPageNumberPresentation( 1, 2 ), { color: undefined, fontStyle: 0 } );
	assert.deepEqual( mallPageNumberPresentation( 3, 2 ), { color: undefined, fontStyle: 0 } );
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

/*
================
laterCatalogue

The SRO_MALL_CATEGORIES catalogue's shape (issue #3257): Alchemy with
its Others tab, the premium ticket badged recommended, and the HOT
shop's two tabs with a Halloween outfit badged hot.
================
*/
function laterCatalogue() {
	const offer = ( shop, tab, slot, packageId, name, badge ) => ({
		group: 852,
		shop,
		tab,
		slot,
		packageId,
		name,
		description: "Description",
		icon: "item/example.ddj",
		silk: 5,
		giftSilk: 0,
		currencyMask: 2,
		allowsPoints: false,
		purchaseLimit: 5,
		itemIds: [ 100 + packageId % 10 ],
		...(badge ? { badge } : {})
	});
	return {
		tabs: [
			{ shop: 0, tab: 0, category: "MALL_ARCHEMY", label: "UIIT_STT_SILKMALL_ARCHEMY_ASTRAL" },
			{ shop: 0, tab: 2, category: "MALL_ARCHEMY", label: "UIIT_STT_SILKMALL_ETC" },
			{ shop: 4, tab: 0, category: "MALL_PREMIUM", label: "UIIT_STT_SILKMALL_PREMIUM_PREMIUM" },
			{ shop: 5, tab: 0, category: "MALL_HOT", label: "UIIT_STT_SILKMALL_EVENT" },
			{ shop: 5, tab: 1, category: "MALL_HOT", label: "UIIT_STT_SILKMALL_AVATA" }
		],
		offers: [
			offer( 0, 0, 0, 90002, "Astral Stone" ),
			offer( 0, 2, 0, 90001, "Destruction Rondo", "new" ),
			offer( 4, 0, 0, 90003, "Premium Goldtime", "recommended" ),
			offer( 5, 0, 0, 90005, "Halloween Attach", "hot" ),
			offer( 5, 1, 0, 90004, "Halloween Dress", "hot" )
		],
		silk: 100,
		giftSilk: 0,
		points: 0,
		pending: false,
		revision: 0
	};
}

test("the later rail orders Expendables first and the HOT shop before the basket; the native rail stands", () => {
	assert.deepEqual(
		mallCategoriesLater().map( category => category.key ),
		[ "MALL_CONSUME", "MALL_AVATAR", "MALL_PET", "MALL_PREMIUM", "MALL_ARCHEMY", "MALL_HOT", "basket" ]
	);
	assert.deepEqual(
		mallCategories().map( category => category.key ),
		[ "hot", "MALL_CONSUME", "MALL_AVATAR", "MALL_PET", "package", "MALL_PREMIUM", "MALL_ARCHEMY", "basket" ]
	);
	// The later captions and the N/H/R marks the offer rows draw.
	assert.equal( mallTabCaptionLater( "MALL_HOT", 0 ), "Halloween Exclusive" );
	assert.equal( mallTabCaptionLater( "MALL_HOT", 1 ), "Halloween Models" );
	assert.equal( mallTabCaptionLater( "MALL_HOT", 2 ), "Recommended" );
	assert.equal( mallTabCaptionLater( "MALL_PREMIUM", 1 ), "Gears" );
	assert.equal( mallTabCaptionLater( "MALL_PREMIUM", 0 ), undefined );
	assert.equal( mallTabCaptionLater( "MALL_ARCHEMY", 2 ), "Others" );
	assert.equal( mallBadgeMark( "new" )?.letter, "N" );
	assert.equal( mallBadgeMark( "hot" )?.letter, "H" );
	assert.equal( mallBadgeMark( "recommended" )?.letter, "R" );
	assert.equal( mallBadgeMark( undefined ), undefined );
	// Alchemy's subtitle names tabs 0 and 1 as before; tab 2 has none.
	assert.equal( mallDescription( "MALL_ARCHEMY", 0 ), "UIIT_STT_SILKMALL_MAIN_SUB_ARCHEMY_ASTRAL" );
	assert.equal( mallDescription( "MALL_ARCHEMY", 1 ), "UIIT_STT_SILKMALL_MAIN_SUB_ARCHEMY_ATHANASIA" );
	assert.equal( mallDescription( "MALL_ARCHEMY", 2 ), "" );
});

test("the later mode browses the HOT shop, gathers the Recommended tab and searches across tabs", () => {
	const mall = createItemMall(), state = laterCatalogue();
	mall.open();
	mall.setCategoriesLater( true );
	assert.deepEqual(
		mall.read( state ).categories.map( category => category.key ),
		mallCategoriesLater().map( category => category.key )
	);
	// The HOT shop is rail index 5: two server tabs plus the virtual one.
	mall.browse( 5 );
	const hot = mall.read( state );
	assert.equal( hot.tabs.length, 3 );
	assert.deepEqual( hot.offers.map( offer => offer.name ), [ "Halloween Attach" ] );
	mall.browse( 5, 1 );
	assert.deepEqual( mall.read( state ).offers.map( offer => offer.name ), [ "Halloween Dress" ] );
	// The virtual Recommended tab gathers badged offers from every tab.
	mall.browse( 5, 2 );
	assert.deepEqual( mall.read( state ).offers.map( offer => offer.name ), [ "Premium Goldtime" ] );
	// Alchemy is rail index 4; the Rondo stands badged in its Others tab.
	mall.browse( 4 );
	assert.deepEqual( mall.read( state ).offers.map( offer => offer.name ), [ "Astral Stone" ] );
	mall.browse( 4, 1 );
	const rondo = mall.read( state ).offers;
	assert.deepEqual( rondo.map( offer => offer.name ), [ "Destruction Rondo" ] );
	assert.equal( rondo[0]?.badge, "new" );
	// The search looks across the whole catalogue, case folded.
	mall.setSearch( "halloween" );
	assert.deepEqual(
		mall.read( state ).offers.map( offer => offer.name ),
		[ "Halloween Attach", "Halloween Dress" ]
	);
	mall.setSearch( "" );
	assert.deepEqual( mall.read( state ).offers.map( offer => offer.name ), [ "Destruction Rondo" ] );
	// Switching the row off restarts browsing on the native rail.
	mall.setCategoriesLater( false );
	const native = mall.read( state );
	assert.equal( native.category, -1 );
	assert.equal( native.search, "" );
	assert.deepEqual(
		native.categories.map( category => category.key ),
		mallCategories().map( category => category.key )
	);
	// The basket is the native rail's eighth entry and still reads.
	mall.browse( 7 );
	mall.reserve( state.offers[0] );
	assert.deepEqual( mall.read( state ).offers.map( offer => offer.name ), [ "Astral Stone" ] );
});

test("the catalogue decoder keeps a known badge and drops an unknown one", () => {
	const offer = ( slot, badge ) => ({
		group: 852,
		shop: 0,
		tab: 0,
		slot,
		currencyMask: 2,
		packageId: 90000 + slot,
		name: "Package " + slot,
		description: "Description",
		icon: "item/example.ddj",
		silk: 5,
		giftSilk: 0,
		allowsPoints: false,
		purchaseLimit: 5,
		itemIds: [ 42 ],
		...(badge === null ? {} : { badge })
	});
	const payload = new TextEncoder().encode( JSON.stringify( {
		version: 1,
		tabs: [ { shop: 0, tab: 0, category: "MALL_ARCHEMY", label: "Astral" } ],
		offers: [ offer( 0, "new" ), offer( 1, "bogus" ), offer( 2, null ) ],
		silk: 0,
		giftSilk: 0,
		points: 0
	} ) );
	const { state } = mallProjection( payload );
	assert.equal( state.offers[0]?.badge, "new" );
	assert.equal( state.offers[1]?.badge, undefined );
	assert.equal( state.offers[2]?.badge, undefined );
});

test("the lump-buy boxes caption their total row with the total-price line", () => {
	// 528920 sets the total control's text on both boxes that draw a
	// total row: UIIT_MSG_QUESTION_SILKMALL_ZZIM_LUMP_BUY_TOTAL
	// ("Total price"), at 529846 for the worn box and 529BE7 for the
	// basket, on the rect the layouts already draw.
	const basket = mallQuestionLayout( "basket", 3 );
	assert.equal( basket.totalCaption, "UIIT_MSG_QUESTION_SILKMALL_ZZIM_LUMP_BUY_TOTAL" );
	assert.equal( basket.totalY, 74 );
	const worn = mallQuestionLayout( "worn", 3 );
	assert.equal( worn.totalCaption, "UIIT_MSG_QUESTION_SILKMALL_ZZIM_LUMP_BUY_TOTAL" );
	assert.equal( worn.totalY, 152 );
	// The reservation and removal boxes draw no total row.
	assert.equal( mallQuestionLayout( "reserve", 1 ).totalCaption, undefined );
	assert.equal( mallQuestionLayout( "remove", 1 ).totalCaption, undefined );
});
