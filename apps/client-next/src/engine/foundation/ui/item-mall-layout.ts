/*
===========================================================================

item-mall-layout.ts - native Item Mall page composition

Authored resources own geometry, textures and fonts. This module selects the
sections created by each native owner and describes the programmatic menu.

===========================================================================
*/

import type { AuthoredControl, AuthoredLayout } from "./authored-layout";

export const MALL_MENU_COUNT = 8;
export const MALL_ROWS_PER_PAGE = 6;
/*
================
compactMallBagLayout

Port-only secondary bag: retain native 32px cells and paginate the available
rows instead of shrinking the glyphs or placing slots below a short viewport.
================
*/
export function compactMallBagLayout( options: {
	width: number;
	height: number;
	total: number;
	equipment: number;
	requestedPage: number;
} ) {
	const { width, height, total, equipment, requestedPage } = options;
	const margin = 16, pitch = 36, top = 128, footer = 56;
	const columns = Math.max( 1, Math.min( 8, Math.floor( (width - margin * 2) / pitch ) ) );
	const rows = Math.max( 1, Math.floor( (height - top - footer) / pitch ) );
	const pageSize = Math.min( 32, columns * rows );
	const capacity = Math.max( 0, total - equipment );
	const pages = Math.max( 1, Math.ceil( capacity / pageSize ) );
	const page = Math.max( 0, Math.min( pages - 1, requestedPage ) );
	const left = (width - columns * pitch) / 2;
	return {
		page,
		pages,
		columns,
		rows: Math.ceil( pageSize / columns ),
		left,
		top,
		slots: Array.from( { length: pageSize }, ( _, index ) => ({
			slot: equipment + page * pageSize + index,
			enabled: page * pageSize + index < capacity,
			rect: [ left + index % columns * pitch, top + Math.floor( index / columns ) * pitch, 32, 32 ] as const
		}) )
	};
}
const MENU_X = 12;
const MENU_Y = 84;
const MENU_STEP = 30;
const MENU_WIDTH = 116;
const MENU_HEIGHT = 28;
const ART = "/assets/images/Media_extracted/interface/mall/";

// The two rails are fixed readonly tables, one copy each, so the frame
// that reads a rail allocates nothing for it.
const MALL_CATEGORIES = [
	{ key: "hot", text: "HOT_STORE", icon: "hot" },
	{ key: "MALL_CONSUME", text: "CONSUMPTION", icon: "consum" },
	{ key: "MALL_AVATAR", text: "AVATA", icon: "avatar" },
	{ key: "MALL_PET", text: "PET", icon: "pet" },
	{ key: "package", text: "PACKAGE", icon: "bundle" },
	{ key: "MALL_PREMIUM", text: "PREMIUM", icon: "premium" },
	{ key: "MALL_ARCHEMY", text: "ARCHEMY", icon: "alchemy" },
	{ key: "basket", text: "SHOPPING_BASKET_LIST", icon: "choicelist" }
] as const;

const MALL_CATEGORIES_LATER = [
	{ key: "MALL_CONSUME", text: "CONSUMPTION", icon: "consum" },
	{ key: "MALL_AVATAR", text: "AVATA", icon: "avatar" },
	{ key: "MALL_PET", text: "PET", icon: "pet" },
	{ key: "MALL_PREMIUM", text: "PREMIUM", icon: "premium" },
	{ key: "MALL_ARCHEMY", text: "ARCHEMY", icon: "alchemy" },
	{ key: "MALL_HOT", text: "HOT_STORE", icon: "hot" },
	{ key: "basket", text: "SHOPPING_BASKET_LIST", icon: "choicelist" }
] as const;

/*
================
mallCategories
================
*/
export function mallCategories() {
	return MALL_CATEGORIES;
}

/*
================
mallCategoriesLater

The later client's rail (port-only, issue #3257): Expendables, Avatar,
Pet, Premium, Alchemy and the HOT shop, then the basket. Only the
Experimental mall categories row reads it, and only a server running
SRO_MALL_CATEGORIES lists the HOT shop's tabs; the native rail above is
untouched.
================
*/
export function mallCategoriesLater() {
	return MALL_CATEGORIES_LATER;
}

/*
================
MallCategoryList

One rail entry list, native or later: mallCategories() or
mallCategoriesLater().
================
*/
export type MallCategoryList = readonly { key: string; text: string; icon: string; }[];

