/*
===========================================================================

renewal-mall-view.ts - the renewal Item Mall window as draw operations

Not in v1.150: the later client's mall. It is the desktop Item Mall
whenever the later import is published (owner 2026-11-20, agreed with
rptlee: the default, not an Experimental row); a build without the
import keeps the classic window. The window is the later client's
CIFNewItemMall, built from its published
layouts (foundation/ui/renewal-mall.ts). This module turns the mall's
browsing state into a flat list of draw operations; the renderer
(ui.ts) paints them with its ordinary primitives and owns nothing else
of the window. The purchase, points and question dialogs stay the
shared ones the classic window opens.

Every action id is one the classic window already dispatches
(item-mall-category, item-mall-tab, item-mall-buy, item-mall-wear,
item-mall-reserve, item-mall-root:4/5, item-mall-buy-all, item-mall-page,
item-mall-home, item-mall-search, item-mall-close), plus the card popup's
item-mall-popup:<index> and item-mall-popup-close.

===========================================================================
*/

import type { UiRect } from "@/engine/contracts/ui";
import type { MallOffer, MallTab } from "@/engine/contracts/item-mall";
import { type MallCategoryList, mallTabCaptionLater } from "@/engine/foundation/ui/item-mall-layout";
import {
	RENEWAL_ART,
	RENEWAL_GOODS_PER_PAGE,
	RENEWAL_MALL_HEIGHT,
	RENEWAL_MALL_TITLE_HEIGHT,
	RENEWAL_MALL_WIDTH,
	RENEWAL_SUB_TABS_MAX,
	RENEWAL_WINDOW_FRAME,
	renewalBadgeArt,
	renewalGoodsRect,
	renewalMallControl,
	renewalPagerRects,
	renewalPopupButtonRect,
	renewalPopupRect,
	renewalRailIcon,
	renewalRailRect,
	renewalSubTabRect,
	type RenewalMallControl,
	type RenewalMallData
} from "@/engine/foundation/ui/renewal-mall";

type Color = readonly [number, number, number, number];

/*
================
RenewalMallOp

One draw operation, in screen pixels. "frame" is an eight-part ring
under prefix; "tile" repeats its picture over rect; "button" and "edit"
are hit areas as well as pixels; "icon" is an item icon by its catalogue
name; "doll" is the try-on mannequin.
================
*/
export type RenewalMallOp =
	| { readonly kind: "frame"; readonly prefix: string; readonly rect: UiRect; }
	| { readonly kind: "image"; readonly path: string; readonly rect: UiRect; }
	| { readonly kind: "tile"; readonly path: string; readonly rect: UiRect; }
	| { readonly kind: "icon"; readonly icon: string; readonly rect: UiRect; }
	| {
		readonly kind: "text";
		readonly value: string;
		readonly rect: UiRect;
		readonly color: Color;
		readonly hAlign: number;
		readonly vAlign: number;
		// Word-wrapped from the rect's top (a card's name); otherwise one
		// line, aligned in the rect and cut with "..." when too long.
		readonly wrap: boolean;
	}
	| {
		readonly kind: "button";
		readonly id: string;
		readonly label: string;
		readonly rect: UiRect;
		readonly texture: string;
		readonly disabled: boolean;
		readonly selected: boolean;
	}
	| {
		readonly kind: "edit";
		readonly id: string;
		readonly label: string;
		readonly rect: UiRect;
		readonly value: string;
		readonly maxLength: number;
		// Where the value's text is drawn; the renderer puts the blinking
		// caret after it while the field has focus.
		readonly textRect: UiRect;
	}
	| { readonly kind: "doll"; readonly rect: UiRect; };

