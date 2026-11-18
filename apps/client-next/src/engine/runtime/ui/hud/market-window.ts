/*
===========================================================================

market-window.ts - the consignment market window, drawn

Port-only, not native (SRO_CONSIGNMENT, published at world entry): v1.150
has no consignment, so the window has no authored layout. It is built
from the client's own chrome - the framed window, the native buttons,
item slots with their tooltips, the gold captions - so it reads as part
of the game. One page, two columns:

	left   the category chips; the search line with the degree, currency
	       and order steps; fifteen listings (item, degree, quantity,
	       price, seller, time left) each with its own Buy, PM and Offer;
	       the listing count and the pages
	right  List an item (the picked item, Gold or Silk, the price, the
	       fee, anonymous); below it My listings, History and Offers, or
	       the bag while the seller picks an item; the market's rules

A buy or an offer opens a box over the window that asks where the item
goes. Every rule and number shown is the server's (foundation/gameplay
market.ts); this file only lays them out (market-hud.ts holds the drafts).

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { MarketDestination, MarketOffer, MarketRow, MarketState } from "@/engine/contracts/market";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import {
	marketAmount,
	marketCurrencyText,
	marketFee,
	marketTimeLeft,
	MARKET_MAX_DEGREE,
	MARKET_PRICE_DIGITS
} from "@/engine/foundation/gameplay/market";
import type { UiAuthoredFrame } from "../frame/frame-contract";
import {
	MARKET_BAG_COLUMNS,
	MARKET_BAG_FIRST_SLOT,
	MARKET_BAG_ROWS,
	MARKET_HISTORY_ROWS,
	MARKET_MINE_ROWS,
	MARKET_OFFER_ROWS,
	MARKET_SORT_ORDER,
	marketBusy,
	marketOfferAt,
	marketOfferPages,
	marketOwnRow,
	marketActiveListings,
	marketListingsFull,
	marketPriceValue,
	marketSearchNarrowed,
	marketShownListings,
	marketWriting,
	type MarketMenu,
	type MarketHudState
} from "./market-hud";

// The window's size and its inner margin.
const WIDTH = 1000;
const HEIGHT = 600;
const MARGIN = 14;
// The left column: the listings.
const LEFT_WIDTH = 680;
// The search line: the name field, the dropdowns and the Search button.
const QUERY_WIDTH = 130;
const SEARCH_WIDTH = 56;
const MENU_GAP = 4;
const MENU_ROW = 20;
// The open list's lit rows: the current choice (the option window's) and
// the one under the pointer.
const MENU_CURRENT: UiQuad["color"] = [ .2, .25, .3, 1 ];
const MENU_HOVER: UiQuad["color"] = [ .12, .14, .17, 1 ];
// The native interface art the window draws, and its sizes.
const IFCOMMON = "/assets/images/Media_extracted/interface/ifcommon/";
// The body's tile: the NPC talk window's on the same mframe (com_bg_tile_d),
// dark enough to meet the frame's inner edge art without a seam.
const WINDOW_TILE = IFCOMMON + "bg_tile/com_bg_tile_d.png";
const ARROW_SIZE = 16;
const ARROW_INSET = 6;
const CHECK_SIZE = 16;
// The Price header's sort mark: two triangles of four pixel rows in a
// 10-pixel box, set this far right of the caption.
const SORT_TRIANGLE_ROWS = 4;
const SORT_MARK_HEIGHT = 10;
const SORT_MARK_GAP = 6;
const CHIP_TOP = 38;
const FILTER_TOP = 68;
const TABLE_TOP = 100;
const HEADER_HEIGHT = 22;
const ROW_PITCH = 28;
// The table runs to the window's bottom margin, level with the rules
// panel; its last band is the footer bar (count, status, pages).
const TABLE_HEIGHT = HEIGHT - TABLE_TOP - 12;
const ROW_ICON = 22;
const BROWSE_ROWS = 15;
const FOOTER_HEIGHT = TABLE_HEIGHT - HEADER_HEIGHT - BROWSE_ROWS * ROW_PITCH - 2;
// The right column: List an item, the account's pages, the rules.
const RIGHT_LEFT = MARGIN + LEFT_WIDTH + MARGIN;
const RIGHT_WIDTH = WIDTH - RIGHT_LEFT - MARGIN;
const SELL_TOP = 38;
const SELL_HEIGHT = 162;
const PAGES_TOP = 208;
const RULES_HEIGHT = 74;
const SIDE_PITCH = 24;
const SIDE_ICON = 20;
const BAG_CELL = 34;
const BAG_ICON = 30;
// The native button's height, for rows that hold one.
const BUTTON_HEIGHT = 25;
// The empty result's card, centred in the table's rows.
const EMPTY_CARD_WIDTH = 380;
const EMPTY_CARD_HEIGHT = 116;
const CLEAR_SEARCH_WIDTH = 110;
// The buy and offer box.
const DIALOG_WIDTH = 330;
const DIALOG_HEIGHT = 170;

const GOLD: UiQuad["color"] = [ 252 / 255, 210 / 255, 100 / 255, 1 ];
const DIM: UiQuad["color"] = [ 170 / 255, 170 / 255, 160 / 255, 1 ];
const OK: UiQuad["color"] = [ 150 / 255, 230 / 255, 130 / 255, 1 ];
const REFUSED: UiQuad["color"] = [ 255 / 255, 130 / 255, 110 / 255, 1 ];
const SILK: UiQuad["color"] = [ 130 / 255, 200 / 255, 255 / 255, 1 ];
const PLUSSED: UiQuad["color"] = [ 140 / 255, 180 / 255, 255 / 255, 1 ];
// A sealed (rare) item's name, as the bag's help colours one.
const SEALED: UiQuad["color"] = [ 255 / 255, 200 / 255, 60 / 255, 1 ];

/*
================
Column

One listing column, offset from the table's left edge.
================
*/
interface Column {
	readonly x: number;
	readonly width: number;
	readonly align: 0 | 1 | 2;
}
const COLUMN = {
	item: { x: 8, width: 164, align: 0 },
	degree: { x: 172, width: 46, align: 1 },
	qty: { x: 218, width: 26, align: 1 },
	price: { x: 246, width: 106, align: 0 },
	seller: { x: 354, width: 80, align: 0 },
	left: { x: 436, width: 56, align: 0 }
} as const satisfies Record<string, Column>;
// Where each row's Buy, Send PM and Make offer start.
const ACTIONS_LEFT = 496;
const PANEL: UiQuad["color"] = [ .02, .03, .025, .82 ];
const HEADER: UiQuad["color"] = [ .09, .075, .04, .9 ];
const EDGE: UiQuad["color"] = [ .55, .45, .24, .85 ];
const LINE: UiQuad["color"] = [ .55, .45, .24, .3 ];
const STRIPE: UiQuad["color"] = [ 1, 1, 1, .03 ];
const PICKED: UiQuad["color"] = [ 252 / 255, 210 / 255, 100 / 255, .14 ];

