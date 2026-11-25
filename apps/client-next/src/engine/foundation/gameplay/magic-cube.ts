/*
===========================================================================

magic-cube.ts - Magic Cube TID gate, filter, and linked-storage apply

Port-only, not native (#3906). v1.150 has no Magic Cube. TID 3/2/3
(band 0x40, group 0x180) is the mall cube; Param10 filter bits match
the reinforce-recipe codename markers the server uses
(game/magiccube). The use confirm key is UIIT_MSG_MAGIC_CUBE_USE.
Deposit / withdraw wire bodies follow the v1.696 jump table
(0x7f7be0 / 0x7f7b90). Linked storage is a port-only browser
projection beside the CSOItem StoredItemCount word.

===========================================================================
*/

import type { InventoryItem } from "@/engine/contracts/gameplay";

export const MAGIC_CUBE_USE_CONFIRM_KEY = "UIIT_MSG_MAGIC_CUBE_USE";

export const MAGIC_CUBE_FILTER_WEAPON = 1;
export const MAGIC_CUBE_FILTER_ARMOR = 2;
export const MAGIC_CUBE_FILTER_ACCESSARY = 4;
export const MAGIC_CUBE_FILTER_SHIELD = 8;

export const MAGIC_CUBE_MOVE_PUSH = 0x27;
export const MAGIC_CUBE_MOVE_POP = 0x28;

/*
================
isMagicCube

TID 3/2/3: item class, band 0x40, group 0x180 (same gate as the CSOItem
body reader for StoredItemCount).
================
*/
export function isMagicCube( typeFlags: number ): boolean {
	return (typeFlags & 2) === 0 && (typeFlags & 0x1c) === 0x0c && (typeFlags & 0x60) === 0x40 &&
		(typeFlags & 0x780) === 0x180;
}

/*
================
magicCubeElixirFilter

The Param10 bit an elixir's codename names. Zero when the row is not one
of the four reinforce categories a cube accepts.
================
*/
export function magicCubeElixirFilter( codename: string | undefined ): number {
	if ( !codename ) return 0;
	if ( codename.includes( "REINFORCE_RECIPE_WEAPON" ) ) return MAGIC_CUBE_FILTER_WEAPON;
	if ( codename.includes( "REINFORCE_RECIPE_ARMOR" ) ) return MAGIC_CUBE_FILTER_ARMOR;
	if ( codename.includes( "REINFORCE_RECIPE_ACCESSARY" ) ) return MAGIC_CUBE_FILTER_ACCESSARY;
	if ( codename.includes( "REINFORCE_RECIPE_SHIELD" ) ) return MAGIC_CUBE_FILTER_SHIELD;
	return 0;
}

/*
================
magicCubeMatchesFilter

Whether the elixir belongs in a cube authored with the given Param10 bit
(itemdata Param10 / nativeFields itemParam10_2c0).
================
*/
export function magicCubeMatchesFilter( cube: InventoryItem, elixir: InventoryItem ): boolean {
	const want = cube.nativeFields?.itemParam10_2c0 ?? 0;
	const got = magicCubeElixirFilter( elixir.codename );
	return want === MAGIC_CUBE_FILTER_WEAPON || want === MAGIC_CUBE_FILTER_ARMOR ||
			want === MAGIC_CUBE_FILTER_ACCESSARY || want === MAGIC_CUBE_FILTER_SHIELD ?
		got === want :
		false;
}

/*
================
magicCubeCapacity

Authored slot count (Param9 / itemParam9_2bc), or the linked-storage
capacity already published on the cube. Zero when neither is known.
================
*/
export function magicCubeCapacity( cube: InventoryItem ): number {
	if ( cube.linkedStorage && cube.linkedStorage.capacity > 0 ) return cube.linkedStorage.capacity;
	const authored = cube.nativeFields?.itemParam9_2bc ?? 0;
	return authored === 10 || authored === 20 || authored === 30 || authored === 50 ? authored : 0;
}

/*
================
magicCubeStoredCount

Sum of elixir stack counts inside the cube (the CSOItem Data word).
================
*/
export function magicCubeStoredCount( rows: readonly InventoryItem[] ): number {
	let total = 0;
	for ( const row of rows ) total += row.quantity;
	return total;
}

/*
================
encodeMagicCubePush

0x706D body for deposit: [0x27][src][dst][u16 qty].
================
*/
export function encodeMagicCubePush( source: number, cubeSlot: number, quantity: number ): Uint8Array {
	const p = new Uint8Array( 5 );
	p[0] = MAGIC_CUBE_MOVE_PUSH;
	p[1] = source;
	p[2] = cubeSlot;
	new DataView( p.buffer ).setUint16( 3, quantity, true );
	return p;
}

