/*
===========================================================================

item-state-delta.ts - atomic field updates for native inventory receipts

Summoner state shares the native rent-state byte. Preserve the retained pet
reference and lease when an actor is dismissed, dies or is revived.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
// 7654B0: selected fields are read in bit order. Commit only after the full body.
/*
================
itemStateDelta
================
*/
export function itemStateDelta(
	p: Uint8Array,
	items: ReadonlyMap<number, InventoryItem>,
	refs: ReadonlyMap<number, number>,
	names: ReadonlyMap<number, string>
) {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let offset = 0;
	/*
================
take
================
	*/
	function take( n: number ) {
		if ( offset + n > p.length ) throw Error( "Truncated item state delta" );
		const at = offset;
		offset += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true ),
		u64 = () => v.getBigUint64( take( 8 ), true );
	const slot = u8(), mask = u8(), previous = items.get( slot );
	if ( !previous ) throw Error( "Item state delta references absent slot" );
	let item = { ...previous };
	if ( mask & 1 ) {
		const refObjId = u32(), typeFlags = refs.get( refObjId );
		if ( typeFlags === undefined ) throw Error( "Unknown item delta reference" );
		item = { ...item, refObjId, typeFlags, name: names.get( refObjId ) };
	}
	if ( mask & 2 ) item = { ...item, plus: u8() };
	if ( mask & 4 ) item = { ...item, variance: u64().toString() };
	if ( mask & 8 ) item = { ...item, quantity: u16() };
	if ( mask & 16 ) item = { ...item, durability: u32() };
	if ( mask & 32 ) {
		const count = u8();
		if ( count > 12 ) throw Error( "Invalid item magic count" );
		const magic = [];
		for ( let i = 0; i < count; i++ ) magic.push( u64().toString() );
		item = { ...item, magic };
	}
	if ( mask & 64 ) {
		const value = u8();
		if ( slot >= 13 ) {
			const summoner = (item.typeFlags & 0x7fe) === 0xcc;
			if ( summoner && (value < 1 || value > 4) ) throw Error( "Invalid summon state delta" );
			item = {
				...item,
				slotState: value,
				...(summoner ? { summon: { ...item.summon, state: value, rentals: item.summon?.rentals ?? [] } } : {})
			};
		}
	}
	if ( mask & 128 ) {
		const value = u32();
		if ( slot >= 13 ) {
			item = {
				...item,
				durationMs: (BigInt( value ) * 1000n).toString(),
				...(item.summon ? { summon: { ...item.summon, remainingSeconds: value } } : {})
			};
		}
	}
	if ( offset !== p.length ) throw Error( "Invalid item state delta length" );
	return { slot, item: item.quantity === 0 ? null : item };
}
