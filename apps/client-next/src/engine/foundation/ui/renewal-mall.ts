/*
===========================================================================

renewal-mall.ts - the later client's renewal Item Mall: data and geometry

Not in v1.150. The pinned VSRO client ships a second mall window,
CIFNewItemMall, beside the classic CIFItemMall; it is the desktop Item
Mall whenever its import is published (owner 2026-11-20).
scripts/import_vsro_renewal_mall.py publishes its layouts
(resinfo/ifnewitemmall*.txt) as assets/data/renewal-mall.json and its
art under Media_extracted/later/; the catalogue is absent when the later
client was never imported, and the classic mall stands then.

The layout files place the window's own controls (frames, backgrounds,
search, main page, the character panel, its buttons, my info) and the
inside of one goods card. They do not place the repeated children: the
category rail entries, the sub-category tabs and the goods cards are
created in code, and no v1.188 client binary is at hand to read it.
INFERENCE for those three: each repeats inside its authored parent on
its picture's own size with a 4 or 8 pixel gap, which is what the art
fits (the rail's 116x32 entries in the 124-wide left frame, three 96x24
tabs and a 2x5 grid of 200x84 cards in the 424x519 middle background).

===========================================================================
*/

import type { UiRect } from "@/engine/contracts/ui";

export const RENEWAL_MALL_CATALOG = "/assets/data/renewal-mall.json";
export const RENEWAL_MALL_FORMAT = "sro-renewal-mall-later";

// The window: GDR_RENEWAL_ITEM_MALL_SUB_FRAME spans 11..790 by 39..589,
// so the outer frame is the 800x600 the authored children fill.
export const RENEWAL_MALL_WIDTH = 800;
export const RENEWAL_MALL_HEIGHT = 600;
// Height of the outer frame's title band (mall_wnd_left_up is 40x68; the
// authored children start at y 39).
export const RENEWAL_MALL_TITLE_HEIGHT = 36;

// The goods grid: two columns of five 200x84 cards (mall_item_wnd).
export const RENEWAL_GOODS_COLUMNS = 2;
export const RENEWAL_GOODS_ROWS = 5;
// RENEWAL_GOODS_COLUMNS * RENEWAL_GOODS_ROWS.
export const RENEWAL_GOODS_PER_PAGE = 10;
const GOODS_LEFT = 145;
const GOODS_TOP = 108;
const GOODS_PITCH_X = 208;
const GOODS_PITCH_Y = 88;
const GOODS_WIDTH = 200;
const GOODS_HEIGHT = 84;

// The category rail: 116x32 entries (mall_item_tab) under the main page
// button, the main slot's 20x20 icon and 77x16 title inside each.
const RAIL_LEFT = 19;
const RAIL_TOP = 80;
const RAIL_PITCH = 36;
const RAIL_WIDTH = 116;
const RAIL_HEIGHT = 32;

// The sub-category tabs: 96x24 (mall_sub_tab), over the goods grid.
const SUB_LEFT = 145;
const SUB_TOP = 78;
const SUB_PITCH = 100;
const SUB_WIDTH = 96;
const SUB_HEIGHT = 24;
export const RENEWAL_SUB_TABS_MAX = 4;

// The pager under the grid (mall_page_prev / mall_page_next, 36x12).
const PAGER_TOP = 552;
const PAGER_PREV_LEFT = 300;
const PAGER_NEXT_LEFT = 422;
const PAGER_WIDTH = 36;
const PAGER_HEIGHT = 12;

// The popup a goods card opens (GDR_RENEWAL_ITEM_MALL_POPUP_MENU, 126x143,
// mall_sub_wnd04_ frame): three 104x32 buttons from 16 pixels in.
export const RENEWAL_POPUP_WIDTH = 126;
export const RENEWAL_POPUP_HEIGHT = 143;
const POPUP_BUTTON_LEFT = 11;
const POPUP_BUTTON_TOP = 16;
const POPUP_BUTTON_PITCH = 38;

