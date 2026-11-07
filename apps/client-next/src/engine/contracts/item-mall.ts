/*
===========================================================================

item-mall.ts - authoritative mall merchandise and client presentation choices

The catalogue identifies a package by its native shop address. Monetary values
are integer game currencies. Funding services are outside this contract.

===========================================================================
*/

/*
================
MallOffer
================
*/
export interface MallOffer {
	readonly currencyMask: number;
	readonly group: number;
	readonly shop: number;
	readonly tab: number;
	readonly slot: number;
	readonly packageId: number;
	readonly name: string;
	readonly description: string;
	readonly icon: string;
	readonly silk: number;
	readonly giftSilk: number;
	readonly allowsPoints: boolean;
	readonly purchaseLimit: number;
	readonly itemIds: readonly number[];
}

/*
================
MallTab
================
*/
export interface MallTab {
	readonly shop: number;
	readonly tab: number;
	readonly category: string;
	readonly label: string;
}

/*
================
MallState

Balances and completion revisions are publications, never optimistic UI edits.
================
*/
export interface MallState {
	readonly tabs: readonly MallTab[];
	readonly offers: readonly MallOffer[];
	readonly silk: number;
	readonly giftSilk: number;
	readonly points: number;
	readonly pending: boolean;
	readonly revision: number;
	readonly error?: string;
}

/*
================
MallSilkHistoryRow

One recorded silk use. At is whole unix seconds, charged is silk plus gift
silk spent, newest first in MallSilkHistoryState.
================
*/
export interface MallSilkHistoryRow {
	readonly at: number;
	readonly charged: number;
}

/*
================
MallSilkHistoryState

The Buying list's last inquiry: the period asked (0..3, the native combo's
1 day, 7 days, 1 month, 3 months), its recorded uses and the whole
period's charged total, which the row cap never trims. Port-only
publication: the native window's Inquire is an empty stub and no packet
exists, so the port answers on its control transport (#2269).
================
*/
export interface MallSilkHistoryState {
	readonly period: number;
	readonly total: number;
	readonly rows: readonly MallSilkHistoryRow[];
	readonly pending: boolean;
}

/*
================
MallPreviewState

Presentation resolves body compatibility; the UI owns the requested outfit.
================
*/
export interface MallPreviewState {
	readonly gid?: number;
	readonly wearable: readonly number[];
}
