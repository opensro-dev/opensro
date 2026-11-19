/*
===========================================================================

market-window-side.ts - the consignment window's right column

Port-only, not native (SRO_CONSIGNMENT). The seller's side of the
window: List an item, the account's pages (My listings, History, Offers,
or the bag while an item is picked) and the market's rules. Every number
is the server's (foundation/gameplay market.ts); market-window.ts draws
the browse column beside it.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { MarketOffer, MarketRow } from "@/engine/contracts/market";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import { marketAmount, marketFee, MARKET_PRICE_DIGITS } from "@/engine/foundation/gameplay/market";
import {
	MARKET_BAG_COLUMNS,
	MARKET_BAG_FIRST_SLOT,
	MARKET_BAG_ROWS,
	MARKET_HISTORY_ROWS,
	MARKET_MINE_ROWS,
	MARKET_OFFER_ROWS,
	marketActiveListings,
	marketListingsFull,
	marketOfferAt,
	marketOfferPages,
	marketPriceValue,
	marketShownListings
} from "./market-hud";
import {
	BUTTON_HEIGHT,
	choice,
	DIM,
	GOLD,
	HEIGHT,
	itemCaption,
	itemColor,
	LINE,
	OK,
	pager,
	type Pen,
	priceColor,
	priceText,
	REFUSED,
	RIGHT_LEFT,
	RIGHT_WIDTH
} from "./market-window-pen";

const SELL_TOP = 38;
const SELL_HEIGHT = 162;
const PAGES_TOP = 208;
const RULES_HEIGHT = 74;
const SIDE_PITCH = 24;
const SIDE_ICON = 20;
const BAG_CELL = 34;
const BAG_ICON = 30;
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
sideColumn

The right column, top to bottom.
================
*/
export function sideColumn( pen: Pen ) {
	sellPanel( pen );
	pagesPanel( pen );
	rulesPanel( pen );
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
