/*
===========================================================================

specialty-deal.ts - the trade goods window's quantity, sums and trade scale

CIFSpecialtyDeal (ifspecialtydeal.cpp) prices a purchase or sale of trade
goods between a specialty shop and the summoned transport. Everything here is
pure arithmetic over the window's inputs: the unit buy price (+0x7D0), the
quantity (+0x7CC), the player's levelgold.txt basis and the transport's
reference run speed. The trade scale is the native one
(CIFSpecialtyDeal_ComputeTradeScale 649050); picking a scale row sets the
quantity (CIFSpecialtyDeal_OnTradeScaleSelected 64A9E0).

===========================================================================
*/

// 0xBEC690: the purchase thresholds of trade scales 1..4; above the last is 5.
const TRADE_SCALE_1 = 408;
const TRADE_SCALE_2 = 918;
const TRADE_SCALE_3 = 1428;
const TRADE_SCALE_4 = 2142;
export const TRADE_SCALE_MAX = 5;
// 0xBCF3C8: the run speed the trade scale is normalized against.
const TRADE_SCALE_SPEED = 40;
// 6492C0 / 6496E0: TRADESCALE1..5, or the first three on a Korean client's
// #$T shard (GameConfig +0x12A, 745FA2; see default-wear-policy.ts).
const TRADE_SCALE_ROWS = 5;
const TRADE_SCALE_ROWS_RESTRICTED = 3;
const NATIVE_LANGUAGE_KOREAN = 0;
// type.txt Language index of the English client this port ships
// (defaultWearPolicy.mjs: Korean, Chinese, Taiwan, Japan, English, Vietnam);
// the HUD loads the English text tables (resources.ts).
export const NATIVE_LANGUAGE_ENGLISH = 4;
const SHARD_MARKER = "#$T";
// Trade goods: TypeID 3/3/8 (ITEM_ETC_TRADE_*), as container-transfer.ts and
// the server's inventory.IsTradeGoods test the type word.
const TRADE_GOODS_MASK = 0x7fe;
const TRADE_GOODS_TYPE = 0x46c;

/*
================
isTradeGoods
================
*/
export function isTradeGoods( typeFlags: number ): boolean {
	return (typeFlags & TRADE_GOODS_MASK) === TRADE_GOODS_TYPE;
}

/*
================
tradeScaleThreshold

0xBEC690[index]: 0 below scale 1, the four authored thresholds, then none.
================
*/
function tradeScaleThreshold( index: number ): number {
	switch ( index ) {
		case 0:
			return 0;
		case 1:
			return TRADE_SCALE_1;
		case 2:
			return TRADE_SCALE_2;
		case 3:
			return TRADE_SCALE_3;
		case 4:
			return TRADE_SCALE_4;
		default:
			return Infinity;
	}
}

/*
================
tradeGoldBasis

(g * 10) >> 3 of the levelgold.txt column-2 value g, as 649050 and 64A9E0
both derive it (lea esi,[eax+eax*4]; add esi,esi; shr esi,3).
================
*/
export function tradeGoldBasis( levelGold: number ): number {
	return (levelGold * 10) >>> 3;
}

/*
================
tradeScale

CIFSpecialtyDeal_ComputeTradeScale (649050): the purchase sum's 64-bit
integer quotient by the basis, scaled by speed2 / 40 and truncated, then the
first scale whose threshold it does not exceed (0 for a zero quotient, at
most 5). A zero basis or speed has no scale; native asserts speed2 != 0.
================
*/
export function tradeScale( sum: bigint, basis: number, speed2: number ): number {
	if ( basis <= 0 || speed2 <= 0 || sum <= 0n ) return 0;
	const units = Number( sum / BigInt( basis ) );
	// fild qword; fdivr 40.0 (40 / speed2); fdivp; truncating fistp.
	const q = Math.trunc( units / (TRADE_SCALE_SPEED / speed2) );
	if ( q === 0 ) return 0;
	let scale = 0;
	while ( scale < TRADE_SCALE_MAX && q > tradeScaleThreshold( scale ) ) scale++;
	return scale;
}

