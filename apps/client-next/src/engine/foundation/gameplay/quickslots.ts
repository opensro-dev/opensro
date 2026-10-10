/*
===========================================================================

quickslots.ts - native hotbar bindings and command resolution

Bindings name inventory slots, skills or actions. Activation resolves the
current state; dragging references never moves items or grants skills.

===========================================================================
*/
import { itemActivation } from "./item-activation";

export const TRACE_ACTION_ID = 1003;
// actionwnddata 1011 "Helper status" (UIIT_STT_HELPER, icon_cha_helper).
export const HELPER_ACTION_ID = 1011;
const HOTBAR_PAGE_COUNT = 4;
const HOTBAR_PAGE_SLOTS = 10;
const EQUIPMENT_SLOT_COUNT = 13;
// Inventory move addresses are bytes; the live capacity narrows this wire bound.
export const MAX_INVENTORY_SLOT_COUNT = 256;
const QUICK_SLOT_COUNT = 51;
const EXTENDED_SLOT_START = 41;

/*
================
QuickSlot
================
*/
export interface QuickSlot {
	readonly slot: number;
	readonly kind: number;
	readonly payload: number;
}
/*
================
hotbarSlot

571E40 exposes four pages; 572990 leaves common slot zero invariant.
================
*/
export function hotbarSlot( page: number, key: number ): number {
	if (
		!Number.isInteger( page ) || page < 0 || page >= HOTBAR_PAGE_COUNT || !Number.isInteger( key ) || key < 0 ||
		key > HOTBAR_PAGE_SLOTS
	) {
		throw Error( "Invalid native hotbar index" );
	}
	return key === 0 ? 0 : page * HOTBAR_PAGE_SLOTS + key;
}
/*
================
quickSlotItemSlot

59BF50/592B80 bag indices exclude the equipment slots.
================
*/
export function quickSlotItemSlot( row: QuickSlot ): number | null {
	quickSlot( row );
	return row.kind === 0x46 ? row.payload + EQUIPMENT_SLOT_COUNT : row.kind === 0x47 ? row.payload : null;
}
/*
================
hotbarActions
================
*/
export function hotbarActions() {
	return [
		{ id: 1000, name: "Sit / Stand" },
		{ id: 4000, name: "Greeting" },
		{ id: 4001, name: "Laugh" },
		{ id: 4002, name: "Salute" },
		{ id: 4003, name: "Yes" },
		{ id: 4004, name: "Rush" },
		{ id: 4005, name: "Joy" },
		{ id: 4006, name: "No" },
		{ id: 5000, name: "Pet charm" }
	] as const;
}
/*
================
actionEmote

695420 maps action IDs to authored emote bytes, not their ordinal indices.
================
*/
export function actionEmote( id: number ): number | null {
	return id >= 4000 && id <= 4006 ? [ 0, 6, 1, 5, 2, 3, 4 ][id - 4000]! : null;
}
/*
================
quickSlotCommand

CIFUnderBar_DispatchQuickslotAction (572770) through its kind table
(57295C, kinds 0x25..0x4A): 0x46 uses the bag item the binding still names,
0x49 a skill, 0x4A an action, and 0x25 a pet command (the UI runs that
through the command bar's owner). Every other kind, the equipment binding
0x47 included, does nothing: a worn item is never taken off from the bar.
The bindings follow their items (574800, reconcileQuickslotInventory), so a
weapon equipped from the bar leaves its binding at 0x47.

Trace reaches the same worker command as the action panel. Actor life and
target type are checked there against admitted entities, not cached here.
================
*/
export function quickSlotCommand(
	row: QuickSlot,
	state: import("@/engine/contracts/gameplay").GameplayState,
	mountedOn = 0
): import("@/engine/contracts/gameplay").GameplayCommand | null {
	if ( row.kind === 0x46 ) {
		return itemActivation(
			row.payload + EQUIPMENT_SLOT_COUNT,
			state.inventory,
			state.inventorySlotCount,
			state.inventoryPending
		);
	}
	if ( row.kind === 0x49 && state.skills?.includes( row.payload ) ) {
		return { kind: "skill", skillId: row.payload, ...(state.target ? { gid: state.target } : {}) };
	}
	if ( row.kind === 0x4a ) {
		const id = row.payload & 0xffffff;
		if ( id === 1000 && !mountedOn || id === 1001 || actionEmote( id ) !== null ) {
			return { kind: "action-command", id };
		}
		if ( id === 5000 && state.cosRecords?.some( c => c.band === 4 && !c.dead && c.hp > 0 ) ) {
			return { kind: "action-command", id };
		}
		if ( id === 1002 && state.target ) return { kind: "attack", gid: state.target };
		if ( id === HELPER_ACTION_ID ) return { kind: "helper-mark" };
		// 695420 action 1003 refuses only a blocked interface, not a rider.
		if ( id === TRACE_ACTION_ID && state.target && !state.targetPending ) {
			return { kind: "action-command", id };
		}
	}
	return null;
}
/*
================
quickSlot

572080/572E00 bind client configuration; binding is not action execution.
================
*/
export function quickSlot( value: QuickSlot ): QuickSlot {
	const { slot, kind, payload } = value;
	if (
		!Number.isInteger( slot ) || slot < 0 || slot >= QUICK_SLOT_COUNT ||
		![ 0, 0x25, 0x46, 0x47, 0x49, 0x4a, 0x4e ].includes( kind ) || !Number.isInteger( payload ) || payload < 0 ||
		payload > 0xffffffff
	) throw new Error( "Invalid quickslot binding" );
	if (
		kind === 0x46 && payload >= MAX_INVENTORY_SLOT_COUNT - EQUIPMENT_SLOT_COUNT || kind === 0x47 && payload >= 13 ||
		kind === 0x4e && payload >= 4
	) {
		throw new Error( "Invalid quickslot item slot" );
	}
	return { slot, kind, payload: kind === 0 ? 0 : payload };
}
/*
================
quickSlotPacket
================
*/
export function quickSlotPacket( value: QuickSlot ) {
	const row = quickSlot( value ), payload = Uint8Array.of( 1, row.slot, row.kind, 0, 0, 0, 0 );
	new DataView( payload.buffer ).setUint32( 3, row.payload, true );
	return { opcode: 0x7541, payload };
}
/*
================
skillBindings

Validate the entire bootstrap before publishing a new binding table.
================
*/
export function skillBindings( value: unknown ) {
	const character = (value as { character?: { skills?: number[]; quickSlots?: QuickSlot[]; }; })?.character;
	const skills = character?.skills ?? [], rows = character?.quickSlots ?? [];
	if (
		!Array.isArray( skills ) || skills.length > 4096 ||
		skills.some( id => !Number.isInteger( id ) || id <= 0 || id > 0xffffffff ) ||
		new Set( skills ).size !== skills.length || !Array.isArray( rows ) || rows.length > QUICK_SLOT_COUNT
	) throw new Error( "Invalid skill/bootstrap bindings" );
	const bindings = rows.map( quickSlot );
	if ( new Set( bindings.map( row => row.slot ) ).size !== bindings.length ) throw new Error( "Duplicate quickslot" );
	return {
		skills: [ ...skills ],
		quickSlots: bindings.filter( row => row.kind !== 0 ).sort( ( a, b ) => a.slot - b.slot )
	};
}

