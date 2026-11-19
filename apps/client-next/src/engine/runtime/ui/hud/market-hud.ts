/*
===========================================================================

market-hud.ts - the consignment market window's drafts and its requests

Port-only, not native (SRO_CONSIGNMENT on the server, published to the
client at world entry): v1.150 has no consignment. The window opens
anywhere in the world from its button beside FPS (or a storage keeper's
menu): the listings on the left, List an item and the account's pages
(My listings, History, Offers, the bag picker) on the right, and one
dialog at a time for a buy or an offer, each of which asks where the
item goes (the inventory or the warehouse).

This module owns what the player is typing and choosing; the worker owns
what the server said (foundation/gameplay market.ts) and drops a request
while a write is in flight. activate() turns a click into the requests
to send, so the coordinator only forwards them; market-window.ts draws.

===========================================================================
*/
import type {
	MarketCurrency,
	MarketDestination,
	MarketOffer,
	MarketOp,
	MarketRace,
	MarketRarity,
	MarketRequest,
	MarketRow,
	MarketSort,
	MarketState
} from "@/engine/contracts/market";
import {
	MARKET_MAX_DEGREE,
	MARKET_MAX_PAGE,
	MARKET_QUERY_LIMIT,
	marketNoticeText,
	marketPriceDraft
} from "@/engine/foundation/gameplay/market";

// The right column's pages: the account's listings, its history, its
// offers, and the bag the seller picks an item from.
export type MarketTab = "mine" | "history" | "offers" | "sell";

// The pages a tab click may show (the bag opens from Pick from bag).
export const MARKET_TABS: readonly MarketTab[] = [ "mine", "history", "offers", "sell" ];
// The search orders in the order the sort button steps through them.
export const MARKET_SORT_ORDER: readonly MarketSort[] = [ "newest", "ending", "price-asc", "price-desc" ];
// The currency filter's steps: any, then each currency.
export const MARKET_CURRENCY_FILTERS: readonly (MarketCurrency | "")[] = [ "", "gold", "silk" ];
// The race and rarity filters, "" for all.
export const MARKET_RACES: readonly (MarketRace | "")[] = [ "", "ch", "eu" ];
export const MARKET_RARITIES: readonly (MarketRarity | "")[] = [ "", "sealed", "normal" ];
// The search line's dropdowns; "" is none open.
export type MarketMenu = "" | "degree" | "race" | "rarity" | "currency" | "sort";
export const MARKET_MENUS: readonly MarketMenu[] = [ "degree", "race", "rarity", "currency", "sort" ];
// K opens and closes the market (virtual key 0x4B): no native default
// binding uses it (input-options.ts), and a player's own binding wins.
export const MARKET_HOTKEY_VK = 0x4b;
// A refusal or a success stays on the window's status line this long.
export const MARKET_STATUS_MS = 8000;
// The rows one page of each of the account's lists shows: as many as fit
// above the page's footer (the pages, the destination and the answers),
// so a full page never runs under its buttons (market-window.ts).
export const MARKET_MINE_ROWS = 7;
export const MARKET_OFFER_ROWS = 6;
export const MARKET_HISTORY_ROWS = 6;
// The bag picker's cells: seven columns, six rows a page.
export const MARKET_BAG_COLUMNS = 7;
export const MARKET_BAG_ROWS = 6;
// The first bag slot an item can be listed from (0-12 are worn).
export const MARKET_BAG_FIRST_SLOT = 13;
// The writes: their answers refresh the account page and the search.
const MARKET_WRITES: readonly MarketOp[] = [
	"list",
	"buy",
	"cancel",
	"takeback",
	"offer",
	"accept",
	"reject",
	"withdraw"
];

/*
================
MarketDialog

The open buy or offer box: the listing it is for, where the item goes,
and the offered price being typed.
================
*/
export interface MarketDialog {
	readonly kind: "buy" | "offer";
	readonly listing: MarketRow;
	readonly to: MarketDestination;
	readonly price: string;
}