export const RENEWAL_ART = "/assets/images/Media_extracted/later/interface/mall/";
export const RENEWAL_WINDOW_FRAME = "/assets/images/Media_extracted/later/interface/frame/mall_wnd_";

/*
================
RenewalMallControl

One authored control: its rect (texture-sized when the file leaves it
zero), the text inset, the published texture or frame prefix, the caption
key and its font, colour and alignment.
================
*/
export interface RenewalMallControl {
	readonly name: string;
	readonly id: number;
	readonly cls: string;
	readonly rect: UiRect;
	readonly inset: UiRect;
	readonly texture: string;
	readonly frame: boolean;
	readonly text: string;
	readonly fontIndex: number;
	readonly fontColor: readonly [number, number, number, number];
	readonly hAlign: number;
	readonly vAlign: number;
}

/*
================
RenewalMallSection

The controls of one published section, by authored name.
================
*/
export type RenewalMallSection = Readonly<Record<string, RenewalMallControl>>;

/*
================
RenewalMallData
================
*/
export interface RenewalMallData {
	readonly window: RenewalMallSection;
	readonly windowInfo: RenewalMallSection;
	readonly myInfo: RenewalMallSection;
	readonly itemSlot: RenewalMallSection;
	// The later client's English for caption keys v1.150 may lack; the
	// renderer prefers the native text when it has the key.
	readonly strings: Readonly<Record<string, string>>;
	readonly images: readonly string[];
}

const SECTION_KEYS = [ "window", "windowInfo", "myInfo", "itemSlot" ] as const;

// Every control the window draws by name (runtime/ui/hud/renewal-mall-view.ts).
// The decoder refuses a catalogue missing any of them, so the view never
// meets a gap while it draws.
const REQUIRED_CONTROLS = {
	window: [
		"GDR_RENEWAL_ITEM_MALL_POPUP_MENU",
		"GDR_RENEWAL_ITEM_MALL_MAIN_BTN",
		"GDR_RENEWAL_ITEM_MALL_SEARCH",
		"GDR_RENEWAL_ITEM_MALL_SEARCH_BTN",
		"GDR_RENEWAL_ITEM_MALL_SEARCH_BG",
		"GDR_RENEWAL_ITEM_MALL_RIGHT_SILKRECIEVELIST_BTN04",
		"GDR_RENEWAL_ITEM_MALL_RIGHT_SILKBUYLIST_BTN03",
		"GDR_RENEWAL_ITEM_MALL_RIGHT_BTN02",
		"GDR_RENEWAL_ITEM_MALL_RIGHT_BTN01",
		"GDR_RENEWAL_ITEM_MALL_RIGHT",
		"GDR_RENEWAL_ITEM_MALL_RIGHT_BG",
		"GDR_RENEWAL_ITEM_MALL_RIGHT_BG_WND",
		"GDR_RENEWAL_ITEM_MALL_LEFT_FRAME",
		"GDR_RENEWAL_ITEM_MALL_LEFT_BG",
		"GDR_RENEWAL_ITEM_MALL_MID_BG",
		"GDR_RENEWAL_ITEM_MALL_SUB_FRAME",
		"GDR_RENEWAL_ITEM_MALL_SUB_BG",
		"GDR_RENEWAL_ITEM_MALL_BG"
	],
	windowInfo: [ "GDR_ITEM_MALL_MYINFO" ],
	myInfo: [
		"GDR_ITEM_MALL_MYINFO_STA_POINT_STR",
		"GDR_ITEM_MALL_MYINFO_STA_GIFT_SILK_STR",
		"GDR_ITEM_MALL_MYINFO_STA_SILK_STR",
		"GDR_ITEM_MALL_MYINFO_STA_POINT_P",
		"GDR_ITEM_MALL_MYINFO_STA_GIFT_SILK",
		"GDR_ITEM_MALL_MYINFO_STA_SILK",
		"GDR_ITEM_MALL_MYINFO_POINT",
		"GDR_ITEM_MALL_MYINFO_GIFT_SILK",
		"GDR_ITEM_MALL_MYINFO_SILK",
		"GDR_ITEM_MALL_MYINFO_LEVEL",
		"GDR_ITEM_MALL_MYINFO_NAME",
		"GDR_ITEM_MALL_MYINFO_COUPON_EXCHANGE_BTN",
		"GDR_ITEM_MALL_MYINFO_BUY_SILK_BTN",
		"GDR_ITEM_MALL_MYINFO_BG"
	],
	itemSlot: [
		"GDR_RENEWAL_ITEM_MALL_ITEM_NAME",
		"GDR_RENEWAL_ITEM_MALL_ITEM_SILK",
		"GDR_RENEWAL_ITEM_MALL_ITEM_SILK_STR",
		"GDR_RENEWAL_ITEM_MALL_ITEM_COUT",
		"GDR_RENEWAL_SHOP_SLOT_ICON"
	]
} as const;

