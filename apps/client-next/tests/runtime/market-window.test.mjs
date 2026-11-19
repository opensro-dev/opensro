/*
===========================================================================

market-window.test.mjs - the consignment market window (port-only,
SRO_CONSIGNMENT): the HUD button and the storage keeper's row only while
the server runs the market, named and anonymous sellers, the buy and offer boxes that ask where
the item goes, the Sell terms, and the requests each click sends.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { defined } from "../helpers/defined.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { createMarketHud } = await import( "../../src/engine/runtime/ui/hud/market-hud.ts" );

/** @typedef {import("../../src/engine/contracts/gameplay.ts").InventoryItem} InventoryItem */
/** @typedef {import("../../src/engine/contracts/market.ts").MarketRow} MarketRow */
/** @typedef {import("../../src/engine/contracts/market.ts").MarketState} MarketState */

/** @typedef {import("../../src/engine/contracts/market.ts").MarketRequest} MarketRequest */
/** @typedef {ReturnType<typeof createMarketHud>} MarketHud */
/** @typedef {Parameters<MarketHud["activate"]>[1]} MarketContext */

/*
================
requests

What a click sends; the click must be one the window handles.
================
*/
/** @param {MarketHud} hud @param {string} id @param {MarketContext} context @returns {readonly MarketRequest[]} */
const requests = ( hud, id, context ) => defined( hud.activate( id, context ), id ).requests;

/*
================
search

The one search a filter click sends.
================
*/
/** @param {MarketHud} hud @param {string} id @param {MarketContext} context */
function search( hud, id, context ) {
	const sent = requests( hud, id, context );
	assert.equal( sent.length, 1, id );
	const request = defined( sent[0], id );
	if ( request.op !== "search" ) throw new Error( `${id} sent ${request.op}, not a search` );
	return request;
}

/** @param {string} name @param {Record<string, unknown>} [extra] @returns {InventoryItem} */
const item = ( name, extra = {} ) => /** @type {InventoryItem} */ ({
	slot: 0,
	refObjId: 3117,
	typeFlags: 1,
	name,
	quantity: 1,
	plus: 0,
	...extra
});
/** @param {number} id @param {Partial<MarketRow>} [extra] @returns {MarketRow} */
const row = ( id, extra = {} ) => ({
	id,
	version: 2,
	currency: "gold",
	price: 1000,
	listedMs: 0,
	expiresMs: Date.now() + 5 * 3600000,
	state: "open",
	seller: "",
	anonymous: false,
	category: "weapon",
	degree: 1,
	sealed: false,
	item: item( "Iron Sword" ),
	...extra
});
const rules = { feePercent: 1, durationMs: 86400000, openCap: 20, offerCap: 20 };
/** @param {Partial<MarketState>} [extra] @returns {MarketState} */
const market = ( extra = {} ) => ({
	categories: [ { slug: "weapon", name: "Weapons" } ],
	browse: {
		rows: [
			row( 7, { seller: "Kekw" } ),
			row( 8, { item: item( "Bow" ), price: 50, currency: "silk" } ),
			row( 9, { seller: "Me" } )
		],
		page: 1,
		pages: 2,
		total: 17
	},
	mine: {
		listings: [ row( 9, { seller: "Me", anonymous: true, state: "held" } ) ],
		offersIn: [ {
			id: 4,
			listingId: 9,
			currency: "gold",
			asked: 1000,
			price: 800,
			buyer: "Lulu",
			to: "bag",
			state: "open",
			atMs: 1,
			item: item( "Iron Sword" )
		} ],
		offersOut: [],
		purchases: [],
		silk: 40,
		warehouseGold: 0,
		rules
	},
	pending: null,
	notice: null,
	events: [],
	...extra
});

/*
================
storageKeeper

The production UI talking to a storage keeper (capability 4), on a shard
that runs the market or not (the world-entry consignment rule).
================
*/
function storageKeeper( enabled ) {
	const commands = [], f = uiFixture( command => commands.push( command ) );
	let now = 1000, seen = null;
	const step = () => {
		for ( let i = 0; i < 20; i++ ) seen = f.ui.step( f.state, now += 50 ) ?? seen;
		return new Map( (seen?.controls ?? []).map( control => [ control.id, control ] ) );
	};
	f.state.gameplay = {
		...f.state.gameplay,
		npcConversation: { gid: 77, phase: "menu" },
		target: 77,
		targetCapabilities: 4,
		...(enabled ? { consignment: true } : {}),
		market: market(),
		social: { ...(f.state.gameplay.social ?? {}), localName: "Me" }
	};
	const sent = () => commands.filter( c => c.command?.kind === "market" ).map( c => c.command.request );
	return { f, step, sent };
}

