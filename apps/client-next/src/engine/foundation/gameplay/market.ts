/*
===========================================================================

market.ts - the consignment market's wire and rules (opensro#3529)

Port-only, not native (SRO_CONSIGNMENT): v1.150 has no consignment. The
window's requests and the server's answers ride MARKET_CONTROL as JSON
(server internal/game/action/market.go). createMarket checks a request
before it leaves, decodes each answer as a whole (a malformed answer
throws, as every wire violation in this session does) and keeps what the
window shows. The escrow is the server's: nothing here moves an item or
a coin. An item delivered into the bag arrives as the native pickup
result, and a listed bag item leaves as the native drop result, so the
inventory follows them on its usual path.

The small rules the window shows (the seller's fee, the time left, a
refusal's line, an event's line) live here too, so the HUD only lays
them out.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type {
	MarketBrowse,
	MarketCategory,
	MarketCurrency,
	MarketDestination,
	MarketEvent,
	MarketEventKind,
	MarketMine,
	MarketNotice,
	MarketOffer,
	MarketOp,
	MarketPurchase,
	MarketRequest,
	MarketRow,
	MarketState
} from "@/engine/contracts/market";
import type { CommercePreview } from "./commerce";
import { commerceInteger } from "./commerce";
import { MARKET_CONTROL } from "./commerce-controls";

// The answer layout this client reads (action market.go marketAnswerVersion).
const MARKET_ANSWER_VERSION = 2;
// An answer never exceeds this many bytes: 150 rows of previews at most.
const MARKET_ANSWER_LIMIT = 1048576;
// The server's bounds (publicstats market.go): a page of 15, at most 100
// pages, a degree up to 11, a 64-character search.
export const MARKET_PAGE_SIZE = 15;
export const MARKET_MAX_PAGE = 100;
export const MARKET_MAX_DEGREE = 11;
export const MARKET_QUERY_LIMIT = 64;
// A price the window accepts: twelve digits stays a safe integer and far
// above any gold a character carries; the server judges the real bound.
export const MARKET_PRICE_DIGITS = 12;
const MARKET_MAX_PRICE = 999_999_999_999;
// The rows of one list on the account page (game/market historyLimit).
const MARKET_ROW_LIMIT = 50;
// The account page previews its listings and both offer lists in one
// reference frame, so an item's preview slot runs up to three lists
// (3 x MARKET_ROW_LIMIT).
const MARKET_PREVIEW_LIMIT = 150;
// A request answered by nothing within this long frees the window again.
const MARKET_REQUEST_TIMEOUT_MS = 15000;
// Bag slots start past the 13 equipment sockets; the server judges the
// bag's end.
const MARKET_FIRST_BAG_SLOT = 13;
const MARKET_LAST_SLOT = 255;
// The events kept for the chat; older ones were printed long ago.
export const MARKET_EVENT_LIMIT = 16;
// One answer's events at most (a claim pays a character's sales at once).
const MARKET_EVENTS_PER_ANSWER = 64;
// A character or item name on the wire.
const MARKET_NAME_LIMIT = 256;

const MARKET_OPS = [
	"search",
	"mine",
	"list",
	"buy",
	"cancel",
	"takeback",
	"offer",
	"accept",
	"reject",
	"withdraw"
] as const;
const MARKET_SORTS = [ "newest", "ending", "price-asc", "price-desc" ] as const;
const MARKET_STATES = [ "open", "sold", "returned", "held" ] as const;
const MARKET_OFFER_STATES = [ "open", "accepted", "rejected", "withdrawn", "closed" ] as const;
const MARKET_EVENT_KINDS = [
	"sold",
	"offer",
	"offer-accepted",
	"offer-rejected",
	"offer-withdrawn"
] as const;
// The pushed answer that carries events (action market.go marketOpEvent).
const MARKET_EVENT_OP = "event";

// What each refusal tells the player. The codes are the site's
// (publicstats marketError) plus the lane's own (action market.go).
const MARKET_REFUSALS = {
	"unavailable": "The consignment market is closed right now.",
	"request": "That request could not be sent.",
	"busy": "Close your stall or exchange first.",
	"buyer-busy": "The buyer is trading right now. Try again in a moment.",
	"no-item": "That item is no longer in your inventory.",
	"not-found": "That listing is gone.",
	"changed": "Someone else bought it first, or the listing changed.",
	"warehouse-full": "Your warehouse is full. Make room first.",
	"bag-full": "Your inventory is full. Make room or send it to the warehouse.",
	"not-held": "That item is not waiting to be taken back.",
	"cap": "You already have the most listings allowed. Items waiting in My listings count too.",
	"own-listing": "That is your own listing.",
	"not-tradeable": "That item cannot be traded.",
	"funds": "You do not have enough to pay for it.",
	"price": "Enter a price the market accepts.",
	"no-character": "That character cannot pay.",
	"online": "That character is in the world. Do it in game.",
	"offer-price": "An offer must be below the asking price.",
	"offer-open": "You already have an offer on this listing.",
	"offer-cap": "You already have the most open offers allowed.",
	"offer-gone": "That offer is no longer open.",
	"buyer-funds": "The buyer can no longer pay. The offer was closed.",
	"buyer-full": "The buyer's inventory and warehouse are both full."
} as const;

/*
================
marketDestinationText
================
*/
function marketDestinationText( to: MarketDestination | "" ): string {
	return to === "bag" ? "your inventory" : "your warehouse";
}

