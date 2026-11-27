/*
===========================================================================

renewal-mall-fixture.mjs - the renewal Item Mall catalogue and owner fixtures

The published control rows, catalogue file, offers, browsing state and view
renewal-mall.test.mjs builds its cases from, kept apart from the cases
themselves.

===========================================================================
*/
import "./native-source-loader.mjs";
import { defined } from "./defined.mjs";
const { createItemMall } = await import( "../../src/engine/runtime/ui/hud/item-mall.ts" );
const { renewalMallView } = await import( "../../src/engine/runtime/ui/hud/renewal-mall-view.ts" );
const { decodeRenewalMall, RENEWAL_GOODS_PER_PAGE } = await import( "../../src/engine/foundation/ui/renewal-mall.ts" );

export const LATER = "/assets/images/Media_extracted/later/interface/";

/*
================
control

One published control row, as import_vsro_renewal_mall.py writes it.
================
*/
export function control( name, id, rect, texture = "", text = "", cls = "CIFStatic" ) {
	return {
		name,
		id,
		class: cls,
		rect,
		inset: [ 0, 0, 0, 0 ],
		texture,
		frame: texture !== "" && !texture.endsWith( ".png" ),
		text,
		fontIndex: 0,
		fontColor: [ 1, 1, 1, 1 ],
		hAlign: 0,
		vAlign: 1
	};
}