const SORT_CAPTIONS: Readonly<Record<(typeof MARKET_SORT_ORDER)[number], string>> = {
	"newest": "Newest",
	"ending": "Ending soon",
	"price-asc": "Price: low",
	"price-desc": "Price: high"
};
const STATE_CAPTIONS: Readonly<Record<MarketRow["state"], string>> = {
	open: "On sale",
	sold: "Sold",
	returned: "Returned",
	held: "Take back"
};
const OFFER_CAPTIONS: Readonly<Record<MarketOffer["state"], string>> = {
	open: "Waiting",
	accepted: "Accepted",
	rejected: "Declined",
	withdrawn: "Withdrawn",
	closed: "Closed"
};

/*
================
MarketPaint

The frame's primitives, handed in by ui.ts, their owner: the window,
the native buttons and close box, a plain rect, and an item slot with its
tooltip control (bound to ui.ts's slot context).
================
*/
type Mark = readonly [number, number, number, number];
export interface MarketPaint {
	beginWindow( frame: UiAuthoredFrame ): Mark;
	endWindow( frame: UiAuthoredFrame, mark: Mark, owner: string ): void;
	windowOrigin(
		frame: UiAuthoredFrame,
		spec: { name: string; initial: [number, number, number, number]; id: string; }
	): UiRect;
	windowBox(
		frame: UiAuthoredFrame,
		spec: { title: string; x: number; y: number; width: number; height: number; tile?: string; }
	): void;
	closeButton( frame: UiAuthoredFrame, x: number, y: number, id: "market-close" ): void;
	button(
		frame: UiAuthoredFrame,
		spec: {
			id: string;
			value: string;
			x: number;
			y: number;
			width: number;
			disabled?: boolean;
			selected?: boolean;
		}
	): void;
	rect( frame: UiAuthoredFrame, spec: { r: UiRect; color: UiQuad["color"]; } ): void;
	// A native interface image (ifcommon art) at its own size.
	image( frame: UiAuthoredFrame, spec: { r: UiRect; path: string; } ): void;
	// The option window's combo box: the black field and the native drop
	// arrow (com_qst_downarrow_button), with its focus and press art.
	comboBox(
		frame: UiAuthoredFrame,
		spec: { r: UiRect; id: string; label: string; value: string; }
	): void;
	nativeItem(
		frame: UiAuthoredFrame,
		spec: { id: string; item: InventoryItem | undefined; r: UiRect; selected?: boolean; }
	): void;
}

/*
================
MarketWindowView

What one frame of the window reads: the HUD's drafts, the worker's
market and bag, the character, the clock and the caret.
================
*/
export interface MarketWindowView {
	readonly hud: MarketHudState;
	readonly market: MarketState | undefined;
	readonly bag: readonly InventoryItem[];
	readonly gold: number;
	readonly localName: string;
	readonly now: number;
	readonly focus: string | null;
	readonly caretVisible: boolean;
}

/*
================
drawMarketWindow
================
*/
export function drawMarketWindow( frame: UiAuthoredFrame, frameDraw: MarketPaint, view: MarketWindowView ) {
	const admission = frameDraw.beginWindow( frame );
	const [px, py] = frameDraw.windowOrigin( frame, {
		name: "ConsignmentMarket",
		initial: [ Math.max( 0, (frame.w - WIDTH) / 2 ), Math.max( 0, (frame.h - HEIGHT) / 3 ), WIDTH, HEIGHT ],
		id: "window-drag:ConsignmentMarket"
	} );
	frameDraw.windowBox( frame, {
		title: "Consignment Market",
		x: px,
		y: py,
		width: WIDTH,
		height: HEIGHT,
		tile: WINDOW_TILE
	} );
	frameDraw.closeButton( frame, px + WIDTH - 26, py + 10, "market-close" );
	const pen = penOf( frame, frameDraw, view, px, py );
	pen.text(
		`Gold: ${marketAmount( view.gold )}  ·  Silk: ${marketAmount( view.market?.mine?.silk ?? 0 )}`,
		[ px + WIDTH - 340, py + 8, 300, 22 ],
		GOLD,
		2
	);
	// The column rule between the listings and the seller's side.
	frameDraw.rect( frame, {
		r: [ px + RIGHT_LEFT - MARGIN / 2, py + CHIP_TOP, 1, HEIGHT - CHIP_TOP - 12 ],
		color: LINE
	} );
	browseColumn( pen );
	sellPanel( pen );
	pagesPanel( pen );
	rulesPanel( pen );
	if ( view.hud.menu ) filterMenu( pen );
	if ( view.hud.dialog ) dialogBox( pen );
	frameDraw.endWindow( frame, admission, "market" );
}

// ============================================================================

/*
================
Pen

The window's painters, bound to its origin.
================
*/
type Pen = ReturnType<typeof penOf>;

/*
================
penOf
================
*/
function penOf( frame: UiAuthoredFrame, frameDraw: MarketPaint, view: MarketWindowView, px: number, py: number ) {
	const busy = marketBusy( view.market ), writing = marketWriting( view.market );
	return {
		frame,
		frameDraw,
		view,
		px,
		py,
		busy,
		writing,
		/*
		================
		text
		================
		*/
		text( value: string, r: UiRect, color: UiQuad["color"] = frame.white, hAlign: 0 | 1 | 2 = 0 ) {
			frame.quads.push( ...frame.text.quads( value, r, r, color, { hAlign, vAlign: 1, overflow: "ellipsis" } ) );
		},
		/*
		================
		panel

		A dark inset with the window's gilt edge.
		================
		*/
		panel( r: UiRect, edge: UiQuad["color"] = EDGE ) {
			frameDraw.rect( frame, { r, color: PANEL } );
			for (
				const line of [
					[ r[0], r[1], r[2], 1 ],
					[ r[0], r[1] + r[3] - 1, r[2], 1 ],
					[ r[0], r[1], 1, r[3] ],
					[ r[0] + r[2] - 1, r[1], 1, r[3] ]
				] as UiRect[]
			) frameDraw.rect( frame, { r: line, color: edge } );
		},
		/*
		================
		button
		================
		*/
		button( id: string, value: string, x: number, y: number, width: number, disabled = false, selected = false ) {
			frameDraw.button( frame, { id, value, x, y, width, disabled, selected } );
		},
		/*
		================
		item

		An item slot: the icon, its count and its tooltip control.
		================
		*/
		item( id: string, item: InventoryItem | undefined, r: UiRect, selected = false ) {
			frameDraw.rect( frame, { r, color: [ 0, 0, 0, .55 ] } );
			frameDraw.nativeItem( frame, { id, item, r, selected } );
		},
		/*
		================
		field

		A typed field: its frame, the text, a placeholder while empty, and
		the caret while focused.
		================
		*/
		field( id: string, value: string, r: UiRect, placeholder: string, maxLength: number ) {
			this.panel( r, view.focus === id ? GOLD : EDGE );
			const inner: UiRect = [ r[0] + 6, r[1], r[2] - 12, r[3] ];
			frame.controls.push( { id, label: placeholder, kind: "text", value, rect: r, maxLength } );
			if ( value ) this.text( value, inner );
			else if ( view.focus !== id ) this.text( placeholder, inner, DIM );
			if ( view.focus === id && view.caretVisible ) {
				const x = inner[0] + Math.min( inner[2], frame.text.run( value ).width );
				frameDraw.rect( frame, { r: [ x, r[1] + 5, 1, r[3] - 10 ], color: frame.white } );
			}
		},
		/*
		================
		row

		A list row's stripe, rule and pick control; picked rows are gilded.
		================
		*/
		row( id: string, r: UiRect, index: number, picked: boolean, label: string ) {
			if ( picked ) frameDraw.rect( frame, { r, color: PICKED } );
			else if ( index % 2 ) frameDraw.rect( frame, { r, color: STRIPE } );
			frameDraw.rect( frame, { r: [ r[0], r[1] + r[3] - 1, r[2], 1 ], color: LINE } );
			frame.controls.push( { id, label, rect: r, kind: "button", selected: picked } );
		}
	};
}