/*
================
marketNoticeText
================
*/
export function marketNoticeText( notice: Pick<MarketNotice, "op" | "ok" | "code" | "to"> ): string {
	if ( !notice.ok ) {
		return (MARKET_REFUSALS as Readonly<Record<string, string>>)[notice.code] ?? MARKET_REFUSALS.unavailable;
	}
	switch ( notice.op ) {
		case "list":
			return "Listed. The market holds the item until it sells.";
		case "buy":
			return `Bought. The item is in ${marketDestinationText( notice.to )}.`;
		case "cancel":
			return `Listing cancelled. The item is back in ${marketDestinationText( notice.to )}.`;
		case "takeback":
			return `Taken back into ${marketDestinationText( notice.to )}.`;
		case "offer":
			return "Offer sent. The seller can accept or decline it.";
		case "accept":
			return "Offer accepted. The item is sold.";
		case "reject":
			return "Offer declined.";
		case "withdraw":
			return "Offer withdrawn.";
		default:
			return "";
	}
}

/*
================
marketCurrencyText
================
*/
export function marketCurrencyText( currency: MarketCurrency ): string {
	return currency === "silk" ? "Silk" : "Gold";
}

// A market event's colour in the bottom-right message list: bright green,
// apart from the tan game lines and the blue tips, so a sale or an offer
// is seen at a glance (port-only, #3529).
export const MARKET_NOTICE_ARGB = 0xff7cfc9a;

/*
================
marketEventText

The chat line of one event. A gold sale's line says the gold reached
the character; a Silk sale's, the account.
================
*/
export function marketEventText(
	event: Pick<MarketEvent, "kind" | "item" | "currency" | "price" | "paid" | "who">
): string {
	const price = `${marketAmount( event.price )} ${marketCurrencyText( event.currency )}`;
	const who = event.who || "A buyer";
	switch ( event.kind ) {
		case "sold":
			return `[Market] ${event.item} sold for ${price}. You received ${marketAmount( event.paid )} ${
				marketCurrencyText( event.currency )
			} after the fee.`;
		case "offer":
			return `[Market] ${who} offers ${price} for your ${event.item}.`;
		case "offer-accepted":
			return `[Market] Your offer of ${price} for ${event.item} was accepted.`;
		case "offer-rejected":
			return `[Market] Your offer of ${price} for ${event.item} was declined.`;
		case "offer-withdrawn":
			return `[Market] ${who} withdrew the offer of ${price} for your ${event.item}.`;
	}
}

/*
================
marketFee

The seller's fee on a sale at price (domain.MarketRules.MarketFee): whole
units, rounded down, so a small sale can cost nothing.
================
*/
export function marketFee( price: number, feePercent: number ): number {
	return Math.floor( price * feePercent / 100 );
}

