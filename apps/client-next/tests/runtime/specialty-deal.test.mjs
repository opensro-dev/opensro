/*
===========================================================================

specialty-deal.test.mjs - the trade goods window's scale and quantities

CIFSpecialtyDeal_ComputeTradeScale (649050) and
CIFSpecialtyDeal_OnTradeScaleSelected (64A9E0) on authored inputs: the
level-80 levelgold basis (305) and a donkey's run speed (30).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const deal = await import( sourceFileUrl( "src/engine/foundation/gameplay/specialty-deal.ts" ).href );
const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);

const LEVEL_80_GOLD = 305;
const DONKEY_SPEED = 30;
const BASIS = deal.tradeGoldBasis( LEVEL_80_GOLD );

test("the basis is (g * 10) >> 3 of levelgold column 2", () => {
	assert.equal( BASIS, 381 );
	assert.equal( deal.tradeGoldBasis( 28 ), 35 );
});

test("649050 scales the integer quotient by speed2 / 40 and buckets it", () => {
	// 100000 / 381 = 262; 262 / (40 / 30) = 196.5, truncated 196 <= 408.
	assert.equal( deal.tradeScale( 100000n, BASIS, DONKEY_SPEED ), 1 );
	assert.equal( deal.tradeScale( 0n, BASIS, DONKEY_SPEED ), 0 );
	// A quotient below one basis truncates to zero: scale 0.
	assert.equal( deal.tradeScale( 380n, BASIS, 40 ), 0 );
	// Thresholds are inclusive: q == 408 is still scale 1, 409 is 2.
	assert.equal( deal.tradeScale( 408n * 381n, BASIS, 40 ), 1 );
	assert.equal( deal.tradeScale( 409n * 381n, BASIS, 40 ), 2 );
	assert.equal( deal.tradeScale( 2142n * 381n, BASIS, 40 ), 4 );
	assert.equal( deal.tradeScale( 2143n * 381n, BASIS, 40 ), 5 );
	assert.equal( deal.tradeScale( 10n ** 12n, BASIS, 40 ), 5 );
	assert.equal( deal.tradeScale( 100000n, BASIS, 0 ), 0, "no speed, no scale" );
});

test("649D20 shows scale 0 as the first row and caps at the listed rows", () => {
	assert.equal( deal.tradeScaleRow( 0, 5 ), 0 );
	assert.equal( deal.tradeScaleRow( 1, 5 ), 0 );
	assert.equal( deal.tradeScaleRow( 5, 5 ), 4 );
	assert.equal( deal.tradeScaleRow( 5, 3 ), 2 );
	assert.equal( deal.tradeScaleRows( 4, "Global" ), 5 );
	assert.equal( deal.tradeScaleRows( 0, "Server#$T" ), 3 );
	assert.equal( deal.tradeScaleRows( 0, "Server" ), 5 );
});

test("64A9E0 sets the most the picked scale holds", () => {
	for ( const unitBuy of [ 97, 1000, 4321 ] ) {
		for ( let row = 0; row < 4; row++ ) {
			const quantity = deal.tradeScaleQuantity( {
				row,
				basis: BASIS,
				speed2: DONKEY_SPEED,
				unitBuy,
				maxStack: 10,
				capacity: 8
			} );
			const at = deal.tradeScale( BigInt( quantity * unitBuy ), BASIS, DONKEY_SPEED );
			const above = deal.tradeScale( BigInt( (quantity + 1) * unitBuy ), BASIS, DONKEY_SPEED );
			assert.ok( at <= row + 1, `row ${row} unit ${unitBuy}: ${quantity} reaches scale ${at}` );
			assert.ok( above >= row + 1, `row ${row} unit ${unitBuy}: one more stays at ${above}` );
		}
	}
	// The fifth row fills the transport: max stack times bag capacity.
	assert.equal(
		deal.tradeScaleQuantity( {
			row: 4,
			basis: BASIS,
			speed2: DONKEY_SPEED,
			unitBuy: 97,
			maxStack: 10,
			capacity: 8
		} ),
		80
	);
});

test("the sums and the request loop", () => {
	assert.deepEqual( deal.dealSums( 3, 1000 ), { buy: 3000n } );
	assert.deepEqual( deal.dealSums( 3, 1000, 4500n ), { buy: 3000n, sell: 4500n, profit: 1500n } );
	// 64A660: at most one stack per request until the target is moved.
	assert.equal( deal.dealChunk( 25, 0, 10 ), 10 );
	assert.equal( deal.dealChunk( 25, 20, 10 ), 5 );
	assert.equal( deal.dealChunk( 25, 25, 10 ), 0 );
});

test("levelData's tradeGoldBasis rows parse; malformed rows stay missing", () => {
	assert.deepEqual(
		deal.tradeGoldBases( {
			1: { tradeGoldBasis: 28 },
			2: { tradeGoldBasis: 0 },
			3: {},
			80: { tradeGoldBasis: 305 }
		} ),
		{ 1: 28, 80: 305 }
	);
	assert.deepEqual( deal.tradeGoldBases( null ), {} );
});

test("SRO_TRADE_TIERS rates a purchase by gold against the shard's marks", () => {
	assert.equal( deal.tradeTierScale( 0n ), 0 );
	assert.equal( deal.tradeTierScale( 1n ), 1 );
	assert.equal( deal.tradeTierScale( 9_999_999n ), 1 );
	assert.equal( deal.tradeTierScale( 10_000_000n ), 2 );
	assert.equal( deal.tradeTierScale( 34_999_999n ), 3 );
	assert.equal( deal.tradeTierScale( 50_000_000n ), 5 );
	// The native scale of the same purchase is unchanged by the gold rule.
	assert.equal( deal.tradeScale( 100000n, BASIS, DONKEY_SPEED ), 1 );
});

test("SRO_TRADE_TIERS combo rows buy the most under the next mark; the last fills the transport", () => {
	const input = { basis: BASIS, speed2: DONKEY_SPEED, unitBuy: 300_000, maxStack: 40, capacity: 4 };
	// (10M - 1) / 300k = 33 goods stay at one star; 34 would reach two.
	assert.equal( deal.tradeTierQuantity( { ...input, row: 0 } ), 33 );
	assert.equal( deal.tradeTierScale( 33n * 300_000n ), 1 );
	assert.equal( deal.tradeTierScale( 34n * 300_000n ), 2 );
	assert.equal( deal.tradeTierQuantity( { ...input, row: 3 } ), 166 );
	assert.equal( deal.tradeTierQuantity( { ...input, row: 4 } ), 160 );
	assert.equal( deal.tradeTierQuantity( { ...input, row: 0, unitBuy: 0 } ), 0 );
});

test("the server's SRO_TRADE_TIERS rule reaches the HUD snapshot and clears on a native reentry", () => {
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
	game.bootstrap( { simulationProtocolVersion: 1, tradeTiers: true } );
	game.seed( pose );
	assert.equal( game.take()?.tradeTiers, true );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( pose );
	assert.equal( game.take()?.tradeTiers, undefined );
	game.dispose();
});