/*
================
catalogueFile

A published renewal-mall.json with the authored rects of the pinned
later client's layouts.
================
*/
export function catalogueFile() {
	const art = LATER + "mall/";
	return {
		format: "sro-renewal-mall-later",
		version: 1,
		sections: {
			window: [
				control( "GDR_RENEWAL_ITEM_MALL_POPUP_MENU", 100, [ 0, 0, 126, 143 ], LATER + "frame/mall_sub_wnd04_" ),
				control(
					"GDR_RENEWAL_ITEM_MALL_MAIN_BTN",
					104,
					[ 18, 45, 120, 28 ],
					art + "mall_mainpage.png",
					"",
					"CIFButton"
				),
				control(
					"GDR_RENEWAL_ITEM_MALL_SEARCH",
					106,
					[ 141, 43, 332, 24 ],
					"",
					"UIIT_STT_SILKMALL_SEARCH_DEFAULT",
					"CIFEdit"
				),
				control(
					"GDR_RENEWAL_ITEM_MALL_SEARCH_BTN",
					107,
					[ 475, 43, 88, 24 ],
					art + "mall_button.png",
					"UIIT_CTL_PARTYMATCH_PSEARCH_FIND",
					"CIFButton"
				),
				control( "GDR_RENEWAL_ITEM_MALL_SEARCH_BG", 108, [ 141, 43, 332, 24 ], art + "mall_find_wnd.png" ),
				control(
					"GDR_RENEWAL_ITEM_MALL_RIGHT_SILKRECIEVELIST_BTN04",
					132,
					[ 676, 416, 104, 32 ],
					art + "mall_avata_button.png",
					"UIIT_STT_SILKMALL_RECEIVE_LIST",
					"CIFButton"
				),
				control(
					"GDR_RENEWAL_ITEM_MALL_RIGHT_SILKBUYLIST_BTN03",
					131,
					[ 571, 416, 104, 32 ],
					art + "mall_avata_button.png",
					"UIIT_STT_SILKMALL_BUY_LIST",
					"CIFButton"
				),
				control(
					"GDR_RENEWAL_ITEM_MALL_RIGHT_BTN02",
					109,
					[ 676, 377, 104, 32 ],
					art + "mall_avata_button.png",
					"UIIT_CTL_SILKMALL_INIT_ITEM",
					"CIFButton"
				),
				control(
					"GDR_RENEWAL_ITEM_MALL_RIGHT_BTN01",
					110,
					[ 571, 377, 104, 32 ],
					art + "mall_avata_button.png",
					"UIIT_STT_BUY",
					"CIFButton"
				),
				control( "GDR_RENEWAL_ITEM_MALL_RIGHT", 111, [ 567, 46, 215, 332 ] ),
				control( "GDR_RENEWAL_ITEM_MALL_RIGHT_BG", 112, [ 564, 42, 224, 372 ], art + "mall_charac_frame.png" ),
				control(
					"GDR_RENEWAL_ITEM_MALL_RIGHT_BG_WND",
					130,
					[ 564, 414, 224, 124 ],
					art + "mall_charac_wnd.png"
				),
				control(
					"GDR_RENEWAL_ITEM_MALL_LEFT_FRAME",
					114,
					[ 15, 42, 124, 542 ],
					LATER + "frame/mall_ava_wnd_",
					"",
					"CIFFrame"
				),
				control(
					"GDR_RENEWAL_ITEM_MALL_LEFT_BG",
					115,
					[ 25, 52, 83, 510 ],
					LATER + "ifcommon/bg_tile/com_bg_tile_s.png"
				),
				control( "GDR_RENEWAL_ITEM_MALL_MID_BG", 116, [ 137, 72, 424, 519 ], art + "mall_back_wnd.png" ),
				control(
					"GDR_RENEWAL_ITEM_MALL_SUB_FRAME",
					117,
					[ 11, 39, 779, 550 ],
					LATER + "frame/mall_sub_wnd_",
					"",
					"CIFFrame"
				),
				control(
					"GDR_RENEWAL_ITEM_MALL_SUB_BG",
					118,
					[ 31, 59, 739, 510 ],
					LATER + "ifcommon/bg_tile/com_bg_tile_n.png"
				),
				control(
					"GDR_RENEWAL_ITEM_MALL_BG",
					119,
					[ 40, 68, 720, 484 ],
					LATER + "ifcommon/bg_tile/com_bg_tile_m.png"
				)
			],
			windowInfo: [
				control( "GDR_ITEM_MALL_MYINFO", 120, [ 562, 449, 223, 135 ], "", "", "CIFNewItemMallMyInfo" )
			],
			myInfo: [
				control(
					"GDR_ITEM_MALL_MYINFO_STA_POINT_STR",
					200,
					[ 7, 50, 106, 15 ],
					"",
					"UIIT_STT_SILKMALL_ITEM_POINT"
				),
				control(
					"GDR_ITEM_MALL_MYINFO_STA_GIFT_SILK_STR",
					201,
					[ 5, 69, 106, 15 ],
					"",
					"UIIT_STT_SILKMALL_ITEM_GIFTSILK"
				),
				control(
					"GDR_ITEM_MALL_MYINFO_STA_SILK_STR",
					202,
					[ 7, 29, 106, 15 ],
					"",
					"UIIT_STT_SILKMALL_ITEM_SILK"
				),
				control(
					"GDR_ITEM_MALL_MYINFO_STA_POINT_P",
					203,
					[ 199, 50, 14, 15 ],
					"",
					"UIIT_STT_SILKMALL_P_POINT"
				),
				control( "GDR_ITEM_MALL_MYINFO_STA_GIFT_SILK", 204, [ 201, 68, 14, 14 ], art + "mall_silk_icon.png" ),
				control( "GDR_ITEM_MALL_MYINFO_STA_SILK", 205, [ 201, 27, 14, 14 ], art + "mall_silk_icon.png" ),
				control( "GDR_ITEM_MALL_MYINFO_POINT", 206, [ 115, 50, 82, 15 ] ),
				control( "GDR_ITEM_MALL_MYINFO_GIFT_SILK", 207, [ 115, 69, 82, 15 ] ),
				control( "GDR_ITEM_MALL_MYINFO_SILK", 208, [ 115, 29, 82, 15 ] ),
				control( "GDR_ITEM_MALL_MYINFO_LEVEL", 209, [ 115, 9, 98, 15 ] ),
				control( "GDR_ITEM_MALL_MYINFO_NAME", 210, [ 7, 9, 106, 15 ] ),
				control(
					"GDR_ITEM_MALL_MYINFO_COUPON_EXCHANGE_BTN",
					211,
					[ 114, 92, 108, 44 ],
					art + "mall_big_button.png",
					"UIIT_STT_SILKMALL_COUPON_EXCHANGE",
					"CIFButton"
				),
				control(
					"GDR_ITEM_MALL_MYINFO_BUY_SILK_BTN",
					212,
					[ 3, 92, 108, 44 ],
					art + "mall_big_button.png",
					"UIIT_STT_SILKMALL_BUY_SILK",
					"CIFButton"
				),
				control( "GDR_ITEM_MALL_MYINFO_BG", 214, [ 3, 0, 220, 88 ], art + "mall_charac_textbox.png" )
			],
			itemSlot: [
				control( "GDR_RENEWAL_ITEM_MALL_ITEM_NAME", 801, [ 60, 9, 130, 26 ], "", "", "CIFTextBox" ),
				control( "GDR_RENEWAL_ITEM_MALL_ITEM_SILK", 805, [ 124, 42, 51, 12 ] ),
				control( "GDR_RENEWAL_ITEM_MALL_ITEM_SILK_STR", 806, [ 179, 40, 14, 14 ], art + "mall_silk_icon.png" ),
				control( "GDR_RENEWAL_ITEM_MALL_ITEM_COUT", 807, [ 107, 63, 83, 12 ] ),
				control( "GDR_RENEWAL_SHOP_SLOT_ICON", 900, [ 8, 8, 45, 45 ], "", "", "CIFSlotWithHelpForPackage" )
			],
			mainSlot: [],
			subSlot: []
		},
		strings: {
			UIIT_STT_SILKMALL_SEARCH_DEFAULT: "Write Item name",
			UIIT_CTL_SILKMALL_INIT_ITEM: "Original",
			UIIT_STT_BUY: "Buy (later)"
		},
		images: [ { path: art + "mall_item_wnd.png", size: [ 200, 84 ] } ]
	};
}