/*
================
marketTimeLeft

A listing's time left as the window prints it: hours and minutes, or
"Ending" in its last minute.
================
*/
export function marketTimeLeft( expiresMs: number, nowMs: number ): string {
	const minutes = Math.floor( Math.max( 0, expiresMs - nowMs ) / 60000 );
	if ( minutes < 1 ) return "Ending";
	const hours = Math.floor( minutes / 60 );
	return hours > 0 ? `${hours}h ${minutes % 60}m` : `${minutes}m`;
}

/*
================
marketAmount

A price with thousands separators, the way the native gold fields print.
================
*/
export function marketAmount( value: number ): string {
	return Math.max( 0, Math.trunc( value ) ).toString().replace( /\B(?=(\d{3})+(?!\d))/g, "," );
}

/*
================
marketPriceDraft

A typed price kept to digits and to MARKET_PRICE_DIGITS, without leading
zeros: what the price field holds.
================
*/
export function marketPriceDraft( typed: string ): string {
	return typed.replace( /[^0-9]/g, "" ).replace( /^0+/, "" ).slice( 0, MARKET_PRICE_DIGITS );
}

/*
================
marketRequestPayload

The wire body of a request, or an Error for one the server would refuse
anyway: the window never sends a request it knows is malformed.
================
*/
export function marketRequestPayload( request: MarketRequest ): Uint8Array {
	const body: Record<string, unknown> = { op: request.op };
	const idOf = ( id: number, what: string ) => {
		if ( !Number.isSafeInteger( id ) || id < 1 ) throw Error( `Invalid market ${what}` );
		return id;
	};
	const priceOf = ( price: number ) => {
		if ( !Number.isSafeInteger( price ) || price < 1 || price > MARKET_MAX_PRICE ) {
			throw Error( "Invalid market price" );
		}
		return price;
	};
	const toOf = ( to: MarketDestination ) => {
		if ( to !== "bag" && to !== "warehouse" ) throw Error( "Invalid market destination" );
		return to;
	};
	switch ( request.op ) {
		case "search": {
			const q = (request.q ?? "").trim();
			if ( q.length > MARKET_QUERY_LIMIT ) throw Error( "Market search too long" );
			const page = request.page ?? 1;
			if ( !Number.isInteger( page ) || page < 1 || page > MARKET_MAX_PAGE ) throw Error( "Invalid market page" );
			if ( q ) body.q = q;
			if ( request.category ) body.category = request.category;
			if ( request.degree !== undefined ) {
				const degree = request.degree;
				if ( !Number.isInteger( degree ) || degree < 0 || degree > MARKET_MAX_DEGREE ) {
					throw Error( "Invalid market degree" );
				}
				if ( degree ) body.degree = degree;
			}
			if ( request.race ) {
				if ( request.race !== "ch" && request.race !== "eu" ) throw Error( "Invalid market race" );
				body.race = request.race;
			}
			if ( request.rarity ) {
				if ( request.rarity !== "sealed" && request.rarity !== "normal" ) {
					throw Error( "Invalid market rarity" );
				}
				body.rarity = request.rarity;
			}
			if ( request.currency ) body.currency = request.currency;
			if ( request.sort ) body.sort = request.sort;
			body.page = page;
			break;
		}
		case "mine":
			break;
		case "list":
			if (
				!Number.isInteger( request.slot ) || request.slot < MARKET_FIRST_BAG_SLOT ||
				request.slot > MARKET_LAST_SLOT
			) {
				throw Error( "Invalid market slot" );
			}
			body.slot = request.slot;
			body.currency = request.currency;
			body.price = priceOf( request.price );
			if ( request.anonymous ) body.anonymous = true;
			break;
		case "buy":
			body.listing = idOf( request.listing, "listing" );
			if ( !Number.isSafeInteger( request.version ) || request.version < 0 ) {
				throw Error( "Invalid market version" );
			}
			body.version = request.version;
			body.to = toOf( request.to );
			break;
		case "cancel":
		case "takeback":
			body.listing = idOf( request.listing, "listing" );
			body.to = toOf( request.to );
			break;
		case "offer":
			body.listing = idOf( request.listing, "listing" );
			body.price = priceOf( request.price );
			body.to = toOf( request.to );
			break;
		case "accept":
		case "reject":
		case "withdraw":
			body.offer = idOf( request.offer, "offer" );
			break;
	}
	return new TextEncoder().encode( JSON.stringify( body ) );
}

