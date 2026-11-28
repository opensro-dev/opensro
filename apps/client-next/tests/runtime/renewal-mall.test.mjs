/*
===========================================================================

renewal-mall.test.mjs - the later client's renewal Item Mall window

The renewal window (port-only, Experimental "Renewal Item Mall") is the
later client's CIFNewItemMall built from its published layouts. These
tests pin the catalogue decoder, the browsing state it adds to the mall
owner (the later rail, the badged main page, the card popup, the ten-card
page) and the view's draw operations: which action each control sends,
when a control is disabled, and that every repeated child stays inside
the authored parent it repeats in.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { defined } from "../helpers/defined.mjs";
import { LATER, control, catalogueFile, offers, state, view, renewalMall } from "../helpers/renewal-mall-fixture.mjs";
const { createItemMall } = await import( "../../src/engine/runtime/ui/hud/item-mall.ts" );
const { mallCategoriesLater } = await import( "../../src/engine/foundation/ui/item-mall-layout.ts" );
const { silkWalletHelp } = await import( "../../src/engine/foundation/ui/mall-wallet.ts" );
const {
	decodeRenewalMall,
	optionalRenewalMall,
	renewalGoodsCells,
	renewalGoodsRect,
	renewalMallOrigin,
	renewalNameLayout,
	renewalPopupRect,
	renewalRailRect,
	renewalSubTabRect,
	RENEWAL_GOODS_AREA,
	RENEWAL_GOODS_PER_PAGE,
	renewalPagerRects,
	RENEWAL_MALL_HEIGHT,
	RENEWAL_MALL_WIDTH
} = await import( "../../src/engine/foundation/ui/renewal-mall.ts" );
const { renewalMallPictures, renewalMallView } = await import( "../../src/engine/runtime/ui/hud/renewal-mall-view.ts" );

const buttons = ops => ops.filter( op => op.kind === "button" );
const button = ( ops, id ) => buttons( ops ).find( op => op.id === id );
const inside = ( outer, inner ) =>
	inner[0] >= outer[0] && inner[1] >= outer[1] &&
	inner[0] + inner[2] <= outer[0] + outer[2] && inner[1] + inner[3] <= outer[1] + outer[3];

test("a catalogue missing a control the view draws is refused by name, and the HUD keeps the classic mall", () => {
	// The import that shipped without the card's icon slot (its own
	// NewShopSlotIconForPackage section) failed the whole game load.
	const file = catalogueFile();
	file.sections.itemSlot = file.sections.itemSlot.filter( row => row.name !== "GDR_RENEWAL_SHOP_SLOT_ICON" );
	assert.throws(
		() => decodeRenewalMall( file ),
		/Incomplete renewal mall catalogue: no GDR_RENEWAL_SHOP_SLOT_ICON/
	);
	const warn = console.warn;
	const warned = [];
	console.warn = ( ...args ) => warned.push( args );
	try {
		assert.equal( optionalRenewalMall( file ), null );
		assert.equal( optionalRenewalMall( { format: "something-else" } ), null );
		assert.equal( optionalRenewalMall( null ), null );
	} finally {
		console.warn = warn;
	}
	assert.equal( warned.length, 2 );
	assert.notEqual( optionalRenewalMall( catalogueFile() ), null );
});

test("a card name keeps two lines in its 26-pixel box, the rest joined onto the second", () => {
	// The live build allowed one line: the font's full line (over 13 px)
	// fitted the 26 px box once, so "Magic stone of Astral(Lvl.1)" lost the
	// part that tells the stones apart.
	const two = renewalNameLayout( [ "Magic stone of", "Astral(Lvl.1)" ], 26 );
	assert.deepEqual( two.lines, [ "Magic stone of", "Astral(Lvl.1)" ] );
	assert.equal( two.pitch, 13 );
	assert.deepEqual( renewalNameLayout( [ "Cat Summon Scroll" ], 26 ).lines, [ "Cat Summon Scroll" ] );
	assert.deepEqual(
		renewalNameLayout( [ "A very long", "premium name", "that goes on" ], 26 ).lines,
		[ "A very long", "premium name that goes on" ]
	);
	assert.deepEqual( renewalNameLayout( [], 26 ).lines, [] );
});

test("an absent catalogue decodes to null, a foreign one throws", () => {
	assert.equal( decodeRenewalMall( null ), null );
	assert.equal( decodeRenewalMall( undefined ), null );
	assert.throws( () => decodeRenewalMall( { format: "something-else", sections: {} } ), /Invalid renewal mall/ );
	const file = catalogueFile();
	file.sections.window[1].texture = "https://example.invalid/x.png";
	assert.throws( () => decodeRenewalMall( file ), /Foreign renewal mall texture/ );
	const missing = catalogueFile();
	delete /** @type {Record<string, unknown>} */ (missing.sections).myInfo;
	assert.throws( () => decodeRenewalMall( missing ), /Missing renewal mall section myInfo/ );
});

