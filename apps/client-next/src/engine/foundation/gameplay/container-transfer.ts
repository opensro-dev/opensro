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
import { etcCarriesPlusByte } from "./inventory-item";

// Expendable stackable class bits: (typeFlags & 0x7E) === 0x6C.
const STACKABLE_MASK = 0x7e;
const STACKABLE_CLASS = 0x6c;
const ITEM_TYPE_MASK = 0xfffe;
const ELIXIR_TYPE = 0x0d6c;
const HP_POTION_TYPE = 0x08ec;
const MP_POTION_TYPE = 0x10ec;
const VIGOR_POTION_TYPE = 0x18ec;
const PET_POTION_TYPE = 0x20ec;
const PET_VIGOR_POTION_TYPE = 0x48ec;
const LUCKY_POWDER_TYPE = 0x156c;

/*
================
retainedOversizedStack

Port-only, not native: mirror inventory.retainedOversizedStack after an
operator lowers SRO_STACK_SIZES. Only plain configurable families qualify.
================
*/
function retainedOversizedStack( item: InventoryItem, cap: number ): boolean {
	if (
		cap < 1 || item.quantity <= cap || item.plus !== 0 || item.variance !== "0" ||
		item.magic.length !== 0 || (item.transformRefObjId ?? 0) !== 0 || item.summon || item.label
	) return false;
	const type = item.typeFlags & ITEM_TYPE_MASK;
	if ( type === ELIXIR_TYPE ) return true;
	if ( cap === 1 ) return false;
	switch ( type ) {
		case HP_POTION_TYPE:
		case MP_POTION_TYPE:
		case VIGOR_POTION_TYPE:
		case PET_POTION_TYPE:
		case PET_VIGOR_POTION_TYPE:
		case LUCKY_POWDER_TYPE:
			return true;
		default:
			return false;
	}
}

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
	const splittable = cap > 1 || retainedOversizedStack( a, cap );
	if ( cap > 1 && b && sameStackIdentity( a, b ) ) {
		const dest = b.quantity >= cap ? a.quantity : Math.min( cap, a.quantity + b.quantity );
		const remain = b.quantity >= cap ? b.quantity : a.quantity + b.quantity - dest;
		slots.set( move.destination, { ...b, quantity: dest } );
		if ( remain ) slots.set( move.source, { ...a, quantity: remain } );
		else slots.delete( move.source );
	} else if ( splittable && !b && move.quantity < a.quantity ) {
		if ( move.quantity < 1 ) throw Error( `Invalid ${label} split quantity` );
		slots.set( move.source, { ...a, quantity: a.quantity - move.quantity } );
		slots.set( move.destination, { ...a, slot: move.destination, quantity: move.quantity } );
	} else {
		if ( splittable && !b && move.quantity !== a.quantity ) throw Error( `Invalid ${label} split quantity` );
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
	if ( other && sameStackIdentity( item, other ) && stackable( item ) ) {
		const cap = caps.get( item.refObjId );
		if (
			cap === undefined || !Number.isInteger( cap ) || cap < 1 || cap > 65535 ||
			![ item, other ].every( row =>
				Number.isInteger( row.quantity ) && row.quantity > 0 && row.quantity <= 65535 &&
				(row.quantity <= cap || retainedOversizedStack( row, cap ))
			)
		) throw Error( "Invalid transfer stack limit/count" );
		const dest = other.quantity >= cap ? item.quantity : Math.min( cap, item.quantity + other.quantity );
		const remain = other.quantity >= cap ? other.quantity : item.quantity + other.quantity - dest;
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

/*
================
nativeStackIdentity

490230 compares the original cargo-owner string before merging trade goods.
================
*/
export function nativeStackIdentity( a: InventoryItem, b: InventoryItem ): boolean {
	if ( a.refObjId !== b.refObjId ) return false;
	if ( (a.typeFlags & 0x7fe) === 0x46c || (b.typeFlags & 0x7fe) === 0x46c ) {
		return (a.label ?? "") === (b.label ?? "");
	}
	return true;
}

/*
================
sameStackIdentity

The merge identity: nativeStackIdentity, and port-only, not native, a
stone's plus (its assimilation value) must match too. Natively stones stack
1, so no merge reaches the extra test; the server applies the same rule
(stackIdentityMatches, #583).
================
*/
export function sameStackIdentity( a: InventoryItem, b: InventoryItem ): boolean {
	if ( !nativeStackIdentity( a, b ) ) return false;
	if ( etcCarriesPlusByte( a.typeFlags ) || etcCarriesPlusByte( b.typeFlags ) ) return a.plus === b.plus;
	return true;
}