/*
================
priceText
================
*/
function priceText( price: number, currency: MarketRow["currency"] ): string {
	return `${marketAmount( price )} ${marketCurrencyText( currency )}`;
}

/*
================
priceColor
================
*/
function priceColor( currency: MarketRow["currency"] ): UiQuad["color"] {
	return currency === "silk" ? SILK : GOLD;
}

/*
================
itemCaption

An item's name with its plus, as the bag's help names it.
================
*/
function itemCaption( item: InventoryItem ): string {
	return (item.name ?? "") + (item.plus > 0 ? ` (+${item.plus})` : "");
}

/*
================
itemColor

A plussed item reads blue, as the site's list and the bag's help do.
================
*/
function itemColor( pen: Pen, item: InventoryItem ): UiQuad["color"] {
	return item.plus > 0 ? PLUSSED : pen.frame.white;
}

/*
================
pager

The Previous / n of m / Next pair every list shares. The arrows are the
game's own (com_left_arrow, com_right_arrow, with their disabled art);
the compact pair is the arrows alone. A single page draws nothing.
================
*/
function pager( pen: Pen, prefix: string, page: number, pages: number, right: number, y: number, compact = false ) {
	// One page has nowhere to go: no dead buttons.
	if ( pages <= 1 ) return;
	const step = compact ? 30 : 74, middle = compact ? 46 : 56;
	const left = right - (2 * step + middle + 8);
	pagerButton( pen, prefix + "prev", compact ? "" : "Previous", left, y, step, page <= 0, "left" );
	pen.text(
		`${page + 1} / ${Math.max( 1, pages )}`,
		[ left + step + 4, y, middle, BUTTON_HEIGHT ],
		pen.frame.white,
		1
	);
	pagerButton(
		pen,
		prefix + "next",
		compact ? "" : "Next",
		left + step + middle + 8,
		y,
		step,
		page + 1 >= pages,
		"right"
	);
}

/*
================
pagerButton

A pager button with the native arrow on its outer side (centred when it
has no caption); the caption fills the rest.
================
*/
function pagerButton(
	pen: Pen,
	id: string,
	caption: string,
	x: number,
	y: number,
	width: number,
	disabled: boolean,
	side: "left" | "right"
) {
	pen.button( id, "", x, y, width, disabled );
	const arrowX = !caption ?
		x + (width - ARROW_SIZE) / 2 :
		side === "left" ?
		x + ARROW_INSET :
		x + width - ARROW_INSET - ARROW_SIZE;
	pen.frameDraw.image( pen.frame, {
		r: [ arrowX, y + (BUTTON_HEIGHT - ARROW_SIZE) / 2, ARROW_SIZE, ARROW_SIZE ],
		path: IFCOMMON + `com_${side}_arrow${disabled ? "_disable" : ""}.png`
	} );
	if ( !caption ) return;
	// The caption centres in what the arrow leaves.
	const room = width - ARROW_INSET - ARROW_SIZE;
	const textX = side === "left" ? x + ARROW_INSET + ARROW_SIZE : x;
	pen.text( caption, [ textX, y, room, BUTTON_HEIGHT ], disabled ? DIM : pen.frame.white, 1 );
}

// ============================================================================