test("the decoder keys controls by name and keeps the later strings", () => {
	const data = defined( decodeRenewalMall( catalogueFile() ) );
	assert.deepEqual( data.window.GDR_RENEWAL_ITEM_MALL_MID_BG.rect, [ 137, 72, 424, 519 ] );
	assert.equal( data.window.GDR_RENEWAL_ITEM_MALL_LEFT_FRAME.frame, true );
	assert.equal( data.strings.UIIT_CTL_SILKMALL_INIT_ITEM, "Original" );
	assert.deepEqual( data.images, [ LATER + "mall/mall_item_wnd.png" ] );
});

test("the window centres on the screen and never leaves its top-left", () => {
	assert.deepEqual( renewalMallOrigin( 1024, 768 ), [ 112, 84 ] );
	assert.deepEqual( renewalMallOrigin( 640, 480 ), [ 0, 0 ] );
});

/*
================
named

Offers with the given display names, in catalogue order.
================
*/
function named( names ) {
	return names.map( ( name, slot ) => ({
		...offers( 1 )[0],
		slot,
		packageId: 40000 + slot,
		name,
		badge: undefined
	}) );
}

test("a gendered pair shares one row, male left and female right, whatever the catalogue order", () => {
	assert.deepEqual(
		renewalGoodsCells( named( [ "Devil (M)", "Devil (F)", "Saint (F)", "Saint (M)" ] ) ),
		[ 0, 1, 3, 2 ]
	);
});

test("a missing partner leaves its cell empty, so no later row shifts", () => {
	// The drift on the later avatar pages: one outfit sold for one sex
	// moved every pair after it across the row boundary.
	const cells = renewalGoodsCells( named( [ "Knight (M)", "Fairy (M)", "Fairy (F)", "Pirate (M)", "Pirate (F)" ] ) );
	assert.deepEqual( cells, [ 0, -1, 1, 2, 3, 4 ] );
	for ( let row = 0; row < cells.length; row += 2 ) {
		const [left, right] = [ cells[row], cells[row + 1] ];
		if ( left >= 0 ) {
			assert.match(
				named( [ "Knight (M)", "Fairy (M)", "Fairy (F)", "Pirate (M)", "Pirate (F)" ] )[left].name,
				/\(M\)$/
			);
		}
		if ( right >= 0 ) {
			assert.match(
				named( [ "Knight (M)", "Fairy (M)", "Fairy (F)", "Pirate (M)", "Pirate (F)" ] )[right].name,
				/\(F\)$/
			);
		}
	}
});

test("ungendered goods flow two to a row, and a repeated sex starts a new pair row", () => {
	assert.deepEqual( renewalGoodsCells( named( [ "Potion", "Scroll", "Stone" ] ) ), [ 0, 1, 2, -1 ] );
	assert.deepEqual( renewalGoodsCells( named( [ "Hat (M)", "Hat (M)", "Hat (F)" ] ) ), [ 0, 2, 1, -1 ] );
});

test("the renewal owner pages by grid cells, keeping every row a pair across pages", () => {
	const mall = renewalMall();
	mall.browse( 1, 1 );
	// Six pairs with one lone male outfit first: thirteen offers, fourteen
	// cells, two pages of five rows.
	const list = named( [
		"Lone (M)",
		...[ "A", "B", "C", "D", "E", "F" ].flatMap( base => [ base + " (M)", base + " (F)" ] )
	] );
	const catalogue = { ...state(), offers: list };
	const first = mall.read( catalogue );
	assert.equal( first.count, 14 );
	assert.deepEqual( first.grid, [ 0, -1, 1, 2, 3, 4, 5, 6, 7, 8 ] );
	assert.deepEqual( first.offers.map( offer => offer.name ).slice( 0, 3 ), [ "Lone (M)", "A (M)", "A (F)" ] );
	mall.paginate( 1, first.count );
	const second = mall.read( catalogue );
	assert.deepEqual( second.offers.map( offer => offer.name ), [ "E (M)", "E (F)", "F (M)", "F (F)" ] );
	assert.deepEqual( second.grid, [ 0, 1, 2, 3 ] );
	// The classic window keeps its plain list and draws no grid.
	const classic = createItemMall();
	classic.open();
	classic.browse( 0 );
	assert.equal( classic.read( catalogue ).grid, null );
});

