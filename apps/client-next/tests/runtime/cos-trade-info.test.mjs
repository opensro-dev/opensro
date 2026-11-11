/*
===========================================================================

cos-trade-info.test.mjs - the transport bag's TradeInfo section and sell-all

The v1.150 transport bag shows the cargo's cost, the server's trade level as
tinted stars and a sell-all button (CIFCosBag 6A8240 / 6A7DD0 / 6A85E0).
The port had none of them.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const info = await import( sourceFileUrl( "src/engine/foundation/gameplay/cos-trade-info.ts" ).href );

const rows = [
	{ slot: 3, quantity: 40, price: 348 },
	{ slot: 0, quantity: 10, price: 100 },
	{ slot: 30, quantity: 5, price: 0 }
];

test("the total is count x buy price over every row, printed with %d", () => {
	assert.equal( info.cargoBuyTotal( rows ), "14920" );
	assert.equal( info.cargoBuyTotal( [] ), "0" );
	// A signed 32-bit sum, as the native int accumulator.
	assert.equal(
		info.cargoBuyTotal( [ { slot: 0, quantity: 40, price: 60_000_000 } ] ),
		String( (40 * 60_000_000) | 0 )
	);
});

test("the stars cap at five and share one tint per count", () => {
	assert.equal( info.tradeStarCount( 9 ), 5 );
	assert.equal( info.tradeStarCount( -1 ), 0 );
	assert.deepEqual( info.tradeStarTint( 3 ), [ 1, 1, 1, 1 ] );
	assert.deepEqual( info.tradeStarTint( 1 ), [ 0x3c / 255, 0x9b / 255, 0xe8 / 255, 1 ] );
	assert.deepEqual( info.tradeStarTint( 5 ), [ 0xfa / 255, 0x48 / 255, 0x48 / 255, 1 ] );
});

test("sell-all needs the special trade open, a stocked bag and no request in flight", () => {
	assert.equal( info.sellAllEnabled( { busy: false, specialTradeOpen: true, rows } ), true );
	assert.equal( info.sellAllEnabled( { busy: true, specialTradeOpen: true, rows } ), false );
	assert.equal( info.sellAllEnabled( { busy: false, specialTradeOpen: false, rows } ), false );
	assert.equal( info.sellAllEnabled( { busy: false, specialTradeOpen: true, rows: [] } ), false );
});

test("the run sells whole stacks in slot order, one at a time", () => {
	let step = info.sellAllStep( null, rows, false, 0 );
	assert.deepEqual( step, { kind: "send", row: rows[1] } );
	const run = { slot: 0, quantity: 10, sentAt: 0 };
	// Unanswered: wait; acknowledged (the slot emptied): the next slot.
	assert.equal( info.sellAllStep( run, rows, true, 100 ).kind, "wait" );
	assert.equal( info.sellAllStep( run, rows, false, 100 ).kind, "wait" );
	step = info.sellAllStep( run, rows.slice( 0, 1 ).concat( rows[2] ), false, 200 );
	assert.deepEqual( step, { kind: "send", row: rows[0] } );
	// The last stack sold ends the run.
	assert.equal( info.sellAllStep( { slot: 30, quantity: 5, sentAt: 0 }, [], false, 300 ).kind, "done" );
});

test("a sale left unanswered for 5000 ms ends the run", () => {
	const run = { slot: 0, quantity: 10, sentAt: 1000 };
	assert.equal( info.sellAllStep( run, rows, true, 1000 + info.SELL_ALL_TIMEOUT_MS - 1 ).kind, "wait" );
	assert.equal( info.sellAllStep( run, rows, true, 1000 + info.SELL_ALL_TIMEOUT_MS ).kind, "done" );
});

test("the bag rows carry each item's published buy price", () => {
	assert.deepEqual(
		info.cosCargoRows( [ { slot: 2, quantity: 7, tooltip: { fields: { price: 348, maxStack: 40 } } }, {
			slot: 4,
			quantity: 1
		} ] ),
		[
			{ slot: 2, quantity: 7, price: 348, maxStack: 40 },
			{ slot: 4, quantity: 1, price: 0, maxStack: 0 }
		]
	);
});

test("the progress line drives a load from 1 to 5 stars", () => {
	// One stack of the goods in front of the player: 348 x 40. The
	// published (stars, gap) pairs below are a 101-stack transport
	// loading up: 20, 21, 41, 71, then 101 stacks aboard.
	const stack = 348 * 40;
	assert.equal( info.tradeProgressLine( { stars: 1, gap: 2784 }, stack ), "1 star; 1 more stack for 2" );
	assert.equal( info.tradeProgressLine( { stars: 2, gap: 270048 }, stack ), "2 stars; 20 more stacks for 3" );
	assert.equal( info.tradeProgressLine( { stars: 3, gap: 413424 }, stack ), "3 stars; 30 more stacks for 4" );
	assert.equal( info.tradeProgressLine( { stars: 4, gap: 417600 }, stack ), "4 stars; 30 more stacks for 5" );
	assert.equal( info.tradeProgressLine( { stars: 5, gap: 0 }, stack ), "5 stars" );
	// The issue's own example shape.
	assert.equal( info.tradeProgressLine( { stars: 3, gap: 12 * stack }, stack ), "3 stars; 12 more stacks for 4" );
});

test("the progress line is absent without an answer and bare without a priceable stack", () => {
	assert.equal( info.tradeProgressLine( null, 100 ), "" );
	assert.equal( info.tradeProgressLine( undefined, 100 ), "" );
	assert.equal( info.tradeProgressLine( { stars: 0, gap: 1 }, 0 ), "0 stars" );
	// An unmeasurable load (the server named no gap) reads as its stars.
	assert.equal( info.tradeProgressLine( { stars: 1, gap: 0 }, 100 ), "1 star" );
	assert.equal( info.tradeProgressLine( { stars: 3, gap: 50 }, 0 ), "3 stars" );
});