/*
================
browseColumn

The left column: chips, the search line, the listings and their pages.
================
*/
function browseColumn( pen: Pen ) {
	const { px, py, view } = pen, hud = view.hud, browse = view.market?.browse;
	const left = px + MARGIN;
	// The category chips: All, then every category the server names.
	let x = left;
	const chips = [ { slug: "", name: "All" }, ...(view.market?.categories ?? []) ];
	for ( const chip of chips ) {
		const width = Math.max( 40, pen.frame.text.run( chip.name ).width + 22 );
		if ( x + width > left + LEFT_WIDTH ) break;
		pen.button(
			"market-category:" + chip.slug,
			chip.name,
			x,
			py + CHIP_TOP,
			width,
			false,
			hud.category === chip.slug
		);
		x += width + 4;
	}
	// The search line: the name, the five dropdowns (filterMenus) and Search.
	const y = py + FILTER_TOP;
	pen.field( "market-query", hud.query, [ left, y, QUERY_WIDTH, BUTTON_HEIGHT ], "Search items...", 64 );
	for ( const menu of filterMenus( hud ) ) {
		pen.frameDraw.comboBox( pen.frame, {
			r: [ left + menu.x, y, menu.width, BUTTON_HEIGHT ],
			id: "market-menu:" + menu.name,
			label: menu.label,
			value: menu.caption
		} );
	}
	pen.button( "market-search", "Search", left + LEFT_WIDTH - SEARCH_WIDTH, y, SEARCH_WIDTH );
	// The listings.
	const table: UiRect = [ left, py + TABLE_TOP, LEFT_WIDTH, TABLE_HEIGHT ];
	pen.panel( table );
	pen.frameDraw.rect( pen.frame, {
		r: [ table[0] + 1, table[1] + 1, table[2] - 2, HEADER_HEIGHT - 1 ],
		color: HEADER
	} );
	const headers: readonly (readonly [string, Column])[] = [
		[ "Item", COLUMN.item ],
		[ "Degree", COLUMN.degree ],
		[ "Qty", COLUMN.qty ],
		[ "Price", COLUMN.price ],
		[ "Seller", COLUMN.seller ],
		[ "Left", COLUMN.left ]
	];
	for ( const [caption, column] of headers ) {
		pen.text( caption, [ left + column.x, table[1], column.width, HEADER_HEIGHT ], GOLD, column.align );
	}
	// The Price header sorts: cheapest first, then dearest first.
	sortMark(
		pen,
		left + COLUMN.price.x + pen.frame.text.run( "Price" ).width + SORT_MARK_GAP,
		table[1] + (HEADER_HEIGHT - SORT_MARK_HEIGHT) / 2,
		hud.sort
	);
	pen.frame.controls.push( {
		id: "market-sort-price",
		label: "Sort by price",
		kind: "button",
		rect: [ left + COLUMN.price.x, table[1], COLUMN.price.width, HEADER_HEIGHT ],
		selected: hud.sort === "price-asc" || hud.sort === "price-desc"
	} );
	const rows = browse?.rows ?? [];
	rows.slice( 0, BROWSE_ROWS ).forEach( ( row, i ) => {
		const top = table[1] + HEADER_HEIGHT + i * ROW_PITCH;
		const own = marketOwnRow( row, view.localName ), picked = hud.selected === row.id;
		const buyable = row.state === "open" && !own;
		const cell = ( column: Column ): UiRect => [ left + column.x, top, column.width, ROW_PITCH ];
		pen.row(
			"market-pick:" + row.id,
			[ left + 1 + ROW_ICON + 8, top, ACTIONS_LEFT - ROW_ICON - 12, ROW_PITCH ],
			i,
			picked,
			row.item.name ?? ""
		);
		pen.item(
			"market-item:browse:" + row.id,
			row.item,
			[ left + 5, top + (ROW_PITCH - ROW_ICON) / 2, ROW_ICON, ROW_ICON ],
			picked
		);
		pen.text(
			itemCaption( row.item ),
			[ left + COLUMN.item.x + ROW_ICON + 6, top, COLUMN.item.width - ROW_ICON - 6, ROW_PITCH ],
			row.sealed ? SEALED : itemColor( pen, row.item )
		);
		pen.text( row.degree ? String( row.degree ) : "-", cell( COLUMN.degree ), pen.frame.white, 1 );
		pen.text( String( Math.max( 1, row.item.quantity ) ), cell( COLUMN.qty ), pen.frame.white, 1 );
		pen.text( priceText( row.price, row.currency ), cell( COLUMN.price ), priceColor( row.currency ) );
		pen.text(
			own ? "Yours" : row.seller || "Anonymous",
			cell( COLUMN.seller ),
			own || !row.seller ? DIM : pen.frame.white
		);
		pen.text( marketTimeLeft( row.expiresMs, view.now ), cell( COLUMN.left ), DIM );
		const by = top + (ROW_PITCH - BUTTON_HEIGHT) / 2;
		let x = left + ACTIONS_LEFT;
		for (
			const [kind, caption, width, disabled] of [
				[ "buy", "Buy", 34, !buyable || pen.busy ],
				[ "whisper", "Send PM", 58, !buyable || !row.seller ],
				[ "offer", "Make offer", 76, !buyable || pen.busy ]
			] as const
		) {
			pen.button( `market-row-${kind}:${row.id}`, caption, x, by, width, disabled );
			x += width + 2;
		}
	} );
	// The empty slots keep the table's stripes and rules, so a short page
	// reads as a table and not as a hole; an empty result is one card on a
	// clean ground instead.
	for ( let i = Math.min( rows.length, BROWSE_ROWS ); i < BROWSE_ROWS && rows.length; i++ ) {
		const slot: UiRect = [ left + 1, table[1] + HEADER_HEIGHT + i * ROW_PITCH, LEFT_WIDTH - 2, ROW_PITCH ];
		if ( i % 2 ) pen.frameDraw.rect( pen.frame, { r: slot, color: STRIPE } );
		pen.frameDraw.rect( pen.frame, { r: [ slot[0], slot[1] + slot[3] - 1, slot[2], 1 ], color: LINE } );
	}
	if ( browse && !rows.length ) {
		emptyResult( pen, [ left, table[1] + HEADER_HEIGHT, LEFT_WIDTH, BROWSE_ROWS * ROW_PITCH ] );
	}
	// The footer bar inside the table: the count, the last write's outcome
	// and, when there is more than one, the pages.
	const bar: UiRect = [ table[0] + 1, table[1] + table[3] - FOOTER_HEIGHT, table[2] - 2, FOOTER_HEIGHT - 1 ];
	pen.frameDraw.rect( pen.frame, { r: bar, color: HEADER } );
	pen.frameDraw.rect( pen.frame, { r: [ bar[0], bar[1], bar[2], 1 ], color: EDGE } );
	const total = browse?.total ?? 0;
	pen.text(
		`${marketAmount( total )} listing${total === 1 ? "" : "s"}`,
		[ bar[0] + 10, bar[1], 160, bar[3] ],
		GOLD
	);
	statusLine( pen, [ bar[0] + 170, bar[1], bar[2] - 2 * 170, bar[3] ] );
	pager(
		pen,
		"market-",
		hud.page - 1,
		Math.max( 1, browse?.pages ?? 1 ),
		bar[0] + bar[2] - 6,
		bar[1] + (bar[3] - BUTTON_HEIGHT) / 2
	);
}

/*
================
sortMark

The sort triangles beside a header: a dim pair while the column does
not sort, a gold one pointing the way it does. Drawn as rows of pixels:
the game font has no arrow glyphs and ifcommon has no up arrow.
================
*/
function sortMark( pen: Pen, x: number, y: number, sort: MarketHudState["sort"] ) {
	const up = sort === "price-asc", down = sort === "price-desc";
	const centre = Math.floor( (SORT_MARK_HEIGHT - SORT_TRIANGLE_ROWS) / 2 );
	if ( !down ) sortTriangle( pen, x, y + (up ? centre : 0), true, up ? GOLD : DIM );
	if ( !up ) {
		sortTriangle( pen, x, y + (down ? centre : SORT_MARK_HEIGHT - SORT_TRIANGLE_ROWS), false, down ? GOLD : DIM );
	}
}

/*
================
sortTriangle
================
*/
function sortTriangle( pen: Pen, x: number, y: number, up: boolean, color: UiQuad["color"] ) {
	for ( let row = 0; row < SORT_TRIANGLE_ROWS; row++ ) {
		const width = 2 * (up ? row : SORT_TRIANGLE_ROWS - 1 - row) + 1;
		pen.frameDraw.rect( pen.frame, {
			r: [ x + SORT_TRIANGLE_ROWS - 1 - (width - 1) / 2, y + row, width, 1 ],
			color
		} );
	}
}