test("the storage keeper offers the market only while the server runs it", () => {
	for ( const enabled of [ false, true ] ) {
		const { f, step } = storageKeeper( enabled );
		try {
			const controls = step();
			assert.ok( controls.has( "storage-open" ), "the storage row" );
			assert.equal( controls.has( "market-open" ), enabled, `server market ${enabled}` );
			// The button beside FPS toggles the window anywhere, but only on a
			// shard that runs the market.
			f.ui.event( { kind: "activate", id: "market-toggle" } );
			step();
			assert.equal( f.hasText( "Consignment Market" ), enabled, `the button opens it: ${enabled}` );
			f.ui.event( { kind: "activate", id: "market-toggle" } );
			step();
			assert.equal( f.hasText( "Consignment Market" ), false, "a second click closes it" );
		} finally {
			f.dispose();
		}
	}
});

test("the window names sellers, hides anonymous ones and never sells a player its own listing", () => {
	const { f, step, sent } = storageKeeper( true );
	try {
		step();
		f.ui.event( { kind: "activate", id: "market-open" } );
		let controls = step();
		assert.ok( f.hasText( "Consignment Market" ) );
		assert.deepEqual( sent().map( r => r.op ), [ "mine", "search" ], "opening asks for the account and page 1" );
		assert.ok( f.hasText( "Kekw" ) && f.hasText( "Anonymous" ) && f.hasText( "Yours" ) );
		assert.ok( f.hasText( "1,000 Gold" ) && f.hasText( "50 Silk" ) );
		assert.ok( f.hasText( "Offers (1)" ), "the open offer on the tab" );

		// Every row carries its own Buy, PM and Offer.
		assert.equal( controls.get( "market-row-buy:9" )?.disabled, true, "its own listing" );
		assert.equal( controls.get( "market-row-offer:9" )?.disabled, true, "no offer on its own listing" );
		assert.equal( controls.get( "market-row-buy:8" )?.disabled, false );
		assert.equal( controls.get( "market-row-whisper:8" )?.disabled, true, "an anonymous seller takes no whisper" );
		assert.equal( controls.get( "market-row-whisper:7" )?.disabled, false );
		// The right column: List an item and the account's pages.
		assert.ok(
			f.hasText( "List an item" ) && controls.has( "market-tab:mine" ) && controls.has( "market-tab:history" )
		);
		f.ui.event( { kind: "activate", id: "market-row-whisper:7" } );
		step();
		f.ui.event( { kind: "activate", id: "market-row-buy:7" } );
		controls = step();
		assert.ok( f.hasText( "Where should the item go?" ), "a row's Buy opens the box for that row" );
	} finally {
		f.dispose();
	}
});

test("a buy asks where the item goes and sends that place", () => {
	const { f, step, sent } = storageKeeper( true );
	try {
		step();
		f.ui.event( { kind: "activate", id: "market-open" } );
		step();
		f.ui.event( { kind: "activate", id: "market-pick:7" } );
		step();
		f.ui.event( { kind: "activate", id: "market-buy" } );
		let controls = step();
		assert.ok( f.hasText( "Where should the item go?" ) );
		assert.equal( controls.get( "market-dialog-to:bag" )?.selected, true, "the inventory is the default" );
		f.ui.event( { kind: "activate", id: "market-dialog-to:warehouse" } );
		step();
		f.ui.event( { kind: "activate", id: "market-dialog-confirm" } );
		step();
		assert.deepEqual( sent().at( -1 ), { op: "buy", listing: 7, version: 2, to: "warehouse" } );

		// While the buy is in flight, no second write leaves.
		f.state.gameplay = { ...f.state.gameplay, market: market( { pending: "buy" } ) };
		controls = step();
		assert.equal( controls.get( "market-dialog-confirm" )?.disabled, true );
		assert.ok( f.hasText( "Waiting for the market..." ) );
		// The answer refreshes the account and the page and closes the box.
		const before = sent().length;
		f.state.gameplay = {
			...f.state.gameplay,
			market: market( { notice: { op: "buy", ok: true, code: "", to: "warehouse", sequence: 1 } } )
		};
		controls = step();
		assert.ok( f.hasText( "Bought. The item is in your warehouse." ) );
		assert.deepEqual( sent().slice( before ).map( r => r.op ), [ "mine", "search" ] );
		assert.ok( !controls.has( "market-dialog-confirm" ) );
	} finally {
		f.dispose();
	}
});