/*
================
MarketHudState
================
*/
export interface MarketHudState {
	readonly open: boolean;
	readonly tab: MarketTab;
	// Browse: the search drafts and the picked listing.
	readonly query: string;
	readonly category: string;
	readonly currency: MarketCurrency | "";
	readonly sort: MarketSort;
	readonly degree: number;
	readonly race: MarketRace | "";
	readonly rarity: MarketRarity | "";
	// The open dropdown of the search line.
	readonly menu: MarketMenu;
	readonly page: number;
	readonly selected: number;
	// Sell: the bag slot, its price, currency and the anonymous choice, and
	// the bag picker's page.
	readonly sellSlot: number;
	readonly bagPage: number;
	readonly sellCurrency: MarketCurrency;
	readonly sellPrice: string;
	readonly anonymous: boolean;
	// My listings: the picked listing, the page and where a returned item goes.
	readonly mineSelected: number;
	readonly minePage: number;
	// History: its page.
	readonly historyPage: number;
	readonly returnTo: MarketDestination;
	// Offers: the list shown (received or made), the picked offer, "in:<id>"
	// or "out:<id>", and each list's page.
	readonly offerSide: "in" | "out";
	readonly offerSelected: string;
	readonly offerPages: { readonly in: number; readonly out: number; };
	readonly dialog: MarketDialog | null;
	readonly status: { readonly text: string; readonly ok: boolean; readonly until: number; } | null;
	// The last notice sequence the status line took.
	readonly noticeSeen: number;
}

/*
================
MarketActivation

What one click asks the coordinator for: the requests to send, and a
whisper to begin (the seller's or the buyer's name).
================
*/
export interface MarketActivation {
	readonly requests: readonly MarketRequest[];
	readonly whisper?: string;
}

/*
================
MarketContext

What a click is judged against: the worker's market view and the
local character's name (its own listings are never bought or offered on).
================
*/
export interface MarketContext {
	readonly market: MarketState | undefined;
	readonly localName: string;
	readonly now: number;
}

const CLOSED: MarketHudState = {
	open: false,
	tab: "mine",
	query: "",
	category: "",
	currency: "",
	sort: "newest",
	degree: 0,
	race: "",
	rarity: "",
	menu: "",
	page: 1,
	selected: 0,
	sellSlot: -1,
	bagPage: 0,
	sellCurrency: "gold",
	sellPrice: "",
	anonymous: false,
	mineSelected: 0,
	minePage: 0,
	historyPage: 0,
	returnTo: "bag",
	offerSide: "in",
	offerSelected: "",
	offerPages: { in: 0, out: 0 },
	dialog: null,
	status: null,
	noticeSeen: 0
};

/*
================
marketHistoryCount

The History page's entries: what the account bought and its listings
that sold.
================
*/
export function marketHistoryCount( mine: MarketState["mine"] | undefined ): number {
	return (mine?.purchases.length ?? 0) + (mine?.listings.length ?? 0) - marketShownListings( mine ).length;
}

/*
================
marketShownListings

My listings' rows: what is on sale or held for the seller. A sold or
returned listing is done and lives in History.
================
*/
export function marketShownListings( mine: MarketState["mine"] | undefined ) {
	return mine?.listings.filter( row => row.state === "open" || row.state === "held" ) ?? [];
}

/*
================
marketActiveListings

The account's listings that count toward the cap: open and held (the
store's countActiveListings).
================
*/
export function marketActiveListings( mine: MarketState["mine"] | undefined ): number {
	return marketShownListings( mine ).length;
}

/*
================
marketListingsFull

Whether the account is at its listing cap: the server refuses another
(store MarketList, ErrMarketCap), so the window says so before the click.
================
*/
export function marketListingsFull( mine: MarketState["mine"] | undefined ): boolean {
	return !!mine && marketActiveListings( mine ) >= mine.rules.openCap;
}

/*
================
marketSearchNarrowed

Whether the search line narrows the listings at all: a name, a category,
a degree or a filter. The empty result offers to clear only then.
================
*/
export function marketSearchNarrowed( s: MarketHudState ): boolean {
	return !!(s.query.trim() || s.category || s.degree || s.race || s.rarity || s.currency);
}

/*
================
marketBusy

A request is in flight: every write button waits for its answer. A read
counts too, because the worker sends one request at a time and would
drop a write clicked during a read without a word.
================
*/
export function marketBusy( market: MarketState | undefined ): boolean {
	return !!market?.pending;
}