/*
================
mallMenuControl

CIFItemMall_OnCreate (6BBFA0) creates eight 116x28 tabs at a 30-pixel pitch.
The eighth tab uses the second texture family. 6BC2A5 and 6BC33E establish
the text insets separately from the icon; 6BC466 selects centered text.
categories is the rail list in force (mallCategoriesLater under the
Experimental row); the native list is the default, so the native rail
draws exactly as before. The later rail has seven entries, so the
basket takes the second texture family as the eighth did.
================
*/
export function mallMenuControl(
	template: AuthoredControl,
	index: number,
	selected: boolean,
	disabled = false,
	categories: MallCategoryList = mallCategories()
): AuthoredControl {
	if ( !Number.isInteger( index ) || index < 0 || index >= categories.length ) {
		throw Error( "Invalid Item Mall category" );
	}
	return {
		...template,
		name: "item-mall-category:" + index,
		id: index,
		client: [ 33, 8, 19, 0 ],
		hAlign: 1,
		vAlign: 0,
		text: "UIIT_STT_SILKMALL_" + categories[index]!.text,
		rect: [ MENU_X, MENU_Y + index * MENU_STEP, MENU_WIDTH, MENU_HEIGHT ],
		texture: disabled ?
			ART + "mall_tab_disable.png" :
			ART + (index === categories.length - 1 ? "mall_tab2_" : "mall_tab_") +
			(selected ? "on" : "off") + ".png"
	};
}

/*
================
mallDescription

Native 6C17D0 / 6C2390 / 6C29D0 / 6C6350 / 6C6840 switch on
wire tab indices. Translations contain PML and use the common guide renderer.
================
*/
export function mallDescription( category: string, tab: number ): string {
	const prefix = "UIIT_STT_SILKMALL_MAIN_CONSUMPTION_SUBTITLE_";
	switch ( category ) {
		case "MALL_CONSUME":
			return tab >= 0 && tab < 5 ? prefix + [ "SPECIAL", "ORDER_SHEET", "POTION", "COMMUNITY", "TOOL" ][tab] : "";
		case "MALL_AVATAR":
			return tab >= 0 && tab < 4 ? prefix + [ "FLEAMARKET", "DRESS", "HAT", "ATTACH" ][tab] : "";
		case "MALL_PET":
			return tab === 0 ? prefix + "COSETC" : "UIIT_STT_SILKMALL_PET_CONSUME";
		case "MALL_PREMIUM":
			return "UIIT_STT_SILKMALL_MAIN_PREMIUM";
		case "MALL_ARCHEMY":
			// Tab 2 (Others) exists only with the later categories;
			// natively the shop has tabs 0 and 1 alone.
			return tab === 0 ?
				"UIIT_STT_SILKMALL_MAIN_SUB_ARCHEMY_ASTRAL" :
				tab === 1 ?
				"UIIT_STT_SILKMALL_MAIN_SUB_ARCHEMY_ATHANASIA" :
				"";
		default:
			return "";
	}
}

/*
================
mallTabCaptionLater

The later tabs' captions (port-only, issue #3257): the v2.355 mall
names its sub-categories, and v1.150 carries no string for them, so the
captions are the port's literals. category and position are the tab's
shop codename and its index within it; a tab the later mall does not
rename answers undefined and the renderer falls back to the server's
label.
================
*/
export function mallTabCaptionLater( category: string, position: number ): string | undefined {
	if ( category === "MALL_HOT" ) {
		return [ "Halloween Exclusive", "Halloween Models", "Recommended" ][position];
	}
	if ( category === "MALL_PREMIUM" ) {
		return [ undefined, "Gears", "Others" ][position];
	}
	if ( category === "MALL_ARCHEMY" ) {
		return [ undefined, undefined, "Others" ][position];
	}
	return undefined;
}

/*
================
mallBadgeMark

The N/H/R mark on the goods (port-only, issue #3257): the letter and
the chip colour the offer rows draw at the icon's top-right corner,
for a badge the catalogue stamps. Colours are the port's own; the
later client draws its authored art, which v1.150 does not ship.
================
*/
export function mallBadgeMark(
	badge: import("@/engine/contracts/item-mall").MallBadge | undefined
): { letter: string; color: readonly [number, number, number, number]; } | undefined {
	switch ( badge ) {
		case "new":
			return { letter: "N", color: [ 0.16, 0.62, 0.28, 1 ] };
		case "hot":
			return { letter: "H", color: [ 0.85, 0.3, 0.15, 1 ] };
		case "recommended":
			return { letter: "R", color: [ 0.85, 0.65, 0.1, 1 ] };
		default:
			return undefined;
	}
}

/*
================
mallMenuArtwork
================
*/
export function mallMenuArtwork(): readonly string[] {
	return [
		...mallCategories().flatMap( category => [
			ART + "mall_" + category.icon + "_icon.png",
			...(category.key === "basket" ? [] : [ ART + "mall_" + category.icon + "_icon_disable.png" ])
		] ),
		...[ "mall_tab_", "mall_tab2_" ].flatMap( family =>
			[ "on", "off" ].map( state => ART + family + state + ".png" )
		),
		ART + "mall_tab_disable.png"
	];
}