/*
================
encodeMagicCubePop

0x706D body for withdraw: [0x28][u32 RefObjID][src][dst][u16 qty].
================
*/
export function encodeMagicCubePop(
	refObjId: number,
	cubeSlot: number,
	destination: number,
	quantity: number
): Uint8Array {
	const p = new Uint8Array( 9 );
	p[0] = MAGIC_CUBE_MOVE_POP;
	const v = new DataView( p.buffer );
	v.setUint32( 1, refObjId, true );
	p[5] = cubeSlot;
	p[6] = destination;
	v.setUint16( 7, quantity, true );
	return p;
}

/*
================
applyMagicCubePushLocal

Move quantity of elixir into the cube's linked storage. Returns null when
the bag rows cannot account for the server's grant (wrong identity or
over-take).
================
*/
export function applyMagicCubePushLocal(
	cube: InventoryItem,
	elixir: InventoryItem,
	quantity: number,
	stackCap = 0
): { cube: InventoryItem; elixirLeft: InventoryItem | null; } | null {
	if ( quantity < 1 || elixir.quantity < quantity ) return null;
	const capacity = magicCubeCapacity( cube );
	if ( capacity < 1 ) return null;
	const rows = [ ...(cube.linkedStorage?.rows ?? []) ];
	const mergeAt = rows.findIndex( row => row.refObjId === elixir.refObjId );
	if ( mergeAt >= 0 ) {
		const existing = rows[mergeAt]!;
		if ( stackCap > 0 && existing.quantity + quantity > stackCap ) return null;
		rows[mergeAt] = { ...existing, quantity: existing.quantity + quantity };
	} else {
		if ( rows.length >= capacity ) return null;
		const free = freeLinkedSlot( rows, capacity );
		if ( free < 0 ) return null;
		rows.push( {
			slot: free,
			refObjId: elixir.refObjId,
			typeFlags: elixir.typeFlags,
			quantity,
			plus: 0,
			durability: 0,
			variance: "0",
			magic: [],
			name: elixir.name,
			codename: elixir.codename,
			icon: elixir.icon
		} );
		rows.sort( ( a, b ) => a.slot - b.slot );
	}
	const left = elixir.quantity - quantity;
	return {
		cube: {
			...cube,
			quantity: magicCubeStoredCount( rows ),
			linkedStorage: { capacity, rows }
		},
		elixirLeft: left > 0 ? { ...elixir, quantity: left } : null
	};
}

/*
================
applyMagicCubePopLocal

Withdraw quantity of the named elixir RefObjID from the cube into a bag
destination row (or a fresh empty slot).
================
*/
export function applyMagicCubePopLocal(
	cube: InventoryItem,
	refObjId: number,
	quantity: number,
	destination: InventoryItem | null,
	destinationSlot: number,
	stackCap = 0
): { cube: InventoryItem; destination: InventoryItem; } | null {
	if ( quantity < 1 || !cube.linkedStorage ) return null;
	const rows = [ ...cube.linkedStorage.rows ];
	const at = rows.findIndex( row => row.refObjId === refObjId && row.quantity > 0 );
	if ( at < 0 ) return null;
	const source = rows[at]!;
	if ( source.quantity < quantity ) return null;
	const left = source.quantity - quantity;
	if ( left > 0 ) rows[at] = { ...source, quantity: left };
	else rows.splice( at, 1 );
	let bagRow: InventoryItem;
	if ( destination ) {
		if ( destination.refObjId !== refObjId ) return null;
		if ( stackCap > 0 && destination.quantity + quantity > stackCap ) return null;
		bagRow = { ...destination, quantity: destination.quantity + quantity };
	} else {
		bagRow = {
			slot: destinationSlot,
			refObjId: source.refObjId,
			typeFlags: source.typeFlags,
			quantity,
			plus: 0,
			durability: 0,
			variance: "0",
			magic: [],
			name: source.name,
			codename: source.codename,
			icon: source.icon
		};
	}
	return {
		cube: {
			...cube,
			quantity: magicCubeStoredCount( rows ),
			linkedStorage: { capacity: cube.linkedStorage.capacity, rows }
		},
		destination: bagRow
	};
}

/*
================
freeLinkedSlot

Lowest unused linked-storage slot index inside capacity.
================
*/
function freeLinkedSlot( rows: readonly InventoryItem[], capacity: number ): number {
	const used = new Set( rows.map( row => row.slot ) );
	for ( let i = 0; i < capacity; i++ ) {
		if ( !used.has( i ) ) return i;
	}
	return -1;
}