test("Escape closes the box, then the window", () => {
	const { f, step } = storageKeeper( true );
	try {
		step();
		f.ui.event( { kind: "activate", id: "market-open" } );
		step();
		f.ui.event( { kind: "activate", id: "market-pick:7" } );
		f.ui.event( { kind: "activate", id: "market-offer" } );
		assert.ok( step().has( "market-offer-price" ) );
		f.ui.event( { kind: "key", code: "Escape" } );
		let controls = step();
		assert.ok( !controls.has( "market-offer-price" ) && controls.has( "market-close" ) );
		f.ui.event( { kind: "key", code: "Escape" } );
		controls = step();
		assert.ok( !controls.has( "market-close" ) );
	} finally {
		f.dispose();
	}
});

test("a shard without the market closes the window", () => {
	const { f, step } = storageKeeper( true );
	try {
		step();
		f.ui.event( { kind: "activate", id: "market-open" } );
		assert.ok( step().has( "market-close" ) );
		f.state.gameplay = { ...f.state.gameplay, consignment: undefined };
		assert.ok( !step().has( "market-close" ) );
	} finally {
		f.dispose();
	}
});

test("the drafts: an offer below the price, a listing's terms, the take-back place", () => {
	const hud = createMarketHud();
	const context = { market: market(), localName: "Me", now: 0 };
	hud.open();
	hud.activate( "market-pick:7", context );
	hud.activate( "market-offer", context );
	hud.edit( "market-offer-price", "1000" );
	assert.deepEqual( requests( hud, "market-dialog-confirm", context ), [], "at the asking price" );
	hud.edit( "market-offer-price", "0x9a00" );
	assert.equal( defined( hud.state().dialog ).price, "900" );
	assert.deepEqual( requests( hud, "market-dialog-confirm", context ), [ {
		op: "offer",
		listing: 7,
		price: 900,
		to: "bag"
	} ] );
	hud.activate( "market-dialog-cancel", context );

	hud.activate( "market-tab:sell", context );
	assert.deepEqual( requests( hud, "market-list", context ), [], "no item picked" );
	hud.activate( "market-sell-slot:20", context );
	hud.activate( "market-sell-currency:silk", context );
	hud.activate( "market-anonymous", context );
	hud.edit( "market-price", "050" );
	assert.deepEqual( requests( hud, "market-list", context ), [ {
		op: "list",
		slot: 20,
		currency: "silk",
		price: 50,
		anonymous: true
	} ] );

	hud.activate( "market-tab:mine", context );
	hud.activate( "market-mine:9", context );
	assert.deepEqual( requests( hud, "market-cancel", context ), [], "a held listing is taken back" );
	hud.activate( "market-return-to:warehouse", context );
	assert.deepEqual( requests( hud, "market-takeback", context ), [ {
		op: "takeback",
		listing: 9,
		to: "warehouse"
	} ] );

	hud.activate( "market-tab:offers", context );
	hud.activate( "market-offer-in:4", context );
	assert.deepEqual( hud.activate( "market-offer-whisper", context ), { requests: [], whisper: "Lulu" } );
	assert.deepEqual( requests( hud, "market-withdraw", context ), [], "an incoming offer is not withdrawn" );
	assert.deepEqual( requests( hud, "market-accept", context ), [ { op: "accept", offer: 4 } ] );
	const busy = { ...context, market: market( { pending: "accept" } ) };
	assert.deepEqual( requests( hud, "market-reject", busy ), [], "one write at a time" );
});

