/*
===========================================================================

market.ts - the consignment market's publication (opensro#3529)

Port-only, not native (SRO_CONSIGNMENT on the server, published to the
client at world entry): v1.150 has no consignment. The
worker decodes the server's market control answers (foundation/gameplay
market.ts) and publishes what the window shows; the window's drafts
(search text, price, tab, the open dialog) are the HUD's own
(runtime/ui/hud/market-hud.ts).

A listing names its seller unless the seller chose to list it
anonymously; the buyer picks its bag or its warehouse for every item it
receives; a gold sale pays the listing character.

===========================================================================
*/
import type { InventoryItem } from "./gameplay";

// The currencies a listing is priced in (domain.MarketGold, MarketSilk).
export type MarketCurrency = "gold" | "silk";

// Where a bought, accepted or returned item goes (domain.MarketToBag,
// MarketToWarehouse).
export type MarketDestination = "bag" | "warehouse";

// A listing's states: open is buyable; sold and returned are final; held
// is a returned item that found the warehouse full and waits to be taken
// back.
export type MarketListingState = "open" | "sold" | "returned" | "held";

// An offer's states (domain.MarketOffer*): only open can be answered.
export type MarketOfferState = "open" | "accepted" | "rejected" | "withdrawn" | "closed";

// The search orders (publicstats MarketSort*).
export type MarketSort = "newest" | "ending" | "price-asc" | "price-desc";

// The requests the window sends (action market.go marketOp*).
export type MarketOp =
	| "search"
	| "mine"
	| "list"
	| "buy"
	| "cancel"
	| "takeback"
	| "offer"
	| "accept"
	| "reject"
	| "withdraw";

// What the server tells a party unasked (action market.go marketEvent*).
export type MarketEventKind = "sold" | "offer" | "offer-accepted" | "offer-rejected" | "offer-withdrawn";

/*
================
MarketRow

One listing: the server's numbers and the item as the shop previews it,
so the window draws the native icon and tooltip. Seller is the listing
character's name, empty when it listed anonymously; anonymous is set
only on the seller's own rows.
================
*/
export interface MarketRow {
	readonly id: number;
	readonly version: number;
	readonly currency: MarketCurrency;
	readonly price: number;
	readonly listedMs: number;
	readonly expiresMs: number;
	readonly state: MarketListingState;
	readonly seller: string;
	readonly anonymous: boolean;
	readonly category: string;
	readonly degree: number;
	// A sealed (rare) item.
	readonly sealed: boolean;
	readonly item: InventoryItem;
}

/*
================
MarketOffer

One offer on a listing: the asked price beside the offered one, who
offered (the seller's view) and where the item goes if it is accepted.
================
*/
export interface MarketOffer {
	readonly id: number;
	readonly listingId: number;
	readonly currency: MarketCurrency;
	readonly asked: number;
	readonly price: number;
	readonly buyer: string;
	readonly to: MarketDestination;
	readonly state: MarketOfferState;
	readonly atMs: number;
	readonly item: InventoryItem;
}

/*
================
MarketCategory
================
*/
export interface MarketCategory {
	readonly slug: string;
	readonly name: string;
}

/*
================
MarketPurchase
================
*/
export interface MarketPurchase {
	readonly listingId: number;
	readonly currency: MarketCurrency;
	readonly price: number;
	readonly fee: number;
	readonly atMs: number;
	readonly to: MarketDestination;
	readonly item: string;
}

/*
================
MarketRules

The server's numbers; the window writes none of its own.
================
*/
export interface MarketRules {
	readonly feePercent: number;
	readonly durationMs: number;
	readonly openCap: number;
	readonly offerCap: number;
}

/*
================
MarketBrowse

The last search page the server answered.
================
*/
export interface MarketBrowse {
	readonly rows: readonly MarketRow[];
	readonly page: number;
	readonly pages: number;
	readonly total: number;
}

/*
================
MarketMine

The account's side: its listings, the offers on them and the ones it
made, its purchases, its Silk and its warehouse gold.
================
*/
export interface MarketMine {
	readonly listings: readonly MarketRow[];
	readonly offersIn: readonly MarketOffer[];
	readonly offersOut: readonly MarketOffer[];
	readonly purchases: readonly MarketPurchase[];
	readonly silk: number;
	readonly warehouseGold: number;
	readonly rules: MarketRules;
}

/*
================
MarketNotice

The outcome of the last write the window sent: ok, or the refusal's
code (the site's codes plus the lane's own). To is where a delivered
item went. Sequence rises on every answer, so the window shows each
outcome once.
================
*/
export interface MarketNotice {
	readonly op: MarketOp;
	readonly ok: boolean;
	readonly code: string;
	readonly to: MarketDestination | "";
	readonly sequence: number;
}

/*
================
MarketEvent

What happened to this character's listings or offers while the window
did not ask: a sale, an offer, an answer to its offer. Sequence rises
with each, so the chat prints each once.
================
*/
export interface MarketEvent {
	readonly kind: MarketEventKind;
	readonly item: string;
	readonly currency: MarketCurrency;
	readonly price: number;
	readonly paid: number;
	readonly who: string;
	readonly sequence: number;
}

/*
================
MarketState
================
*/
export interface MarketState {
	readonly categories: readonly MarketCategory[];
	readonly browse: MarketBrowse | null;
	readonly mine: MarketMine | null;
	// The op awaiting its answer, or null.
	readonly pending: MarketOp | null;
	readonly notice: MarketNotice | null;
	// The latest events, oldest first (foundation market.ts keeps a few).
	readonly events: readonly MarketEvent[];
}

/*
================
MarketRequest

One window request. Which fields count follows the op (action market.go
marketRequest); the worker checks each before it is sent.
================
*/
// The search's race (who wears it) and rarity filters (publicstats
// market.go MarketRace* / MarketRarity*).
export type MarketRace = "ch" | "eu";
export type MarketRarity = "sealed" | "normal";

export type MarketRequest =
	| {
		readonly op: "search";
		readonly q?: string;
		readonly category?: string;
		readonly degree?: number;
		readonly race?: MarketRace;
		readonly rarity?: MarketRarity;
		readonly currency?: MarketCurrency;
		readonly sort?: MarketSort;
		readonly page?: number;
	}
	| { readonly op: "mine"; }
	| {
		readonly op: "list";
		readonly slot: number;
		readonly currency: MarketCurrency;
		readonly price: number;
		readonly anonymous: boolean;
	}
	| { readonly op: "buy"; readonly listing: number; readonly version: number; readonly to: MarketDestination; }
	| { readonly op: "cancel" | "takeback"; readonly listing: number; readonly to: MarketDestination; }
	| { readonly op: "offer"; readonly listing: number; readonly price: number; readonly to: MarketDestination; }
	| { readonly op: "accept" | "reject" | "withdraw"; readonly offer: number; };