test("the catalogue's own name keys pair by their _M_ / _W_ segment", () => {
	// Offer names are text keys; the live build's grid never paired on the
	// English "(M)" ending because it never saw the English.
	const cells = renewalGoodsCells( named( [
		"SN_ITEM_MALL_AVATAR_M_HALLOWEEN",
		"SN_ITEM_MALL_AVATAR_M_HALLOWEEN_HAT",
		"SN_ITEM_MALL_AVATAR_W_HALLOWEEN",
		"SN_ITEM_MALL_AVATAR_W_HALLOWEEN_HAT",
		"SN_ITEM_MALL_AVATAR_M_PIRATE"
	] ) );
	assert.deepEqual( cells, [ 0, 2, 1, 3, 4, -1 ] );
	// A key without a sex segment falls back to its English name.
	assert.deepEqual(
		renewalGoodsCells( named( [ "SN_BROOM_A", "SN_BROOM_B" ] ), {
			SN_BROOM_A: "Witch's Broomstick (F)",
			SN_BROOM_B: "Witch's Broomstick (M)"
		} ),
		[ 1, 0 ]
	);
});

/*
================
sexed

The Knight and Saint pairs with an ungendered scroll between them,
each offer carrying the gender the server read from its item.
================
*/
function sexed() {
	const sexes = [ "male", "female", undefined, "male", "female" ];
	return named( [ "Knight (M)", "Knight (F)", "Scroll", "Saint (M)", "Saint (F)" ] )
		.map( ( offer, index ) => ({ ...offer, gender: sexes[index] }) );
}

test("the gender filter keeps the chosen sex and every ungendered good (#4480)", () => {
	const mall = renewalMall();
	mall.browse( 1, 1 );
	const catalogue = { ...state(), offers: sexed() };
	let read = mall.read( catalogue );
	assert.equal( read.gender, "all" );
	assert.equal( read.gendered, true );
	assert.deepEqual(
		read.offers.map( offer => offer.name ),
		[ "Knight (M)", "Knight (F)", "Scroll", "Saint (M)", "Saint (F)" ]
	);
	mall.setGender( "male" );
	read = mall.read( catalogue );
	assert.deepEqual( read.offers.map( offer => offer.name ), [ "Knight (M)", "Scroll", "Saint (M)" ] );
	mall.setGender( "female" );
	read = mall.read( catalogue );
	assert.deepEqual( read.offers.map( offer => offer.name ), [ "Knight (F)", "Scroll", "Saint (F)" ] );
	// A word that is not a sex changes nothing.
	mall.setGender( "bogus" );
	assert.equal( mall.read( catalogue ).gender, "female" );
	// The classic window's list is the filtered one too, but only the
	// renewal view draws the control; an ungendered list reports none.
	mall.setGender( "all" );
	assert.equal( mall.read( { ...state(), offers: named( [ "Potion", "Scroll" ] ) } ).gendered, false );
});

test("the view draws the gender segments only over gendered goods, the chosen one selected (#4480)", () => {
	const mall = renewalMall();
	mall.browse( 1, 1 );
	const catalogue = { ...state(), offers: sexed() };
	let ops = view( mall, { state: catalogue } );
	for ( const sex of [ "all", "male", "female" ] ) {
		assert.ok( button( ops, "item-mall-gender:" + sex ), sex );
	}
	assert.equal( button( ops, "item-mall-gender:all" )?.selected, true );
	// On the pager's row, left of its previous arrow, inside the goods area.
	const pager = renewalPagerRects();
	for ( const sex of [ "all", "male", "female" ] ) {
		const rect = defined( button( ops, "item-mall-gender:" + sex ) ).rect;
		assert.ok( rect[0] >= RENEWAL_GOODS_AREA[0] && rect[0] + rect[2] <= pager.prev[0], sex + " clears the pager" );
		assert.ok(
			rect[1] + rect[3] <= RENEWAL_GOODS_AREA[1] + RENEWAL_GOODS_AREA[3],
			sex + " stays in the goods area"
		);
	}
	mall.setGender( "female" );
	ops = view( mall, { state: catalogue } );
	assert.equal( button( ops, "item-mall-gender:female" )?.selected, true );
	assert.equal( button( ops, "item-mall-gender:all" )?.selected, false );
	// No gendered goods in the list: no segments.
	const plain = view( mall, { state: { ...state(), offers: named( [ "Potion", "Scroll" ] ) } } );
	assert.equal( button( plain, "item-mall-gender:all" ), undefined );
});

