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
import type { MarketRow } from "@/engine/contracts/market";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import {
	marketAmount,
	MARKET_MAX_DEGREE,
	MARKET_PRICE_DIGITS,
	marketTimeLeft
} from "@/engine/foundation/gameplay/market";
import type { UiAuthoredFrame } from "../frame/frame-contract";
import {
	MARKET_SORT_ORDER,
	marketOwnRow,
	marketPriceValue,
	marketSearchNarrowed,
	type MarketMenu,
	type MarketHudState
} from "./market-hud";
import {
	BUTTON_HEIGHT,
	destinationButtons,
	DIM,
	EDGE,
	GOLD,
	HEIGHT,
	IFCOMMON,
	itemCaption,
	itemColor,
	LEFT_WIDTH,
	LINE,
	MARGIN,
	type MarketPaint,
	type MarketWindowView,
	OK,
	pager,
	type Pen,
	penOf,
	priceColor,
	priceText,
	REFUSED,
	RIGHT_LEFT,
	STRIPE,
	WIDTH
} from "./market-window-pen";
import { sideColumn } from "./market-window-side";

export type { MarketPaint, MarketWindowView } from "./market-window-pen";

// The search line: the name field, the dropdowns and the Search button.
const QUERY_WIDTH = 130;
const SEARCH_WIDTH = 56;
const MENU_GAP = 4;
const MENU_ROW = 20;
// The open list's lit rows: the current choice (the option window's) and
// the one under the pointer.
const MENU_CURRENT: UiQuad["color"] = [ .2, .25, .3, 1 ];
const MENU_HOVER: UiQuad["color"] = [ .12, .14, .17, 1 ];
// The body's tile: the NPC talk window's on the same mframe (com_bg_tile_d),
// dark enough to meet the frame's inner edge art without a seam.
const WINDOW_TILE = IFCOMMON + "bg_tile/com_bg_tile_d.png";
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
// The empty result's card, centred in the table's rows.
const EMPTY_CARD_WIDTH = 380;
const EMPTY_CARD_HEIGHT = 116;
const CLEAR_SEARCH_WIDTH = 110;
// The buy and offer box.
const DIALOG_WIDTH = 330;
const DIALOG_HEIGHT = 170;
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
const HEADER: UiQuad["color"] = [ .09, .075, .04, .9 ];
const SORT_CAPTIONS: Readonly<Record<(typeof MARKET_SORT_ORDER)[number], string>> = {
	"newest": "Newest",
	"ending": "Ending soon",
	"price-asc": "Price: low",
	"price-desc": "Price: high"
};

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
	sideColumn( pen );
	if ( view.hud.menu ) filterMenu( pen );
	if ( view.hud.dialog ) dialogBox( pen );
	frameDraw.endWindow( frame, admission, "market" );
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