/*
================
decodeRenewalMall

The published catalogue, or null when the later client was never
imported (an absent file). A present file that does not decode is an
error: the importer and this decoder are one contract.
================
*/
export function decodeRenewalMall( raw: unknown ): RenewalMallData | null {
	if ( raw == null ) return null;
	const record = ( v: unknown ): v is Record<string, unknown> =>
		typeof v === "object" && v !== null && !Array.isArray( v );
	const numbers = ( v: unknown, count: number ) => {
		if (
			!Array.isArray( v ) || v.length !== count ||
			v.some( n => typeof n !== "number" || !Number.isFinite( n ) )
		) {
			throw Error( "Invalid renewal mall numbers" );
		}
		return v as number[];
	};
	if ( !record( raw ) || raw.format !== RENEWAL_MALL_FORMAT || !record( raw.sections ) ) {
		throw Error( "Invalid renewal mall catalogue" );
	}
	const sections: Record<string, RenewalMallSection> = {};
	for ( const key of SECTION_KEYS ) {
		const rows = raw.sections[key];
		if ( !Array.isArray( rows ) ) throw Error( "Missing renewal mall section " + key );
		const section: Record<string, RenewalMallControl> = {};
		for ( const row of rows ) {
			if (
				!record( row ) || typeof row.name !== "string" || typeof row.class !== "string" ||
				!Number.isInteger( row.id ) || typeof row.texture !== "string" || typeof row.text !== "string" ||
				typeof row.frame !== "boolean"
			) {
				throw Error( "Invalid renewal mall control" );
			}
			if ( row.texture && !row.texture.startsWith( "/assets/images/" ) ) {
				throw Error( "Foreign renewal mall texture" );
			}
			const color = numbers( row.fontColor, 4 );
			section[row.name] = {
				name: row.name,
				id: row.id as number,
				cls: row.class,
				rect: numbers( row.rect, 4 ) as unknown as UiRect,
				inset: numbers( row.inset, 4 ) as unknown as UiRect,
				texture: row.texture,
				frame: row.frame,
				text: row.text,
				fontIndex: Number.isInteger( row.fontIndex ) ? row.fontIndex as number : 0,
				fontColor: [ color[0]!, color[1]!, color[2]!, color[3]! ],
				hAlign: Number.isInteger( row.hAlign ) ? row.hAlign as number : 0,
				vAlign: Number.isInteger( row.vAlign ) ? row.vAlign as number : 0
			};
		}
		for ( const name of REQUIRED_CONTROLS[key] ) {
			if ( !section[name] ) throw Error( "Incomplete renewal mall catalogue: no " + name );
		}
		sections[key] = section;
	}
	const images = Array.isArray( raw.images ) ?
		raw.images.flatMap( image =>
			record( image ) && typeof image.path === "string" && image.path.startsWith( "/assets/images/" ) ?
				[ image.path ] :
				[]
		) :
		[];
	const strings: Record<string, string> = {};
	if ( record( raw.strings ) ) {
		for ( const [key, value] of Object.entries( raw.strings ) ) {
			if ( typeof value === "string" ) strings[key] = value;
		}
	}
	return {
		window: sections.window!,
		windowInfo: sections.windowInfo!,
		myInfo: sections.myInfo!,
		itemSlot: sections.itemSlot!,
		strings,
		images
	};
}