/*
================
extendedSlot

CIFExtQuickSlot 548300 binds ten fixed underbar slots.
================
*/
export function extendedSlot( index: number ): number {
	if ( !Number.isInteger( index ) || index < 0 || index >= HOTBAR_PAGE_SLOTS ) {
		throw Error( "Invalid extended quickslot index" );
	}
	return EXTENDED_SLOT_START + index;
}
/*
================
quickSlotDrag
================
*/
export function quickSlotDrag(
	source: string,
	slot: number,
	state: import("@/engine/contracts/gameplay").GameplayState
): QuickSlot | null {
	if ( source.startsWith( "hotbar:" ) ) {
		const row = state.quickSlots?.find( r => r.slot === Number( source.slice( 7 ) ) );
		return row ? quickSlot( { ...row, slot } ) : null;
	}
	if ( source.startsWith( "skill:" ) ) {
		const id = Number( source.slice( 6 ) );
		return state.skills?.includes( id ) ? quickSlot( { slot, kind: 0x49, payload: id } ) : null;
	}
	if ( source.startsWith( "action:" ) ) {
		const id = Number( source.slice( 7 ) );
		return quickSlot( { slot, kind: id === 2 ? 0x25 : 0x4a, payload: id } );
	}
	if ( source.startsWith( "slot:" ) ) {
		const id = Number( source.slice( 5 ) );
		return Number.isInteger( id ) && id >= 0 && id < (state.inventorySlotCount ?? MAX_INVENTORY_SLOT_COUNT) &&
				state.inventory.some( r => r.slot === id ) ?
			quickSlot( { slot, kind: id < 13 ? 0x47 : 0x46, payload: id < 13 ? id : id - 13 } ) :
			null;
	}
	return null;
}

/*
================
quickSlotDrop

574B80 swaps hotbar sources and persists both slots. Other sources copy.
================
*/
export function quickSlotDrop(
	source: string,
	slot: number,
	state: import("@/engine/contracts/gameplay").GameplayState
): QuickSlot[] {
	const binding = quickSlotDrag( source, slot, state );
	if ( !binding ) return [];
	if ( !source.startsWith( "hotbar:" ) ) return [ binding ];
	const from = Number( source.slice( 7 ) );
	if ( from === slot ) return [];
	const displaced = state.quickSlots?.find( row => row.slot === slot );
	return [ quickSlot( displaced ? { ...displaced, slot: from } : { slot: from, kind: 0, payload: 0 } ), binding ];
}
