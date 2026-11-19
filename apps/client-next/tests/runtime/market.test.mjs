/*
===========================================================================
market.test.mjs - the consignment market's wire: checked requests, whole
answers, named and anonymous sellers, offers, destinations and events.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import { defined } from "../helpers/defined.mjs";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const {
	createMarket,
	marketEventText,
	marketNoticeText,
	marketRequestPayload,
	MARKET_EVENT_LIMIT
} = await import( "../../src/engine/foundation/gameplay/market.ts" );
const encode = p => new TextEncoder().encode( JSON.stringify( p ) );
const decode = bytes => JSON.parse( new TextDecoder().decode( bytes ) );
const item = slot => ({ slot, refObjId: 3117, typeFlags: 1, name: "Sword", body: [ 1, 2 ] });
const row = ( id, extra = {} ) => ({
	id,
	version: 1,
	currency: "gold",
	price: 900,
	listedMs: 1,
	expiresMs: 2,
	state: "open",
	category: "weapon",
	degree: 1,
	item: item( 0 ),
	...extra
});
const offer = ( id, slot, extra = {} ) => ({
	id,
	listingId: 4,
	currency: "gold",
	asked: 900,
	price: 700,
	buyer: "Lulu",
	to: "bag",
	state: "open",
	atMs: 3,
	item: item( slot ),
	...extra
});
const rules = { feePercent: 1, durationMs: 86400000, openCap: 20, offerCap: 20 };
/** @returns {any} */
function preview( r ) {
	return { refObjId: r.refObjId, name: r.name, slot: r.slot };
}

test("every write names where its item goes, and an offer is checked before it leaves", () => {
	assert.deepEqual( decode( marketRequestPayload( { op: "buy", listing: 5, version: 2, to: "bag" } ) ), {
		op: "buy",
		listing: 5,
		version: 2,
		to: "bag"
	} );
	assert.deepEqual( decode( marketRequestPayload( { op: "offer", listing: 5, price: 700, to: "warehouse" } ) ), {
		op: "offer",
		listing: 5,
		price: 700,
		to: "warehouse"
	} );
	assert.deepEqual( decode( marketRequestPayload( { op: "accept", offer: 3 } ) ), { op: "accept", offer: 3 } );
	assert.deepEqual( decode( marketRequestPayload( { op: "search", race: "eu", rarity: "sealed", degree: 11 } ) ), {
		op: "search",
		degree: 11,
		race: "eu",
		rarity: "sealed",
		page: 1
	} );
	assert.deepEqual(
		decode( marketRequestPayload( { op: "list", slot: 20, currency: "silk", price: 5, anonymous: true } ) ),
		{ op: "list", slot: 20, currency: "silk", price: 5, anonymous: true }
	);
	assert.equal(
		"anonymous" in
			decode( marketRequestPayload( { op: "list", slot: 20, currency: "silk", price: 5, anonymous: false } ) ),
		false
	);
	for (
		const bad of [
			{ op: "buy", listing: 5, version: 2, to: "mailbox" },
			{ op: "cancel", listing: 5 },
			{ op: "offer", listing: 5, price: 0, to: "bag" },
			{ op: "offer", listing: 0, price: 10, to: "bag" },
			{ op: "reject", offer: -1 },
			{ op: "list", slot: 6, currency: "gold", price: 5, anonymous: false },
			{ op: "search", race: "kr" },
			{ op: "search", rarity: "unique" },
			{ op: "search", degree: 12 }
		]
	) {
		// Each is malformed on purpose, so it has no MarketRequest type.
		assert.throws( () => marketRequestPayload( /** @type {any} */ (bad) ), Error, JSON.stringify( bad ) );
	}
});

test("the account page reads sellers, both offer lists and the offer cap", () => {
	const market = createMarket( preview );
	market.request( { op: "mine" }, 0 );
	market.receive( encode( {
		version: 2,
		op: "mine",
		ok: true,
		mine: {
			listings: [ row( 1, { seller: "Kekw", anonymous: true } ) ],
			offersIn: [ offer( 3, 1 ) ],
			offersOut: [ offer( 9, 2, { buyer: "Kekw", to: "warehouse" } ) ],
			purchases: [ {
				listingId: 2,
				currency: "silk",
				price: 50,
				fee: 0,
				atMs: 4,
				to: "bag",
				slot: 21,
				item: "Bow"
			} ],
			silk: 9,
			warehouseGold: 0,
			rules
		}
	} ) );
	const mine = defined( market.state().mine );
	assert.equal( mine.listings[0].seller, "Kekw" );
	assert.equal( mine.listings[0].anonymous, true );
	assert.equal( mine.offersIn[0].buyer, "Lulu" );
	assert.equal( mine.offersOut[0].to, "warehouse" );
	assert.equal( mine.purchases[0].item, "Bow" );
	assert.equal( mine.rules.offerCap, 20 );
	assert.equal( market.state().pending, null );
	assert.equal( market.state().notice, null );
});

