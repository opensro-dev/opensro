/*
===========================================================================

merchant.ts - the NPC store window's selection, quote and paging

Presentation state for CIFStore: the authored thirty-slot pages and tabs,
what a click selects (buy, sell, buyback), the quantity editor mode and the
price quote, including honor-priced packages.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { ShopState, ShopOffer, BuybackOffer } from "@/engine/foundation/gameplay/commerce";
import { SHOP_CURRENCY_HONOR } from "@/engine/foundation/gameplay/commerce";
import type { AuthoredLayout } from "./authored-layout";

/*
================
merchantDialogPage
================
*/
export function merchantDialogPage( layout: AuthoredLayout, confirm: boolean ): AuthoredLayout {
	return Object.fromEntries(
		Object.entries( layout ).filter( ( [name] ) =>
			confirm ?
				name.startsWith( "GDR_MBS_CONFIRM_" ) || name.startsWith( "GDR_ITEMMALL_CONFIRM_" ) :
				name.startsWith( "GDR_MBS_" ) && !name.startsWith( "GDR_MBS_CONFIRM_" )
		)
	);
}

// Modal identity is captured at open, never an array index that can shift after
// a sale/catalogue update. A replacement item cannot inherit an old confirmation.
export type MerchantSelection =
	| {
		readonly kind: "buy";
		readonly cosGid?: number;
		readonly npc: number;
		readonly tab: number;
		readonly slot: number;
		readonly binding: string;
	}
	| {
		readonly kind: "sell";
		readonly cosGid?: number;
		readonly npc: number;
		readonly slot: number;
		readonly binding: string;
		readonly previousQuotes: ShopState["saleQuotes"];
	}
	| { readonly kind: "buyback"; readonly npc: number; readonly id: number; readonly binding: string; };

/*
================
merchantQuantityMode

Active package purchase 6C0540 always focuses the quantity editor.
Sale 5983A0 selects focus by type; buyback 5980F0 restores the whole record.
================
*/
export function merchantQuantityMode(
	kind: MerchantSelection["kind"],
	item: InventoryItem | ShopOffer | BuybackOffer
): "hidden" | "editable" | "readonly" {
	if ( kind === "buyback" ) return "readonly";
	if ( kind === "buy" ) return "editable";
	const reference = "typeFlags" in item ?
		item :
		"maxStack" in item ?
		item.items?.find( row => row.refObjId === item.refObjId ) ??
			item.previews?.find( row => row.refObjId === item.refObjId ) :
		item.item ?? item.preview;
	// Missing reference metadata cannot authorize an editable quantity.
	if ( !reference ) return "readonly";
	if ( (reference.typeFlags & 0x7e) === 0x6c ) return "editable";
	return "readonly";
}

/*
================
merchantBinding
================
*/
export function merchantBinding( item: InventoryItem | ShopOffer | BuybackOffer ): string {
	if ( "slot" in item && "typeFlags" in item ) {
		return JSON.stringify( [
			item.refObjId,
			item.quantity,
			item.plus,
			item.durability,
			item.variance,
			item.magic,
			item.typeFlags,
			item.summon,
			item.durationMs,
			item.label
		] );
	}
	if ( "maxStack" in item ) {
		return JSON.stringify( [ item.refObjId, item.price, item.maxStack, item.purchaseLimit, item.contents ] );
	}
	return JSON.stringify( [ item.refObjId, item.price, item.quantity, item.plus ] );
}

/*
================
merchantSelection
================
*/
export function merchantSelection(
	kind: "buy" | "sell" | "buyback",
	index: number,
	shop: ShopState,
	inventory: readonly InventoryItem[]
): MerchantSelection | null {
	if ( kind === "buy" ) {
		const item = shop.offers[index];
		return item ?
			{
				kind,
				cosGid: shop.cosGid,
				npc: shop.npc,
				tab: item.tab,
				slot: item.slot,
				binding: merchantBinding( item )
			} :
			null;
	}
	if ( kind === "sell" ) {
		const item = inventory.find( i => i.slot === index );
		return item && item.slot >= (shop.cosGid ? 0 : 13) ?
			{
				kind,
				cosGid: shop.cosGid,
				npc: shop.npc,
				slot: item.slot,
				binding: merchantBinding( item ),
				previousQuotes: shop.saleQuotes
			} :
			null;
	}
	const item = shop.buyback?.[index];
	return item ? { kind, npc: shop.npc, id: item.id, binding: merchantBinding( item ) } : null;
}