test("the search draft filters nothing until Search applies it, and matches the English name", () => {
	const mall = renewalMall();
	mall.setNames( { SN_DEVIL: "Devil Wing Armor (M)", SN_SAINT: "Saint Knight Armor (M)" } );
	const catalogue = {
		...state(),
		offers: named( [ "SN_DEVIL", "SN_SAINT" ] ).map( offer => ({ ...offer, badge: "new" }) )
	};
	mall.setSearchDraft( "saint" );
	let read = mall.read( catalogue );
	assert.equal( read.searchDraft, "saint" );
	assert.equal( read.search, "" );
	assert.equal( read.offers.length, 2 );
	mall.applySearch();
	read = mall.read( catalogue );
	assert.deepEqual( read.offers.map( offer => offer.name ), [ "SN_SAINT" ] );
	// The key itself is not what a player searches for.
	mall.setSearchDraft( "sn_" );
	mall.applySearch();
	assert.equal( mall.read( catalogue ).offers.length, 0 );
	// An empty draft clears the search; browsing clears both.
	mall.setSearchDraft( "  " );
	mall.applySearch();
	assert.equal( mall.read( catalogue ).search, "" );
	mall.setSearchDraft( "devil" );
	mall.browse( 1 );
	assert.equal( mall.read( catalogue ).searchDraft, "" );
});

test("Search is a live button, the field shows the draft, and only card names wrap", () => {
	const mall = renewalMall();
	mall.browse( 1, 1 );
	mall.setSearchDraft( "Dress" );
	// The offers' names are their own text keys here; translate them.
	const strings = Object.fromEntries( offers( 14 ).map( offer => [ offer.name, offer.name ] ) );
	const ops = view( mall, { strings } );
	assert.equal( button( ops, "item-mall-search-find" ).disabled, false );
	assert.equal( defined( ops.find( op => op.kind === "edit" ) ).value, "Dress" );
	const texts = ops.filter( op => op.kind === "text" );
	assert.ok( texts.some( op => op.value === "Dress" && !op.wrap ) );
	const wrapped = texts.filter( op => op.wrap ).map( op => op.value );
	assert.ok( wrapped.length > 0 );
	assert.ok( wrapped.every( value => /^Dress \d+$/.test( value ) ), "only offer names wrap: " + wrapped );
	// Single-line captions are centred vertically in their rect.
	assert.ok( texts.filter( op => !op.wrap ).every( op => op.vAlign === 1 ) );
});

test("the focused empty search field drops its prompt for the caret, and says where its text sits", () => {
	const mall = renewalMall();
	const prompt = "Write Item name";
	assert.ok( view( mall ).some( op => op.kind === "text" && op.value === prompt ) );
	const focused = view( mall, { searchFocused: true } );
	assert.ok( !focused.some( op => op.kind === "text" && op.value === prompt ) );
	const edit = defined( focused.find( op => op.kind === "edit" ) );
	assert.ok(
		inside( edit.rect, edit.textRect ) || edit.textRect[0] >= edit.rect[0],
		"the caret's line sits in the field"
	);
	// Typed text stays drawn whether or not the field has focus.
	mall.setSearchDraft( "devil" );
	assert.ok( view( mall, { searchFocused: true } ).some( op => op.kind === "text" && op.value === "devil" ) );
});

test("the view draws each card in its grid cell and leaves empty cells bare", () => {
	const mall = renewalMall();
	mall.browse( 1, 1 );
	const list = named( [ "Lone (M)", "A (M)", "A (F)" ] );
	const ops = view( mall, { state: { ...state(), offers: list } } );
	const cards = buttons( ops ).filter( op => op.id.startsWith( "item-mall-popup:" ) );
	assert.deepEqual( cards.map( op => op.id ), [ "item-mall-popup:0", "item-mall-popup:1", "item-mall-popup:2" ] );
	assert.deepEqual( cards.map( op => op.rect ), [
		renewalGoodsRect( 0 ),
		renewalGoodsRect( 2 ),
		renewalGoodsRect( 3 )
	] );
	// The popup of an offer opens beside its cell, not its list position.
	mall.openPopup( 1 );
	const popupOps = view( mall, { state: { ...state(), offers: list } } );
	const buy = button( popupOps, "item-mall-buy:1" );
	assert.ok( buy );
	assert.ok( inside( renewalPopupRect( 2 ), buy.rect ) );
});