/*
================
RenewalMallBrowse

The parts of the mall owner's read() this window draws.
================
*/
export interface RenewalMallBrowse {
	readonly categories: MallCategoryList;
	readonly category: number;
	readonly tab: number;
	readonly tabs: readonly MallTab[];
	readonly page: number;
	readonly pageSize: number;
	readonly count: number;
	readonly offers: readonly MallOffer[];
	readonly search: string;
	readonly searchDraft: string;
	readonly popup: number;
	readonly worn: readonly MallOffer[];
	readonly batchPending: boolean;
	// grid[cell] is the page offer drawn in that cell, or -1 for an empty
	// cell (renewalGoodsCells); null draws the offers in reading order.
	readonly grid: readonly number[] | null;
}

/*
================
RenewalMallInput

Everything the window reads, in one struct: the published layouts, the
browsing state, the balances and character, the screen origin, the
client's text table (the later client's strings fill the keys it
lacks), whether an inventory change is in flight, and which offers on
the page can be tried on (parallel to browse.offers).
================
*/
export interface RenewalMallInput {
	readonly data: RenewalMallData;
	readonly browse: RenewalMallBrowse;
	readonly balance: { readonly silk: number; readonly giftSilk: number; readonly points: number; } | undefined;
	readonly character: { readonly name: string; readonly level: number | undefined; };
	readonly origin: readonly [number, number];
	readonly strings: Readonly<Record<string, string>>;
	readonly busy: boolean;
	readonly wearable: readonly boolean[];
	// Whether the search field has the keyboard focus: its prompt gives
	// way to the caret.
	readonly searchFocused: boolean;
}

const WHITE: Color = [ 1, 1, 1, 1 ];
const GOLD: Color = [ 1, 0.86, 0.45, 1 ];
const MUTED: Color = [ 0.62, 0.6, 0.55, 1 ];
// The window title the outer frame carries: the classic window's own
// (ginterface GDR_ITEM_MALL's Text, "Item Mall").
const TITLE_KEY = "UIIT_STT_SILKMALL";
const SEARCH_MAX = 32;