/*
================
marketWriting

A write is in flight: the status line says so (a read is quick and
silent).
================
*/
export function marketWriting( market: MarketState | undefined ): boolean {
	return marketBusy( market ) && market?.pending !== "search" && market?.pending !== "mine";
}

/*
================
marketOfferPages

How many pages one offer list fills.
================
*/
export function marketOfferPages( offers: readonly unknown[] ): number {
	return Math.max( 1, Math.ceil( offers.length / MARKET_OFFER_ROWS ) );
}

/*
================
marketOwnRow

The local character's own listing: it shows "Yours" and offers no buy.
An anonymous listing of its own reads as its own only on My listings.
================
*/
export function marketOwnRow( row: MarketRow, localName: string ): boolean {
	return row.seller !== "" && row.seller === localName;
}

/*
================
marketPriceValue

A typed price as a number, 0 for none.
================
*/
export function marketPriceValue( draft: string ): number {
	return /^\d+$/.test( draft ) ? Number( draft ) : 0;
}

/*
================
marketOfferAt

The offer an "in:<id>" or "out:<id>" selection names.
================
*/
export function marketOfferAt( market: MarketState | undefined, selection: string ): MarketOffer | undefined {
	const [side, id] = selection.split( ":" );
	const list = side === "in" ? market?.mine?.offersIn : side === "out" ? market?.mine?.offersOut : undefined;
	return list?.find( offer => offer.id === Number( id ) );
}

/*
================
chooseFilter

The search drafts one dropdown choice sets, or null for a value outside
that menu's list (a choice id the window never drew).
================
*/
function chooseFilter( menu: MarketMenu, value: string ): Partial<MarketHudState> | null {
	switch ( menu ) {
		case "degree": {
			const degree = Number( value );
			return value !== "" && Number.isInteger( degree ) && degree >= 0 && degree <= MARKET_MAX_DEGREE ?
				{ degree } :
				null;
		}
		case "race":
			return MARKET_RACES.includes( value as MarketRace ) ? { race: value as MarketRace | "" } : null;
		case "rarity":
			return MARKET_RARITIES.includes( value as MarketRarity ) ? { rarity: value as MarketRarity | "" } : null;
		case "currency":
			return MARKET_CURRENCY_FILTERS.includes( value as MarketCurrency ) ?
				{ currency: value as MarketCurrency | "" } :
				null;
		case "sort":
			return MARKET_SORT_ORDER.includes( value as MarketSort ) ? { sort: value as MarketSort } : null;
	}
	return null;
}