test("rail, sub tabs and goods cards repeat inside the parents the layouts author", () => {
	const data = defined( decodeRenewalMall( catalogueFile() ) );
	const left = data.window.GDR_RENEWAL_ITEM_MALL_LEFT_FRAME.rect;
	const mid = data.window.GDR_RENEWAL_ITEM_MALL_MID_BG.rect;
	for ( let index = 0; index < mallCategoriesLater().length; index++ ) {
		assert.ok( inside( left, renewalRailRect( index ) ), "rail entry " + index );
	}
	// The cards, tabs and pager stay on the textured part of the middle
	// art, never on its scroll-bar track (the live build's right column
	// ran 10 pixels onto it, a strip with no background).
	const area = RENEWAL_GOODS_AREA;
	assert.ok( inside( mid, area ), "the textured area lies in the middle background" );
	assert.equal( area[0] + area[2], mid[0] + 407, "the track starts at the art's column 407" );
	for ( let index = 0; index < 4; index++ ) {
		assert.ok( inside( area, renewalSubTabRect( index ) ), "sub tab " + index );
	}
	const pager = renewalPagerRects();
	for ( const rect of [ pager.prev, pager.next, pager.label ] ) assert.ok( inside( area, rect ), "pager" );
	for ( let index = 0; index < RENEWAL_GOODS_PER_PAGE; index++ ) {
		assert.ok( inside( area, renewalGoodsRect( index ) ), "card " + index );
		assert.ok(
			inside( [ 0, 0, RENEWAL_MALL_WIDTH, RENEWAL_MALL_HEIGHT ], renewalPopupRect( index ) ),
			"popup " + index
		);
	}
	// Cards never overlap.
	const [a, b] = [ renewalGoodsRect( 0 ), renewalGoodsRect( 1 ) ];
	assert.ok( a[0] + a[2] <= b[0] );
	assert.ok( a[1] + a[3] <= renewalGoodsRect( 2 )[1] );
});

test("the renewal window browses the later rail and its switch restarts at the main page", () => {
	const mall = createItemMall();
	mall.open();
	mall.browse( 2 );
	mall.setRenewal( true );
	const read = mall.read( state() );
	assert.equal( read.renewal, true );
	assert.equal( read.categories, mallCategoriesLater() );
	assert.equal( read.category, -1 );
	// The classic categories row is redundant under the renewal window:
	// turning it on or off leaves browsing where it is.
	mall.browse( 1 );
	mall.setCategoriesLater( true );
	mall.setCategoriesLater( false );
	assert.equal( mall.read( state() ).category, 1 );
	mall.setRenewal( false );
	assert.equal( mall.read( state() ).renewal, false );
	assert.equal( mall.read( state() ).category, -1 );
});

test("the renewal main page lists the badged goods only, and a search lists matches from every tab", () => {
	const mall = renewalMall();
	assert.deepEqual( mall.read( state() ).offers.map( offer => offer.packageId ), [ 30000, 30001 ] );
	mall.setSearch( "dress 1" );
	assert.deepEqual(
		mall.read( state() ).offers.map( offer => offer.packageId ),
		[ 30001, 30010, 30011, 30012, 30013 ]
	);
	// The classic window keeps its empty front page.
	const classic = createItemMall();
	classic.open();
	assert.equal( classic.read( state() ).offers.length, 0 );
});

test("ten-card pages are accepted, more are not", () => {
	const mall = renewalMall();
	mall.browse( 1, 1 );
	assert.equal( mall.read( state() ).pageSize, 10 );
	assert.equal( mall.read( state() ).offers.length, 10 );
	mall.setPageSize( 11 );
	assert.equal( mall.read( state() ).pageSize, 10 );
});

test("a card popup toggles on its card and closes on any browse, page, search or choice", () => {
	const mall = renewalMall();
	mall.browse( 1, 1 );
	mall.openPopup( 3 );
	assert.equal( mall.read( state() ).popup, 3 );
	mall.openPopup( 3 );
	assert.equal( mall.read( state() ).popup, -1 );
	mall.openPopup( 10 );
	assert.equal( mall.read( state() ).popup, -1 );
	for (
		const close of [
			() => mall.browse( 1, 1 ),
			() => mall.paginate( 1, 14 ),
			() => mall.setSearch( "x" ),
			() => mall.choose( mall.read( state() ).offers[0] ),
			() => mall.closePopup(),
			() => mall.close()
		]
	) {
		mall.open();
		mall.setSearch( "" );
		mall.cancel();
		mall.openPopup( 1 );
		assert.equal( mall.read( state() ).popup, 1 );
		close();
		assert.equal( mall.read( state() ).popup, -1 );
	}
});