/*
================
renewalMallView

The whole window, back to front: chrome, the rail, the search row, the
sub tabs, the goods grid and its pager, the character panel, my info,
then the open card popup on top.
================
*/
export function renewalMallView( input: RenewalMallInput ): RenewalMallOp[] {
	const ops: RenewalMallOp[] = [];
	const [ox, oy] = input.origin;
	const { browse, data } = input;
	const copy = ( key: string ) => input.strings[key] || data.strings[key] || "";
	const at = ( r: UiRect ): UiRect => [ ox + r[0], oy + r[1], r[2], r[3] ];
	const control = ( name: string ) => renewalMallControl( data.window, name );

	/*
	================
	authored

	One authored picture control at its own rect: a frame ring, a tiled
	background (the com_bg_tile_* fills) or a plain picture.
	================
	*/
	function authored( c: RenewalMallControl, dx = ox, dy = oy ) {
		if ( !c.texture ) return;
		const rect: UiRect = [ dx + c.rect[0], dy + c.rect[1], c.rect[2], c.rect[3] ];
		if ( c.frame ) ops.push( { kind: "frame", prefix: c.texture, rect } );
		else if ( c.texture.includes( "/bg_tile/" ) ) ops.push( { kind: "tile", path: c.texture, rect } );
		else ops.push( { kind: "image", path: c.texture, rect } );
	}

	/*
	================
	label
	================
	*/
	function label( value: string, rect: UiRect, color: Color = WHITE, hAlign = 0, vAlign = 1, wrap = false ) {
		if ( value ) ops.push( { kind: "text", value, rect, color, hAlign, vAlign, wrap } );
	}

	/*
	================
	button

	An authored button: its picture, caption key and font colour.
	================
	*/
	function button( c: RenewalMallControl, id: string, disabled: boolean, dx = ox, dy = oy ) {
		ops.push( {
			kind: "button",
			id,
			label: c.text ? copy( c.text ) : "",
			rect: [ dx + c.rect[0], dy + c.rect[1], c.rect[2], c.rect[3] ],
			texture: c.texture,
			disabled,
			selected: false
		} );
	}

	// Chrome: the outer frame and the authored backgrounds, as the
	// layout file orders them back to front (it lists the front first).
	ops.push( {
		kind: "frame",
		prefix: RENEWAL_WINDOW_FRAME,
		rect: [ ox, oy, RENEWAL_MALL_WIDTH, RENEWAL_MALL_HEIGHT ]
	} );
	label(
		copy( TITLE_KEY ),
		[ ox, oy + 8, RENEWAL_MALL_WIDTH, RENEWAL_MALL_TITLE_HEIGHT - 16 ],
		GOLD,
		1
	);
	for (
		const name of [
			"GDR_RENEWAL_ITEM_MALL_SUB_FRAME",
			"GDR_RENEWAL_ITEM_MALL_SUB_BG",
			"GDR_RENEWAL_ITEM_MALL_BG",
			"GDR_RENEWAL_ITEM_MALL_MID_BG",
			"GDR_RENEWAL_ITEM_MALL_LEFT_FRAME",
			"GDR_RENEWAL_ITEM_MALL_LEFT_BG",
			"GDR_RENEWAL_ITEM_MALL_RIGHT_BG",
			"GDR_RENEWAL_ITEM_MALL_RIGHT_BG_WND",
			"GDR_RENEWAL_ITEM_MALL_SEARCH_BG"
		]
	) {
		authored( control( name ) );
	}

	// The main page button and the category rail under it.
	button( control( "GDR_RENEWAL_ITEM_MALL_MAIN_BTN" ), "item-mall-home", false );
	for ( let index = 0; index < browse.categories.length; index++ ) {
		const category = browse.categories[index]!;
		const rect = at( renewalRailRect( index ) );
		const on = index === browse.category;
		ops.push( {
			kind: "button",
			id: "item-mall-category:" + index,
			label: "",
			rect,
			texture: RENEWAL_ART + (on ? "mall_item_tab_on.png" : "mall_item_tab_off.png"),
			disabled: false,
			selected: on
		} );
		// The main slot: the 20x20 icon at (7,6), the title at (32,10).
		ops.push( {
			kind: "image",
			path: renewalRailIcon( category.icon ),
			rect: [ rect[0] + 7, rect[1] + 6, 20, 20 ]
		} );
		label(
			copy( "UIIT_STT_SILKMALL_" + category.text ),
			[ rect[0] + 32, rect[1] + 8, rect[2] - 36, 16 ],
			on ? GOLD : WHITE
		);
	}

	// The search row: the edit holds a draft over its background; the
	// Search button (or Enter) applies it (item-mall-search-find).
	const search = control( "GDR_RENEWAL_ITEM_MALL_SEARCH" );
	// The edit is a hit area over drawn pixels: the typed text, or the
	// authored prompt while it is empty and unfocused.
	const searchText = at( [ search.rect[0] + 8, search.rect[1] + 2, search.rect[2] - 16, search.rect[3] - 4 ] );
	ops.push( {
		kind: "edit",
		id: "item-mall-search",
		label: copy( search.text ),
		rect: at( [ search.rect[0] + 6, search.rect[1] + 2, search.rect[2] - 12, search.rect[3] - 4 ] ),
		value: browse.searchDraft,
		maxLength: SEARCH_MAX,
		textRect: searchText
	} );
	if ( browse.searchDraft !== "" ) label( browse.searchDraft, searchText, WHITE );
	else if ( !input.searchFocused ) label( copy( search.text ), searchText, MUTED );
	const find = control( "GDR_RENEWAL_ITEM_MALL_SEARCH_BTN" );
	button( find, "item-mall-search-find", input.busy );
	label( copy( find.text ), at( find.rect ), WHITE, 1 );

	// The sub-category tabs of the open category.
	if ( browse.category >= 0 && browse.search === "" ) {
		const shown = Math.min( browse.tabs.length, RENEWAL_SUB_TABS_MAX );
		for ( let index = 0; index < shown; index++ ) {
			const on = index === browse.tab;
			const caption = mallTabCaptionLater( browse.tabs[index]!.category, index ) ??
				copy( browse.tabs[index]!.label );
			const rect = at( renewalSubTabRect( index ) );
			ops.push( {
				kind: "button",
				id: "item-mall-tab:" + index,
				label: caption,
				rect,
				texture: RENEWAL_ART + (on ? "mall_sub_tab_on.png" : "mall_sub_tab_off.png"),
				disabled: false,
				selected: on
			} );
			// A button op is pixels and a hit area; its caption is drawn
			// here, as the rail's and the panel buttons' are.
			label( caption, rect, on ? GOLD : WHITE, 1 );
		}
	}

	// The goods grid.
	const slot = data.itemSlot;
	const slotName = renewalMallControl( slot, "GDR_RENEWAL_ITEM_MALL_ITEM_NAME" );
	const slotSilk = renewalMallControl( slot, "GDR_RENEWAL_ITEM_MALL_ITEM_SILK" );
	const slotSilkMark = renewalMallControl( slot, "GDR_RENEWAL_ITEM_MALL_ITEM_SILK_STR" );
	const slotIcon = renewalMallControl( slot, "GDR_RENEWAL_SHOP_SLOT_ICON" );
	const slotCount = renewalMallControl( slot, "GDR_RENEWAL_ITEM_MALL_ITEM_COUT" );
	const grid = renewalPageGrid( browse );
	for ( let cell = 0; cell < grid.length; cell++ ) {
		const index = grid[cell]!;
		if ( index < 0 ) continue;
		const offer = browse.offers[index]!;
		const card = at( renewalGoodsRect( cell ) );
		const open = index === browse.popup;
		ops.push( {
			kind: "button",
			id: "item-mall-popup:" + index,
			label: "",
			rect: card,
			texture: RENEWAL_ART + (open ? "mall_item_wnd_focus.png" : "mall_item_wnd.png"),
			disabled: input.busy || browse.batchPending,
			selected: open
		} );
		const iconRect: UiRect = [
			card[0] + slotIcon.rect[0] + 6,
			card[1] + slotIcon.rect[1] + 6,
			32,
			32
		];
		ops.push( { kind: "icon", icon: offer.icon, rect: iconRect } );
		const badge = renewalBadgeArt( offer.badge );
		if ( badge ) ops.push( { kind: "image", path: badge, rect: [ card[0] + 8, card[1] + 8, 12, 12 ] } );
		label(
			copy( offer.name ),
			[ card[0] + slotName.rect[0], card[1] + slotName.rect[1], slotName.rect[2], slotName.rect[3] ],
			slotName.fontColor,
			0,
			0,
			true
		);
		label(
			String( offer.silk ),
			[ card[0] + slotSilk.rect[0], card[1] + slotSilk.rect[1], slotSilk.rect[2], slotSilk.rect[3] ],
			GOLD,
			2
		);
		authored( slotSilkMark, card[0], card[1] );
		if ( offer.giftSilk > 0 ) {
			label(
				offer.giftSilk + " " + copy( "UIIT_STT_SILKMALL_ITEM_GIFTSILK" ),
				[ card[0] + slotCount.rect[0], card[1] + slotCount.rect[1], slotCount.rect[2], slotCount.rect[3] ],
				MUTED,
				2
			);
		}
	}
	// An empty page draws an empty grid: no client table carries a
	// "no goods" text for the mall, and the port invents none.

	// The pager: the arrows step one page; the label is "page / pages".
	const pages = Math.max( 1, Math.ceil( browse.count / browse.pageSize ) );
	if ( pages > 1 ) {
		const pager = renewalPagerRects();
		ops.push( {
			kind: "button",
			id: "item-mall-page:" + Math.max( 0, browse.page - 1 ),
			label: "",
			rect: at( pager.prev ),
			texture: RENEWAL_ART + "mall_page_prev.png",
			disabled: browse.page === 0,
			selected: false
		} );
		label( browse.page + 1 + " / " + pages, at( pager.label ), WHITE, 1 );
		ops.push( {
			kind: "button",
			id: "item-mall-page:" + Math.min( pages - 1, browse.page + 1 ),
			label: "",
			rect: at( pager.next ),
			texture: RENEWAL_ART + "mall_page_next.png",
			disabled: browse.page >= pages - 1,
			selected: false
		} );
	}

	// The character panel: the try-on mannequin and its four buttons.
	// Buy buys what is worn (the basket, while the basket is open); Reset
	// takes everything off. INFERENCE: the classic window's "buy what is
	// worn" and "take all off" (6BBAB0, 6C2390) are the same two actions.
	ops.push( { kind: "doll", rect: at( control( "GDR_RENEWAL_ITEM_MALL_RIGHT" ).rect ) } );
	const basketOpen = browse.categories[browse.category]?.key === "basket";
	button(
		control( "GDR_RENEWAL_ITEM_MALL_RIGHT_BTN01" ),
		basketOpen ? "item-mall-buy-all" : "item-mall-root:4",
		input.busy || browse.batchPending || (basketOpen ? browse.count === 0 : browse.worn.length === 0)
	);
	button(
		control( "GDR_RENEWAL_ITEM_MALL_RIGHT_BTN02" ),
		"item-mall-root:5",
		browse.worn.length === 0
	);
	// The buying and receiving lists stay disabled, as the classic
	// window's Buying List and Obtained List are born (6BC17E..6BC197).
	button( control( "GDR_RENEWAL_ITEM_MALL_RIGHT_SILKBUYLIST_BTN03" ), "item-mall-renewal-buy-list", true );
	button( control( "GDR_RENEWAL_ITEM_MALL_RIGHT_SILKRECIEVELIST_BTN04" ), "item-mall-renewal-receive-list", true );
	for ( const c of [ "GDR_RENEWAL_ITEM_MALL_RIGHT_BTN01", "GDR_RENEWAL_ITEM_MALL_RIGHT_BTN02" ] ) {
		const node = control( c );
		label( copy( node.text ), at( node.rect ), node.fontColor, 1 );
	}
	for (
		const c of [
			"GDR_RENEWAL_ITEM_MALL_RIGHT_SILKBUYLIST_BTN03",
			"GDR_RENEWAL_ITEM_MALL_RIGHT_SILKRECIEVELIST_BTN04"
		]
	) {
		const node = control( c );
		label( copy( node.text ), at( node.rect ), MUTED, 1 );
	}

	// My info, at the window's MyInfo section origin.
	const infoOrigin = renewalMallControl( data.windowInfo, "GDR_ITEM_MALL_MYINFO" ).rect;
	const ix = ox + infoOrigin[0], iy = oy + infoOrigin[1];
	const info = data.myInfo;
	const infoControl = ( name: string ) => renewalMallControl( info, name );
	const infoRect = ( name: string ): UiRect => {
		const r = infoControl( name ).rect;
		return [ ix + r[0], iy + r[1], r[2], r[3] ];
	};
	authored( infoControl( "GDR_ITEM_MALL_MYINFO_BG" ), ix, iy );
	label( input.character.name, infoRect( "GDR_ITEM_MALL_MYINFO_NAME" ), GOLD );
	if ( input.character.level !== undefined ) {
		label( "Lv. " + input.character.level, infoRect( "GDR_ITEM_MALL_MYINFO_LEVEL" ), WHITE, 2 );
	}
	for (
		const [caption, value, mark, amount] of [
			[
				"GDR_ITEM_MALL_MYINFO_STA_SILK_STR",
				"GDR_ITEM_MALL_MYINFO_SILK",
				"GDR_ITEM_MALL_MYINFO_STA_SILK",
				input.balance?.silk
			],
			[
				"GDR_ITEM_MALL_MYINFO_STA_GIFT_SILK_STR",
				"GDR_ITEM_MALL_MYINFO_GIFT_SILK",
				"GDR_ITEM_MALL_MYINFO_STA_GIFT_SILK",
				input.balance?.giftSilk
			],
			[
				"GDR_ITEM_MALL_MYINFO_STA_POINT_STR",
				"GDR_ITEM_MALL_MYINFO_POINT",
				"GDR_ITEM_MALL_MYINFO_STA_POINT_P",
				input.balance?.points
			]
		] as const
	) {
		label( copy( infoControl( caption ).text ), infoRect( caption ), WHITE );
		label( String( amount ?? 0 ), infoRect( value ), GOLD, 2 );
		const markControl = infoControl( mark );
		if ( markControl.texture ) authored( markControl, ix, iy );
		else label( copy( markControl.text ), infoRect( mark ), WHITE );
	}
	// Buy silk and Coupon open retail web pages; the port has none to
	// link to, so both stay disabled as the classic window's do.
	for ( const name of [ "GDR_ITEM_MALL_MYINFO_BUY_SILK_BTN", "GDR_ITEM_MALL_MYINFO_COUPON_EXCHANGE_BTN" ] ) {
		const node = infoControl( name );
		button( node, "item-mall-renewal-" + node.id, true, ix, iy );
		label( copy( node.text ), infoRect( name ), MUTED, 1 );
	}

	// The open card's popup: buy, preview (try on) and basket.
	const popupOffer = browse.popup >= 0 ? browse.offers[browse.popup] : undefined;
	if ( popupOffer ) {
		const popup = at( renewalPopupRect( Math.max( 0, grid.indexOf( browse.popup ) ) ) );
		const frame = renewalMallControl( data.window, "GDR_RENEWAL_ITEM_MALL_POPUP_MENU" );
		ops.push( { kind: "frame", prefix: frame.texture, rect: popup } );
		// The classic slot's reserve button caption (ifitemmallshopslot
		// button 6, "Reserve"); inside the basket it removes ("Delete").
		const basketLabel = basketOpen ? "UIIT_STT_SILKMALL_DEL" : "UIIT_STT_SILKMALL_ZZIM";
		const entries = [
			[ "item-mall-buy:" + browse.popup, "UIIT_STT_BUY", false ],
			[ "item-mall-wear:" + browse.popup, "UIIT_STT_SILKMALL_PUT_ON", input.wearable[browse.popup] !== true ],
			[ "item-mall-reserve:" + browse.popup, basketLabel, false ]
		] as const;
		for ( let index = 0; index < entries.length; index++ ) {
			const [id, key, off] = entries[index]!;
			const rect = renewalPopupButtonRect( popup, index );
			ops.push( {
				kind: "button",
				id,
				label: copy( key ),
				rect,
				texture: RENEWAL_ART + "mall_avata_button.png",
				disabled: off || input.busy || browse.batchPending,
				selected: false
			} );
			label( copy( key ), rect, off ? MUTED : WHITE, 1 );
		}
	}
	return ops;
}

/*
================
renewalPageGrid

The page's cells: the owner's grid when it laid one out, else the page
offers in reading order. Never more cells than the grid holds.
================
*/
function renewalPageGrid( browse: RenewalMallBrowse ): readonly number[] {
	if ( browse.grid ) return browse.grid.slice( 0, RENEWAL_GOODS_PER_PAGE );
	return browse.offers.slice( 0, RENEWAL_GOODS_PER_PAGE ).map( ( _, index ) => index );
}

/*
================
renewalMallPictures

Every picture the window may draw this frame, for the window's resource
admission: the ops' own pictures and frame parts. Item icons are the
renderer's to resolve.
================
*/
export function renewalMallPictures( ops: readonly RenewalMallOp[], frameParts: readonly string[] ): string[] {
	const out = new Set<string>();
	for ( const op of ops ) {
		if ( op.kind === "image" || op.kind === "tile" ) out.add( op.path );
		else if ( op.kind === "button" && op.texture ) out.add( op.texture );
		else if ( op.kind === "frame" ) { for ( const part of frameParts ) out.add( op.prefix + part + ".png" ); }
	}
	return [ ...out ];
}