test("a read in flight holds the writes, but only a write says it is waiting", () => {
	const { f, step } = storageKeeper( true );
	try {
		step();
		f.ui.event( { kind: "activate", id: "market-open" } );
		step();
		f.ui.event( { kind: "activate", id: "market-pick:7" } );
		assert.equal( step().get( "market-row-buy:7" )?.disabled, false, "nothing in flight" );
		f.state.gameplay = { ...f.state.gameplay, market: market( { pending: "search" } ) };
		const controls = step();
		assert.equal( controls.get( "market-row-buy:7" )?.disabled, true, "the worker would drop a write sent now" );
		assert.ok( !f.hasText( "Waiting for the market..." ), "a read is silent" );
	} finally {
		f.dispose();
	}
	const hud = createMarketHud();
	hud.open();
	hud.activate( "market-tab:offers", { market: market(), localName: "Me", now: 0 } );
	hud.activate( "market-offer-in:4", { market: market(), localName: "Me", now: 0 } );
	const reading = { market: market( { pending: "mine" } ), localName: "Me", now: 0 };
	assert.deepEqual( requests( hud, "market-accept", reading ), [], "no write during a read" );
});

test("each offer list pages past its rows", () => {
	const mine = defined( market().mine );
	/** @param {number} id */
	const offer = id => ({ ...defined( mine.offersIn[0] ), id });
	const many = market( {
		mine: { ...mine, offersIn: Array.from( { length: 8 }, ( _, i ) => offer( 10 + i ) ) }
	} );
	const context = { market: many, localName: "Me", now: 0 };
	const hud = createMarketHud();
	hud.open();
	hud.activate( "market-tab:offers", context );
	hud.activate( "market-offers-in-prev", context );
	assert.equal( hud.state().offerPages.in, 0, "no page before the first" );
	hud.activate( "market-offers-in-next", context );
	hud.activate( "market-offers-in-next", context );
	assert.equal( hud.state().offerPages.in, 1, "8 offers fill two pages of 6" );
	assert.equal( hud.state().offerPages.out, 0, "the other list keeps its page" );
});

test("the chips, the dropdowns and a pick from the bag", () => {
	const context = { market: market(), localName: "Me", now: 0 };
	const hud = createMarketHud();
	hud.open();
	assert.equal( search( hud, "market-category:weapon", context ).category, "weapon" );
	assert.deepEqual( requests( hud, "market-category:nope", context ), [], "an unknown category" );
	// A dropdown opens, a choice searches and closes it; any other click
	// closes it too, and a choice the window never drew does nothing.
	hud.activate( "market-menu:degree", context );
	assert.equal( hud.state().menu, "degree" );
	assert.equal( search( hud, "market-choose:degree:11", context ).degree, 11 );
	assert.equal( hud.state().menu, "" );
	hud.activate( "market-menu:race", context );
	hud.activate( "market-pick:7", context );
	assert.equal( hud.state().menu, "", "a click elsewhere closes the list" );
	const raced = search( hud, "market-choose:race:eu", context );
	assert.equal( raced.race, "eu" );
	assert.equal( raced.degree, 11, "the other filters stay" );
	assert.equal( search( hud, "market-choose:rarity:sealed", context ).rarity, "sealed" );
	for (
		const bad of [
			"market-choose:race:kr",
			"market-choose:degree:12",
			"market-choose:degree:",
			"market-choose:nope:1"
		]
	) {
		assert.deepEqual( requests( hud, bad, context ), [], bad );
	}
	assert.equal( search( hud, "market-choose:degree:0", context ).degree, undefined, "All degrees" );
	// The Price header: cheapest first, then dearest first.
	assert.equal( search( hud, "market-sort-price", context ).sort, "price-asc" );
	assert.equal( search( hud, "market-sort-price", context ).sort, "price-desc" );
	assert.equal( search( hud, "market-category:", context ).category, undefined, "All" );
	hud.activate( "market-sell-pick", context );
	assert.equal( hud.state().tab, "sell", "the + opens the bag" );
	hud.activate( "market-sell-slot:20", context );
	assert.equal( hud.state().sellSlot, 20 );
	assert.equal( hud.state().tab, "mine", "a pick closes the bag" );
	hud.activate( "market-sell-clear", context );
	assert.equal( hud.state().sellSlot, -1 );
	hud.activate( "market-offer-out:4", context );
	assert.equal( hud.state().offerSide, "out" );
});

test("K toggles the market and Ctrl+click lists a bag item", () => {
	const { f, step } = storageKeeper( true );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			npcConversation: undefined,
			// The fixture's bag is empty: this is its only item.
			inventory: [ /** @type {InventoryItem} */ ({ slot: 20, refObjId: 3633, quantity: 1, name: "Sword" }) ]
		};
		step();
		f.ui.event( { kind: "key", code: "KeyK" } );
		step();
		assert.ok( f.hasText( "Consignment Market" ), "K opens it" );
		assert.equal( f.hasText( "Click it to put it back." ), false, "nothing picked yet" );
		f.ui.event( { kind: "activate", id: "slot:20", ctrl: true } );
		step();
		assert.ok( f.hasText( "Click it to put it back." ), "Ctrl+click put the bag item in List an item" );
		f.ui.event( { kind: "key", code: "KeyK" } );
		step();
		assert.equal( f.hasText( "Consignment Market" ), false, "K closes it" );
	} finally {
		f.dispose();
	}
});

