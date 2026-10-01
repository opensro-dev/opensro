/*
===========================================================================

item-mall-catalog.ts - bounded decoding of the authoritative mall projection

Only validated scalar fields enter gameplay state. Delivery item bodies remain
owned by the shared inventory decoder, including item-family validation.

===========================================================================
*/
import type { MallOffer, MallState, MallTab } from "@/engine/contracts/item-mall";
import { commerceInteger, commerceJson } from "./commerce";

const MAX_MALL_OFFERS = 65536;
const MAX_MALL_TABS = 256;
const MAX_PACKAGE_ITEMS = 96;
const MAX_MALL_TEXT = 256;

/*
================
record
================
*/
function record( value: unknown ): Record<string, unknown> {
	if ( !value || typeof value !== "object" || Array.isArray( value ) ) throw Error( "Invalid mall row" );
	return value as Record<string, unknown>;
}

/*
================
caption
================
*/
function caption( value: unknown ): string {
	if ( typeof value !== "string" || value.length > MAX_MALL_TEXT ) throw Error( "Invalid mall caption" );
	return value;
}

/*
================
mallProjection
================
*/
export function mallProjection( payload: Uint8Array ) {
	const value = commerceJson( payload );
	if (
		!Array.isArray( value.tabs ) || value.tabs.length > MAX_MALL_TABS || !Array.isArray( value.offers ) ||
		value.offers.length > MAX_MALL_OFFERS
	) throw Error( "Invalid mall catalogue" );
	const tabKeys = new Set<string>();
	const tabs: MallTab[] = value.tabs.map( value => {
		const row = record( value ),
			shop = commerceInteger( row.shop, 255 ),
			tab = commerceInteger( row.tab, 255 ),
			key = shop + ":" + tab;
		if ( tabKeys.has( key ) ) throw Error( "Duplicate mall tab" );
		tabKeys.add( key );
		return { shop, tab, category: caption( row.category ), label: caption( row.label ) };
	} );
	const addresses = new Set<string>();
	const offers: MallOffer[] = value.offers.map( value => {
		const row = record( value ),
			group = commerceInteger( row.group, 65535, 1 ),
			shop = commerceInteger( row.shop, 255 ),
			tab = commerceInteger( row.tab, 255 ),
			slot = commerceInteger( row.slot, 255 );
		const key = group + ":" + shop + ":" + tab + ":" + slot;
		if ( addresses.has( key ) || !tabKeys.has( shop + ":" + tab ) ) throw Error( "Invalid mall address" );
		addresses.add( key );
		if (
			typeof row.allowsPoints !== "boolean" || !Array.isArray( row.itemIds ) || row.itemIds.length === 0 ||
			row.itemIds.length > MAX_PACKAGE_ITEMS
		) throw Error( "Invalid mall package" );
		const currencyMask = commerceInteger( row.currencyMask, 22, 1 );
		if ( (currencyMask & ~22) !== 0 || Boolean( currencyMask & 16 ) !== row.allowsPoints ) {
			throw Error( "Invalid mall currency mask" );
		}
		const icon = caption( row.icon ).replaceAll( "\\", "/" );
		if (
			icon.startsWith( "/" ) || /[:%?#\u0000-\u001f\u007f]/.test( icon ) ||
			icon.split( "/" ).includes( ".." )
		) {
			throw Error( "Invalid mall artwork path" );
		}
		const silk = commerceInteger( row.silk ), giftSilk = commerceInteger( row.giftSilk );
		if ( silk !== 0 && !(currencyMask & 2) || giftSilk !== 0 && !(currencyMask & 4) ) {
			throw Error( "Mall price has no matching currency" );
		}
		return {
			group,
			shop,
			tab,
			slot,
			currencyMask,
			packageId: commerceInteger( row.packageId, 0xffffffff, 1 ),
			name: caption( row.name ),
			description: caption( row.description ),
			icon,
			silk,
			giftSilk,
			allowsPoints: row.allowsPoints,
			purchaseLimit: commerceInteger( row.purchaseLimit, 65535, 1 ),
			itemIds: row.itemIds.map( id => commerceInteger( id, 0xffffffff, 1 ) )
		};
	} );
	const state: MallState = {
		tabs,
		offers,
		silk: commerceInteger( value.silk ),
		giftSilk: commerceInteger( value.giftSilk ),
		points: commerceInteger( value.points ),
		pending: false,
		revision: 0
	};
	return { state, items: value.items };
}