/*
================
decodeCurrency
================
*/
function decodeCurrency( value: unknown ): MarketCurrency {
	if ( value !== "gold" && value !== "silk" ) throw Error( "Invalid market currency" );
	return value;
}

/*
================
decodeDestination
================
*/
function decodeDestination( value: unknown ): MarketDestination {
	if ( value !== "bag" && value !== "warehouse" ) throw Error( "Invalid market destination" );
	return value;
}

/*
================
decodeName

A name the server sent, or "" when it sent none (an anonymous seller).
================
*/
function decodeName( value: unknown ): string {
	if ( value === undefined ) return "";
	if ( typeof value !== "string" || value.length > MARKET_NAME_LIMIT ) throw Error( "Invalid market name" );
	return value;
}

/*
================
decodePurchase
================
*/
function decodePurchase( value: unknown ): MarketPurchase {
	const p = value as Record<string, unknown> | null;
	if ( !p || typeof p !== "object" ) throw Error( "Invalid market purchase" );
	return {
		listingId: commerceInteger( p.listingId, Number.MAX_SAFE_INTEGER, 1 ),
		currency: decodeCurrency( p.currency ),
		price: commerceInteger( p.price, Number.MAX_SAFE_INTEGER ),
		fee: commerceInteger( p.fee, Number.MAX_SAFE_INTEGER ),
		atMs: commerceInteger( p.atMs, Number.MAX_SAFE_INTEGER ),
		to: decodeDestination( p.to ),
		item: decodeName( p.item )
	};
}

/*
================
decodeEvent
================
*/
function decodeEvent( value: unknown, sequence: number ): MarketEvent {
	const e = value as Record<string, unknown> | null;
	if ( !e || typeof e !== "object" || !(MARKET_EVENT_KINDS as readonly unknown[]).includes( e.kind ) ) {
		throw Error( "Invalid market event" );
	}
	return {
		kind: e.kind as MarketEventKind,
		item: decodeName( e.item ),
		currency: decodeCurrency( e.currency ),
		price: commerceInteger( e.price, Number.MAX_SAFE_INTEGER ),
		paid: e.paid === undefined ? 0 : commerceInteger( e.paid, Number.MAX_SAFE_INTEGER ),
		who: decodeName( e.who ),
		sequence
	};
}