test("the rail, sub tabs and cards send the classic window's actions", () => {
	const mall = renewalMall();
	mall.browse( 1, 1 );
	const ops = view( mall );
	for ( let index = 0; index < mallCategoriesLater().length; index++ ) {
		assert.ok( button( ops, "item-mall-category:" + index ), "category " + index );
	}
	assert.equal( button( ops, "item-mall-category:1" ).selected, true );
	// Each sub tab draws its caption inside its own rect, not only as the
	// hit area's label (the first renewal build drew blank tabs).
	for ( const [index, caption] of [ [ 0, "Booth" ], [ 1, "Dress" ] ] ) {
		const tab = button( ops, "item-mall-tab:" + index );
		assert.equal( tab.label, caption );
		const drawn = ops.find( op => op.kind === "text" && op.value === caption );
		assert.ok( drawn, caption + " is drawn" );
		assert.ok( inside( tab.rect, drawn.rect ), caption + " sits on its tab" );
	}
	assert.equal( button( ops, "item-mall-tab:1" ).selected, true );
	assert.ok( button( ops, "item-mall-home" ) );
	const cards = buttons( ops ).filter( op => op.id.startsWith( "item-mall-popup:" ) );
	assert.deepEqual( cards.map( op => op.id ), Array.from( { length: 10 }, ( _, i ) => "item-mall-popup:" + i ) );
	assert.deepEqual( cards[0].rect, renewalGoodsRect( 0 ) );
	const edit = defined( ops.find( op => op.kind === "edit" ) );
	assert.equal( edit.id, "item-mall-search" );
	assert.equal( edit.maxLength, 32 );
	// The pager steps one page each way; on page one the back arrow is off.
	assert.equal( button( ops, "item-mall-page:0" ).disabled, true );
	assert.equal( button( ops, "item-mall-page:1" ).disabled, false );
	// Two tabs fit the row: it does not scroll.
	assert.equal( buttons( ops ).some( op => op.id.startsWith( "item-mall-tab-scroll:" ) ), false );
});

/*
================
petState

The v1.150 Pet category's five tabs (refshoptab 2287-2291): one more
than the row holds.
================
*/
function petState() {
	return {
		...state(),
		tabs: [ "PET_GROWTH", "PET_SILK", "VEHICLE", "CART", "PET_STORES" ].map( ( name, tab ) => ({
			shop: 2,
			tab,
			category: "MALL_PET",
			label: "SN_TAB_" + name
		}) )
	};
}

test("a category with five tabs scrolls its tab row, so its fifth tab can be chosen (issue #4093)", () => {
	const mall = renewalMall();
	mall.browse( 2 );
	const draw = () => view( mall, { state: petState() } );
	const tabIds = ops => buttons( ops ).filter( op => op.id.startsWith( "item-mall-tab:" ) ).map( op => op.id );
	let ops = draw();
	assert.deepEqual( tabIds( ops ), [ "item-mall-tab:0", "item-mall-tab:1", "item-mall-tab:2" ] );
	assert.equal( button( ops, "item-mall-tab-scroll:0" ).disabled, true );
	assert.equal( button( ops, "item-mall-tab-scroll:1" ).disabled, false );
	// The tabs and both arrows stay on the goods area and never overlap.
	const row = buttons( ops ).filter( op => op.id.startsWith( "item-mall-tab" ) ).map( op => op.rect );
	assert.equal( row.length, 5 );
	for ( const [index, rect] of row.entries() ) {
		assert.ok( inside( RENEWAL_GOODS_AREA, rect ), "row control " + index );
		for ( const other of row.slice( index + 1 ) ) {
			assert.ok( rect[0] + rect[2] <= other[0] || other[0] + other[2] <= rect[0], "row controls overlap" );
		}
	}
	// Each arrow steps one tab; past the last the row stays put.
	mall.scrollTabs( 1 );
	assert.deepEqual( tabIds( draw() ), [ "item-mall-tab:1", "item-mall-tab:2", "item-mall-tab:3" ] );
	mall.scrollTabs( 9 );
	ops = draw();
	assert.deepEqual( tabIds( ops ), [ "item-mall-tab:2", "item-mall-tab:3", "item-mall-tab:4" ] );
	assert.equal( button( ops, "item-mall-tab-scroll:1" ).disabled, false );
	assert.equal( button( ops, "item-mall-tab-scroll:2" ).disabled, true );
	assert.deepEqual( button( ops, "item-mall-tab:2" ).rect, renewalSubTabRect( 0 ) );
	// Choosing a tab keeps the row where it stands.
	mall.browse( 2, 4 );
	ops = draw();
	assert.deepEqual( tabIds( ops ), [ "item-mall-tab:2", "item-mall-tab:3", "item-mall-tab:4" ] );
	assert.equal( button( ops, "item-mall-tab:4" ).selected, true );
	// Another category starts its row at its first tab.
	mall.browse( 0 );
	mall.browse( 2 );
	assert.deepEqual( tabIds( draw() ), [ "item-mall-tab:0", "item-mall-tab:1", "item-mall-tab:2" ] );
	// A malformed scroll is ignored.
	mall.scrollTabs( -1 );
	mall.scrollTabs( 1.5 );
	assert.equal( mall.read( petState() ).tabFirst, 0 );
});

