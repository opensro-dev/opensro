/*
===========================================================================

market-window.ts - the consignment market window, drawn

Port-only, not native (SRO_CONSIGNMENT, the Experimental "Consignment
market" row): v1.150 has no consignment, so the window has no authored
layout. It is built from the client's own chrome - the framed window,
the native buttons, item slots with their tooltips, the gold captions -
so it reads as part of the game:

	Browse       a search line, the category, currency and order steps,
	             fifteen rows (item, seller, price, time left) and the
	             picked row's Whisper, Make offer and Buy
	Sell         the bag, the picked item's price, currency, fee and
	             anonymous choice
	My listings  the account's listings with Cancel or Take back, into
	             the inventory or the warehouse
	Offers       the offers on the account's items and the ones it made

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
	MARKET_PRICE_DIGITS
} from "@/engine/foundation/gameplay/market";
import type { UiAuthoredFrame } from "../frame/frame-contract";
import {
	MARKET_MINE_ROWS,
	MARKET_OFFER_ROWS,
	MARKET_SORT_ORDER,
	marketBusy,
	marketOfferPages,
	marketWriting,
	marketOfferAt,
	marketOwnRow,
	marketPriceValue,
	type MarketHudState,
	type MarketTab
} from "./market-hud";

// The window's size and its inner margin.
const WIDTH = 600;
const HEIGHT = 524;
const MARGIN = 14;
// The tab strip and the content below it.
const TAB_TOP = 36;
const TAB_WIDTH = 104;
const CONTENT_TOP = 72;
// A list row: its pitch and its item icon.
const ROW_PITCH = 22;
const ROW_ICON = 20;
// The footer's buttons sit this far above the window's bottom edge.
const FOOTER_BOTTOM = 36;
// The sell tab's bag: twelve columns of 30-pixel cells.
const BAG_COLUMNS = 12;
const BAG_CELL = 30;
const BAG_ICON = 26;
const BAG_FIRST_SLOT = 13;
// The buy and offer box.
const DIALOG_WIDTH = 330;
const DIALOG_HEIGHT = 170;

const GOLD: UiQuad["color"] = [ 252 / 255, 210 / 255, 100 / 255, 1 ];
const DIM: UiQuad["color"] = [ 170 / 255, 170 / 255, 160 / 255, 1 ];
const OK: UiQuad["color"] = [ 150 / 255, 230 / 255, 130 / 255, 1 ];
const REFUSED: UiQuad["color"] = [ 255 / 255, 130 / 255, 110 / 255, 1 ];
const SILK: UiQuad["color"] = [ 190 / 255, 200 / 255, 255 / 255, 1 ];
const PANEL: UiQuad["color"] = [ .02, .03, .025, .82 ];
const EDGE: UiQuad["color"] = [ .55, .45, .24, .85 ];
const STRIPE: UiQuad["color"] = [ 1, 1, 1, .035 ];
const PICKED: UiQuad["color"] = [ 252 / 255, 210 / 255, 100 / 255, .16 ];

const TAB_CAPTIONS: Readonly<Record<MarketTab, string>> = {
	browse: "Browse",
	sell: "Sell",
	mine: "My listings",
	offers: "Offers"
};
const SORT_CAPTIONS: Readonly<Record<(typeof MARKET_SORT_ORDER)[number], string>> = {
	"newest": "Newest",
	"ending": "Ending soon",
	"price-asc": "Lowest price",
	"price-desc": "Highest price"
};
const STATE_CAPTIONS: Readonly<Record<MarketRow["state"], string>> = {
	open: "On sale",
	sold: "Sold",
	returned: "Returned",
	held: "Waiting for you"
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
		spec: { title: string; x: number; y: number; width: number; height: number; }
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
		initial: [ (frame.w - WIDTH) / 2, Math.max( 0, (frame.h - HEIGHT) / 3 ), WIDTH, HEIGHT ],
		id: "window-drag:ConsignmentMarket"
	} );
	frameDraw.windowBox( frame, { title: "Consignment Market", x: px, y: py, width: WIDTH, height: HEIGHT } );
	frameDraw.closeButton( frame, px + WIDTH - 26, py + 10, "market-close" );
	const pen = penOf( frame, frameDraw, view, px, py );
	const openOffers = view.market?.mine?.offersIn.filter( offer => offer.state === "open" ).length ?? 0;
	for ( const [i, tab] of ([ "browse", "sell", "mine", "offers" ] as const).entries() ) {
		const caption = tab === "offers" && openOffers ? `${TAB_CAPTIONS[tab]} (${openOffers})` : TAB_CAPTIONS[tab];
		frameDraw.button( frame, {
			id: "market-tab:" + tab,
			value: caption,
			x: px + MARGIN + i * (TAB_WIDTH + 4),
			y: py + TAB_TOP,
			width: TAB_WIDTH,
			selected: view.hud.tab === tab
		} );
	}
	const mine = view.market?.mine;
	pen.text(
		`${marketAmount( view.gold )} Gold    ${marketAmount( mine?.silk ?? 0 )} Silk`,
		[ px + WIDTH - MARGIN - 160, py + TAB_TOP, 160, 24 ],
		GOLD,
		2
	);
	frameDraw.rect( frame, { r: [ px + MARGIN, py + CONTENT_TOP - 6, WIDTH - 2 * MARGIN, 1 ], color: EDGE } );
	if ( view.hud.tab === "browse" ) browseTab( pen );
	else if ( view.hud.tab === "sell" ) sellTab( pen );
	else if ( view.hud.tab === "mine" ) mineTab( pen );
	else offersTab( pen );
	statusLine( pen );
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
		panel( r: UiRect ) {
			frameDraw.rect( frame, { r, color: PANEL } );
			for (
				const edge of [
					[ r[0], r[1], r[2], 1 ],
					[ r[0], r[1] + r[3] - 1, r[2], 1 ],
					[ r[0], r[1], 1, r[3] ],
					[ r[0] + r[2] - 1, r[1], 1, r[3] ]
				] as UiRect[]
			) frameDraw.rect( frame, { r: edge, color: EDGE } );
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
			this.panel( r );
			const inner: UiRect = [ r[0] + 4, r[1], r[2] - 8, r[3] ];
			frame.controls.push( { id, label: placeholder, kind: "text", value, rect: r, maxLength } );
			if ( value ) this.text( value, inner );
			else if ( view.focus !== id ) this.text( placeholder, inner, DIM );
			if ( view.focus === id && view.caretVisible ) {
				const x = inner[0] + Math.min( inner[2], frame.text.run( value ).width );
				frameDraw.rect( frame, { r: [ x, r[1] + 4, 1, r[3] - 8 ], color: frame.white } );
			}
		},
		/*
		================
		row

		A list row's stripe and pick control; picked rows are gilded.
		================
		*/
		row( id: string, r: UiRect, index: number, picked: boolean, label: string ) {
			if ( picked ) frameDraw.rect( frame, { r, color: PICKED } );
			else if ( index % 2 ) frameDraw.rect( frame, { r, color: STRIPE } );
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
listHeader
================
*/
function listHeader( pen: Pen, y: number, columns: readonly (readonly [string, number, number, 0 | 1 | 2])[] ) {
	for ( const [caption, left, width, align] of columns ) {
		pen.text( caption, [ pen.px + left, y, width, 16 ], DIM, align );
	}
	pen.frameDraw.rect( pen.frame, { r: [ pen.px + MARGIN, y + 17, WIDTH - 2 * MARGIN, 1 ], color: EDGE } );
}

// ============================================================================

/*
================
browseTab
================
*/
function browseTab( pen: Pen ) {
	const { px, py, view } = pen, hud = view.hud, browse = view.market?.browse;
	const y = py + CONTENT_TOP;
	pen.field( "market-query", hud.query, [ px + MARGIN, y, 170, 24 ], "Search item names", 64 );
	const category = hud.category ?
		view.market?.categories.find( c => c.slug === hud.category )?.name ?? hud.category :
		"All items";
	pen.button( "market-category", category, px + 190, y, 116 );
	const currency = hud.currency ? marketCurrencyText( hud.currency ) : "Any currency";
	pen.button( "market-currency", currency, px + 310, y, 100 );
	pen.button( "market-sort", SORT_CAPTIONS[hud.sort], px + 414, y, 100 );
	pen.button( "market-search", "Search", px + 518, y, 68 );
	const top = y + 34;
	listHeader( pen, top, [
		[ "Item", MARGIN + ROW_ICON + 8, 230, 0 ],
		[ "Seller", 290, 110, 0 ],
		[ "Price", 404, 110, 2 ],
		[ "Time left", 520, 66, 2 ]
	] );
	const rows = browse?.rows ?? [];
	rows.forEach( ( row, i ) => {
		const r: UiRect = [ px + MARGIN, top + 20 + i * ROW_PITCH, WIDTH - 2 * MARGIN, ROW_PITCH ];
		const own = marketOwnRow( row, view.localName );
		pen.row(
			"market-pick:" + row.id,
			[ r[0] + ROW_ICON + 4, r[1], r[2] - ROW_ICON - 4, r[3] ],
			i,
			hud.selected === row.id,
			row.item.name ?? ""
		);
		pen.item(
			"market-item:browse:" + row.id,
			row.item,
			[ r[0] + 1, r[1] + 1, ROW_ICON, ROW_ICON ],
			hud.selected === row.id
		);
		pen.text( itemCaption( row.item ), [ r[0] + ROW_ICON + 8, r[1], 226, ROW_PITCH ] );
		pen.text(
			own ? "Yours" : row.seller || "Anonymous",
			[ px + 290, r[1], 110, ROW_PITCH ],
			own || !row.seller ? DIM : pen.frame.white
		);
		pen.text(
			priceText( row.price, row.currency ),
			[ px + 404, r[1], 110, ROW_PITCH ],
			priceColor( row.currency ),
			2
		);
		pen.text( marketTimeLeft( row.expiresMs, view.now ), [ px + 520, r[1], 66, ROW_PITCH ], DIM, 2 );
	} );
	if ( browse && !rows.length ) {
		pen.text(
			"Nothing matches. Try another name or category.",
			[ px + MARGIN, top + 40, WIDTH - 2 * MARGIN, 20 ],
			DIM,
			1
		);
	}
	const footer = py + HEIGHT - FOOTER_BOTTOM;
	const pages = Math.max( 1, browse?.pages ?? 1 );
	pen.button( "market-prev", "<", px + MARGIN, footer, 32, hud.page <= 1 );
	pen.text( `${hud.page} / ${pages}`, [ px + MARGIN + 34, footer, 60, 24 ], pen.frame.white, 1 );
	pen.button( "market-next", ">", px + MARGIN + 96, footer, 32, hud.page >= pages );
	const picked = rows.find( row => row.id === hud.selected );
	const buyable = !!picked && picked.state === "open" && !marketOwnRow( picked, view.localName );
	pen.button( "market-whisper", "Whisper", px + 314, footer, 84, !buyable || !picked?.seller );
	pen.button( "market-offer", "Make offer", px + 402, footer, 92, !buyable || pen.busy );
	pen.button( "market-buy", "Buy", px + 498, footer, 88, !buyable || pen.busy );
}

/*
================
sellTab
================
*/
function sellTab( pen: Pen ) {
	const { px, py, view } = pen, hud = view.hud, rules = view.market?.mine?.rules;
	const top = py + CONTENT_TOP;
	pen.text( "Pick an item from your inventory", [ px + MARGIN, top, 360, 18 ], GOLD );
	const bag = view.bag.filter( item => item.slot >= BAG_FIRST_SLOT );
	const lastSlot = Math.max( BAG_FIRST_SLOT + BAG_COLUMNS * 4 - 1, ...bag.map( item => item.slot ) );
	const grid: UiRect = [ px + MARGIN, top + 22, BAG_COLUMNS * BAG_CELL + 4, 0 ];
	const rowsOf = Math.ceil( (lastSlot - BAG_FIRST_SLOT + 1) / BAG_COLUMNS );
	pen.panel( [ grid[0], grid[1], grid[2], rowsOf * BAG_CELL + 4 ] );
	for ( let slot = BAG_FIRST_SLOT; slot <= lastSlot; slot++ ) {
		const index = slot - BAG_FIRST_SLOT;
		const r: UiRect = [
			grid[0] + 2 + index % BAG_COLUMNS * BAG_CELL + (BAG_CELL - BAG_ICON) / 2,
			grid[1] + 2 + Math.floor( index / BAG_COLUMNS ) * BAG_CELL + (BAG_CELL - BAG_ICON) / 2,
			BAG_ICON,
			BAG_ICON
		];
		const item = bag.find( row => row.slot === slot );
		if ( hud.sellSlot === slot ) {
			pen.frameDraw.rect( pen.frame, { r: [ r[0] - 2, r[1] - 2, r[2] + 4, r[3] + 4 ], color: GOLD } );
		}
		pen.item( item ? "market-sell-slot:" + slot : "market-sell-empty:" + slot, item, r, hud.sellSlot === slot );
	}
	// The terms: the picked item, its price and what the sale pays.
	const left = px + 406, width = WIDTH - MARGIN - 406;
	pen.panel( [ left, top + 22, width, 320 ] );
	const item = bag.find( row => row.slot === hud.sellSlot );
	pen.text(
		item ? itemCaption( item ) : "No item picked",
		[ left + 8, top + 30, width - 16, 18 ],
		item ? GOLD : DIM
	);
	pen.text( "Currency", [ left + 8, top + 56, width - 16, 18 ], DIM );
	pen.button( "market-sell-currency:gold", "Gold", left + 8, top + 76, 78, false, hud.sellCurrency === "gold" );
	pen.button( "market-sell-currency:silk", "Silk", left + 90, top + 76, 78, false, hud.sellCurrency === "silk" );
	pen.text( "Price", [ left + 8, top + 108, width - 16, 18 ], DIM );
	pen.field(
		"market-price",
		hud.sellPrice,
		[ left + 8, top + 128, width - 16, 24 ],
		"Enter a price",
		MARKET_PRICE_DIGITS
	);
	const price = marketPriceValue( hud.sellPrice ), fee = marketFee( price, rules?.feePercent ?? 0 );
	pen.text(
		`Fee (${rules?.feePercent ?? 0}%): ${marketAmount( fee )}`,
		[ left + 8, top + 160, width - 16, 18 ],
		DIM
	);
	pen.text(
		`You receive ${priceText( price - fee, hud.sellCurrency )}`,
		[ left + 8, top + 178, width - 16, 18 ],
		priceColor( hud.sellCurrency )
	);
	pen.button(
		"market-anonymous",
		(hud.anonymous ? "[x] " : "[  ] ") + "Sell anonymously",
		left + 8,
		top + 206,
		width - 16,
		false,
		hud.anonymous
	);
	const hours = Math.round( (rules?.durationMs ?? 0) / 3600000 );
	const open = view.market?.mine?.listings.filter( row => row.state === "open" ).length ?? 0;
	pen.text( `Listed for ${hours} hours`, [ left + 8, top + 240, width - 16, 18 ], DIM );
	pen.text( `Open listings ${open} / ${rules?.openCap ?? 0}`, [ left + 8, top + 258, width - 16, 18 ], DIM );
	pen.button( "market-list", "List item", left + 8, top + 290, width - 16, !item || price < 1 || pen.busy );
	pen.text(
		hud.sellCurrency === "gold" ?
			"The market holds the item until it sells. Gold goes to the character that listed it." :
			"The market holds the item until it sells. Silk goes to your account.",
		[ px + MARGIN, py + HEIGHT - FOOTER_BOTTOM, WIDTH - 2 * MARGIN, 24 ],
		DIM
	);
}

/*
================
mineTab
================
*/
function mineTab( pen: Pen ) {
	const { px, py, view } = pen, hud = view.hud, mine = view.market?.mine;
	const top = py + CONTENT_TOP;
	listHeader( pen, top, [
		[ "Item", MARGIN + ROW_ICON + 8, 230, 0 ],
		[ "Price", 270, 110, 2 ],
		[ "State", 392, 110, 0 ],
		[ "Time left", 506, 80, 2 ]
	] );
	const listings = mine?.listings ?? [];
	const shown = listings.slice( hud.minePage * MARKET_MINE_ROWS, (hud.minePage + 1) * MARKET_MINE_ROWS );
	shown.forEach( ( row, i ) => {
		const r: UiRect = [ px + MARGIN, top + 20 + i * ROW_PITCH, WIDTH - 2 * MARGIN, ROW_PITCH ];
		pen.row(
			"market-mine:" + row.id,
			[ r[0] + ROW_ICON + 4, r[1], r[2] - ROW_ICON - 4, r[3] ],
			i,
			hud.mineSelected === row.id,
			row.item.name ?? ""
		);
		pen.item(
			"market-item:mine:" + row.id,
			row.item,
			[ r[0] + 1, r[1] + 1, ROW_ICON, ROW_ICON ],
			hud.mineSelected === row.id
		);
		pen.text( itemCaption( row.item ) + (row.anonymous ? "  (anonymous)" : ""), [
			r[0] + ROW_ICON + 8,
			r[1],
			226,
			ROW_PITCH
		] );
		pen.text(
			priceText( row.price, row.currency ),
			[ px + 270, r[1], 110, ROW_PITCH ],
			priceColor( row.currency ),
			2
		);
		pen.text(
			STATE_CAPTIONS[row.state],
			[ px + 392, r[1], 110, ROW_PITCH ],
			row.state === "held" ? GOLD : pen.frame.white
		);
		pen.text(
			row.state === "open" ? marketTimeLeft( row.expiresMs, view.now ) : "",
			[ px + 506, r[1], 80, ROW_PITCH ],
			DIM,
			2
		);
	} );
	if ( mine && !listings.length ) {
		pen.text(
			"You have nothing listed. Sell an item from the Sell tab.",
			[ px + MARGIN, top + 40, WIDTH - 2 * MARGIN, 20 ],
			DIM,
			1
		);
	}
	const footer = py + HEIGHT - FOOTER_BOTTOM;
	const pages = Math.max( 1, Math.ceil( listings.length / MARKET_MINE_ROWS ) );
	pen.button( "market-mine-prev", "<", px + MARGIN, footer, 32, hud.minePage <= 0 );
	pen.text( `${hud.minePage + 1} / ${pages}`, [ px + MARGIN + 34, footer, 60, 24 ], pen.frame.white, 1 );
	pen.button( "market-mine-next", ">", px + MARGIN + 96, footer, 32, hud.minePage + 1 >= pages );
	destinationButtons( pen, "market-return-to:", hud.returnTo, px + 160, footer );
	const picked = listings.find( row => row.id === hud.mineSelected );
	pen.button( "market-cancel", "Cancel listing", px + 386, footer, 100, picked?.state !== "open" || pen.busy );
	pen.button( "market-takeback", "Take back", px + 490, footer, 96, picked?.state !== "held" || pen.busy );
}

/*
================
offersTab
================
*/
function offersTab( pen: Pen ) {
	const { px, py, view } = pen, hud = view.hud, mine = view.market?.mine;
	const section = ( title: string, side: "in" | "out", offers: readonly MarketOffer[], top: number ) => {
		pen.text( title, [ px + MARGIN, top, 300, 18 ], GOLD );
		listHeader( pen, top + 20, [
			[ "Item", MARGIN + ROW_ICON + 8, 200, 0 ],
			[ side === "in" ? "From" : "Asked", 236, 110, side === "in" ? 0 : 2 ],
			[ "Offered", 350, 120, 2 ],
			[ "State", 486, 100, 0 ]
		] );
		const pages = marketOfferPages( offers ), page = Math.min( hud.offerPages[side], pages - 1 );
		if ( pages > 1 ) {
			const right = px + WIDTH - MARGIN;
			pen.button( `market-offers-${side}-prev`, "<", right - 130, top - 5, 32, page <= 0 );
			pen.text( `${page + 1} / ${pages}`, [ right - 96, top, 60, 18 ], pen.frame.white, 1 );
			pen.button( `market-offers-${side}-next`, ">", right - 32, top - 5, 32, page + 1 >= pages );
		}
		offers.slice( page * MARKET_OFFER_ROWS, (page + 1) * MARKET_OFFER_ROWS ).forEach( ( offer, i ) => {
			const key = side + ":" + offer.id, picked = hud.offerSelected === key;
			const r: UiRect = [ px + MARGIN, top + 40 + i * ROW_PITCH, WIDTH - 2 * MARGIN, ROW_PITCH ];
			pen.row(
				"market-offer-" + key,
				[ r[0] + ROW_ICON + 4, r[1], r[2] - ROW_ICON - 4, r[3] ],
				i,
				picked,
				offer.item.name ?? ""
			);
			pen.item( "market-item:" + key, offer.item, [ r[0] + 1, r[1] + 1, ROW_ICON, ROW_ICON ], picked );
			pen.text( itemCaption( offer.item ), [ r[0] + ROW_ICON + 8, r[1], 196, ROW_PITCH ] );
			if ( side === "in" ) pen.text( offer.buyer, [ px + 236, r[1], 110, ROW_PITCH ] );
			else pen.text( priceText( offer.asked, offer.currency ), [ px + 236, r[1], 110, ROW_PITCH ], DIM, 2 );
			pen.text(
				priceText( offer.price, offer.currency ),
				[ px + 350, r[1], 120, ROW_PITCH ],
				priceColor( offer.currency ),
				2
			);
			pen.text(
				OFFER_CAPTIONS[offer.state],
				[ px + 486, r[1], 100, ROW_PITCH ],
				offer.state === "open" ? pen.frame.white : DIM
			);
		} );
		if ( mine && !offers.length ) {
			pen.text(
				side === "in" ? "No offers on your items." : "You have made no offers.",
				[ px + MARGIN, top + 44, WIDTH - 2 * MARGIN, 20 ],
				DIM,
				1
			);
		}
	};
	section( "Offers on your items", "in", mine?.offersIn ?? [], py + CONTENT_TOP );
	section( "Your offers", "out", mine?.offersOut ?? [], py + CONTENT_TOP + 196 );
	const footer = py + HEIGHT - FOOTER_BOTTOM;
	const offer = marketOfferAt( view.market, hud.offerSelected );
	const incoming = hud.offerSelected.startsWith( "in:" ) && offer?.state === "open";
	const outgoing = hud.offerSelected.startsWith( "out:" ) && offer?.state === "open";
	pen.button( "market-offer-whisper", "Whisper", px + MARGIN, footer, 84, !incoming || !offer?.buyer );
	pen.button( "market-withdraw", "Withdraw", px + 302, footer, 92, !outgoing || pen.busy );
	pen.button( "market-reject", "Decline", px + 398, footer, 92, !incoming || pen.busy );
	pen.button( "market-accept", "Accept", px + 494, footer, 92, !incoming || pen.busy );
}

/*
================
destinationButtons

The inventory or warehouse pair every delivery asks for.
================
*/
function destinationButtons( pen: Pen, prefix: string, to: MarketDestination, x: number, y: number ) {
	pen.button( prefix + "bag", "To inventory", x, y, 108, false, to === "bag" );
	pen.button( prefix + "warehouse", "To warehouse", x + 112, y, 108, false, to === "warehouse" );
}

/*
================
statusLine

The last write's outcome, or that one is on its way.
================
*/
function statusLine( pen: Pen ) {
	const { px, py, view } = pen, status = view.hud.status;
	const r: UiRect = [ px + MARGIN, py + HEIGHT - FOOTER_BOTTOM - 24, WIDTH - 2 * MARGIN, 20 ];
	if ( pen.writing ) pen.text( "Waiting for the market...", r, DIM, 1 );
	else if ( status ) pen.text( status.text, r, status.ok ? OK : REFUSED, 1 );
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
		height: DIALOG_HEIGHT
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