/*
================
filterMenus

The search line's dropdowns: each one's caption for the current choice,
its place on the line, and its choices as [value, caption].
================
*/
function filterMenus( hud: MarketHudState ) {
	const degrees: [string, string][] = [ [ "0", "All degrees" ] ];
	for ( let degree = 1; degree <= MARKET_MAX_DEGREE; degree++ ) {
		degrees.push( [ String( degree ), `Degree ${degree}` ] );
	}
	const menus: {
		name: MarketMenu;
		label: string;
		width: number;
		current: string;
		choices: readonly [string, string][];
	}[] = [
		{ name: "degree", label: "Degree", width: 88, current: String( hud.degree ), choices: degrees },
		{
			name: "race",
			label: "Race",
			width: 88,
			current: hud.race,
			choices: [ [ "", "All races" ], [ "ch", "Chinese" ], [ "eu", "European" ] ]
		},
		{
			name: "rarity",
			label: "Rarity",
			width: 88,
			current: hud.rarity,
			choices: [ [ "", "All rarities" ], [ "sealed", "Sealed" ], [ "normal", "Normal" ] ]
		},
		{
			name: "currency",
			label: "Currency",
			width: 96,
			current: hud.currency,
			choices: [ [ "", "Gold + Silk" ], [ "gold", "Gold" ], [ "silk", "Silk" ] ]
		},
		{
			name: "sort",
			label: "Sort",
			width: 104,
			current: hud.sort,
			choices: MARKET_SORT_ORDER.map( sort => [ sort, SORT_CAPTIONS[sort] ] as [string, string] )
		}
	];
	let x = QUERY_WIDTH + MENU_GAP;
	return menus.map( menu => {
		const placed = {
			...menu,
			x,
			caption: menu.choices.find( ( [value] ) => value === menu.current )?.[1] ?? menu.choices[0]![1]
		};
		x += menu.width + MENU_GAP;
		return placed;
	} );
}

/*
================
filterMenu

The open dropdown's list under its button, over the listings.
================
*/
function filterMenu( pen: Pen ) {
	const { px, py, view } = pen;
	const menu = filterMenus( view.hud ).find( row => row.name === view.hud.menu );
	if ( !menu ) return;
	// The option window's open combo (ui.ts video combos): a black list
	// under the box, the current choice lit, the hovered one lit softer.
	const list: UiRect = [
		px + MARGIN + menu.x,
		py + FILTER_TOP + BUTTON_HEIGHT,
		menu.width,
		menu.choices.length * MENU_ROW
	];
	pen.frameDraw.rect( pen.frame, { r: list, color: [ 0, 0, 0, 1 ] } );
	pen.frame.blocks.push( list );
	menu.choices.forEach( ( [value, caption], i ) => {
		const id = `market-choose:${menu.name}:${value}`;
		const cell: UiRect = [ list[0], list[1] + i * MENU_ROW, list[2], MENU_ROW ];
		const current = value === menu.current;
		if ( current ) pen.frameDraw.rect( pen.frame, { r: cell, color: MENU_CURRENT } );
		else if ( pen.frame.hover === id ) pen.frameDraw.rect( pen.frame, { r: cell, color: MENU_HOVER } );
		pen.text( caption, [ cell[0] + 5, cell[1], cell[2] - 10, MENU_ROW ], current ? GOLD : pen.frame.white );
		pen.frame.controls.push( { id, label: caption, rect: cell, kind: "button", selected: current } );
	} );
}

/*
================
statusLine

The last write's outcome, or that one is on its way.
================
*/
function statusLine( pen: Pen, r: UiRect ) {
	const status = pen.view.hud.status;
	if ( pen.writing ) pen.text( "Waiting for the market...", r, DIM, 1 );
	else if ( status ) pen.text( status.text, r, status.ok ? OK : REFUSED, 1 );
}

// ============================================================================

/*
================
sellPanel

List an item: the picked item, its currency and price, what the sale
pays, the anonymous choice and the List it button.
================
*/
function sellPanel( pen: Pen ) {
	const { px, py, view } = pen, hud = view.hud, rules = view.market?.mine?.rules;
	const left = px + RIGHT_LEFT, top = py + SELL_TOP, width = RIGHT_WIDTH;
	pen.panel( [ left, top, width, SELL_HEIGHT ] );
	pen.text( "List an item", [ left + 10, top + 4, width - 20, 20 ], GOLD );
	const item = view.bag.find( row => row.slot === hud.sellSlot );
	const slot: UiRect = [ left + 10, top + 28, 40, 40 ];
	pen.frameDraw.rect( pen.frame, {
		r: [ slot[0] - 1, slot[1] - 1, slot[2] + 2, slot[3] + 2 ],
		color: item ? GOLD : LINE
	} );
	if ( item ) pen.item( "market-sell-clear", item, slot, true );
	else pen.button( "market-sell-pick", "+", slot[0], slot[1] + 7, slot[2] );
	pen.text(
		item ? itemCaption( item ) : "Pick an item from your bag.",
		[ left + 58, top + 28, width - 68, 20 ],
		item ? itemColor( pen, item ) : DIM
	);
	pen.text(
		item ? "Click it to put it back." : "Or Ctrl+click / drag a bag item here.",
		[ left + 58, top + 48, width - 68, 20 ],
		DIM
	);
	const y = top + 76;
	pen.button( "market-sell-currency:gold", "Gold", left + 10, y, 52, false, hud.sellCurrency === "gold" );
	pen.button( "market-sell-currency:silk", "Silk", left + 64, y, 52, false, hud.sellCurrency === "silk" );
	pen.field(
		"market-price",
		hud.sellPrice,
		[ left + 120, y, width - 130, BUTTON_HEIGHT ],
		"Price",
		MARKET_PRICE_DIGITS
	);
	const price = marketPriceValue( hud.sellPrice ), fee = marketFee( price, rules?.feePercent ?? 0 );
	// At the cap the server refuses another listing (store MarketList,
	// ErrMarketCap): the line says so before the click, not after.
	const full = marketListingsFull( view.market?.mine );
	pen.text(
		full ?
			`${rules?.openCap} / ${rules?.openCap} listed: cancel one to list more.` :
			price ?
			`Fee ${marketAmount( fee )} · you get ${priceText( price - fee, hud.sellCurrency )}` :
			`Fee ${rules?.feePercent ?? 0}% of the price`,
		[ left + 10, y + 28, width - 20, 18 ],
		full ? REFUSED : price ? priceColor( hud.sellCurrency ) : DIM
	);
	const bottom = top + SELL_HEIGHT - BUTTON_HEIGHT - 8;
	// The anonymous choice, one line of the game's check box.
	choice( pen, "market-anonymous", "Anonymous", [ left + 10, bottom, 92, BUTTON_HEIGHT ], hud.anonymous );
	pen.button( "market-tab:sell", "Pick from bag", left + 106, bottom, 84, false, hud.tab === "sell" );
	pen.button( "market-list", "List it", left + 194, bottom, width - 204, !item || price < 1 || full || pen.busy );
}