/*
================
optionalRenewalMall

The HUD's reading of the published catalogue. The renewal window is an
optional, port-only layer over the classic mall: a catalogue that does
not decode (a stale or partial import) leaves the classic window in
place and is reported once, never a failed game load.
================
*/
export function optionalRenewalMall( raw: unknown ): RenewalMallData | null {
	try {
		return decodeRenewalMall( raw );
	} catch ( error ) {
		console.warn( "renewal mall catalogue ignored; the classic Item Mall stands:", error );
		return null;
	}
}

/*
================
renewalMallControl

One authored control by name; a catalogue missing it is a broken
import, never a silent gap.
================
*/
export function renewalMallControl( section: RenewalMallSection, name: string ): RenewalMallControl {
	const control = section[name];
	if ( !control ) throw Error( "Missing renewal mall control " + name );
	return control;
}

/*
================
renewalMallOrigin

The window's top-left: centered on the screen, never above or left of it.
================
*/
export function renewalMallOrigin( width: number, height: number ): readonly [number, number] {
	return [
		Math.max( 0, Math.floor( (width - RENEWAL_MALL_WIDTH) / 2 ) ),
		Math.max( 0, Math.floor( (height - RENEWAL_MALL_HEIGHT) / 2 ) )
	];
}

/*
================
renewalGoodsRect

The window-relative rect of goods card index (0..9), row-major.
================
*/
export function renewalGoodsRect( index: number ): UiRect {
	const column = index % RENEWAL_GOODS_COLUMNS, row = Math.floor( index / RENEWAL_GOODS_COLUMNS );
	return [ GOODS_LEFT + column * GOODS_PITCH_X, GOODS_TOP + row * GOODS_PITCH_Y, GOODS_WIDTH, GOODS_HEIGHT ];
}

/*
================
renewalRailRect

The window-relative rect of category rail entry index.
================
*/
export function renewalRailRect( index: number ): UiRect {
	return [ RAIL_LEFT, RAIL_TOP + index * RAIL_PITCH, RAIL_WIDTH, RAIL_HEIGHT ];
}

/*
================
renewalSubTabRect

The window-relative rect of sub-category tab index.
================
*/
export function renewalSubTabRect( index: number ): UiRect {
	return [ SUB_LEFT + index * SUB_PITCH, SUB_TOP, SUB_WIDTH, SUB_HEIGHT ];
}

/*
================
renewalPagerRects

The previous and next page arrows, window-relative.
================
*/
export function renewalPagerRects(): { readonly prev: UiRect; readonly next: UiRect; readonly label: UiRect; } {
	return {
		prev: [ PAGER_PREV_LEFT, PAGER_TOP, PAGER_WIDTH, PAGER_HEIGHT ],
		next: [ PAGER_NEXT_LEFT, PAGER_TOP, PAGER_WIDTH, PAGER_HEIGHT ],
		label: [ PAGER_PREV_LEFT + PAGER_WIDTH, PAGER_TOP - 2, PAGER_NEXT_LEFT - PAGER_PREV_LEFT - PAGER_WIDTH, 16 ]
	};
}

/*
================
renewalPopupRect

The card popup beside goods card index: to the card's right on the left
column, to its left on the right column, so it stays inside the window.
================
*/
export function renewalPopupRect( index: number ): UiRect {
	const card = renewalGoodsRect( index );
	const left = index % RENEWAL_GOODS_COLUMNS === 0 ?
		card[0] + card[2] - 40 :
		card[0] - RENEWAL_POPUP_WIDTH + 40;
	const top = Math.min( card[1], RENEWAL_MALL_HEIGHT - RENEWAL_POPUP_HEIGHT - 12 );
	return [ left, top, RENEWAL_POPUP_WIDTH, RENEWAL_POPUP_HEIGHT ];
}

