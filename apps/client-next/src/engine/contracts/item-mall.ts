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
MallPreviewState

Presentation resolves body compatibility; the UI owns the requested outfit.
================
*/
export interface MallPreviewState {
	readonly gid?: number;
	readonly wearable: readonly number[];
}