test("the native text wins and the later strings fill the keys v1.150 lacks", () => {
	const mall = renewalMall();
	const ops = view( mall );
	const texts = ops.filter( op => op.kind === "text" ).map( op => op.value );
	assert.ok( texts.includes( "Buy" ) );
	assert.ok( !texts.includes( "Buy (later)" ) );
	assert.ok( texts.includes( "Original" ) );
	assert.ok( texts.includes( "Write Item name" ) );
	assert.ok( texts.includes( "Hero" ) );
	assert.ok( texts.includes( "Lv. 42" ) );
	assert.ok( texts.includes( "700" ) );
	// Port-only (#4133): the earned balance shows beside the granted one.
	assert.ok( texts.includes( "Earned Silk" ) );
	assert.ok( texts.includes( "25" ) );
});

test("the wallet rows stack Silk, Earned Silk and Bonus Silk, and draw no Point row", () => {
	// #4133 follow-up: four rows crowded a box drawn for three. #4480: the
	// Point row is the English client's hidden one, so three Silk rows show.
	const ops = view( renewalMall() );
	const texts = ops.filter( op => op.kind === "text" );
	const silkValue = defined( texts.find( op => op.value === "700" ) ).rect;
	assert.ok( !texts.some( op => op.value === "5" && op.rect[0] === silkValue[0] ), "the Point balance is drawn" );
	// The balances (silk 700, earned 25, bonus 20) in the value column.
	const rows = [ "700", "25", "20" ].map( value =>
		defined( texts.find( op => op.value === value && op.rect[0] === silkValue[0] ), value ).rect
	);
	for ( let i = 1; i < rows.length; i++ ) {
		assert.ok(
			rows[i][1] - rows[i - 1][1] >= 14,
			`row ${i} at ${rows[i][1]} crowds row ${i - 1} at ${rows[i - 1][1]}`
		);
	}
	// The earned caption shares its value's row, under the Silk row.
	const earnedCaption = defined( texts.find( op => op.value === "Earned Silk" ) ).rect;
	assert.equal( earnedCaption[1], rows[1][1] );
	// The last row stays inside the box's background, above the buttons.
	const bg = defined(
		ops.find( op => op.kind === "image" && op.path.endsWith( "mall_charac_textbox.png" ) ),
		"wallet background"
	).rect;
	const last = rows[rows.length - 1];
	assert.ok( last[1] + last[3] <= bg[1] + bg[3], `the last row ends at ${last[1] + last[3]}, past ${bg[1] + bg[3]}` );
});

test("each wallet row carries hover help over its caption, value and mark (#4480)", () => {
	const help = ops => ops.filter( op => op.kind === "help" );
	const texts = view( renewalMall() ).filter( op => op.kind === "text" );
	const rows = help( view( renewalMall() ) );
	assert.deepEqual( rows.map( op => op.id ), [
		"item-mall-renewal-wallet-silk",
		"item-mall-renewal-wallet-earned",
		"item-mall-renewal-wallet-bonus"
	] );
	assert.deepEqual(
		rows.map( op => op.text ),
		/** @type {const} */ ([ "silk", "earned", "bonus" ]).map( kind => silkWalletHelp( kind, false ) )
	);
	// Each region covers its own row's value and no other row's.
	for ( const [index, value] of [ "700", "25", "20" ].entries() ) {
		const at = defined( texts.find( op => op.value === value ), value ).rect;
		const inside = rect => at[1] >= rect[1] && at[1] + at[3] <= rect[1] + rect[3] && at[0] >= rect[0];
		assert.deepEqual( rows.map( op => inside( op.rect ) ), rows.map( ( _, i ) => i === index ), value );
	}
	// The earned help promises tradable goods only while the shard allows it.
	const tradable = help( view( renewalMall(), { earnedTradable: true } ) )[1].text;
	assert.equal( tradable, silkWalletHelp( "earned", true ) );
	assert.notEqual( tradable, rows[1].text );
});