test("an anonymous listing has no seller, and a malformed answer changes nothing", () => {
	const market = createMarket( preview );
	market.request( { op: "search", page: 1 }, 0 );
	const page = rows => ({
		version: 2,
		op: "search",
		ok: true,
		categories: [],
		search: { rows, page: 1, pages: 1, total: 1 }
	});
	market.receive( encode( page( [ row( 1 ), row( 2, { seller: "Kekw" } ) ] ) ) );
	assert.deepEqual( defined( market.state().browse ).rows.map( r => r.seller ), [ "", "Kekw" ] );
	for (
		const bad of [
			{ ...page( [ row( 1, { seller: 5 } ) ] ) },
			{ ...page( [ row( 1, { anonymous: "yes" } ) ] ) },
			{ ...page( [ row( 1 ) ] ), version: 1 }
		]
	) {
		assert.throws( () => market.receive( encode( bad ) ) );
		assert.equal( defined( market.state().browse ).rows.length, 2 );
	}
});

test("a pushed event never answers the request in flight", () => {
	const market = createMarket( preview );
	market.request( { op: "buy", listing: 4, version: 1, to: "bag" }, 0 );
	market.receive( encode( {
		version: 2,
		op: "event",
		ok: true,
		events: [ { kind: "offer", item: "Sword", currency: "gold", price: 700, who: "Lulu" } ]
	} ) );
	assert.equal( market.state().pending, "buy" );
	assert.equal( market.state().events[0].who, "Lulu" );
	assert.equal( market.state().events[0].sequence, 1 );
	// A second write while one pends is dropped, never sent twice.
	assert.equal( market.request( { op: "buy", listing: 4, version: 1, to: "bag" }, 10 ), null );
	assert.equal( market.request( { op: "mine" }, 10 ), null );
	market.receive( encode( {
		version: 2,
		op: "buy",
		ok: true,
		sale: { listingId: 4, currency: "gold", price: 700, fee: 7, atMs: 5, to: "warehouse", slot: 0 }
	} ) );
	// The sale says where the item went, whatever the request asked.
	assert.equal( defined( market.state().notice ).to, "warehouse" );
	assert.match( marketNoticeText( defined( market.state().notice ) ), /warehouse/ );
	assert.equal( market.state().pending, null );
	for ( let i = 0; i < MARKET_EVENT_LIMIT + 3; i++ ) {
		market.receive( encode( {
			version: 2,
			op: "event",
			ok: true,
			events: [ { kind: "sold", item: "Bow", currency: "silk", price: 50, paid: 50 } ]
		} ) );
	}
	const events = market.state().events;
	assert.equal( events.length, MARKET_EVENT_LIMIT );
	assert.equal( defined( events.at( -1 ) ).sequence, MARKET_EVENT_LIMIT + 4 );
	assert.throws( () =>
		market.receive(
			encode( { version: 2, op: "event", ok: true, events: [ { kind: "stolen", currency: "gold" } ] } )
		)
	);
	market.reset();
	assert.equal( market.state().events.length, 0 );
});

test("a refusal names its reason and the offer events read as chat lines", () => {
	const market = createMarket( preview );
	market.request( { op: "buy", listing: 4, version: 1, to: "bag" }, 0 );
	market.receive( encode( { version: 2, op: "buy", ok: false, error: "bag-full" } ) );
	assert.match( marketNoticeText( defined( market.state().notice ) ), /inventory is full/ );
	assert.equal(
		marketNoticeText( { op: "buy", ok: false, code: "buyer-busy", to: "" } ),
		"The buyer is trading right now. Try again in a moment."
	);
	assert.equal(
		marketEventText( { kind: "sold", item: "Sword", currency: "gold", price: 1000, paid: 990, who: "" } ),
		"[Market] Sword sold for 1,000 Gold. You received 990 Gold after the fee."
	);
	assert.equal(
		marketEventText( { kind: "offer", item: "Sword", currency: "silk", price: 40, paid: 0, who: "Lulu" } ),
		"[Market] Lulu offers 40 Silk for your Sword."
	);
	assert.match(
		marketEventText( { kind: "offer-rejected", item: "Sword", currency: "gold", price: 5, paid: 0, who: "" } ),
		/declined/
	);
});

test("the server's SRO_CONSIGNMENT rule reaches the HUD snapshot and clears on a shard without it", () => {
	const game = createGameplay( () => {} );
	const pose = {
		gid: 7,
		refObjId: 1907,
		kind: "player",
		name: "Tester",
		regionId: 0x6b4f,
		x: 60,
		y: 10,
		z: 100,
		heading: 0
	};
	game.bootstrap( { simulationProtocolVersion: 1, consignment: true } );
	game.seed( pose );
	assert.equal( game.take()?.consignment, true );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( pose );
	assert.equal( game.take()?.consignment, undefined );
	game.dispose();
});