test("every page of the window draws with unique controls", () => {
	// The frame refuses a duplicate control id and stops the runtime; step()
	// would throw on any page that draws one.
	const { f, step } = storageKeeper( true );
	try {
		step();
		f.ui.event( { kind: "activate", id: "market-open" } );
		step();
		assert.ok( f.hasText( "Consignment Market" ) );
		for (
			const id of [
				"market-tab:history",
				"market-tab:offers",
				"market-offer-side:out",
				"market-sell-pick",
				"market-menu:degree",
				"market-menu:race",
				"market-tab:mine"
			]
		) {
			f.ui.event( { kind: "activate", id } );
			const controls = step();
			if ( id === "market-sell-pick" ) assert.ok( f.hasText( "Pick the item to sell" ), "the bag page drew" );
			if ( id === "market-menu:race" ) assert.ok( controls.has( "market-choose:race:eu" ), "the race list drew" );
		}
	} finally {
		f.dispose();
	}
});

/*
================
intersects
================
*/
function intersects( a, b ) {
	return a[0] < b[0] + b[2] && b[0] < a[0] + a[2] && a[1] < b[1] + b[3] && b[1] < a[1] + a[3];
}

test("full account pages page their rows and never run under their buttons", () => {
	const { f, step } = storageKeeper( true );
	const many = 20,
		offer = ( id ) => ({
			id,
			listingId: 9,
			currency: "gold",
			asked: 1000,
			price: 800,
			buyer: "Lulu",
			to: "bag",
			state: "open",
			atMs: id,
			item: item( "Iron Sword" )
		});
	const base = market();
	f.state.gameplay = {
		...f.state.gameplay,
		market: {
			...base,
			mine: {
				...base.mine,
				listings: Array.from(
					{ length: many },
					( _, i ) => row( 100 + i, { state: i % 2 ? "sold" : "open" } )
				),
				offersIn: Array.from( { length: many }, ( _, i ) => offer( 200 + i ) ),
				offersOut: Array.from( { length: many }, ( _, i ) => offer( 300 + i ) ),
				purchases: Array.from( { length: many }, ( _, i ) => ({
					listingId: 400 + i,
					currency: "gold",
					price: 10,
					fee: 0,
					atMs: i,
					to: "bag",
					item: "Bow"
				}) )
			}
		}
	};
	const rows = /^market-(mine|history|offer-in|offer-out):/;
	try {
		step();
		f.ui.event( { kind: "activate", id: "market-open" } );
		for (
			const [tabs, pager] of [
				[ [ "market-tab:mine", "market-mine:100" ], "market-mine-next" ],
				[ [ "market-tab:history" ], "market-history-next" ],
				[ [ "market-tab:offers", "market-offer-side:in" ], "market-offers-in-next" ],
				[ [ "market-tab:offers", "market-offer-side:out" ], "market-offers-out-next" ]
			]
		) {
			for ( const id of tabs ) {
				f.ui.event( { kind: "activate", id } );
				step();
			}
			const controls = [ ...step().values() ];
			const listed = controls.filter( c => rows.test( c.id ) );
			assert.ok( listed.length > 1, `${tabs.at( -1 )} rows` );
			assert.equal( controls.find( c => c.id === pager )?.disabled, false, `${pager} pages the rest` );
			const footer = controls.filter( c =>
				c.kind === "button" && c.id.startsWith( "market-" ) && !rows.test( c.id ) &&
				!c.id.startsWith( "market-item:" ) && !c.id.startsWith( "market-tab:" ) &&
				!c.id.startsWith( "market-offer-side:" ) && c.rect[1] > listed[0].rect[1]
			);
			const page = controls.filter( c =>
				c.kind === "button" && c.id.startsWith( "market-" ) && !c.id.startsWith( "market-item:" ) &&
				c.rect[0] >= listed[0].rect[0] - 1
			);
			for ( const r of listed ) {
				for ( const b of footer.filter( b => b.rect[0] >= listed[0].rect[0] - 1 ) ) {
					assert.ok( !intersects( r.rect, b.rect ), `${r.id} runs under ${b.id}` );
				}
			}
			// Nor do the page's own controls (pages, choices, answers) overlap.
			const own = page.filter( c => !rows.test( c.id ) );
			own.forEach( ( a, i ) =>
				own.slice( i + 1 ).forEach( b => {
					assert.ok( !intersects( a.rect, b.rect ), `${a.id} overlaps ${b.id}` );
				} )
			);
		}
	} finally {
		f.dispose();
	}
});

