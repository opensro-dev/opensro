/*
===========================================================================

commerce.ts - NPC shop, sale and buyback wire projections

Decodes the shop catalogue and item references the server publishes as
JSON, the native 0xB06D sale results (09, and 14 with a COS GID), and the
buyback ledger. Pure decoders: the inventory owner applies their results.

===========================================================================
*/
import { itemTooltipReference, type ItemTooltipReference } from "./item-tooltip-reference";
import type { InventoryItem } from "@/engine/contracts/gameplay";

/*
================
saleResult

v1.150 697e80 / 759a30: native result 09, and 14 with a leading COS GID.
The trailing byte is retained without inventing a buyback-slot interpretation.
================
*/
export function saleResult( opcode: number, p: Uint8Array ) {
	if ( opcode !== 0xb06d || p[0] !== 1 || (p[1] !== 9 && p[1] !== 20) ) return null;
	const cos = p[1] === 20, offset = cos ? 6 : 2;
	if ( p.length !== offset + 8 ) throw Error( "Invalid sale result" );
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength ),
		cosGid = cos ? v.getUint32( 2, true ) : undefined,
		slot = p[offset]!,
		quantity = v.getUint16( offset + 1, true ),
		context = v.getUint32( offset + 3, true );
	if ( !quantity || cos && !cosGid ) throw Error( "Invalid sale identity or quantity" );
	return { cosGid, slot, quantity, context, tag: p[offset + 7]! };
}

/*
================
soldInventory
================
*/
export function soldInventory(
	items: readonly InventoryItem[],
	slot: number,
	quantity: number
): readonly InventoryItem[] {
	const item = items.find( row => row.slot === slot );
	if ( !item || !Number.isInteger( quantity ) || quantity < 1 || quantity > item.quantity ) {
		throw Error( "Stale sale result" );
	}
	return items.flatMap( row =>
		row.slot !== slot ? [ row ] : quantity === row.quantity ? [] : [ { ...row, quantity: row.quantity - quantity } ]
	);
}

export interface CommercePreview {
	readonly refObjId: number;
	readonly typeFlags: number;
	readonly name: string;
	readonly body: readonly number[];
}
// refpricepolicyofitem payment types an NPC shop takes: gold and Training
// Camp honor.
export const SHOP_CURRENCY_GOLD = 1;
export const SHOP_CURRENCY_HONOR = 32;
export interface ShopOffer {
	readonly purchaseLimit?: number;
	readonly previews?: readonly CommercePreview[];
	readonly items?: readonly InventoryItem[];
	readonly icon?: string;
	readonly contents?: readonly {
		readonly refObjId: number;
		readonly name: string;
		readonly quantity: number;
		readonly plus: number;
	}[];
	readonly tab: number;
	readonly slot: number;
	readonly refObjId: number;
	readonly name: string;
	readonly price: string;
	readonly currency?: number;
	readonly maxStack: number;
}
export interface BuybackOffer {
	readonly preview?: CommercePreview;
	readonly item?: InventoryItem;
	readonly icon?: string;
	readonly index: number;
	readonly id: number;
	readonly refObjId: number;
	readonly name: string;
	readonly quantity: number;
	readonly price: string;
	readonly plus: number;
}
export interface ShopState {
	readonly saleQuotes?: readonly {
		readonly noBuyback?: boolean;
		readonly slot: number;
		readonly refObjId: number;
		readonly quantity: number;
		readonly price: string;
	}[];
	readonly tabs?: readonly { readonly index: number; readonly labelSymbol: string; }[];
	readonly buyback?: readonly BuybackOffer[];
	readonly npc: number;
	readonly name: string;
	readonly offers: readonly ShopOffer[];
	readonly error?: string;
}

/*
================
commerceJson
================
*/
export function commerceJson( p: Uint8Array ): Record<string, unknown> {
	if ( p.length > 1048576 ) throw Error( "Oversized commerce response" );
	const r = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( p ) );
	if ( !r || typeof r !== "object" || Array.isArray( r ) || r.version !== 1 ) {
		throw Error( "Unsupported commerce response" );
	}
	return r;
}

/*
================
commerceInteger
================
*/
export function commerceInteger( n: unknown, max = 0xffffffff, min = 0 ): number {
	if ( typeof n !== "number" || !Number.isInteger( n ) || n < min || n > max ) {
		throw Error( "Invalid commerce integer" );
	}
	return n;
}