/*
================
renewalPopupButtonRect

Button index (0 buy, 1 preview, 2 basket) inside the popup.
================
*/
export function renewalPopupButtonRect( popup: UiRect, index: number ): UiRect {
	return [ popup[0] + POPUP_BUTTON_LEFT, popup[1] + POPUP_BUTTON_TOP + index * POPUP_BUTTON_PITCH, 104, 32 ];
}

/*
================
renewalGoodsCells

The goods grid's cells, in reading order, for one filtered offer list:
each cell holds an index into offers, or -1 for an empty cell. A
gendered pair shares one row, male on the left and female on the right,
in the order the pair first appears; a partner the list lacks leaves its
cell empty, so no later row shifts. Offers without a sex flow two to a
row, after the pair rows they appear among. INFERENCE: no v1.188 binary
is at hand; the later mall's avatar pages show each outfit's two
packages side by side, row by row.

An offer's name is its text key. The sex is the key's _M_ / _W_ segment
(SN_ITEM_MALL_AVATAR_M_DEVIL_WING_DRESS); a key without one falls back to
its English name's "(M)" / "(F)" ending, read through names, the client's
text table.
================
*/
export function renewalGoodsCells(
	offers: readonly { readonly name: string; }[],
	names: Readonly<Record<string, string>> = {}
): number[] {
	const keySex = /_(M|W)(?=_|$)/;
	const nameSex = /\s*\((M|F)\)\s*$/;
	/*
	================
	pairOf

	The pair key and side (0 male, 1 female) of one name key, or null.
	================
	*/
	const pairOf = ( name: string ): { key: string; side: number; } | null => {
		const coded = keySex.exec( name );
		if ( coded ) {
			const key = name.slice( 0, coded.index ) + "_*" + name.slice( coded.index + coded[0].length );
			return { key, side: coded[1] === "M" ? 0 : 1 };
		}
		const text = names[name] ?? name;
		const written = nameSex.exec( text );
		if ( !written ) return null;
		return { key: text.slice( 0, written.index ).trim().toLowerCase(), side: written[1] === "M" ? 0 : 1 };
	};
	const cells: number[] = [];
	// Every row each pair key opened, oldest first: a partner fills the
	// earliest row still missing its sex.
	const pairRows = new Map<string, number[]>();
	let open = -1;
	for ( let index = 0; index < offers.length; index++ ) {
		const pair = pairOf( offers[index]!.name );
		if ( !pair ) {
			// An ungendered offer takes the open half of the last plain row,
			// or starts a new row.
			if ( open >= 0 ) {
				cells[open] = index;
				open = -1;
			} else {
				cells.push( index, -1 );
				open = cells.length - 1;
			}
			continue;
		}
		const { key, side } = pair;
		const rows = pairRows.get( key ) ?? [];
		const row = rows.find( start => cells[start + side] === -1 );
		if ( row !== undefined ) {
			cells[row + side] = index;
			continue;
		}
		// A new pair row; a plain row left half open stays as it is.
		open = -1;
		rows.push( cells.length );
		pairRows.set( key, rows );
		cells.push( -1, -1 );
		cells[cells.length - 2 + side] = index;
	}
	return cells;
}

/*
================
renewalBadgeArt

The goods badge picture for a catalogue badge (mall_icon_*, 12x12).
================
*/
export function renewalBadgeArt(
	badge: import("@/engine/contracts/item-mall").MallBadge | undefined
): string | undefined {
	switch ( badge ) {
		case "new":
			return RENEWAL_ART + "mall_icon_new.png";
		case "hot":
			return RENEWAL_ART + "mall_icon_popular.png";
		case "recommended":
			return RENEWAL_ART + "mall_icon_recommendation .png";
		default:
			return undefined;
	}
}

/*
================
renewalRailIcon

A rail category's 20x20 icon (the same icon names the classic rail uses).
================
*/
export function renewalRailIcon( icon: string ): string {
	return RENEWAL_ART + "mall_" + icon + "_icon.png";
}