test("the popup buys, tries on and reserves the card it opened on", () => {
	const mall = renewalMall();
	mall.browse( 1, 1 );
	mall.openPopup( 4 );
	const wearable = Array.from( { length: 10 }, ( _, i ) => i !== 4 );
	const ops = view( mall, { wearable } );
	assert.equal( button( ops, "item-mall-buy:4" ).disabled, false );
	assert.equal( button( ops, "item-mall-wear:4" ).disabled, true );
	assert.equal( button( ops, "item-mall-reserve:4" ).disabled, false );
	// Every popup button carries a caption the client's table has.
	assert.deepEqual(
		[ "item-mall-buy:4", "item-mall-wear:4", "item-mall-reserve:4" ].map( id => button( ops, id ).label ),
		[ "Buy", "Wear Item", "Reserve" ]
	);
	assert.equal( button( ops, "item-mall-popup:4" ).selected, true );
	const busy = view( mall, { busy: true } );
	assert.equal( button( busy, "item-mall-buy:4" ).disabled, true );
	assert.equal( button( busy, "item-mall-popup:0" ).disabled, true );
});

test("Buy buys what is worn, or the whole basket while the basket is open", () => {
	const mall = renewalMall();
	mall.browse( 1, 1 );
	let ops = view( mall );
	assert.equal( button( ops, "item-mall-root:4" ).disabled, true );
	assert.equal( button( ops, "item-mall-root:5" ).disabled, true );
	mall.previewState( { wearable: offers( 14 ).flatMap( offer => offer.itemIds ) } );
	mall.wear( mall.read( state() ).offers[0], state() );
	ops = view( mall );
	assert.equal( button( ops, "item-mall-root:4" ).disabled, false );
	assert.equal( button( ops, "item-mall-root:5" ).disabled, false );
	mall.reserve( state().offers[2] );
	mall.browse( mallCategoriesLater().findIndex( entry => entry.key === "basket" ) );
	ops = view( mall );
	assert.equal( button( ops, "item-mall-buy-all" ).disabled, false );
	assert.equal( button( ops, "item-mall-root:4" ), undefined );
});

test("the lists, buy silk and coupon stay disabled as the classic window's are", () => {
	const ops = view( renewalMall() );
	for (
		const id of [
			"item-mall-renewal-buy-list",
			"item-mall-renewal-receive-list",
			"item-mall-renewal-211",
			"item-mall-renewal-212"
		]
	) {
		assert.equal( button( ops, id ).disabled, true, id );
	}
});

test("an empty main page says so, and the admission lists every picture and frame part once", () => {
	const mall = renewalMall();
	const ops = view( mall, { state: { ...state(), offers: [] } } );
	assert.equal( buttons( ops ).some( op => op.id.startsWith( "item-mall-popup:" ) ), false );
	const parts = [ "left_up", "right_up", "right_down", "left_down", "left_side", "mid_up", "right_side", "mid_down" ];
	const pictures = renewalMallPictures( ops, parts );
	assert.equal( new Set( pictures ).size, pictures.length );
	assert.ok( pictures.includes( LATER + "frame/mall_wnd_left_up.png" ) );
	assert.ok( pictures.includes( LATER + "mall/mall_back_wnd.png" ) );
	assert.ok( pictures.includes( LATER + "mall/mall_item_tab_off.png" ) );
	assert.ok( pictures.every( path => path.startsWith( LATER ) ) );
});

test("one sex chosen flows its offers two to a row, with no empty partner cells", () => {
	// The Male filter listed every outfit alone on its row: one column.
	const males = named( [ "Knight (M)", "Fairy (M)", "Pirate (M)", "Devil (M)", "Scarf" ] );
	assert.deepEqual( renewalGoodsCells( males, {}, false ), [ 0, 1, 2, 3, 4 ] );
	assert.deepEqual( renewalGoodsCells( males ), [ 0, -1, 1, -1, 2, -1, 3, -1, 4, -1 ], "All keeps the pair rows" );
});