/*
================
commerceGold
================
*/
function commerceGold( value: unknown ): boolean {
	return typeof value === "string" && /^\d{1,19}$/.test( value ) && BigInt( value ) <= 0x7fffffffffffffffn;
}

/*
================
shopCatalog
================
*/
export function shopCatalog( p: Uint8Array ): ShopState {
	const r = commerceJson( p );
	commerceInteger( r.npc, 0xffffffff, 1 );
	if (
		typeof r.name !== "string" || r.name.length > 256 ||
		r.error !== undefined && (typeof r.error !== "string" || r.error.length > 256) || !Array.isArray( r.offers ) ||
		r.offers.length > 65536
	) throw Error( "Invalid shop catalogue" );
	const seen = new Set<string>();
	for ( const row of r.offers ) {
		if ( !row || typeof row !== "object" ) throw Error( "Invalid shop offer" );
		commerceInteger( row.tab, 255 );
		commerceInteger( row.slot, 255 );
		commerceInteger( row.refObjId, 0xffffffff, 1 );
		commerceInteger( row.maxStack, 65535, 1 );
		if ( row.purchaseLimit !== undefined ) commerceInteger( row.purchaseLimit, 65535, 1 );
		if ( typeof row.name !== "string" || row.name.length > 256 || !commerceGold( row.price ) ) {
			throw Error( "Invalid shop price" );
		}
		if (
			row.currency !== undefined && row.currency !== SHOP_CURRENCY_GOLD && row.currency !== SHOP_CURRENCY_HONOR
		) throw Error( "Invalid shop currency" );
		if ( row.contents !== undefined ) {
			if ( !Array.isArray( row.contents ) || row.contents.length > 96 ) throw Error( "Invalid package contents" );
			for ( const item of row.contents ) {
				if ( !item || typeof item !== "object" || typeof item.name !== "string" || item.name.length > 256 ) {
					throw Error( "Invalid package item" );
				}
				commerceInteger( item.refObjId, 0xffffffff, 1 );
				commerceInteger( item.quantity, 65535, 1 );
				commerceInteger( item.plus, 255 );
			}
		}
		if ( row.previews !== undefined ) {
			if ( !Array.isArray( row.previews ) || row.previews.length > 96 ) throw Error( "Invalid shop previews" );
			for ( const preview of row.previews ) commercePreview( preview );
			if ( !row.previews.length || row.previews[0].refObjId !== row.refObjId ) {
				throw Error( "Mismatched shop preview" );
			}
		}
		const key = row.tab + ":" + row.slot;
		if ( seen.has( key ) ) throw Error( "Duplicate shop offer" );
		seen.add( key );
	}
	if ( r.tabs !== undefined ) {
		if ( !Array.isArray( r.tabs ) || r.tabs.length > 256 ) throw Error( "Invalid shop tabs" );
		const seenTabs = new Set<number>();
		for ( const tab of r.tabs ) {
			if (
				!tab || typeof tab !== "object" || typeof tab.labelSymbol !== "string" || tab.labelSymbol.length > 256
			) throw Error( "Invalid shop tab" );
			const index = commerceInteger( tab.index, 255 );
			if ( seenTabs.has( index ) ) throw Error( "Duplicate shop tab" );
			seenTabs.add( index );
		}
	}
	if ( r.buyback !== undefined && r.buyback !== null ) buybackEntries( r.buyback );
	if ( r.saleQuotes !== undefined && r.saleQuotes !== null ) {
		if ( !Array.isArray( r.saleQuotes ) || r.saleQuotes.length > 256 ) throw Error( "Invalid sale quotes" );
		const slots = new Set<number>();
		for ( const q of r.saleQuotes ) {
			if ( !q || typeof q !== "object" ) throw Error( "Invalid sale quote" );
			if ( q.noBuyback !== undefined && typeof q.noBuyback !== "boolean" ) {
				throw Error( "Invalid buyback policy" );
			}
			const slot = commerceInteger( q.slot, 255, 13 );
			commerceInteger( q.refObjId, 0xffffffff, 1 );
			commerceInteger( q.quantity, 65535, 1 );
			if ( slots.has( slot ) || !commerceGold( q.price ) ) throw Error( "Invalid sale quote" );
			slots.add( slot );
		}
	}
	return r as unknown as ShopState;
}

