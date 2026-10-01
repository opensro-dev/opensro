import type { CosRecord } from "@/engine/contracts/gameplay";
import { decodeInventoryItem } from "./inventory-item";
import { planContainerMove } from "./container-transfer";

export function cosGroundRequest( record: CosRecord, kind: "pickup" | "drop", target: number ) {
	if ( record.dead || !record.hp || !record.inventory ) throw Error( "COS container unavailable" );
	const drop = kind === "drop";
	if (
		!Number.isInteger( target ) || target < (drop ? 0 : 1) || target > (drop ? 255 : 0xffffffff) ||
		drop && !record.inventory.some( row => row.slot === target )
	) throw Error( "Invalid COS ground target" );
	const payload = new Uint8Array( drop ? 6 : 9 ), v = new DataView( payload.buffer );
	payload[0] = drop ? 0x12 : 0x11;
	v.setUint32( 1, record.gid, true );
	if ( drop ) payload[5] = target;
	else v.setUint32( 5, target, true );
	return { opcode: 0x706d, payload };
}

export function cosGroundResult( record: CosRecord, p: Uint8Array, refs: ReadonlyMap<number, number> ): CosRecord {
	if (
		p.length < 7 || p[0] !== 1 || ![ 0x11, 0x12 ].includes( p[1]! ) ||
		new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 2, true ) !== record.gid || !record.inventory
	) throw Error( "Invalid COS ground result" );
	const slot = p[6]!;
	if ( p[1] === 0x12 ) {
		if ( p.length !== 7 || !record.inventory.some( row => row.slot === slot ) ) {
			throw Error( "Invalid COS drop source" );
		}
		return { ...record, inventory: record.inventory.filter( row => row.slot !== slot ) };
	}
	if ( slot === 254 ) {
		if ( p.length !== 11 ) throw Error( "Invalid COS gold pickup" );
		return record;
	}
	if ( slot >= record.status ) throw Error( "COS pickup slot exceeds capacity" );
	const decoded = decodeInventoryItem( p, 7, refs );
	if ( !decoded.item || decoded.next !== p.length ) throw Error( "Invalid COS pickup item" );
	return {
		...record,
		inventory: [ ...record.inventory.filter( row => row.slot !== slot ), { ...decoded.item, slot } ].sort( (
			a,
			b
		) => a.slot - b.slot )
	};
}

function slots( record: CosRecord, source: number, destination: number, quantity: number ) {
	if (
		record.dead || record.hp === 0 || !record.inventory || !Number.isInteger( record.status ) || record.status < 1
	) throw Error( "COS container unavailable" );
	if (
		!Number.isInteger( source ) || !Number.isInteger( destination ) || source < 0 || destination < 0 ||
		source >= record.status || destination >= record.status || source === destination ||
		!Number.isInteger( quantity ) || quantity < 0 || quantity > 65535
	) throw Error( "Invalid COS container transfer" );
	if ( !record.inventory.some( row => row.slot === source ) ) throw Error( "Empty COS source slot" );
}

// 697E80 case 10, outgoing and incoming: no player submove count.
export function cosContainerRequest( record: CosRecord, source: number, destination: number, quantity: number ) {
	slots( record, source, destination, quantity );
	const payload = new Uint8Array( 9 ), v = new DataView( payload.buffer );
	payload[0] = 0x10;
	v.setUint32( 1, record.gid, true );
	payload[5] = source;
	payload[6] = destination;
	v.setUint16( 7, quantity, true );
	return { opcode: 0x706d, payload };
}

export function cosContainerResult( record: CosRecord, p: Uint8Array, caps: ReadonlyMap<number, number> ): CosRecord {
	if ( p.length !== 10 || p[0] !== 1 || p[1] !== 0x10 ) throw Error( "Invalid COS container result" );
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	if ( v.getUint32( 2, true ) !== record.gid ) throw Error( "COS container identity mismatch" );
	const source = p[6]!, destination = p[7]!, quantity = v.getUint16( 8, true );
	slots( record, source, destination, quantity );
	// Shared native 756A60 arithmetic: merge ignores requested split quantity.
	return {
		...record,
		inventory: planContainerMove( record.inventory!, { source, destination, quantity }, caps, "COS" )
	};
}
