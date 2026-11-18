/*
===========================================================================

market-hud.ts - the consignment market window's drafts and its requests

Port-only, not native (SRO_CONSIGNMENT on the server, the Experimental
"Consignment market" row): v1.150 has no consignment. The window opens
from a storage keeper's menu and has four tabs - Browse, Sell, My
listings, Offers - and one dialog at a time for a buy or an offer, each
of which asks where the item goes (the inventory or the warehouse).

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
	MarketRequest,
	MarketRow,
	MarketSort,
	MarketState
} from "@/engine/contracts/market";
import {
	MARKET_MAX_PAGE,
	MARKET_QUERY_LIMIT,
	marketNoticeText,
	marketPriceDraft
} from "@/engine/foundation/gameplay/market";

export type MarketTab = "browse" | "sell" | "mine" | "offers";

// The tabs in the order the window shows them.
export const MARKET_TABS: readonly MarketTab[] = [ "browse", "sell", "mine", "offers" ];
// The search orders in the order the sort button steps through them.
export const MARKET_SORT_ORDER: readonly MarketSort[] = [ "newest", "ending", "price-asc", "price-desc" ];
// The currency filter's steps: any, then each currency.
export const MARKET_CURRENCY_FILTERS: readonly (MarketCurrency | "")[] = [ "", "gold", "silk" ];
// A refusal or a success stays on the window's status line this long.
export const MARKET_STATUS_MS = 8000;
// The rows one page of the account's own lists shows.
export const MARKET_MINE_ROWS = 14;
// The rows each offer list shows.
export const MARKET_OFFER_ROWS = 6;
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
	readonly page: number;
	readonly selected: number;
	// Sell: the bag slot, its price, currency and the anonymous choice.
	readonly sellSlot: number;
	readonly sellCurrency: MarketCurrency;
	readonly sellPrice: string;
	readonly anonymous: boolean;
	// My listings: the picked listing, the page and where a returned item goes.
	readonly mineSelected: number;
	readonly minePage: number;
	readonly returnTo: MarketDestination;
	// Offers: the picked offer, "in:<id>" or "out:<id>".
	readonly offerSelected: string;
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
	tab: "browse",
	query: "",
	category: "",
	currency: "",
	sort: "newest",
	page: 1,
	selected: 0,
	sellSlot: -1,
	sellCurrency: "gold",
	sellPrice: "",
	anonymous: false,
	mineSelected: 0,
	minePage: 0,
	returnTo: "bag",
	offerSelected: "",
	dialog: null,
	status: null,
	noticeSeen: 0
};

/*
================
marketBusy

A write is in flight: every write button waits for its answer.
================
*/
export function marketBusy( market: MarketState | undefined ): boolean {
	return !!market?.pending && market.pending !== "search" && market.pending !== "mine";
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
			if ( id === "market-sort" ) {
				const next = MARKET_SORT_ORDER[(MARKET_SORT_ORDER.indexOf( s.sort ) + 1) % MARKET_SORT_ORDER.length]!;
				s = { ...s, sort: next, page: 1, selected: 0 };
				return { requests: [ search( 1 ) ] };
			}
			if ( id === "market-currency" ) {
				const steps = MARKET_CURRENCY_FILTERS;
				s = { ...s, currency: steps[(steps.indexOf( s.currency ) + 1) % steps.length]!, page: 1, selected: 0 };
				return { requests: [ search( 1 ) ] };
			}
			if ( id === "market-category" ) {
				const slugs = [ "", ...(market?.categories ?? []).map( category => category.slug ) ];
				s = { ...s, category: slugs[(slugs.indexOf( s.category ) + 1) % slugs.length]!, page: 1, selected: 0 };
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
			// Sell.
			if ( id.startsWith( "market-sell-slot:" ) ) {
				s = { ...s, sellSlot: Number( id.slice( 17 ) ) };
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
				if ( s.sellSlot < 0 || price < 1 ) return none;
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
				const pages = Math.max( 1, Math.ceil( (mine?.listings.length ?? 0) / MARKET_MINE_ROWS ) );
				s = {
					...s,
					minePage: Math.min( pages - 1, Math.max( 0, s.minePage + (id === "market-mine-next" ? 1 : -1) ) )
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
			if ( id.startsWith( "market-offer-in:" ) || id.startsWith( "market-offer-out:" ) ) {
				s = { ...s, offerSelected: id.slice( 13 ) };
				return none;
			}
			if ( id.startsWith( "market-item:in:" ) || id.startsWith( "market-item:out:" ) ) {
				s = { ...s, offerSelected: id.slice( 12 ) };
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
