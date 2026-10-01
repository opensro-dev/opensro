/*
===========================================================================

commerce-tooltip.ts - authoritative prices at the tooltip context boundary

An item describes its properties; an offer describes the transaction price.
Keep these separate so identical item instances in different shops, packages,
and buyback entries do not inherit a cached or guessed price.

===========================================================================
*/

import type { GameplayState } from "@/engine/contracts/gameplay";
import { SHOP_CURRENCY_HONOR } from "@/engine/foundation/gameplay/commerce";
import type { TooltipRow } from "./tooltip-rows";

const PRICE_COLOR = 0xffffffff;

/*
================
commerceTooltip

Native 5592B0 appends the active slot's transaction price after item details.
Its package branch at 559AF6 prices the package once, not each contained item.
The server's decimal-string quote already includes merchant adjustments;
preserve it without narrowing 64-bit gold through a JavaScript number. An
honor-priced package (payment index 5) prints "%s : %I64d%s" with
UIIT_STT_TC_HONOR_POINT and UIIT_STT_SILKMALL_P_POINT (559FA8).
================
*/
export function commerceTooltip(
	id: string,
	game: Pick<GameplayState, "shop">,
	labels: {
		readonly price: string;
		readonly gold: string;
		readonly honor: string;
		readonly point: string;
	}
): readonly TooltipRow[] {
	const shop = game.shop;
	if ( !shop || shop.error ) return [];
	let price: string | undefined;
	let honor = false;
	if ( id.startsWith( "shop-offer:" ) ) {
		const offer = shop.offers[Number( id.slice( "shop-offer:".length ) )];
		price = offer?.price;
		honor = offer?.currency === SHOP_CURRENCY_HONOR;
	} else if ( id.startsWith( "shop-buyback:" ) ) {
		price = shop.buyback?.[Number( id.slice( "shop-buyback:".length ) )]?.price;
	}
	if ( price === undefined ) return [];
	const value = honor ? `${labels.honor} : ${price}${labels.point}` : `${labels.price} : ${price} ${labels.gold}`;
	return [
		{ value: " ", color: 0 },
		{ value, color: PRICE_COLOR }
	];
}
