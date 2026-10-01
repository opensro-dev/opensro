/*
===========================================================================

container-transfer.ts - slot arithmetic shared by every item container

The COS bag, the account warehouse and the player bag move items with the
same native rules (756A60 stack merge, 756CF0 whole-source transfers), so
the arithmetic lives here once and each owner validates its own identity
and capacity before calling in.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";

// Expendable stackable class bits: (typeFlags & 0x7E) === 0x6C.
const STACKABLE_MASK = 0x7e;
const STACKABLE_CLASS = 0x6c;

/*
================
stackable
================
*/
export function stackable( item: InventoryItem ): boolean {
	return (item.typeFlags & STACKABLE_MASK) === STACKABLE_CLASS;
}

/*
================
stackCap
================
*/
function stackCap( item: InventoryItem, caps: ReadonlyMap<number, number>, label: string ): number {
	const cap = stackable( item ) ? caps.get( item.refObjId ) : 1;
	if ( cap === undefined || !Number.isInteger( cap ) || cap < 1 || cap > 65535 ) {
		throw Error( `Missing ${label} stack limit` );
	}
	return cap;
}

/*
================
planContainerMove

One move inside a container (the COS bag, the warehouse): 756A60 merges a
compatible stack ignoring the requested split, splits into an empty slot,
or swaps. Callers have validated both slots and the source item.
================
*/
export function planContainerMove(
	rows: readonly InventoryItem[],
	move: { readonly source: number; readonly destination: number; readonly quantity: number; },
	caps: ReadonlyMap<number, number>,
	label: string
): InventoryItem[] {
	const slots = new Map( rows.map( row => [ row.slot, row ] ) );
	const a = slots.get( move.source ), b = slots.get( move.destination );
	if ( !a ) throw Error( `Empty ${label} source slot` );
	const cap = stackCap( a, caps, label );
	if ( cap > 1 && b?.refObjId === a.refObjId ) {
		const dest = b.quantity >= cap ? a.quantity : Math.min( cap, a.quantity + b.quantity );
		const remain = b.quantity >= cap ? b.quantity : a.quantity + b.quantity - dest;
		slots.set( move.destination, { ...b, quantity: dest } );
		if ( remain ) slots.set( move.source, { ...a, quantity: remain } );
		else slots.delete( move.source );
	} else if ( cap > 1 && !b && move.quantity < a.quantity ) {
		if ( move.quantity < 1 ) throw Error( `Invalid ${label} split quantity` );
		slots.set( move.source, { ...a, quantity: a.quantity - move.quantity } );
		slots.set( move.destination, { ...a, slot: move.destination, quantity: move.quantity } );
	} else {
		if ( cap > 1 && !b && move.quantity !== a.quantity ) throw Error( `Invalid ${label} split quantity` );
		slots.set( move.destination, { ...a, slot: move.destination } );
		if ( b ) slots.set( move.source, { ...b, slot: move.source } );
		else slots.delete( move.source );
	}
	return [ ...slots.values() ].sort( ( x, y ) => x.slot - y.slot );
}

/*
================
planWholeTransfer

The whole source stack crosses containers (756CF0): a compatible stack
merges, anything else swaps or moves. Callers validated slot ranges.
================
*/
export function planWholeTransfer(
	from: readonly InventoryItem[],
	to: readonly InventoryItem[],
	source: number,
	destination: number,
	caps: ReadonlyMap<number, number>
): { readonly from: InventoryItem[]; readonly to: InventoryItem[]; } {
	const a = new Map( from.map( row => [ row.slot, row ] ) ), b = new Map( to.map( row => [ row.slot, row ] ) );
	const item = a.get( source );
	if ( !item ) throw Error( "Transfer requires a source item" );
	const other = b.get( destination );
	if ( other && other.refObjId === item.refObjId && stackable( item ) ) {
		const cap = caps.get( item.refObjId );
		if (
			cap === undefined || !Number.isInteger( cap ) || cap < 1 || cap > 65535 ||
			![ item.quantity, other.quantity ].every( n => Number.isInteger( n ) && n > 0 && n <= cap )
		) throw Error( "Invalid transfer stack limit/count" );
		const dest = other.quantity === cap ? item.quantity : Math.min( cap, item.quantity + other.quantity );
		const remain = other.quantity === cap ? cap : item.quantity + other.quantity - dest;
		b.set( destination, { ...other, quantity: dest } );
		if ( remain ) a.set( source, { ...item, quantity: remain } );
		else a.delete( source );
	} else {
		b.set( destination, { ...item, slot: destination } );
		if ( other ) a.set( source, { ...other, slot: source } );
		else a.delete( source );
	}
	const sort = ( rows: Map<number, InventoryItem> ) => [ ...rows.values() ].sort( ( x, y ) => x.slot - y.slot );
	return { from: sort( a ), to: sort( b ) };
}
