/*
===========================================================================

storage-panel.ts - the warehouse window's presentation state and geometry

CIFStorageRoom (resinfo ifstorageroom.txt) draws 30 slots per page,
GDR_STORAGE_SLOT_100..129 in six columns of five rows on a 36-pixel pitch,
a page spinner (id 13) and the deposited gold (ids 10/12/14) beside the
money button (id 11). The page is the only state the window owns; the room
itself belongs to the gameplay worker (storage-room.ts).

===========================================================================
*/
import type { GameplayState, InventoryItem } from "@/engine/contracts/gameplay";
import {
	STORAGE_MOVE_DEPOSIT,
	STORAGE_MOVE_WITHDRAW,
	STORAGE_PAGE_SLOTS,
	type StorageMove
} from "@/engine/foundation/gameplay/storage-room";
import { sameStackIdentity, stackable } from "@/engine/foundation/gameplay/container-transfer";

// GDR_STORAGE_SLOT_100 is the first slot control.
const FIRST_SLOT_ID = 100;

/*
================
storagePages
================
*/
export function storagePages( capacity: number ): number {
	return Math.max( 1, Math.ceil( capacity / STORAGE_PAGE_SLOTS ) );
}

/*
================
storageSlotControlId

The layout control that draws page slot i.
================
*/
export function storageSlotControlId( i: number ): number {
	return FIRST_SLOT_ID + i;
}

/*
================
firstFreeSlot

5B0BF0: a deposit onto an occupied slot lands on the first free slot of
that page; the bag's own right-click deposit uses the first free slot.
================
*/
export function firstFreeSlot(
	items: readonly InventoryItem[],
	start: number,
	end: number
): number | null {
	const used = new Set( items.map( row => row.slot ) );
	for ( let slot = start; slot < end; slot++ ) if ( !used.has( slot ) ) return slot;
	return null;
}

// The bag slots before this one are the equipment slots (CICUser's 13).
const DEFAULT_EQUIPMENT_SLOTS = 13;

/*
================
stackSlot

Port-only, not native: the first slot in [start, end) holding a stack the
whole of `item` merges into, under the same identity and cap as the drag
merge (756A60, container-transfer.ts). Null when none has room.
================
*/
function stackSlot( items: readonly InventoryItem[], item: InventoryItem, start: number, end: number ): number | null {
	const cap = item.tooltip?.fields.maxStack ?? 0;
	if ( !stackable( item ) || cap < 2 ) return null;
	let found: number | null = null;
	for ( const row of items ) {
		if (
			row.slot >= start && row.slot < end && (found === null || row.slot < found) &&
			sameStackIdentity( item, row ) && row.quantity + item.quantity <= cap
		) found = row.slot;
	}
	return found;
}

/*
================
StorageQuickPlace

The open room page, and whether the server published its port-only
storage auto-stack rule (SRO_STORAGE_AUTO_STACK).
================
*/
export interface StorageQuickPlace {
	readonly page: number;
	readonly autoStack: boolean;
}

/*
================
storageQuickMove

One click moves an item across an open warehouse: a room slot withdraws
into the bag's first free slot, and a bag slot deposits into the first free
slot of the room page that is open. The native client does this on
right-click: 567290 (window 0x13 visible) asks 5B0BA0 for the first empty
slot of the window's page (+0x7E4). On a full page native sends slot 0,
which swaps out whatever item is there; the port instead takes the room's
first free slot on any page (port-only, not native: the swap is a native
bug). Ctrl+click does the same as right-click; that is port-only, not
native (owner decision 2026-10-10).

Auto-stack (port-only, not native; owner decision 2026-10-11): when the
server publishes SRO_STORAGE_AUTO_STACK, the item first goes onto a
matching stack with room for all of it, on the open page and then any page
(or anywhere in the bag), as a drag onto that stack would. Off restores the
empty-slot rule exactly.

Null when the room is not open, a move is pending, the slot is empty or
there is no free slot.
================
*/
export function storageQuickMove(
	id: string,
	game: Pick<
		GameplayState,
		"storage" | "inventory" | "inventoryPending" | "equipmentSlotCount" | "inventorySlotCount"
	>,
	place: StorageQuickPlace
): StorageMove | null {
	const room = game.storage;
	if ( !room || room.phase !== "open" || game.inventoryPending ) return null;
	const equipmentSlots = game.equipmentSlotCount ?? DEFAULT_EQUIPMENT_SLOTS;
	if ( id.startsWith( "slot:" ) ) {
		const source = Number( id.slice( 5 ) );
		const item = game.inventory.find( row => row.slot === source );
		if ( !item ) return null;
		const pageStart = place.page * STORAGE_PAGE_SLOTS,
			pageEnd = Math.min( room.capacity, pageStart + STORAGE_PAGE_SLOTS );
		const stacked = place.autoStack ?
			stackSlot( room.items, item, pageStart, pageEnd ) ?? stackSlot( room.items, item, 0, room.capacity ) :
			null;
		const destination = stacked ?? firstFreeSlot( room.items, pageStart, pageEnd ) ??
			firstFreeSlot( room.items, 0, room.capacity );
		if ( destination === null ) return null;
		return { type: STORAGE_MOVE_DEPOSIT, source, destination, quantity: 0, gold: 0 };
	}
	if ( id.startsWith( "storage-slot:" ) ) {
		const source = Number( id.slice( 13 ) );
		const item = room.items.find( row => row.slot === source );
		if ( !item ) return null;
		const bagEnd = game.inventorySlotCount ?? equipmentSlots;
		const stacked = place.autoStack ? stackSlot( game.inventory, item, equipmentSlots, bagEnd ) : null;
		const destination = stacked ?? firstFreeSlot( game.inventory, equipmentSlots, bagEnd );
		if ( destination === null ) return null;
		return { type: STORAGE_MOVE_WITHDRAW, source, destination, quantity: 0, gold: 0 };
	}
	return null;
}

/*
================
createStoragePanel
================
*/
export function createStoragePanel() {
	let page = 0;
	return {
		/*
================
page
================
		*/
		page( capacity: number ): number {
			page = Math.min( page, storagePages( capacity ) - 1 );
			return page;
		},
		/*
================
turn
================
		*/
		turn( delta: number, capacity: number ) {
			page = Math.max( 0, Math.min( storagePages( capacity ) - 1, page + delta ) );
		},
		/*
================
reset
================
		*/
		reset() {
			page = 0;
		}
	};
}