/*
================
merchantQuote
================
*/
export function merchantQuote(
	selection: MerchantSelection | null,
	shop: ShopState | undefined,
	inventory: readonly InventoryItem[],
	draft: string,
	gold: string | undefined
) {
	if ( !selection || !shop || shop.error || shop.npc !== selection.npc ) return null;
	if ( selection.kind !== "buyback" && (selection.cosGid ?? 0) !== (shop.cosGid ?? 0) ) return null;
	const item = selection.kind === "buy" ?
		shop.offers.find( i => i.tab === selection.tab && i.slot === selection.slot ) :
		selection.kind === "sell" ?
		inventory.find( i => i.slot === selection.slot ) :
		shop.buyback?.find( i => i.id === selection.id );
	if ( !item || merchantBinding( item ) !== selection.binding ) return null;
	const maximum = selection.kind === "buy" ?
		("maxStack" in item ? item.purchaseLimit ?? item.maxStack : 1) :
		"quantity" in item ?
		item.quantity :
		1;
	const quantity = selection.kind === "buyback" ? maximum : /^\d{1,5}$/.test( draft ) ? Number( draft ) : 0;
	// No frame between click and worker acknowledgement may reuse a previous
	// quote. Only the inventory owner replaces this array on a catalogue reply.
	const sale = selection.kind === "sell" && shop.saleQuotes !== selection.previousQuotes ?
		shop.saleQuotes?.find( q =>
			q.slot === selection.slot && (q.cosGid ?? 0) === (selection.cosGid ?? 0) && q.refObjId === item.refObjId &&
			q.quantity === maximum
		) :
		undefined;
	const unit = selection.kind === "sell" ? sale?.price : "price" in item ? item.price : undefined;
	const quotedTotal = sale?.totals?.[quantity - 1];
	const total = sale?.totals ?
		(quotedTotal === undefined ? null : BigInt( quotedTotal )) :
		unit === undefined ?
		null :
		BigInt( unit ) * BigInt( selection.kind === "buyback" ? 1 : quantity );
	// Honor is not a balance this client holds; the server answers a short
	// honor balance with the native UIIT_MSG_TC_LACK_HONOR_POINT refusal.
	const honor = "currency" in item && item.currency === SHOP_CURRENCY_HONOR;
	const valid = quantity >= 1 && quantity <= maximum && total !== null && total <= 0x7fffffffffffffffn &&
		(selection.kind === "sell" || honor || gold !== undefined && total <= BigInt( gold ));
	return { item, quantity, maximum, total, valid, quantityMode: merchantQuantityMode( selection.kind, item ) };
}

/*
================
merchantPage

Authored store: thirty fixed slots (6 x 5). Sparse wire slots stay sparse.
Tabs come from authored tab metadata, including empty tabs, not offer order.
================
*/
export function merchantPage( shop: ShopState | undefined, tab: number, page: number, branchTabs?: readonly number[] ) {
	const tabs = (shop?.tabs?.map( t => t.index ) ?? [ ...new Set( shop?.offers.map( i => i.tab ) ?? [] ) ]).filter(
		index => !branchTabs || branchTabs.includes( index )
	);
	const selected = tabs.includes( tab ) ? tab : tabs[0] ?? 0;
	const offers = (shop?.offers ?? []).map( ( item, index ) => ({ item, index }) ).filter( r =>
		r.item.tab === selected
	);
	const pages = Math.max( 1, Math.ceil( (Math.max( -1, ...offers.map( r => r.item.slot ) ) + 1) / 30 ) );
	const current = Math.max( 0, Math.min( pages - 1, page ) );
	return {
		tabs,
		tab: selected,
		page: current,
		pages,
		slots: Array.from( { length: 30 }, ( _, i ) => offers.find( r => r.item.slot === current * 30 + i ) )
	};
}

/*
================
merchantCommand

Container identity is captured with the quote, so a replaced transport cannot
inherit a pending buy or sell confirmation.
================
*/
export function merchantCommand( selection: MerchantSelection, quantity: number ) {
	if ( selection.kind === "buyback" ) return { kind: "shop-buyback" as const, id: selection.id };
	if ( selection.kind === "buy" ) {
		return selection.cosGid ?
			{
				kind: "cos-shop-buy" as const,
				gid: selection.cosGid,
				tab: selection.tab,
				slot: selection.slot,
				quantity
			} :
			{ kind: "shop-buy" as const, tab: selection.tab, slot: selection.slot, quantity };
	}
	return selection.cosGid ?
		{ kind: "cos-shop-sell" as const, gid: selection.cosGid, slot: selection.slot, quantity } :
		{ kind: "shop-sell" as const, slot: selection.slot, quantity };
}