/*
================
offers

count offers in the avatar shop's dress tab, the first two badged.
================
*/
/** @returns {import("../../src/engine/contracts/item-mall.ts").MallOffer[]} */
export function offers( count ) {
	return Array.from(
		{ length: count },
		( _, slot ) => /** @type {import("../../src/engine/contracts/item-mall.ts").MallOffer} */ ({
			group: 852,
			shop: 5,
			tab: 1,
			slot,
			packageId: 30000 + slot,
			name: "Dress " + slot,
			description: "",
			icon: "item/dress.ddj",
			silk: 130,
			giftSilk: 0,
			currencyMask: 2,
			allowsPoints: false,
			purchaseLimit: 1,
			itemIds: [ 500 + slot ],
			...(slot === 0 ? { badge: "new" } : slot === 1 ? { badge: "hot" } : {})
		})
	);
}

/*
================
state
================
*/
/** @returns {import("../../src/engine/contracts/item-mall.ts").MallState} */
export function state( count = 14 ) {
	return {
		tabs: [
			// refshoptab's caption keys, as the catalogue sends them.
			{ shop: 5, tab: 0, category: "MALL_AVATAR", label: "UIIT_STT_SILKMALL_BOOTH" },
			{ shop: 5, tab: 1, category: "MALL_AVATAR", label: "UIIT_STT_SILKMALL_DRESS" }
		],
		offers: offers( count ),
		silk: 700,
		earnedSilk: 0,
		earnedTradable: false,
		giftSilk: 20,
		points: 5,
		pending: false,
		revision: 1
	};
}

/*
================
view

The ops for one mall state, at the screen origin, the text table
holding the native strings that win over the later ones.
================
*/
export function view( mall, options = {} ) {
	const data = defined( decodeRenewalMall( catalogueFile() ) );
	const browse = mall.read( options.state ?? state() );
	return renewalMallView( {
		data,
		browse,
		balance: {
			silk: 700,
			earnedSilk: 25,
			giftSilk: 20,
			points: 5,
			earnedTradable: options.earnedTradable ?? false
		},
		character: { name: "Hero", level: 42 },
		origin: [ 0, 0 ],
		strings: {
			UIIT_STT_BUY: "Buy",
			UIIT_STT_SILKMALL_AVATA: "Avatar",
			UIIT_STT_SILKMALL_BOOTH: "Booth",
			UIIT_STT_SILKMALL_DRESS: "Dress",
			// The client's table carries these mall captions; it has no
			// UIIT_STT_SILKMALL_SHOPPING_BASKET (the blank reserve button).
			UIIT_STT_SILKMALL_ZZIM: "Reserve",
			UIIT_STT_SILKMALL_DEL: "Delete",
			UIIT_STT_SILKMALL_PUT_ON: "Wear Item",
			...options.strings
		},
		busy: options.busy ?? false,
		wearable: options.wearable ?? browse.offers.map( () => true ),
		searchFocused: options.searchFocused ?? false
	} );
}

/*
================
renewalMall

A mall owner open under the renewal window at its ten-card page.
================
*/
export function renewalMall() {
	const mall = createItemMall();
	mall.open();
	mall.setRenewal( true );
	mall.setPageSize( RENEWAL_GOODS_PER_PAGE );
	return mall;
}
