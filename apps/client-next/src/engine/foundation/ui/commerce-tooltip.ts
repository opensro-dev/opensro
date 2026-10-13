/*
===========================================================================

commerce-tooltip.ts - authoritative prices at the tooltip context boundary

An item describes its properties; an offer describes the transaction price.
Keep these separate so identical item instances in different shops, packages,
and buyback entries do not inherit a cached or guessed price.

===========================================================================
*/

import type { GameplayState } from "@/engine/contracts/gameplay";
import { SHOP_CURRENCY_GUILD_POINT, SHOP_CURRENCY_HONOR, type ShopPrice } from "@/engine/foundation/gameplay/commerce";
import type { TooltipRow } from "./tooltip-rows";

const PRICE_COLOR = 0xffffffff;

/*
================
CommerceLabels

The localized words of a price line.
================
*/
export interface CommerceLabels {
	readonly price: string;
	readonly gold: string;
	readonly honor: string;
	readonly point: string;
	readonly guildPoint: string;
	readonly guildPointUnit: string;
}

/*
================
commerceTooltip

Native 5592B0 appends the active slot's transaction price after item details.
Its package branch at 559AF6 prices the package once, not each contained item.
The server's decimal-string quote already includes merchant adjustments;
preserve it without narrowing 64-bit gold through a JavaScript number. An
honor-priced package (payment index 5) prints "%s : %I64d%s" with
UIIT_STT_TC_HONOR_POINT and UIIT_STT_SILKMALL_P_POINT (559FA8). A package
with several price rows gets one line each, in payment order (559AF6 walks
indexes 0..5): gold, then guild points (index 3, UIIT_STT_GUILD_POINT and
UIIT_STT_GUILD_POINT_GP), then honor.
================
*/
export function commerceTooltip(
	id: string,
	game: Pick<GameplayState, "shop">,
	labels: CommerceLabels
): readonly TooltipRow[] {
	const shop = game.shop;
	if ( !shop || shop.error ) return [];
	let rows: readonly ShopPrice[] = [];
	if ( id.startsWith( "shop-offer:" ) ) {
		const offer = shop.offers[Number( id.slice( "shop-offer:".length ) )];
		if ( offer ) rows = offer.prices ?? [ { currency: offer.currency ?? 0, amount: offer.price } ];
	} else if ( id.startsWith( "shop-buyback:" ) ) {
		const price = shop.buyback?.[Number( id.slice( "shop-buyback:".length ) )]?.price;
		if ( price !== undefined ) rows = [ { currency: 0, amount: price } ];
	}
	if ( !rows.length ) return [];
	return [
		{ value: " ", color: 0 },
		...rows.map( row => ({ value: priceLine( row, labels ), color: PRICE_COLOR }) )
	];
}

/*
================
priceLine
================
*/
function priceLine(
	row: ShopPrice,
	labels: CommerceLabels
) {
	if ( row.currency === SHOP_CURRENCY_HONOR ) return `${labels.honor} : ${row.amount}${labels.point}`;
	if ( row.currency === SHOP_CURRENCY_GUILD_POINT ) {
		return `${labels.guildPoint} : ${row.amount}${labels.guildPointUnit}`;
	}
	return `${labels.price} : ${row.amount} ${labels.gold}`;
}
