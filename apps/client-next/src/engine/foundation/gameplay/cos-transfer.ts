/*
===========================================================================

cos-transfer.ts - items crossing between the player bag and a COS bag

===========================================================================
*/
import type { CosRecord, InventoryItem } from "@/engine/contracts/gameplay";
import { planWholeTransfer, sameStackIdentity, stackable } from "./container-transfer";

/*
================
planCosTransfer

Native 697E80 cases 1A/1B: GID + source/destination, no quantity.
================
*/
export function planCosTransfer(
	record: CosRecord,
	player: readonly InventoryItem[],
	toCos: boolean,
	source: number,
	destination: number,
	capacity: number,
	equipment: number,
	caps: ReadonlyMap<number, number>
) {
	if (
		record.dead || record.hp === 0 || !record.inventory || !Number.isInteger( record.status ) || record.status < 1
	) {
		throw Error( "COS container unavailable" );
	}
	if ( !Number.isInteger( capacity ) || !Number.isInteger( equipment ) || equipment < 0 || capacity <= equipment ) {
		throw Error( "Player capacity unavailable" );
	}
	const fromRows = toCos ? player : record.inventory;
	if (
		!Number.isInteger( source ) || !Number.isInteger( destination ) || source < (toCos ? equipment : 0) ||
		source >= (toCos ? capacity : record.status) || destination < (toCos ? 0 : equipment) ||
		destination >= (toCos ? record.status : capacity) || !fromRows.some( row => row.slot === source )
	) throw Error( "Transfer requires valid bag slots and a source item" );
	const moved = planWholeTransfer( fromRows, toCos ? record.inventory : player, source, destination, caps );
	return { player: toCos ? moved.from : moved.to, cos: { ...record, inventory: toCos ? moved.to : moved.from } };
}

/*
================
cosQuickDestination

The slot a quick transfer (one click, no destination chosen) fills: a
stack of the same item that still has room, else the first free slot,
else none. A full stack never qualifies: 756CF0 would only trade its
count with the source, so an item whose stack limit is 1 (the elixirs)
swapped with its twin and appeared not to move.
================
*/
export function cosQuickDestination(
	record: CosRecord,
	player: readonly InventoryItem[],
	toCos: boolean,
	source: number,
	slots: { readonly capacity: number; readonly equipment: number; },
	caps: ReadonlyMap<number, number>
): number | null {
	const item = (toCos ? player : record.inventory ?? []).find( row => row.slot === source );
	if ( !item ) return null;
	const rows = toCos ? record.inventory ?? [] : player,
		start = toCos ? 0 : slots.equipment,
		end = toCos ? record.status : slots.capacity;
	const cap = caps.get( item.refObjId ) ?? 1;
	if ( stackable( item ) ) {
		const stack = rows.find( row =>
			row.slot >= start && row.slot < end && sameStackIdentity( item, row ) && row.quantity < cap
		);
		if ( stack ) return stack.slot;
	}
	const used = new Set( rows.map( row => row.slot ) );
	for ( let slot = start; slot < end; slot++ ) if ( !used.has( slot ) ) return slot;
	return null;
}

/*
================
cosTransferRequest
================
*/
export function cosTransferRequest( gid: number, toCos: boolean, source: number, destination: number ) {
	if ( !Number.isInteger( gid ) || gid < 1 || gid > 0xffffffff ) throw Error( "Invalid COS identity" );
	if ( ![ source, destination ].every( slot => Number.isInteger( slot ) && slot >= 0 && slot <= 255 ) ) {
		throw Error( "Invalid transfer slot" );
	}
	const payload = new Uint8Array( 7 );
	payload[0] = toCos ? 0x1b : 0x1a;
	new DataView( payload.buffer ).setUint32( 1, gid, true );
	payload[5] = source;
	payload[6] = destination;
	return { opcode: 0x706d, payload };
}