/*
================
createMarketHud
================
*/
export function createMarketHud() {
	let s: MarketHudState = CLOSED;

	/*
	================
	search

	The search the drafts describe, at page.
	================
	*/
	const search = ( page: number ): MarketRequest => ({
		op: "search",
		q: s.query.trim().slice( 0, MARKET_QUERY_LIMIT ),
		...(s.category ? { category: s.category } : {}),
		...(s.degree ? { degree: s.degree } : {}),
		...(s.race ? { race: s.race } : {}),
		...(s.rarity ? { rarity: s.rarity } : {}),
		...(s.currency ? { currency: s.currency } : {}),
		sort: s.sort,
		page
	});

	/*
	================
	write

	A write the window may send now: none while another is in flight.
	================
	*/
	const write = ( market: MarketState | undefined, request: MarketRequest ): MarketActivation =>
		marketBusy( market ) ? { requests: [] } : { requests: [ request ] };

	/*
	================
	refresh

	The reads that show a new state: the account page, and the search at
	its page.
	================
	*/
	const refresh = (): MarketRequest[] => [ { op: "mine" }, search( s.page ) ];

	return {
		/*
		================
		open

		A fresh window on Browse, with the account page and the first search
		page asked for. The drafts of a previous opening are gone.
		================
		*/
		open(): readonly MarketRequest[] {
			s = { ...CLOSED, open: true, noticeSeen: s.noticeSeen };
			return refresh();
		},
		/*
		================
		close
		================
		*/
		close() {
			s = { ...CLOSED, noticeSeen: s.noticeSeen };
		},
		/*
		================
		isOpen
		================
		*/
		isOpen(): boolean {
			return s.open;
		},
		/*
		================
		state
		================
		*/
		state(): MarketHudState {
			return s;
		},
		/*
		================
		edit

		One typed field: the search, the sell price or the offer price.
		Returns false for a field this window does not own.
		================
		*/
		edit( id: string, value: string ): boolean {
			if ( !s.open ) return false;
			if ( id === "market-query" ) s = { ...s, query: value.slice( 0, MARKET_QUERY_LIMIT ) };
			else if ( id === "market-price" ) s = { ...s, sellPrice: marketPriceDraft( value ) };
			else if ( id === "market-offer-price" && s.dialog ) {
				s = { ...s, dialog: { ...s.dialog, price: marketPriceDraft( value ) } };
			} else return false;
			return true;
		},
		/*
		================
		observe

		Each frame: a new answer to a write goes on the status line, and a
		successful one refreshes what it changed. Returns the reads to send.
		================
		*/
		observe( market: MarketState | undefined, now: number ): readonly MarketRequest[] {
			const notice = market?.notice;
			if ( s.status && now >= s.status.until ) s = { ...s, status: null };
			if ( !notice || notice.sequence <= s.noticeSeen ) return [];
			s = { ...s, noticeSeen: notice.sequence };
			if ( !s.open ) return [];
			s = { ...s, status: { text: marketNoticeText( notice ), ok: notice.ok, until: now + MARKET_STATUS_MS } };
			if ( !notice.ok || !MARKET_WRITES.includes( notice.op ) ) return [];
			// The listing, the sold row and the answered offer are gone or changed.
			s = { ...s, dialog: null, selected: 0, mineSelected: 0, offerSelected: "" };
			if ( notice.op === "list" ) s = { ...s, sellSlot: -1, sellPrice: "" };
			return refresh();
		},
		/*
		================
		activate

		One click on a market control, or null for a control this window
		does not own.
		================
		*/
		activate( id: string, context: MarketContext ): MarketActivation | null {
			if ( !s.open || !id.startsWith( "market-" ) ) return null;
			const market = context.market, none: MarketActivation = { requests: [] };
			const browse = market?.browse, mine = market?.mine;
			if ( id === "market-close" ) {
				s = { ...CLOSED, noticeSeen: s.noticeSeen };
				return none;
			}
			if ( id.startsWith( "market-tab:" ) ) {
				const tab = id.slice( 11 ) as MarketTab;
				if ( MARKET_TABS.includes( tab ) ) s = { ...s, tab, dialog: null };
				return none;
			}
			if ( s.dialog ) {
				const dialog = s.dialog;
				if ( id === "market-dialog-cancel" ) s = { ...s, dialog: null };
				else if ( id === "market-dialog-to:bag" || id === "market-dialog-to:warehouse" ) {
					s = { ...s, dialog: { ...dialog, to: id.slice( 17 ) as MarketDestination } };
				} else if ( id === "market-dialog-confirm" ) {
					if ( dialog.kind === "buy" ) {
						return write( market, {
							op: "buy",
							listing: dialog.listing.id,
							version: dialog.listing.version,
							to: dialog.to
						} );
					}
					const price = marketPriceValue( dialog.price );
					// The server refuses an offer at or above the price; the window
					// never sends one.
					if ( price < 1 || price >= dialog.listing.price ) return none;
					return write( market, { op: "offer", listing: dialog.listing.id, price, to: dialog.to } );
				}
				// A dialog is modal: the rest of the window waits for it.
				return none;
			}
			if ( id === "market-search" ) {
				s = { ...s, page: 1, selected: 0 };
				return { requests: [ search( 1 ) ] };
			}
			// The empty result's way out: every narrowing back to its default.
			if ( id === "market-clear-search" ) {
				s = {
					...s,
					query: "",
					category: "",
					degree: 0,
					race: "",
					rarity: "",
					currency: "",
					page: 1,
					selected: 0
				};
				return { requests: [ search( 1 ) ] };
			}
			// The dropdowns: a button opens its list, a choice applies and
			// searches, and any other click closes the list.
			if ( id.startsWith( "market-menu:" ) ) {
				const menu = id.slice( 12 ) as MarketMenu;
				if ( MARKET_MENUS.includes( menu ) ) s = { ...s, menu: s.menu === menu ? "" : menu };
				return none;
			}
			if ( id.startsWith( "market-choose:" ) ) {
				const rest = id.slice( 14 ), colon = rest.indexOf( ":" );
				const chosen = chooseFilter( rest.slice( 0, colon ) as MarketMenu, rest.slice( colon + 1 ) );
				if ( !chosen ) return none;
				s = { ...s, ...chosen, menu: "", page: 1, selected: 0 };
				return { requests: [ search( 1 ) ] };
			}
			if ( s.menu ) s = { ...s, menu: "" };
			// The Price header flips between cheapest and dearest first.
			if ( id === "market-sort-price" ) {
				s = { ...s, sort: s.sort === "price-asc" ? "price-desc" : "price-asc", page: 1, selected: 0 };
				return { requests: [ search( 1 ) ] };
			}
			if ( id.startsWith( "market-category:" ) ) {
				const slug = id.slice( 16 );
				if ( slug && !market?.categories.some( category => category.slug === slug ) ) return none;
				s = { ...s, category: slug, page: 1, selected: 0 };
				return { requests: [ search( 1 ) ] };
			}
			if ( id === "market-prev" || id === "market-next" ) {
				const pages = Math.min( MARKET_MAX_PAGE, Math.max( 1, browse?.pages ?? 1 ) );
				const page = Math.min( pages, Math.max( 1, s.page + (id === "market-next" ? 1 : -1) ) );
				if ( page === s.page ) return none;
				s = { ...s, page, selected: 0 };
				return { requests: [ search( page ) ] };
			}
			if ( id.startsWith( "market-pick:" ) || id.startsWith( "market-item:browse:" ) ) {
				const listing = Number( id.slice( id.lastIndexOf( ":" ) + 1 ) );
				if ( browse?.rows.some( row => row.id === listing ) ) s = { ...s, selected: listing };
				return none;
			}
			const rowAction = /^market-row-(buy|offer|whisper):(\d+)$/.exec( id );
			if ( rowAction ) {
				const listing = Number( rowAction[2] );
				if ( !browse?.rows.some( row => row.id === listing ) ) return none;
				s = { ...s, selected: listing };
				id = "market-" + rowAction[1];
			}
			if ( id === "market-buy" || id === "market-offer" ) {
				const row = browse?.rows.find( r => r.id === s.selected );
				if ( !row || row.state !== "open" || marketOwnRow( row, context.localName ) || marketBusy( market ) ) {
					return none;
				}
				s = {
					...s,
					dialog: { kind: id === "market-buy" ? "buy" : "offer", listing: row, to: "bag", price: "" }
				};
				return none;
			}
			if ( id === "market-whisper" ) {
				const row = browse?.rows.find( r => r.id === s.selected );
				return row?.seller && !marketOwnRow( row, context.localName ) ?
					{ requests: [], whisper: row.seller } :
					none;
			}
			// Sell: picking an item closes the bag picker.
			if ( id.startsWith( "market-sell-slot:" ) ) {
				s = { ...s, sellSlot: Number( id.slice( 17 ) ), tab: s.tab === "sell" ? "mine" : s.tab };
				return none;
			}
			if ( id === "market-sell-pick" ) {
				s = { ...s, tab: "sell", dialog: null };
				return none;
			}
			if ( id === "market-sell-clear" ) {
				s = { ...s, sellSlot: -1 };
				return none;
			}
			if ( id === "market-bag-prev" || id === "market-bag-next" ) {
				s = { ...s, bagPage: Math.max( 0, s.bagPage + (id === "market-bag-next" ? 1 : -1) ) };
				return none;
			}
			if ( id === "market-sell-currency:gold" || id === "market-sell-currency:silk" ) {
				s = { ...s, sellCurrency: id.slice( 21 ) as MarketCurrency };
				return none;
			}
			if ( id === "market-anonymous" ) {
				s = { ...s, anonymous: !s.anonymous };
				return none;
			}
			if ( id === "market-list" ) {
				const price = marketPriceValue( s.sellPrice );
				if ( s.sellSlot < 0 || price < 1 || marketListingsFull( mine ) ) return none;
				return write( market, {
					op: "list",
					slot: s.sellSlot,
					currency: s.sellCurrency,
					price,
					anonymous: s.anonymous
				} );
			}
			// My listings.
			if ( id.startsWith( "market-mine:" ) || id.startsWith( "market-item:mine:" ) ) {
				const listing = Number( id.slice( id.lastIndexOf( ":" ) + 1 ) );
				if ( mine?.listings.some( row => row.id === listing ) ) s = { ...s, mineSelected: listing };
				return none;
			}
			if ( id === "market-mine-prev" || id === "market-mine-next" ) {
				const pages = Math.max( 1, Math.ceil( marketShownListings( mine ).length / MARKET_MINE_ROWS ) );
				s = {
					...s,
					minePage: Math.min( pages - 1, Math.max( 0, s.minePage + (id === "market-mine-next" ? 1 : -1) ) )
				};
				return none;
			}
			if ( id === "market-history-prev" || id === "market-history-next" ) {
				const pages = Math.max( 1, Math.ceil( marketHistoryCount( mine ) / MARKET_HISTORY_ROWS ) );
				s = {
					...s,
					historyPage: Math.min(
						pages - 1,
						Math.max( 0, s.historyPage + (id === "market-history-next" ? 1 : -1) )
					)
				};
				return none;
			}
			if ( id === "market-return-to:bag" || id === "market-return-to:warehouse" ) {
				s = { ...s, returnTo: id.slice( 17 ) as MarketDestination };
				return none;
			}
			if ( id === "market-cancel" || id === "market-takeback" ) {
				const row = mine?.listings.find( r => r.id === s.mineSelected );
				const op = row?.state === "open" ? "cancel" : row?.state === "held" ? "takeback" : null;
				if ( !row || op === null || op !== id.slice( 7 ) ) return none;
				return write( market, { op, listing: row.id, to: s.returnTo } );
			}
			// Offers.
			const pager = /^market-offers-(in|out)-(prev|next)$/.exec( id );
			if ( pager ) {
				const side = pager[1] as "in" | "out";
				const pages = marketOfferPages( (side === "in" ? mine?.offersIn : mine?.offersOut) ?? [] );
				const page = Math.min( pages - 1, Math.max( 0, s.offerPages[side] + (pager[2] === "next" ? 1 : -1) ) );
				s = { ...s, offerPages: { ...s.offerPages, [side]: page } };
				return none;
			}
			if ( id === "market-offer-side:in" || id === "market-offer-side:out" ) {
				s = { ...s, offerSide: id.slice( 18 ) as "in" | "out", offerSelected: "" };
				return none;
			}
			if ( id.startsWith( "market-offer-in:" ) || id.startsWith( "market-offer-out:" ) ) {
				const selection = id.slice( 13 );
				s = { ...s, offerSelected: selection, offerSide: selection.startsWith( "in:" ) ? "in" : "out" };
				return none;
			}
			if ( id.startsWith( "market-item:in:" ) || id.startsWith( "market-item:out:" ) ) {
				const selection = id.slice( 12 );
				s = { ...s, offerSelected: selection, offerSide: selection.startsWith( "in:" ) ? "in" : "out" };
				return none;
			}
			const offer = marketOfferAt( market, s.offerSelected );
			if ( id === "market-accept" || id === "market-reject" ) {
				if ( !offer || offer.state !== "open" || !s.offerSelected.startsWith( "in:" ) ) return none;
				return write( market, { op: id === "market-accept" ? "accept" : "reject", offer: offer.id } );
			}
			if ( id === "market-withdraw" ) {
				if ( !offer || offer.state !== "open" || !s.offerSelected.startsWith( "out:" ) ) return none;
				return write( market, { op: "withdraw", offer: offer.id } );
			}
			if ( id === "market-offer-whisper" ) {
				return offer?.buyer && s.offerSelected.startsWith( "in:" ) ?
					{ requests: [], whisper: offer.buyer } :
					none;
			}
			return none;
		}
	};
}

export type MarketHud = ReturnType<typeof createMarketHud>;
