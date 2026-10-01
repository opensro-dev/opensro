/*
===========================================================================

item-mall-wire.ts - native Item Mall purchase encoding

The server resolves merchandise and prices. The only monetary input is the
point contribution selected in the native confirmation dialog.

===========================================================================
*/

export const MALL_MOVE_TYPE = 0x18;
const MALL_PURCHASE_SIZE = 16;
const MAX_BYTE = 0xff;
const MAX_WORD = 0xffff;
const MAX_DWORD = 0xffffffff;

/*
================
MallPurchase

69825E serializes these fields; 7058E0 derives shop identity from reference data.
================
*/
export interface MallPurchase {
	readonly group: number;
	readonly shop: number;
	readonly tab: number;
	readonly slot: number;
	readonly quantity: number;
	readonly points: number;
	readonly packageId: number;
}

/*
================
checkedInteger
================
*/
function checkedInteger( value: number, maximum: number, minimum = 0 ): number {
	if ( !Number.isInteger( value ) || value < minimum || value > maximum ) {
		throw Error( "Invalid Item Mall purchase field" );
	}
	return value;
}

/*
================
mallPurchasePayload

6BFD00 permits points to replace part of the Silk cost. The points field is
an amount, not a currency identifier or a client-supplied total price.
================
*/
export function mallPurchasePayload( request: MallPurchase ): Uint8Array {
	const payload = new Uint8Array( MALL_PURCHASE_SIZE );
	const view = new DataView( payload.buffer );
	payload[0] = MALL_MOVE_TYPE;
	view.setUint16( 1, checkedInteger( request.group, MAX_WORD, 1 ), true );
	payload[3] = checkedInteger( request.shop, MAX_BYTE );
	payload[4] = checkedInteger( request.tab, MAX_BYTE );
	payload[5] = checkedInteger( request.slot, MAX_BYTE );
	view.setUint16( 6, checkedInteger( request.quantity, MAX_WORD, 1 ), true );
	view.setUint32( 8, checkedInteger( request.points, MAX_DWORD ), true );
	view.setUint32( 12, checkedInteger( request.packageId, MAX_DWORD, 1 ), true );
	return payload;
}
