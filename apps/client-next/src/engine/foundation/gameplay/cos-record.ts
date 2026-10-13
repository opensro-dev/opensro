import { decodeInventoryItem } from "./inventory-item";
import type { CosRecord } from "@/engine/contracts/gameplay";
// 830ec0: reference-selected record, with helper gates 5500f0/5500b0/582160
// and trailing-field gates 82d310/82d350/82d370.
export function decodeCosRecord(
	p: Uint8Array,
	refs: ReadonlyMap<number, number>,
	itemRefs: ReadonlyMap<number, number> = new Map()
): CosRecord | null {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = 0;
	function take( n: number ) {
		if ( o + n > p.length ) throw new Error( "Truncated COS record" );
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true );
	const gid = u32(), refObjId = u32(), tid = refs.get( refObjId );
	if ( tid === undefined ) return null;
	const band = (tid >>> 11) & 31;
	if ( (tid & 0x7fe) !== 0x1c6 || band < 1 || band > 6 ) return null;
	if ( !gid ) throw new Error( "Invalid COS identity" );
	const hp = u32(), mp = u32();
	let experience: readonly [number, number] | undefined,
		level: number | undefined,
		satiety: number | undefined,
		commandMode: number | undefined,
		name: string | undefined;
	if ( band === 3 ) {
		experience = [ u32(), u32() ];
		level = u8();
		satiety = u16();
	}
	if ( band === 3 || band === 4 ) {
		commandMode = u32();
		const n = u16(), at = take( n );
		name = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( at, at + n ) );
	}
	// The bag capacity is one byte: v1.150 authors transport bags up to 177
	// slots (the item mall's level 120 transports), so no lower cap applies.
	const status = u8();
	const inventory: import("@/engine/contracts/gameplay").InventoryItem[] = [];
	if ( status !== 0 ) {
		const count = u8(), slots = new Set<number>();
		if ( count > status ) throw Error( "COS inventory exceeds capacity" );
		for ( let i = 0; i < count; i++ ) {
			const slot = u8();
			if ( slot >= status ) throw Error( "Invalid COS inventory slot" );
			if ( slots.has( slot ) ) throw new Error( "Duplicate COS inventory slot" );
			slots.add( slot );
			const decoded = decodeInventoryItem( p, o, itemRefs, refs );
			o = decoded.next;
			if ( decoded.item ) inventory.push( { ...decoded.item, slot } );
		}
	}
	const dead = band !== 1 && band !== 5 && band !== 6 ? u32() !== 0 : false;
	const inventorySlot = band === 3 || band === 4 ? u8() : undefined;
	if ( o !== p.length ) throw new Error( "Invalid COS record length" );
	return {
		gid,
		refObjId,
		band,
		hp,
		mp,
		status,
		dead,
		experience,
		level,
		satiety,
		commandMode,
		name,
		inventorySlot,
		inventory
	};
}