/*
================
createMarket
================
*/
export function createMarket( preview: ( row: CommercePreview ) => InventoryItem ) {
	let categories: readonly MarketCategory[] = [];
	let browse: MarketBrowse | null = null, mine: MarketMine | null = null, notice: MarketNotice | null = null;
	let events: readonly MarketEvent[] = [];
	let pending: MarketOp | null = null, pendingTo: MarketDestination | "" = "", deadline = 0, sequence = 0;
	let eventSequence = 0;

	/*
	================
	decodeItem

	A previewed item, checked against the reference it names.
	================
	*/
	const decodeItem = ( value: unknown ): InventoryItem => {
		const item = value as Record<string, unknown> | null;
		if (
			!item || typeof item !== "object" || typeof item.name !== "string" ||
			item.name.length > MARKET_NAME_LIMIT ||
			!Array.isArray( item.body ) || item.body.length > 2048
		) {
			throw Error( "Invalid market item" );
		}
		commerceInteger( item.slot, MARKET_PREVIEW_LIMIT - 1 );
		const refObjId = commerceInteger( item.refObjId, 0xffffffff, 1 );
		commerceInteger( item.typeFlags, 65535 );
		for ( const b of item.body ) commerceInteger( b, 255 );
		const shown = preview( item as unknown as CommercePreview );
		if ( shown.refObjId !== refObjId ) throw Error( "Market preview reference mismatch" );
		return shown;
	};

	/*
	================
	decodeRow
	================
	*/
	const decodeRow = ( value: unknown ): MarketRow => {
		const r = value as Record<string, unknown> | null;
		if (
			!r || typeof r !== "object" || typeof r.category !== "string" || r.category.length > 32 ||
			!MARKET_STATES.includes( r.state as MarketRow["state"] ) ||
			(r.anonymous !== undefined && typeof r.anonymous !== "boolean")
		) {
			throw Error( "Invalid market row" );
		}
		return {
			id: commerceInteger( r.id, Number.MAX_SAFE_INTEGER, 1 ),
			version: commerceInteger( r.version, Number.MAX_SAFE_INTEGER ),
			currency: decodeCurrency( r.currency ),
			price: commerceInteger( r.price, Number.MAX_SAFE_INTEGER ),
			listedMs: commerceInteger( r.listedMs, Number.MAX_SAFE_INTEGER ),
			expiresMs: commerceInteger( r.expiresMs, Number.MAX_SAFE_INTEGER ),
			state: r.state as MarketRow["state"],
			seller: decodeName( r.seller ),
			anonymous: r.anonymous === true,
			category: r.category,
			degree: commerceInteger( r.degree, MARKET_MAX_DEGREE ),
			sealed: r.sealed === true,
			item: decodeItem( r.item )
		};
	};

	/*
	================
	decodeOffer
	================
	*/
	const decodeOffer = ( value: unknown ): MarketOffer => {
		const o = value as Record<string, unknown> | null;
		if ( !o || typeof o !== "object" || !MARKET_OFFER_STATES.includes( o.state as MarketOffer["state"] ) ) {
			throw Error( "Invalid market offer" );
		}
		return {
			id: commerceInteger( o.id, Number.MAX_SAFE_INTEGER, 1 ),
			listingId: commerceInteger( o.listingId, Number.MAX_SAFE_INTEGER, 1 ),
			currency: decodeCurrency( o.currency ),
			asked: commerceInteger( o.asked, Number.MAX_SAFE_INTEGER ),
			price: commerceInteger( o.price, Number.MAX_SAFE_INTEGER ),
			buyer: decodeName( o.buyer ),
			to: decodeDestination( o.to ),
			state: o.state as MarketOffer["state"],
			atMs: commerceInteger( o.atMs, Number.MAX_SAFE_INTEGER ),
			item: decodeItem( o.item )
		};
	};

	/*
	================
	decodeList
	================
	*/
	const decodeList = <T>( value: unknown, decode: ( row: unknown ) => T ): T[] => {
		if ( !Array.isArray( value ) || value.length > MARKET_ROW_LIMIT ) throw Error( "Invalid market rows" );
		return value.map( decode );
	};

	/*
	================
	receiveEvents

	A pushed answer: events only. It never answers the request in flight,
	so pending stays as it was.
	================
	*/
	const receiveEvents = ( p: Record<string, unknown> ) => {
		if ( !Array.isArray( p.events ) || p.events.length < 1 || p.events.length > MARKET_EVENTS_PER_ANSWER ) {
			throw Error( "Invalid market events" );
		}
		let next = eventSequence;
		const decoded = p.events.map( e => decodeEvent( e, ++next ) );
		eventSequence = next;
		events = [ ...events, ...decoded ].slice( -MARKET_EVENT_LIMIT );
	};

	return {
		/*
		================
		request

		The frame for one window request, or null. Every write waits for its
		answer: a request while a write pends is dropped here, so a double
		click never sends two buys (the window's view of pending is a frame
		behind). A read may replace a pending read. A malformed request still
		throws: the window never builds one.
		================
		*/
		request( request: MarketRequest, now: number ) {
			const reading = request.op === "search" || request.op === "mine";
			if ( pending !== null && now < deadline && !(reading && (pending === "search" || pending === "mine")) ) {
				return null;
			}
			const payload = marketRequestPayload( request );
			pending = request.op;
			pendingTo = "to" in request ? request.to : "";
			deadline = now + MARKET_REQUEST_TIMEOUT_MS;
			return { opcode: MARKET_CONTROL, payload };
		},
		/*
		================
		receive

		One answer, decoded whole before anything changes. A write's answer
		becomes the notice; a read's replaces its own view; a pushed answer
		adds events.
		================
		*/
		receive( payload: Uint8Array ) {
			if ( payload.length > MARKET_ANSWER_LIMIT ) throw Error( "Market answer exceeds limit" );
			const p = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( payload ) );
			if ( !p || typeof p !== "object" || p.version !== MARKET_ANSWER_VERSION || typeof p.ok !== "boolean" ) {
				throw Error( "Invalid market answer" );
			}
			if ( p.op === MARKET_EVENT_OP ) {
				receiveEvents( p );
				return;
			}
			const op: MarketOp | null = (MARKET_OPS as readonly string[]).includes( p.op ) ? p.op as MarketOp : null;
			if ( p.op !== "" && p.op !== undefined && op === null ) throw Error( "Invalid market answer op" );
			if ( p.error !== undefined && (typeof p.error !== "string" || p.error.length > 32) ) {
				throw Error( "Invalid market refusal" );
			}
			let nextCategories = categories, nextBrowse = browse, nextMine = mine;
			if ( p.ok && op === "search" ) {
				const s = p.search;
				if ( !s || !Array.isArray( p.categories ) || p.categories.length > 32 ) {
					throw Error( "Invalid market search" );
				}
				const rawCategories: readonly unknown[] = p.categories;
				nextCategories = rawCategories.map( c => {
					const row = c as Record<string, unknown> | null;
					if ( !row || typeof row.slug !== "string" || typeof row.name !== "string" ) {
						throw Error( "Invalid market category" );
					}
					return { slug: row.slug.slice( 0, 32 ), name: row.name.slice( 0, 64 ) };
				} );
				nextBrowse = {
					rows: decodeList( s.rows, decodeRow ),
					page: commerceInteger( s.page, MARKET_MAX_PAGE, 1 ),
					pages: commerceInteger( s.pages, Number.MAX_SAFE_INTEGER ),
					total: commerceInteger( s.total, Number.MAX_SAFE_INTEGER )
				};
			}
			if ( p.ok && op === "mine" ) {
				const m = p.mine, rules = m?.rules;
				if ( !m || !rules ) throw Error( "Invalid market account" );
				nextMine = {
					listings: decodeList( m.listings, decodeRow ),
					offersIn: decodeList( m.offersIn, decodeOffer ),
					offersOut: decodeList( m.offersOut, decodeOffer ),
					purchases: decodeList( m.purchases, decodePurchase ),
					silk: commerceInteger( m.silk ),
					warehouseGold: commerceInteger( m.warehouseGold, Number.MAX_SAFE_INTEGER ),
					rules: {
						feePercent: commerceInteger( rules.feePercent, 100 ),
						durationMs: commerceInteger( rules.durationMs, Number.MAX_SAFE_INTEGER ),
						openCap: commerceInteger( rules.openCap, 1000 ),
						offerCap: commerceInteger( rules.offerCap, 1000 )
					}
				};
			}
			// A buy's answer names where the item went; an accept falls back,
			// so its sale says too.
			const to: MarketDestination | "" = p.ok && p.sale !== undefined ?
				decodeDestination( (p.sale as Record<string, unknown> | null)?.to ) :
				pendingTo;
			categories = nextCategories;
			browse = nextBrowse;
			mine = nextMine;
			// A read that succeeded says nothing; every write and every refusal
			// is a notice. An answer naming no op is the scene-loading refusal.
			if ( !p.ok || (op !== "search" && op !== "mine") ) {
				notice = {
					op: op ?? pending ?? "search",
					ok: p.ok,
					code: p.ok ? "" : p.error ?? "unavailable",
					to,
					sequence: ++sequence
				};
			}
			pending = null;
			pendingTo = "";
		},
		/*
		================
		expire

		A request the server never answered frees the window.
		================
		*/
		expire( now: number ) {
			if ( pending !== null && now >= deadline ) pending = null;
		},
		/*
		================
		reset

		A new world: every view goes. The event sequence keeps rising, so a
		chat that printed up to it never prints an old number again.
		================
		*/
		reset() {
			categories = [];
			browse = mine = notice = null;
			events = [];
			pending = null;
			pendingTo = "";
		},
		/*
		================
		state
		================
		*/
		state(): MarketState {
			return { categories, browse, mine, pending, notice, events };
		}
	};
}

/*
================
marketSorts
================
*/
export function marketSorts(): readonly (typeof MARKET_SORTS)[number][] {
	return MARKET_SORTS;
}