test("an empty search offers to clear it, and clearing searches everything", () => {
	const { f, step, sent } = storageKeeper( true );
	try {
		step();
		f.ui.event( { kind: "activate", id: "market-open" } );
		f.state.gameplay = {
			...f.state.gameplay,
			market: { ...market(), browse: { rows: [], page: 1, pages: 1, total: 0 } }
		};
		let controls = step();
		assert.ok( f.hasText( "Nothing is on sale yet" ), "an unnarrowed empty market" );
		assert.equal( controls.has( "market-clear-search" ), false );
		f.ui.event( { kind: "activate", id: "market-menu:race" } );
		step();
		f.ui.event( { kind: "activate", id: "market-choose:race:eu" } );
		controls = step();
		assert.ok( f.hasText( "No listings match" ) );
		assert.equal( controls.get( "market-clear-search" )?.disabled, false );
		f.ui.event( { kind: "activate", id: "market-clear-search" } );
		step();
		const last = sent().at( -1 );
		assert.equal( last.op, "search" );
		assert.equal( last.race, undefined, "the filter is gone" );
	} finally {
		f.dispose();
	}
});

test("at the listing cap nothing more is listed, and one gone frees a place", () => {
	const hud = createMarketHud();
	const base = market();
	const mine = defined( base.mine );
	/** @param {number} count */
	const at = ( count ) => ({
		market: {
			...base,
			mine: {
				...mine,
				listings: Array.from( { length: count }, ( _, i ) => row( 100 + i ) ).concat( [
					row( 99, { state: "returned" } )
				] )
			}
		},
		localName: "Me",
		now: 0
	});
	hud.open();
	hud.activate( "market-tab:sell", at( 20 ) );
	hud.activate( "market-sell-slot:20", at( 20 ) );
	hud.edit( "market-price", "50" );
	assert.deepEqual( requests( hud, "market-list", at( 20 ) ), [], "20 / 20 listed" );
	assert.equal( requests( hud, "market-list", at( 19 ) )[0]?.op, "list", "a returned one does not count" );
});

test("My listings holds what is on sale, asks where it goes only once one is picked, and History keeps the rest", () => {
	const { f, step } = storageKeeper( true );
	const base = market();
	f.state.gameplay = {
		...f.state.gameplay,
		market: {
			...base,
			mine: {
				...base.mine,
				listings: [
					row( 50, { item: item( "Drako Sword" ) } ),
					row( 51, { item: item( "Strom Bow" ), state: "returned" } ),
					row( 52, { item: item( "Sky God Bow" ), state: "sold" } )
				]
			}
		}
	};
	try {
		step();
		f.ui.event( { kind: "activate", id: "market-open" } );
		let controls = step();
		assert.ok( controls.has( "market-mine:50" ) );
		assert.ok( !controls.has( "market-mine:51" ) && !controls.has( "market-mine:52" ), "done listings left" );
		assert.ok( !controls.has( "market-return-to:bag" ), "no destination before a pick" );
		assert.ok( f.hasText( "Pick one to cancel it." ) );
		f.ui.event( { kind: "activate", id: "market-mine:50" } );
		controls = step();
		assert.equal( controls.get( "market-return-to:bag" )?.selected, true );
		assert.ok( f.hasText( "Inventory" ) && f.hasText( "Warehouse" ), "both choices drawn whole" );
		f.ui.event( { kind: "activate", id: "market-return-to:warehouse" } );
		controls = step();
		assert.equal( controls.get( "market-return-to:warehouse" )?.selected, true );
		f.ui.event( { kind: "activate", id: "market-tab:history" } );
		step();
		assert.ok( f.hasText( "Strom Bow" ) && f.hasText( "Sky God Bow" ), "returned and sold in History" );
		assert.ok( f.hasText( "Returned" ) );
	} finally {
		f.dispose();
	}
});
