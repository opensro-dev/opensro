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
storageQuickMove

One click moves an item across an open warehouse: a bag slot deposits into
the room's first free slot, and a room slot withdraws into the bag's first
free slot. The native client does this on right-click. Ctrl+click does the
same; that is port-only, not native (owner decision 2026-10-10). Null when
the room is not open, a move is pending, the slot is empty or there is no
free slot.
================
*/
export function storageQuickMove(
	id: string,
	game: Pick<
		GameplayState,
		"storage" | "inventory" | "inventoryPending" | "equipmentSlotCount" | "inventorySlotCount"
	>
): StorageMove | null {
	const room = game.storage;
	if ( !room || room.phase !== "open" || game.inventoryPending ) return null;
	const equipmentSlots = game.equipmentSlotCount ?? DEFAULT_EQUIPMENT_SLOTS;
	if ( id.startsWith( "slot:" ) ) {
		const source = Number( id.slice( 5 ) );
		if ( !game.inventory.some( row => row.slot === source ) ) return null;
		const destination = firstFreeSlot( room.items, 0, room.capacity );
		if ( destination === null ) return null;
		return { type: STORAGE_MOVE_DEPOSIT, source, destination, quantity: 0, gold: 0 };
	}
	if ( id.startsWith( "storage-slot:" ) ) {
		const source = Number( id.slice( 13 ) );
		if ( !room.items.some( row => row.slot === source ) ) return null;
		const destination = firstFreeSlot( game.inventory, equipmentSlots, game.inventorySlotCount ?? equipmentSlots );
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