/*
================
mallControl

Control IDs are local to their native owner. Never search across layouts,
where the same number can refer to an unrelated widget.
================
*/
export function mallControl( layout: AuthoredLayout, id: number ): AuthoredControl {
	const node = Object.values( layout ).find( control => control.id === id );
	if ( !node ) throw Error( "Missing native Item Mall control " + id );
	return node;
}

/*
================
mallTabControl

6C7760..6C77A8 creates 60x24 tabs at (18,6), advancing x by 62.
================
*/
export function mallTabControl( template: AuthoredControl, index: number, selected: boolean ): AuthoredControl {
	const TAB_X = 18, TAB_Y = 6, TAB_WIDTH = 60, TAB_HEIGHT = 24, TAB_STEP = 62;
	return {
		...template,
		name: "item-mall-tab:" + index,
		id: 201 + index,
		rect: [ TAB_X + index * TAB_STEP, TAB_Y, TAB_WIDTH, TAB_HEIGHT ],
		client: [ 4, 7, 6, 0 ],
		hAlign: 1,
		vAlign: 0,
		texture: "/assets/images/Media_extracted/interface/ifcommon/com_tab_" + (selected ? "on" : "off") + ".png"
	};
}

/*
================
mallCategoryIcon
================
*/
export function mallCategoryIcon(
	template: AuthoredControl,
	index: number,
	disabled = false,
	categories: MallCategoryList = mallCategories()
): AuthoredControl {
	const ICON_X = 6, ICON_Y = 5, ICON_SIZE = 20;
	return {
		...template,
		type: "CIFStatic",
		rect: [ template.rect[0] + ICON_X, template.rect[1] + ICON_Y, ICON_SIZE, ICON_SIZE ],
		texture: ART + "mall_" + categories[index]!.icon + "_icon" + (disabled ? "_disable" : "") + ".png"
	};
}

/*
================
mallPageLayout

6C8CDE configures groups of four pages at 20 pixels each. 5335D0 centers
numbers in the manager rectangle; decorations are four pixels wide and the
arrows sit thirteen pixels beyond their outside edges.
================
*/
export function mallPageLayout( rect: readonly [number, number, number, number], page: number, offers: number ) {
	const PAGE_GROUP = 4, PAGE_WIDTH = 20, TEXT_HEIGHT = 14, DECORATION_WIDTH = 4, ARROW_GAP = 13;
	const pages = Math.ceil( offers / MALL_ROWS_PER_PAGE ), first = Math.floor( page / PAGE_GROUP ) * PAGE_GROUP;
	const count = Math.min( PAGE_GROUP, pages - first );
	const x = rect[0] + Math.floor( rect[2] / 2 ) - Math.floor( count * PAGE_WIDTH / 2 );
	const y = rect[1] + Math.floor( rect[3] / 2 ) - TEXT_HEIGHT / 2;
	return {
		first,
		count,
		pages,
		x,
		y,
		width: PAGE_WIDTH,
		height: TEXT_HEIGHT,
		left: x - DECORATION_WIDTH - ARROW_GAP,
		right: x + count * PAGE_WIDTH + DECORATION_WIDTH + ARROW_GAP,
		previous: first - PAGE_GROUP,
		next: first + PAGE_GROUP
	};
}

/*
================
mallPageNumberPresentation

How one page number of the manager draws. The page manager is born with
three colours (its constructor defaults): white for a plain page, and
0xFFFF9951 for both the styled face and the current page. Drawing a page
(CIFPageManager_SetCurrentPage) gives the current page the third colour
and the styled font face, and every other page the white and the plain
face; the mall never recolours them.
================
*/
export function mallPageNumberPresentation( page: number, current: number ): {
	color: AuthoredControl["color"] | undefined;
	fontStyle: number;
} {
	return page === current ?
		{ color: [ 1, 0x99 / 255, 0x51 / 255, 1 ], fontStyle: 2 } :
		{ color: undefined, fontStyle: 0 };
}

/*
================
mallCurrencyRows

6BF320 starts y at 80, advances 22 before each mall row and grows the
window by 22 for Silk/points, but by 20 for Gift Silk (6BF625).
Zero-cost rows are still present when their authored currency bit is set.
================
*/
export function mallCurrencyRows(
	offer: import("@/engine/contracts/item-mall").MallOffer,
	quantity: number,
	points: number
) {
	const ROW_START = 80, ROW_STEP = 22, GIFT_GROWTH = 20, BASE_HEIGHT = 155;
	const rows: { y: number; amount: number; label: string; points: boolean; }[] = [];
	let y = ROW_START, height = BASE_HEIGHT;
	for ( const currency of [ 2, 4, 16 ] ) {
		if ( !(offer.currencyMask & currency) ) continue;
		y += ROW_STEP;
		height += currency === 4 ? GIFT_GROWTH : ROW_STEP;
		rows.push( {
			y,
			amount: currency === 2 ?
				offer.silk * quantity - points :
				currency === 4 ?
				offer.giftSilk * quantity :
				points,
			label: currency === 2 ?
				"UIIT_STT_SILKMALL_SILK" :
				currency === 4 ?
				"UIIT_CTL_SILK_INQUIRY_GIFT_SILK" :
				"UIIT_STT_SILKMALL_P_POINT",
			points: currency === 16
		} );
	}
	return { rows, height, growth: height - BASE_HEIGHT };
}

