/*
===========================================================================

inventory-item.ts - reference-directed native item record decoding

Container readers share this boundary so a variable-length companion record
cannot shift the following item. Unknown states and families fail atomically.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
// CSOItem::Deserialize 78c830; magic list 78b1b0. Returns the next byte so
// container owners can compose rows without guessing a reference-dependent size.
/*
================
decodeInventoryItem
================
*/
export function decodeInventoryItem(
	p: Uint8Array,
	offset: number,
	refs: ReadonlyMap<number, number>,
	objRefs: ReadonlyMap<number, number> = new Map()
): { item: InventoryItem | null; next: number; } {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = offset;
	/*
================
take
================
	*/
	function take( n: number ) {
		if ( o < 0 || o + n > p.length ) throw new Error( "Truncated inventory item" );
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true ),
		u64 = () => v.getBigUint64( take( 8 ), true ).toString();
	const refObjId = u32();
	if ( refObjId === 0 ) return { item: null, next: o };
	const typeFlags = refs.get( refObjId );
	if ( typeFlags === undefined ) throw new Error( "Unknown inventory reference" );
	if ( (typeFlags & 2) || (typeFlags & 0x1c) !== 0xc ) throw new Error( "Unsupported inventory item class" );
	let quantity = 1,
		plus = 0,
		durability = 0,
		variance = "0",
		label: string | undefined,
		summon: InventoryItem["summon"],
		transformRefObjId: number | undefined;
	const magic: string[] = [];
	/*
================
options
================
	*/
	function options() {
		const count = u8();
		if ( count > 12 ) throw new Error( "Invalid inventory magic count" );
		for ( let i = 0; i < count; i++ ) magic.push( u64() );
	}
	const band = typeFlags & 0x60, group = typeFlags & 0x780, sub = typeFlags & 0xf800;
	if ( band === 0x20 ) {
		plus = u8();
		variance = u64();
		durability = u32();
		options();
	} else if ( band === 0x60 ) {
		if ( group === 0x280 ) quantity = u32();
		else {
			quantity = u16();
			if ( group === 0x400 ) {
				const n = u16(), at = take( n );
				label = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( at, at + n ) );
			} else {
				if ( group === 0x580 && (sub === 0x800 || sub === 0x1000) ) plus = u8();
				if ( group === 0x700 && sub === 0x1000 ) options();
			}
		}
	} else if ( band === 0x40 ) {
		if ( group === 0x100 ) transformRefObjId = u32();
		else if ( group === 0x80 ) {
			const state = u8();
			if ( state < 1 || state > 4 ) throw Error( "Invalid summon state" );
			const rentals: { kind: 0 | 5; id: number; seconds: number; tag?: number; flag?: number; }[] = [];
			if ( state === 1 ) summon = { state, rentals };
			else {
				const refObjId = u32(), tid = objRefs.get( refObjId );
				if ( tid === undefined ) throw new Error( "Unknown summoned object reference" );
				if ( (tid & 0x7fe) !== 0x1c6 || ![ 3, 4 ].includes( tid >>> 11 ) ) {
					throw Error( "Invalid summoned object family" );
				}
				let name: string | undefined, remainingSeconds: number | undefined;
				if ( (tid & 0x7fe) === 0x1c6 && [ 3, 4 ].includes( tid >>> 11 ) ) {
					const n = u16(), at = take( n );
					name = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( at, at + n ) );
					if ( tid >>> 11 === 4 ) remainingSeconds = v.getInt32( take( 4 ), true );
					const count = u8();
					for ( let i = 0; i < count; i++ ) {
						const kind = u8(), id = u32(), seconds = v.getInt32( take( 4 ), true );
						if ( kind !== 0 && kind !== 5 ) throw new Error( "Unsupported summon rental variant" );
						rentals.push( { kind, id, seconds, ...(kind === 5 ? { tag: u32(), flag: u8() } : {}) } );
					}
				}
				summon = { state, refObjId, name, remainingSeconds, rentals };
			}
		}
	} else throw new Error( "Unsupported inventory item class" );
	return {
		item: {
			slot: 0,
			refObjId,
			typeFlags,
			quantity,
			plus,
			durability,
			variance,
			magic,
			...(summon ? { summon } : {}),
			...(transformRefObjId === undefined ? {} : { transformRefObjId }),
			...(label === undefined ? {} : { label })
		},
		next: o
	};
}
