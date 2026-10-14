/*
===========================================================================

bag-tab-drop.ts - where an item dropped on a bag page's tab lands

Port-only, not native (Experimental "Drop on bag tabs", the owner's choice
on 2026-10-15, on by default). The original's page tabs answer a button
press only (5373F0 routes 0x201), so a drag released over the other page's
tab does nothing; moving an item between pages takes a click on the item,
a click on the tab and a click on a free slot. With the row on, the drop
asks for the target page's first free slot, through the same move request
a drop on that slot sends.

===========================================================================
*/
import { inventorySlots } from "@/engine/foundation/ui/inventory-layout";

/*
================
bagTabDropSlot

The first free, enabled slot of a bag page, or null when the page is full
or holds the dragged item already.
================
*/
export function bagTabDropSlot(
	items: readonly { readonly slot: number; }[],
	total: number,
	equipment: number,
	page: number,
	source: number
): number | null {
	const layout = inventorySlots( 0, 0, total, equipment, page );
	if ( layout.page !== page || layout.slots.some( cell => cell.slot === source ) ) return null;
	const used = new Set( items.map( item => item.slot ) );
	return layout.slots.find( cell => cell.enabled && !used.has( cell.slot ) )?.slot ?? null;
}
