/*
===========================================================================

cos-trade-info.ts - the transport bag's TradeInfo section and its sell-all

CIFCosBag_BindCompanion (6A9420) builds the ifcosinventory "TradeInfo"
section for a transport only (record type 1): the total the cargo cost
(controls 22..24), the trade level stars (25..30) and the sell-all button
(35). Everything here is read from that window's functions in the v1.150
client:

- CIFCosBag_RefreshSlotIcons (6A8240) sums count x the item's buy price
  (RefItem +0xB0) over every bag slot into a signed int and prints it with
  "%d" (0xBCD078); the section opens on "0" (0xBCCAC8).
- CIFCosBag_SetTradeStarCount (6A7DD0) shows the first n of five stars,
  n capped at 5, all tinted by n.
- CIFCosBag_ConfirmSellAllSpecialty (6A92D0) asks; on yes
  CIFCosBag_RequestQuantityMove (6A85E0) sells the first occupied slot's
  whole stack, and every sale the server acknowledges for that transport
  (CPSMission_ApplyInventoryOperation, 758BD5) sells the next one. A sale
  left unanswered for 5000 ms (the state timer it re-arms) ends the run, as
  does running out of slots.

===========================================================================
*/

// The ifcosinventory TradeInfo controls the window draws with state.
export const COS_TRADE_TOTAL = 23;
export const COS_TRADE_STAR_FIRST = 26;
export const COS_TRADE_STAR_LAST = 30;
export const COS_TRADE_SELL_ALL = 35;
// 6A85E0: CIObject_ScheduleStateTimer( 1, 0x1388 ) after each sale.
export const SELL_ALL_TIMEOUT_MS = 5000;
// 6A7DD0: at most five stars.
export const TRADE_STARS_MAX = 5;
// 6A7DD0's tints per star count (ARGB), 1..5.
const STAR_TINT_1 = 0xff3c9be8;
const STAR_TINT_2 = 0xff34c97c;
const STAR_TINT_3 = 0xffffffff;
const STAR_TINT_4 = 0xffff7e00;
const STAR_TINT_5 = 0xfffa4848;

/*
================
CargoRow

A transport bag row: its slot, count and the item's buy price.
================
*/
export interface CargoRow {
	readonly slot: number;
	readonly quantity: number;
	readonly price: number;
}

/*
================
CargoItem

What a transport bag row carries that the section reads.
================
*/
export interface CargoItem {
	readonly slot: number;
	readonly quantity: number;
	readonly tooltip?: { readonly fields: Readonly<Record<string, number>>; };
}

/*
================
cosCargoRows

The bag's rows with each item's buy price (RefItem +0xB0, the published
reference's price field).
================
*/
export function cosCargoRows( items: readonly CargoItem[] ): CargoRow[] {
	return items.map( item => ({ slot: item.slot, quantity: item.quantity, price: item.tooltip?.fields.price ?? 0 }) );
}

/*
================
cargoBuyTotal

6A8240: the signed 32-bit sum of count x buy price, printed with "%d".
================
*/
export function cargoBuyTotal( rows: readonly CargoRow[] ): string {
	let total = 0;
	for ( const row of rows ) {
		if ( row.quantity > 0 && row.price > 0 ) {
			total = (total + Math.imul( row.quantity, Math.trunc( row.price ) )) | 0;
		}
	}
	return String( total );
}

/*
================
tradeStarCount

6A7DD0: the server's trade level, at most five stars.
================
*/
export function tradeStarCount( level: number ): number {
	return Math.max( 0, Math.min( TRADE_STARS_MAX, Math.trunc( level ) ) );
}

/*
================
tradeStarTint

6A7DD0's colour for every shown star at a star count, as RGBA in 0..1.
================
*/
export function tradeStarTint( count: number ): readonly [number, number, number, number] {
	let argb: number;
	switch ( tradeStarCount( count ) ) {
		case 1:
			argb = STAR_TINT_1;
			break;
		case 2:
			argb = STAR_TINT_2;
			break;
		case 3:
			argb = STAR_TINT_3;
			break;
		case 4:
			argb = STAR_TINT_4;
			break;
		default:
			argb = STAR_TINT_5;
	}
	return [ (argb >>> 16 & 0xff) / 255, (argb >>> 8 & 0xff) / 255, (argb & 0xff) / 255, (argb >>> 24 & 0xff) / 255 ];
}

/*
================
SellAllInput

What decides the sell-all button (CGInterface_ReleaseTradeShopState 6A7D40):
a request in flight disables it, and it needs the special trade function
open (interaction lock 0x800) and a bag that is not empty.
================
*/
export interface SellAllInput {
	readonly busy: boolean;
	readonly specialTradeOpen: boolean;
	readonly rows: readonly CargoRow[];
}

/*
================
sellAllEnabled
================
*/
export function sellAllEnabled( input: SellAllInput ): boolean {
	return !input.busy && input.specialTradeOpen && input.rows.some( row => row.quantity > 0 );
}

/*
================
nextSellAllSlot

6A85E0: the first occupied slot at or after from, in slot order across the
bag's pages, or null when none is left.
================
*/
export function nextSellAllSlot( rows: readonly CargoRow[], from: number ): CargoRow | null {
	let next: CargoRow | null = null;
	for ( const row of rows ) {
		if ( row.quantity <= 0 || row.slot < from ) continue;
		if ( !next || row.slot < next.slot ) next = row;
	}
	return next;
}

/*
================
SellAllRun

A sell-all in progress: the slot of the sale in flight, its count when sent
and when it was sent.
================
*/
export interface SellAllRun {
	readonly slot: number;
	readonly quantity: number;
	readonly sentAt: number;
}

/*
================
SellAllStep

What the run does next: send a slot's whole stack, keep waiting, or stop.
================
*/
export type SellAllStep =
	| { readonly kind: "send"; readonly row: CargoRow; }
	| { readonly kind: "wait"; }
	| { readonly kind: "done"; };

/*
================
sellAllStep

One look at the run. A sale is acknowledged when the request settled and
its slot no longer holds what was sent; then the scan moves on from that
slot. A refused sale gets no acknowledgement natively, so the run waits
out the 5000 ms timer and ends.
================
*/
export function sellAllStep(
	run: SellAllRun | null,
	rows: readonly CargoRow[],
	pending: boolean,
	now: number
): SellAllStep {
	if ( !run ) {
		const first = nextSellAllSlot( rows, 0 );
		return first ? { kind: "send", row: first } : { kind: "done" };
	}
	if ( now - run.sentAt >= SELL_ALL_TIMEOUT_MS ) return { kind: "done" };
	if ( pending ) return { kind: "wait" };
	const left = rows.find( row => row.slot === run.slot )?.quantity ?? 0;
	if ( left === run.quantity ) return { kind: "wait" };
	const next = nextSellAllSlot( rows, run.slot );
	return next ? { kind: "send", row: next } : { kind: "done" };
}