/*
================
buybackEntries
================
*/
export function buybackEntries( value: unknown ): readonly BuybackOffer[] {
	if ( !Array.isArray( value ) || value.length > 5 ) throw Error( "Invalid buyback ledger" );
	const seen = new Set<number>();
	for ( const row of value ) {
		if ( !row || typeof row !== "object" ) throw Error( "Invalid buyback entry" );
		const id = commerceInteger( row.id, 0xffffffff, 1 );
		commerceInteger( row.index, 4 );
		commerceInteger( row.refObjId, 0xffffffff, 1 );
		commerceInteger( row.quantity, 65535, 1 );
		commerceInteger( row.plus, 255 );
		if (
			seen.has( id ) || value.filter( e => e?.index === row.index ).length !== 1 ||
			typeof row.name !== "string" || row.name.length > 256 || !commerceGold( row.price )
		) throw Error( "Invalid buyback entry" );
		if ( row.preview !== undefined ) {
			commercePreview( row.preview );
			if ( row.preview.refObjId !== row.refObjId ) throw Error( "Mismatched buyback preview" );
		}
		seen.add( id );
	}
	return value as readonly BuybackOffer[];
}

/*
================
restoreSlotEntry

The entry the store's repurchase slot shows. CIFStore_RefreshTabSlots
(5B6440) walks the restore list from its back, so slot 0 holds the newest
sale (the highest list ordinal) and slot 4 the oldest, which the next sale
evicts (CGInterface_AllocateNotice148 68F480). Entry indices are list
ordinals, the ones the 0x77E7 request and its 0xB06D removal count.
================
*/
export function restoreSlotEntry( entries: readonly BuybackOffer[], slot: number ): BuybackOffer | undefined {
	return entries.find( row => row.index === entries.length - 1 - slot );
}

export interface CommerceItemReference {
	readonly tooltip?: ItemTooltipReference;
	readonly icon?: string;
	readonly maxStack?: number;
	readonly refObjId: number;
	readonly typeFlags: number;
	readonly name: string;
}

/*
================
commerceReferences
================
*/
export function commerceReferences( p: Uint8Array ): readonly CommerceItemReference[] {
	const r = commerceJson( p );
	if ( !Array.isArray( r.items ) || r.items.length > 256 ) throw Error( "Invalid commerce references" );
	const seen = new Set<number>();
	for ( const row of r.items ) {
		if ( !row || typeof row !== "object" || typeof row.name !== "string" || row.name.length > 256 ) {
			throw Error( "Invalid commerce reference" );
		}
		if ( row.icon !== undefined && (typeof row.icon !== "string" || row.icon.length > 256) ) {
			throw Error( "Invalid item icon" );
		}
		const id = commerceInteger( row.refObjId, 0xffffffff, 1 );
		commerceInteger( row.typeFlags, 65535 );
		if ( row.maxStack !== undefined ) commerceInteger( row.maxStack, 65535, 1 );
		if ( row.purchaseLimit !== undefined ) commerceInteger( row.purchaseLimit, 65535, 1 );
		if ( seen.has( id ) ) throw Error( "Duplicate commerce reference" );
		seen.add( id );
	}
	return r.items.map( row => ({
		...row,
		tooltip: itemTooltipReference( row.nativeFields, row.descriptionSymbol )
	}) ) as readonly CommerceItemReference[];
}

/*
================
commercePreview
================
*/
function commercePreview( value: unknown ): asserts value is CommercePreview {
	const row = value as CommercePreview;
	if (
		!row || typeof row !== "object" || typeof row.name !== "string" || row.name.length > 256 ||
		!Array.isArray( row.body ) || row.body.length < 4 || row.body.length > 2048
	) throw Error( "Invalid commerce preview" );
	commerceInteger( row.refObjId, 0xffffffff, 1 );
	commerceInteger( row.typeFlags, 65535 );
	for ( const byte of row.body ) commerceInteger( byte, 255 );
	const bytes = Uint8Array.from( row.body );
	if ( new DataView( bytes.buffer ).getUint32( 0, true ) !== row.refObjId ) {
		throw Error( "Mismatched commerce preview body" );
	}
}
