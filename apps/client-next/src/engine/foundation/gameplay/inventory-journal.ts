/*
===========================================================================

inventory-journal.ts - which bag slots each inventory answer changed

Port-only diagnostics (owner decision 2026-10-14), not native. A bug report
carries the bag as it stands when the report is sent (state.json), but not
how it got there: a row the client dropped while the server kept it (an
item that turns invisible until a relog) leaves no trace. This journal
keeps, for each 0xB06D, 0xB5BD and 0x3645 frame, the bag slots it changed
and whether each still holds an item afterwards.

Slots and codes only: a row is [atMs, opcode, payload byte 0, payload
byte 1, slot, present after (1, 0)]; a frame that changed no slot keeps one
row with slot -1 and present -1, so refusals are visible too. An item
use (0xB5BD) that only spent a count records nothing. No item
reference, name or count is recorded. The newest INVENTORY_JOURNAL_ROWS
rows are kept.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";

// The answers the journal follows: inventory moves, item use, item state.
const OP_ITEM_MOVE_RESPONSE = 0xb06d;
const OP_ITEM_USE_RESPONSE = 0xb5bd;
const OP_ITEM_STATE = 0x3645;
// How many rows the journal keeps, and the numbers in each row.
export const INVENTORY_JOURNAL_ROWS = 48;
export const INVENTORY_JOURNAL_FIELDS = 6;

/*
================
inventoryJournaled

Whether the journal follows this answer's opcode.
================
*/
export function inventoryJournaled( opcode: number ): boolean {
	return opcode === OP_ITEM_MOVE_RESPONSE || opcode === OP_ITEM_USE_RESPONSE || opcode === OP_ITEM_STATE;
}

/*
================
inventorySignatures

Each occupied slot's comparison key: a slot changed when its key did. The
keys are compared, never recorded.
================
*/
export function inventorySignatures(
	items: readonly Pick<InventoryItem, "slot" | "refObjId" | "quantity" | "plus">[]
): Map<number, string> {
	return new Map( items.map( item => [ item.slot, `${item.refObjId}:${item.quantity}:${item.plus}` ] ) );
}

/*
================
journalInventoryFrame

The journal with this frame's rows appended, trimmed to the newest
INVENTORY_JOURNAL_ROWS.
================
*/
export function journalInventoryFrame(
	journal: readonly number[],
	frame: { readonly atMs: number; readonly opcode: number; readonly payload: Uint8Array; },
	before: ReadonlyMap<number, string>,
	after: ReadonlyMap<number, string>
): number[] {
	const head = [ frame.atMs, frame.opcode, frame.payload[0] ?? -1, frame.payload[1] ?? -1 ];
	const changed = new Set<number>();
	for ( const [slot, key] of before ) if ( after.get( slot ) !== key ) changed.add( slot );
	for ( const [slot, key] of after ) if ( before.get( slot ) !== key ) changed.add( slot );
	const rows: number[] = [];
	for ( const slot of [ ...changed ].sort( ( a, b ) => a - b ) ) {
		// Auto-potion answers about twice a second; a use that only spends
		// a count would push the moves that matter out of the window, so an
		// item use keeps only the slots it emptied or filled.
		if ( frame.opcode === OP_ITEM_USE_RESPONSE && before.has( slot ) === after.has( slot ) ) continue;
		rows.push( ...head, slot, after.has( slot ) ? 1 : 0 );
	}
	if ( rows.length === 0 ) {
		if ( changed.size ) return [ ...journal ];
		rows.push( ...head, -1, -1 );
	}
	const next = [ ...journal, ...rows ];
	const keep = INVENTORY_JOURNAL_ROWS * INVENTORY_JOURNAL_FIELDS;
	return next.length > keep ? next.slice( next.length - keep ) : next;
}