/*
================
tradeScaleRows

How many TRADESCALE rows the combo lists.
================
*/
export function tradeScaleRows( language: number, rawServerName: string | undefined ): number {
	return language === NATIVE_LANGUAGE_KOREAN && !!rawServerName?.includes( SHARD_MARKER ) ?
		TRADE_SCALE_ROWS_RESTRICTED :
		TRADE_SCALE_ROWS;
}

/*
================
tradeScaleRow

649D20: the combo row a purchase's scale selects, scale 0 showing row 0
like scale 1, capped at the last listed row.
================
*/
export function tradeScaleRow( scale: number, rows: number ): number {
	return Math.min( Math.max( scale, 1 ) - 1, rows - 1 );
}

/*
================
TradeScaleQuantityInput
================
*/
export interface TradeScaleQuantityInput {
	readonly row: number;
	readonly basis: number;
	readonly speed2: number;
	readonly unitBuy: number;
	// The last row fills the transport: one stack per bag slot.
	readonly maxStack: number;
	readonly capacity: number;
}

/*
================
tradeScaleQuantity

CIFSpecialtyDeal_OnTradeScaleSelected (64A9E0): the quantity a combo row
sets. Rows 0..3 buy the most that stays inside that scale,
trunc(((T[row + 1] + 1) * basis * 40 / speed2 - 1) / unitBuy); the fifth row
buys a full transport, the item's max stack times its bag capacity.
================
*/
export function tradeScaleQuantity( input: TradeScaleQuantityInput ): number {
	const { row, basis, speed2, unitBuy, maxStack, capacity } = input;
	if ( row + 1 === TRADE_SCALE_MAX ) return Math.max( 0, maxStack * capacity );
	if ( speed2 <= 0 || unitBuy <= 0 ) return 0;
	const limit = tradeScaleThreshold( row + 1 ) + 1;
	const spend = limit * basis * TRADE_SCALE_SPEED / speed2;
	return Math.max( 0, Math.trunc( (spend - 1) / unitBuy ) );
}

/*
================
DealSums

649D20: buy = count * unit buy; a sale also shows the sale sum and the
profit, sale minus buy (digit-grouped by the window).
================
*/
export interface DealSums {
	readonly buy: bigint;
	readonly sell?: bigint;
	readonly profit?: bigint;
}

/*
================
dealSums
================
*/
export function dealSums( count: number, unitBuy: number, sale?: bigint ): DealSums {
	const buy = BigInt( count ) * BigInt( unitBuy );
	return sale === undefined ? { buy } : { buy, sell: sale, profit: sale - buy };
}

/*
================
dealChunk

CIFSpecialtyDeal_RequestTradeGoodsMove (64A660): each request moves what is
left, at most one source stack; the loop closes when the moved total reaches
the target.
================
*/
export function dealChunk( target: number, moved: number, stack: number ): number {
	const left = target - moved;
	if ( left <= 0 ) return 0;
	return Math.min( left, stack );
}

/*
================
tradeGoldBases

levelData.json's tradeGoldBasis (levelgold.txt column 2) by level. Missing
rows stay missing: the window then shows no scale rather than a wrong one.
================
*/
export function tradeGoldBases( raw: unknown ): Readonly<Record<number, number>> {
	const result: Record<number, number> = {};
	if ( !raw || typeof raw !== "object" ) return result;
	for ( const [level, row] of Object.entries( raw as Record<string, { tradeGoldBasis?: unknown; }> ) ) {
		const basis = row?.tradeGoldBasis;
		if ( typeof basis === "number" && Number.isSafeInteger( basis ) && basis > 0 ) result[Number( level )] = basis;
	}
	return result;
}