/*
================
mallQuestionLayout

528920 configures reservation and bulk confirmations. 52859D creates worn
name/cost pairs at y=100 with a 16-pixel pitch; the sentinel row anchors the
native total, points, buttons and final height. On the two boxes that draw
a total row (worn 529846, basket 529BE7) the total control (+0x89C) is
captioned UIIT_MSG_QUESTION_SILKMALL_ZZIM_LUMP_BUY_TOTAL.
================
*/
export function mallQuestionLayout( kind: "reserve" | "remove" | "worn" | "basket", count: number ) {
	const WIDTH = 300, ROW_START = 100, ROW_PITCH = 16;
	const totalY = ROW_START + count * ROW_PITCH;
	return {
		width: WIDTH,
		height: kind === "reserve" ? 203 : kind === "remove" ? 183 : kind === "basket" ? 150 : totalY + 100,
		buttonY: kind === "reserve" ? 167 : kind === "remove" ? 147 : kind === "basket" ? 116 : totalY + 50,
		buttonX: kind === "reserve" || kind === "remove" ? 72 : 70,
		caption: kind === "reserve" ? "UIIT_STT_SILKMALL_DO_ZZIM" : kind === "remove" ?
			"UIIT_STT_CONFIRM_BOX" :
			kind === "worn" ?
			"UIIT_STT_SILKMALL_PUT_ON_CLOSE_BUY" :
			"UIIT_STT_SILKMALL_ZZIM_LUMP_BUY",
		message: kind === "reserve" ? "UIIT_MSG_SILKMALL_ZZIM_CONFIRM" : kind === "remove" ?
			"UIIT_MSG_SILKMALL_ZZIM_DELITE" :
			kind === "worn" ?
			"UIIT_MSG_AVATAR_SILKMALL_LUMP_BUY" :
			"UIIT_MSG_QUESTION_SILKMALL_ZZIM_LUMP_BUY",
		confirm: kind === "reserve" ?
			"UIIT_STT_SILKMALL_ZZIM" :
			kind === "remove" ?
			"UIIT_STT_SILKMALL_DEL" :
			"UIIT_STT_BUY",
		messageRect: kind === "reserve" ? [ 19, 107, 263, 55 ] as const : kind === "remove" ?
			[ 19, 100, 263, 55 ] as const :
			[ 12, 49, 276, 14 ] as const,
		rowStart: ROW_START,
		rowPitch: ROW_PITCH,
		totalY: kind === "basket" ? 74 : totalY + 4,
		pointY: kind === "basket" ? 92 : totalY + 22,
		totalCaption: kind === "worn" || kind === "basket" ?
			"UIIT_MSG_QUESTION_SILKMALL_ZZIM_LUMP_BUY_TOTAL" :
			undefined
	};
}

/*
================
mallSilkPeriodTexts

The Buying list's period combo entries, in the order CIFSilkBuyList
OnCreate (0x6CED10) appends them: 1 day, 7 days, 1 month, 3 months. The
index is the inquiry's whole input (contracts/item-mall.ts).
================
*/
export const mallSilkPeriodTexts = [
	"UIIT_CTL_SILK_INQUIRY_ONEDAY",
	"UIIT_CTL_SILK_INQUIRY_WEEK",
	"UIIT_CTL_SILK_INQUIRY_MONTH",
	"UIIT_CTL_SILK_INQUIRY_THREE_MONTH"
] as const;

/*
================
mallSilkHistoryDate

One history row's date, "YYYY-MM-DD HH:MM" in local time. The native
window never renders a row (its inquiry is an empty stub, 0x6CE6F0), so
the column format is the port's recorded inference (#2269).
================
*/
export function mallSilkHistoryDate( atSeconds: number ): string {
	const date = new Date( atSeconds * 1000 );
	const pad = ( value: number ) => String( value ).padStart( 2, "0" );
	return `${date.getFullYear()}-${pad( date.getMonth() + 1 )}-${pad( date.getDate() )} ` +
		`${pad( date.getHours() )}:${pad( date.getMinutes() )}`;
}
