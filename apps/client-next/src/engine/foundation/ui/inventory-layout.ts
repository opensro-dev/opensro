/*
===========================================================================

inventory-layout.ts - the bag's geometry: pages, cells, lattice, page tabs

One owner for where the player's bag draws in every window that shows it
(the inventory, the gacha and the item mall). The native client places
these by hand in code, not in the layout files, so the numbers here are
the native constants, each cited by address.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";

// 59DF10: a bag page holds 32 cells, four columns of eight, 36 px apart.
const BAG_PAGE_SLOTS = 32;
const BAG_COLUMNS = 4;
const BAG_CELL_PITCH = 36;
const BAG_CELL_SIZE = 32;
// 59DF10: the inventory's first cell sits at (0x12, 0xd) inside the bag pane.
const INVENTORY_CELL_X = 18;
const INVENTORY_CELL_Y = 13;

// The com_tab page tab: 60x24 art (com_tab_on/off), created at a 0x3e stride
// by CIFInventory_RebuildCapacityAndGold (59DF10) and
// CIFGhaCha_CreateBagTabButtons (644170).
export const BAG_TAB_WIDTH = 60;
export const BAG_TAB_HEIGHT = 24;
export const BAG_TAB_STRIDE = 62;
// The bag pane both windows hang the tab row over: GDR_INVENTORY_FRAME,
// 176 px wide in ifinventory.txt and ifghacha.txt alike.
export const BAG_PANE_WIDTH = 176;

/*
================
BagTabOrigin

Where a window's first page tab sits, relative to its bag pane.
================
*/
export interface BagTabOrigin {
	readonly x: number;
	readonly y: number;
}

// 59DF10: the inventory's tabs start at (4, -0x17) from the bag pane.
export const INVENTORY_BAG_TABS = { x: 4, y: -23 } as const satisfies BagTabOrigin;
// 644170: the gacha's tabs start at (0x1db, 0x37) against its bag frame at
// (475, 77) in ifghacha.txt: flush with the frame, 22 px above it.
export const GACHA_BAG_TABS = { x: 0, y: -22 } as const satisfies BagTabOrigin;
// 644C20 creates the gacha's bag cells at (0x1ee, 0x5b) against the same
// frame: one pixel right of and below the inventory's (18, 13).
export const GACHA_BAG_CELL_SHIFT = 1;

/*
================
inventorySlots

59DF10: capacity is bag capacity (wire total minus equipment slots).
Each page has 32 controls; spare controls use pt_block, never wire slots.
================
*/
export function inventorySlots( x: number, y: number, total: number, equipment: number, page: number ) {
	const capacity = Math.max( 0, total - equipment ),
		pages = Math.max( 1, Math.ceil( capacity / BAG_PAGE_SLOTS ) ),
		selected = Math.max( 0, Math.min( pages - 1, page ) );
	return {
		pages,
		page: selected,
		slots: Array.from( { length: BAG_PAGE_SLOTS }, ( _, index ) => ({
			slot: equipment + selected * BAG_PAGE_SLOTS + index,
			enabled: selected * BAG_PAGE_SLOTS + index < capacity,
			rect: [
				x + INVENTORY_CELL_X + (index % BAG_COLUMNS) * BAG_CELL_PITCH,
				y + INVENTORY_CELL_Y + Math.floor( index / BAG_COLUMNS ) * BAG_CELL_PITCH,
				BAG_CELL_SIZE,
				BAG_CELL_SIZE
			] as UiRect
		}) )
	};
}

/*
================
bagTabRow

The page tabs over a bag pane at (paneX, paneY), in paint order.

Native creates one 60x24 tab per page at a 62 px stride and never more
than two (59DF10 asserts byTabNum <= MAX_INVENTORY_TAB_NUM, 2), so its
row always fits the 176 px pane. Up to two tabs this is exactly that.

Port-only, not native (the third page is SRO_INVENTORY_PAGES, #2769):
three tabs at the native stride would run 12 px past the pane into the
equipment pane. The art stays 60x24, drawn 1:1; only the stride tightens
until the row ends as far inside the pane's right edge as it starts
inside the left (owner, 2026-11-08: overlap, art unscaled). Overlapping
tabs paint left to right with the selected tab last, so it lies on top;
the hit test takes the last control under the pointer, so a click lands
on the tab that shows there.
================
*/
export function bagTabRow( paneX: number, paneY: number, origin: BagTabOrigin, pages: number, selected: number ) {
	const room = BAG_PANE_WIDTH - 2 * origin.x - BAG_TAB_WIDTH,
		stride = pages > 1 ? Math.min( BAG_TAB_STRIDE, Math.floor( room / (pages - 1) ) ) : BAG_TAB_STRIDE;
	const row = Array.from( { length: pages }, ( _, page ) => ({
		page,
		rect: [ paneX + origin.x + stride * page, paneY + origin.y, BAG_TAB_WIDTH, BAG_TAB_HEIGHT ] as UiRect
	}) );
	return [ ...row.filter( tab => tab.page !== selected ), ...row.filter( tab => tab.page === selected ) ];
}

/*
================
inventoryLattice

CIFLattice 6F7850: interior, right column, bottom row, bottom-right.
================
*/
export function inventoryLattice( x: number, y: number ) {
	return latticeCells( x, y, BAG_COLUMNS, BAG_PAGE_SLOTS / BAG_COLUMNS );
}

/*
================
latticeCells
================
*/
export function latticeCells( x: number, y: number, columns: number, rows: number ) {
	return Array.from( { length: columns * rows }, ( _, i ) => ({
		rect: [
			x + (i % columns) * BAG_CELL_PITCH,
			y + Math.floor( i / columns ) * BAG_CELL_PITCH,
			BAG_CELL_PITCH,
			BAG_CELL_PITCH
		] as UiRect,
		part: i >= columns * (rows - 1) ?
			(i % columns === columns - 1 ? "right_down" : "left_down") :
			(i % columns === columns - 1 ? "right_up" : "left_up")
	}) );
}

/*
================
equipmentSocket
================
*/
export function equipmentSocket( slot: number ) {
	return [
		"helm",
		"mail",
		"shoulderguard",
		"gauntlet",
		"pants",
		"boots",
		"weapon",
		"shield",
		"specialdress",
		"earring",
		"necklace",
		"l_ring",
		"r_ring"
	][slot];
}