/*
================
pagesPanel

My listings, History and Offers, or the bag while an item is picked.
================
*/
function pagesPanel( pen: Pen ) {
	const { px, py, view } = pen, hud = view.hud, mine = view.market?.mine;
	const left = px + RIGHT_LEFT, top = py + PAGES_TOP, width = RIGHT_WIDTH;
	const height = HEIGHT - PAGES_TOP - RULES_HEIGHT - 20;
	pen.panel( [ left, top, width, height ] );
	const openOffers = mine?.offersIn.filter( offer => offer.state === "open" ).length ?? 0;
	const tabs = [
		[ "mine", "My listings", 82 ],
		[ "history", "History", 64 ],
		[ "offers", openOffers ? `Offers (${openOffers})` : "Offers", 76 ]
	] as const;
	let x = left + 8;
	for ( const [tab, caption, tabWidth] of tabs ) {
		pen.button( "market-tab:" + tab, caption, x, top + 6, tabWidth, false, hud.tab === tab );
		x += tabWidth + 4;
	}
	const area: UiRect = [ left + 8, top + 38, width - 16, height - 46 ];
	if ( hud.tab === "sell" ) bagPage( pen, area );
	else if ( hud.tab === "history" ) historyPage( pen, area );
	else if ( hud.tab === "offers" ) offersPage( pen, area );
	else listingsPage( pen, area );
}

/*
================
sideRow

One row of the right column's lists: icon, caption, a right-aligned
value and a second line in the caption's place when picked.
================
*/
function sideRow(
	pen: Pen,
	spec: {
		id: string;
		item: InventoryItem | undefined;
		r: UiRect;
		index: number;
		picked: boolean;
		caption: string;
		value: string;
		valueColor: UiQuad["color"];
		note: string;
	}
) {
	const { r } = spec;
	pen.row( spec.id, r, spec.index, spec.picked, spec.caption );
	if ( spec.item ) {
		pen.item(
			"market-item:" + spec.id.slice( 7 ),
			spec.item,
			[ r[0] + 2, r[1] + (SIDE_PITCH - SIDE_ICON) / 2, SIDE_ICON, SIDE_ICON ],
			spec.picked
		);
	}
	const textLeft = r[0] + (spec.item ? SIDE_ICON + 6 : 4);
	const valueWidth = Math.min( 110, pen.frame.text.run( spec.value ).width + 4 );
	pen.text(
		spec.caption,
		[ textLeft, r[1], r[0] + r[2] - textLeft - valueWidth - 4, SIDE_PITCH ],
		spec.item ? itemColor( pen, spec.item ) : pen.frame.white
	);
	pen.text( spec.value, [ r[0] + r[2] - valueWidth - 2, r[1], valueWidth, SIDE_PITCH ], spec.valueColor, 2 );
	if ( spec.note ) {
		pen.text( spec.note, [ textLeft, r[1] + SIDE_PITCH - 6, r[2] - textLeft + r[0], 12 ], DIM );
	}
}

/*
================
listingsPage

The account's listings, with where a returned item goes and the Cancel
or Take back of the picked one.
================
*/
function listingsPage( pen: Pen, area: UiRect ) {
	const { view } = pen, hud = view.hud, mine = view.market?.mine, rules = mine?.rules;
	const listings = marketShownListings( mine );
	const active = marketActiveListings( mine );
	// The head line: the count against the cap, and the pages on its right,
	// so the footer line is free for where a cancelled item goes.
	pen.text(
		`${active} / ${rules?.openCap ?? 0} listed · ${Math.round( (rules?.durationMs ?? 0) / 3600000 )} h each`,
		[ area[0], area[1], area[2], BUTTON_HEIGHT ],
		DIM
	);
	const pages = Math.max( 1, Math.ceil( listings.length / MARKET_MINE_ROWS ) );
	pager( pen, "market-mine-", hud.minePage, pages, area[0] + area[2], area[1], true );
	const rowsTop = area[1] + BUTTON_HEIGHT + 3;
	const shown = listings.slice( hud.minePage * MARKET_MINE_ROWS, (hud.minePage + 1) * MARKET_MINE_ROWS );
	shown.forEach( ( row, i ) => {
		sideRow( pen, {
			id: "market-mine:" + row.id,
			item: row.item,
			r: [ area[0], rowsTop + i * SIDE_PITCH, area[2], SIDE_PITCH ],
			index: i,
			picked: hud.mineSelected === row.id,
			caption: itemCaption( row.item ) + (row.anonymous ? " (anon.)" : ""),
			value: row.state === "open" ? priceText( row.price, row.currency ) : STATE_CAPTIONS[row.state],
			valueColor: row.state === "held" ? GOLD : row.state === "open" ? priceColor( row.currency ) : DIM,
			note: ""
		} );
	} );
	if ( mine && !listings.length ) {
		pen.text( "Nothing on sale.", [ area[0], area[1] + 24, area[2], 20 ], pen.frame.white );
		pen.text( "List an item above; sold and returned ones are in History.", [
			area[0],
			area[1] + 44,
			area[2],
			18
		], DIM );
	}
	const bottom = area[1] + area[3], footer = bottom - 2 * BUTTON_HEIGHT - 6;
	const picked = listings.find( row => row.id === hud.mineSelected );
	// Where Cancel or Take back sends the item: asked only once a listing
	// is picked, as a choice beside its own caption, never as two buttons
	// that look like views.
	if ( picked ) {
		pen.text( "Send to", [ area[0], footer, 46, BUTTON_HEIGHT ], DIM );
		choice(
			pen,
			"market-return-to:bag",
			"Inventory",
			[ area[0] + 48, footer, 80, BUTTON_HEIGHT ],
			hud.returnTo === "bag"
		);
		choice(
			pen,
			"market-return-to:warehouse",
			"Warehouse",
			[ area[0] + 132, footer, 92, BUTTON_HEIGHT ],
			hud.returnTo === "warehouse"
		);
	} else if ( listings.length ) {
		pen.text( "Pick one to cancel it.", [ area[0], footer, area[2] - 120, BUTTON_HEIGHT ], DIM );
	}
	const half = (area[2] - 4) / 2;
	pen.button(
		"market-cancel",
		"Cancel listing",
		area[0],
		bottom - BUTTON_HEIGHT,
		half,
		picked?.state !== "open" || pen.busy
	);
	pen.button(
		"market-takeback",
		"Take back",
		area[0] + half + 4,
		bottom - BUTTON_HEIGHT,
		half,
		picked?.state !== "held" || pen.busy
	);
}

/*
================
emptyResult

A search that found nothing: a card in the middle of the rows saying
what was searched for and, when the search line narrows anything, the
one click that clears it. An unnarrowed empty market says so instead.
================
*/
function emptyResult( pen: Pen, rows: UiRect ) {
	const hud = pen.view.hud, narrowed = marketSearchNarrowed( hud );
	const card: UiRect = [
		rows[0] + (rows[2] - EMPTY_CARD_WIDTH) / 2,
		rows[1] + (rows[3] - EMPTY_CARD_HEIGHT) / 2,
		EMPTY_CARD_WIDTH,
		narrowed ? EMPTY_CARD_HEIGHT : EMPTY_CARD_HEIGHT - BUTTON_HEIGHT - 10
	];
	pen.panel( card, EDGE );
	pen.frameDraw.rect( pen.frame, { r: [ card[0] + 1, card[1] + 1, card[2] - 2, 30 ], color: HEADER } );
	pen.text(
		narrowed ? "No listings match" : "Nothing is on sale yet",
		[ card[0], card[1] + 6, card[2], 20 ],
		GOLD,
		1
	);
	const query = hud.query.trim();
	pen.text(
		narrowed ?
			query ? `Nothing for "${query}" with these filters.` : "Nothing with these filters." :
			"Be the first: list an item on the right.",
		[ card[0] + 12, card[1] + 38, card[2] - 24, 18 ],
		pen.frame.white,
		1
	);
	if ( !narrowed ) return;
	pen.text(
		"Try fewer words, another category or a lower degree.",
		[ card[0] + 12, card[1] + 56, card[2] - 24, 18 ],
		DIM,
		1
	);
	pen.button(
		"market-clear-search",
		"Clear search",
		card[0] + (card[2] - CLEAR_SEARCH_WIDTH) / 2,
		card[1] + card[3] - BUTTON_HEIGHT - 10,
		CLEAR_SEARCH_WIDTH,
		pen.busy
	);
}

/*
================
historyPage

What the account bought, newest first, and its listings that are done:
sold, or returned (cancelled, taken back or unsold).
================
*/
function historyPage( pen: Pen, area: UiRect ) {
	const mine = pen.view.market?.mine;
	const entries = [
		...(mine?.purchases ?? []).map( buy => ({
			at: buy.atMs,
			caption: buy.item,
			value: priceText( buy.price, buy.currency ),
			color: priceColor( buy.currency ),
			note: "Bought · to your " + (buy.to === "bag" ? "inventory" : "warehouse")
		}) ),
		...(mine?.listings ?? []).filter( row => row.state === "sold" || row.state === "returned" ).map( row => ({
			at: row.listedMs,
			caption: itemCaption( row.item ),
			value: row.state === "sold" ? priceText( row.price, row.currency ) : "Returned",
			color: row.state === "sold" ? OK : DIM,
			note: row.state === "sold" ? "Sold" : "Cancelled or unsold · back to you"
		}) )
	].sort( ( a, b ) => b.at - a.at );
	const pages = Math.max( 1, Math.ceil( entries.length / MARKET_HISTORY_ROWS ) );
	const page = Math.min( pen.view.hud.historyPage, pages - 1 );
	entries.slice( page * MARKET_HISTORY_ROWS, (page + 1) * MARKET_HISTORY_ROWS ).forEach( ( entry, i ) => {
		sideRow( pen, {
			id: "market-history:" + i,
			item: undefined,
			r: [ area[0], area[1] + i * (SIDE_PITCH + 10), area[2], SIDE_PITCH + 10 ],
			index: i,
			picked: false,
			caption: entry.caption,
			value: entry.value,
			valueColor: entry.color,
			note: entry.note
		} );
	} );
	if ( mine && !entries.length ) {
		pen.text( "Nothing bought or sold yet.", [ area[0], area[1] + 4, area[2], 20 ], pen.frame.white );
	}
	pager( pen, "market-history-", page, pages, area[0] + area[2], area[1] + area[3] - BUTTON_HEIGHT, true );
}

/*
================
offersPage

The offers on the account's items, or the ones it made, with the
answers each side has.
================
*/
function offersPage( pen: Pen, area: UiRect ) {
	const { view } = pen, hud = view.hud, mine = view.market?.mine, side = hud.offerSide;
	const offers = (side === "in" ? mine?.offersIn : mine?.offersOut) ?? [];
	const half = (area[2] - 4) / 2;
	pen.button(
		"market-offer-side:in",
		`Received (${mine?.offersIn.length ?? 0})`,
		area[0],
		area[1],
		half,
		false,
		side === "in"
	);
	pen.button(
		"market-offer-side:out",
		`Made (${mine?.offersOut.length ?? 0})`,
		area[0] + half + 4,
		area[1],
		half,
		false,
		side === "out"
	);
	const pages = marketOfferPages( offers ), page = Math.min( hud.offerPages[side], pages - 1 );
	offers.slice( page * MARKET_OFFER_ROWS, (page + 1) * MARKET_OFFER_ROWS ).forEach( ( offer, i ) => {
		const key = side + ":" + offer.id;
		sideRow( pen, {
			id: "market-offer-" + key,
			item: offer.item,
			r: [ area[0], area[1] + BUTTON_HEIGHT + 4 + i * SIDE_PITCH, area[2], SIDE_PITCH ],
			index: i,
			picked: hud.offerSelected === key,
			caption: side === "in" ? `${offer.buyer}: ${itemCaption( offer.item )}` : itemCaption( offer.item ),
			value: offer.state === "open" ? priceText( offer.price, offer.currency ) : OFFER_CAPTIONS[offer.state],
			valueColor: offer.state === "open" ? priceColor( offer.currency ) : DIM,
			note: ""
		} );
	} );
	if ( mine && !offers.length ) {
		pen.text(
			side === "in" ? "No offers on your items." : "You have made no offers.",
			[ area[0], area[1] + BUTTON_HEIGHT + 8, area[2], 20 ],
			pen.frame.white
		);
	}
	const bottom = area[1] + area[3];
	pager( pen, `market-offers-${side}-`, page, pages, area[0] + area[2], bottom - 2 * BUTTON_HEIGHT - 6, true );
	const offer = marketOfferAt( view.market, hud.offerSelected );
	const open = offer?.state === "open";
	if ( side === "in" ) {
		pen.button(
			"market-offer-whisper",
			"PM",
			area[0],
			bottom - 2 * BUTTON_HEIGHT - 6,
			48,
			!open || !hud.offerSelected.startsWith( "in:" ) || !offer?.buyer
		);
		const incoming = open && hud.offerSelected.startsWith( "in:" );
		const third = (area[2] - 4) / 2;
		pen.button( "market-reject", "Decline", area[0], bottom - BUTTON_HEIGHT, third, !incoming || pen.busy );
		pen.button(
			"market-accept",
			"Accept",
			area[0] + third + 4,
			bottom - BUTTON_HEIGHT,
			third,
			!incoming || pen.busy
		);
	} else {
		const outgoing = open && hud.offerSelected.startsWith( "out:" );
		pen.button(
			"market-withdraw",
			"Withdraw offer",
			area[0],
			bottom - BUTTON_HEIGHT,
			area[2],
			!outgoing || pen.busy
		);
	}
}

/*
================
bagPage

The bag the seller picks an item from; a pick fills List an item.
================
*/
function bagPage( pen: Pen, area: UiRect ) {
	const { view } = pen, hud = view.hud;
	const bag = view.bag.filter( item => item.slot >= MARKET_BAG_FIRST_SLOT );
	const lastSlot = Math.max( MARKET_BAG_FIRST_SLOT, ...bag.map( item => item.slot ) );
	const perPage = MARKET_BAG_COLUMNS * MARKET_BAG_ROWS;
	const pages = Math.max( 1, Math.ceil( (lastSlot - MARKET_BAG_FIRST_SLOT + 1) / perPage ) );
	const page = Math.min( hud.bagPage, pages - 1 );
	pen.text( "Pick the item to sell", [ area[0], area[1], area[2], 16 ], GOLD );
	const gridLeft = area[0] + (area[2] - MARKET_BAG_COLUMNS * BAG_CELL) / 2;
	for ( let index = 0; index < perPage; index++ ) {
		const slot = MARKET_BAG_FIRST_SLOT + page * perPage + index;
		const r: UiRect = [
			gridLeft + index % MARKET_BAG_COLUMNS * BAG_CELL + (BAG_CELL - BAG_ICON) / 2,
			area[1] + 20 + Math.floor( index / MARKET_BAG_COLUMNS ) * BAG_CELL + (BAG_CELL - BAG_ICON) / 2,
			BAG_ICON,
			BAG_ICON
		];
		const item = bag.find( row => row.slot === slot );
		if ( hud.sellSlot === slot ) {
			pen.frameDraw.rect( pen.frame, { r: [ r[0] - 2, r[1] - 2, r[2] + 4, r[3] + 4 ], color: GOLD } );
		}
		pen.item( item ? "market-sell-slot:" + slot : "market-sell-empty:" + slot, item, r, hud.sellSlot === slot );
	}
	const bottom = area[1] + area[3] - BUTTON_HEIGHT;
	pager( pen, "market-bag-", page, pages, area[0] + area[2], bottom, true );
}

/*
================
rulesPanel

The market's rules in the player's words, from the server's numbers.
================
*/
function rulesPanel( pen: Pen ) {
	const { px, py, view } = pen, rules = view.market?.mine?.rules;
	const left = px + RIGHT_LEFT, top = py + HEIGHT - RULES_HEIGHT - 12;
	pen.panel( [ left, top, RIGHT_WIDTH, RULES_HEIGHT ], LINE );
	const lines = [
		"Sale gold goes to the listing character,",
		"Silk to your account. Unsold items return.",
		`The seller pays a ${rules?.feePercent ?? 0}% fee on each sale.`,
		"Hotkey: K, or the market button by FPS."
	];
	lines.forEach( ( line, i ) => pen.text( line, [ left + 8, top + 4 + i * 16, RIGHT_WIDTH - 16, 16 ], DIM ) );
}

/*
================
choice

One option of a pick-one choice: the game's check box (com_checkbutton)
and its caption, the whole line one control.
================
*/
function choice( pen: Pen, id: string, caption: string, r: UiRect, on: boolean ) {
	pen.frameDraw.image( pen.frame, {
		r: [ r[0], r[1] + (r[3] - CHECK_SIZE) / 2, CHECK_SIZE, CHECK_SIZE ],
		path: IFCOMMON + `com_checkbutton_${on ? "on" : "off"}.png`
	} );
	pen.text( caption, [ r[0] + CHECK_SIZE + 4, r[1], r[2] - CHECK_SIZE - 4, r[3] ], on ? pen.frame.white : DIM );
	pen.frame.controls.push( { id, label: caption, kind: "button", rect: r, selected: on } );
}

/*
================
destinationButtons

The inventory or warehouse pair every delivery asks for.
================
*/
function destinationButtons( pen: Pen, prefix: string, to: MarketDestination, x: number, y: number, width = 108 ) {
	pen.button( prefix + "bag", width < 100 ? "Inventory" : "To inventory", x, y, width, false, to === "bag" );
	pen.button(
		prefix + "warehouse",
		width < 100 ? "Warehouse" : "To warehouse",
		x + width + 4,
		y,
		width,
		false,
		to === "warehouse"
	);
}

/*
================
dialogBox

The buy or offer box over the window. A buy names its price; an offer
takes one below the asking price. Both ask where the item goes.
================
*/
function dialogBox( pen: Pen ) {
	const { px, py, view, frame } = pen, dialog = view.hud.dialog!;
	// The window behind the box is shaded; while the box is up its clicks
	// do nothing (market-hud.ts activate).
	pen.frameDraw.rect( frame, { r: [ px + 4, py + 30, WIDTH - 8, HEIGHT - 34 ], color: [ 0, 0, 0, .55 ] } );
	const x = px + (WIDTH - DIALOG_WIDTH) / 2, y = py + (HEIGHT - DIALOG_HEIGHT) / 2;
	pen.frameDraw.windowBox( frame, {
		title: dialog.kind === "buy" ? "Buy item" : "Make an offer",
		x,
		y,
		width: DIALOG_WIDTH,
		height: DIALOG_HEIGHT,
		tile: WINDOW_TILE
	} );
	const listing = dialog.listing;
	pen.item( "market-item:dialog:" + listing.id, listing.item, [ x + 16, y + 38, 32, 32 ] );
	pen.text( itemCaption( listing.item ), [ x + 56, y + 38, DIALOG_WIDTH - 72, 16 ], GOLD );
	pen.text(
		(dialog.kind === "buy" ? "Price " : "Asking ") + priceText( listing.price, listing.currency ),
		[ x + 56, y + 54, DIALOG_WIDTH - 72, 16 ],
		priceColor( listing.currency )
	);
	let ready = true;
	if ( dialog.kind === "offer" ) {
		pen.field(
			"market-offer-price",
			dialog.price,
			[ x + 16, y + 78, DIALOG_WIDTH - 32, 24 ],
			"Your offer, below the asking price",
			MARKET_PRICE_DIGITS
		);
		const price = marketPriceValue( dialog.price );
		ready = price >= 1 && price < listing.price;
	} else {
		pen.text( "Where should the item go?", [ x + 16, y + 80, DIALOG_WIDTH - 32, 20 ], DIM );
	}
	destinationButtons( pen, "market-dialog-to:", dialog.to, x + 16, y + 106 );
	pen.button( "market-dialog-cancel", "Cancel", x + DIALOG_WIDTH - 172, y + DIALOG_HEIGHT - 34, 76 );
	pen.button(
		"market-dialog-confirm",
		dialog.kind === "buy" ? "Buy" : "Send offer",
		x + DIALOG_WIDTH - 92,
		y + DIALOG_HEIGHT - 34,
		76,
		!ready || pen.busy
	);
}
