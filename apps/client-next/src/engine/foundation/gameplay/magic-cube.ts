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
export const MAGIC_CUBE_MOVE_CONSUME = 0x29;

const MAGIC_CUBE_DESC = "Stores matching elixirs. Empty it before selling or listing on the market.";

// Port-only (#3906): v1.150 ships no Magic Cube text rows. These fallback
// strings match the v1.696 Media.pk2 English rows for UI notices and all
// sixteen Item Mall packages so titles, tooltips and search resolve even
// before client textdataname.en.json is rebuilt with SRO_MAGIC_CUBE=on.
// A shipped / rebuilt catalog entry always wins over this table.
export const MAGIC_CUBE_COPY: Readonly<Record<string, string>> = Object.freeze( {
	UIIT_MSG_MAGIC_CUBE_USE: "Will you use the Magic Cube?",
	UIIT_STT_MAGIC_CUBE_INVENTORY_FULL: "Insufficient space. You cannot put any more items in the Magic Cube.",
	UIIT_STT_MAGIC_CUBE_STORE_SELL_ERROR: "You cannot sell the Magic Cube with items still in it",
	SN_ITEM_MALL_MAGIC_CUBE_WEAPON: "Magic Cube (Weapon) - 50 slots",
	SN_ITEM_MALL_MAGIC_CUBE_ARMOR: "Magic Cube (Protector) - 50 slots",
	SN_ITEM_MALL_MAGIC_CUBE_ACCESSARY: "Magic Cube (Accessory) - 50 slots",
	SN_ITEM_MALL_MAGIC_CUBE_SHIELD: "Magic Cube (Shield) - 50 slots",
	SN_ITEM_MALL_MAGIC_CUBE_WEAPON_10: "Magic Cube (Weapon) - 10 slots",
	SN_ITEM_MALL_MAGIC_CUBE_ARMOR_10: "Magic Cube (Protector) - 10 slots",
	SN_ITEM_MALL_MAGIC_CUBE_ACCESSARY_10: "Magic Cube (Accessory) - 10 slots",
	SN_ITEM_MALL_MAGIC_CUBE_SHIELD_10: "Magic Cube (Shield) - 10 slots",
	SN_ITEM_MALL_MAGIC_CUBE_WEAPON_20: "Magic Cube (Weapon) - 20 slots",
	SN_ITEM_MALL_MAGIC_CUBE_ARMOR_20: "Magic Cube (Protector) - 20 slots",
	SN_ITEM_MALL_MAGIC_CUBE_ACCESSARY_20: "Magic Cube (Accessory) - 20 slots",
	SN_ITEM_MALL_MAGIC_CUBE_SHIELD_20: "Magic Cube (Shield) - 20 slots",
	SN_ITEM_MALL_MAGIC_CUBE_WEAPON_30: "Magic Cube (Weapon) - 30 slots",
	SN_ITEM_MALL_MAGIC_CUBE_ARMOR_30: "Magic Cube (Protector) - 30 slots",
	SN_ITEM_MALL_MAGIC_CUBE_ACCESSARY_30: "Magic Cube (Accessory) - 30 slots",
	SN_ITEM_MALL_MAGIC_CUBE_SHIELD_30: "Magic Cube (Shield) - 30 slots",
	SN_ITEM_MALL_MAGIC_CUBE_WEAPON_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_ARMOR_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_ACCESSARY_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_SHIELD_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_WEAPON_10_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_ARMOR_10_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_ACCESSARY_10_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_SHIELD_10_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_WEAPON_20_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_ARMOR_20_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_ACCESSARY_20_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_SHIELD_20_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_WEAPON_30_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_ARMOR_30_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_ACCESSARY_30_TT_DESC: MAGIC_CUBE_DESC,
	SN_ITEM_MALL_MAGIC_CUBE_SHIELD_30_TT_DESC: MAGIC_CUBE_DESC
} );

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
export function magicCubeCapacity( cube: InventoryItem, authoredCapacity = 0 ): number {
	if ( cube.linkedStorage && cube.linkedStorage.capacity > 0 ) return cube.linkedStorage.capacity;
	const authored = authoredCapacity > 0 ? authoredCapacity : (cube.nativeFields?.itemParam9_2bc ?? 0);
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

Move quantity of elixir into the cube's linked storage, matching the
server's GrantStack rule: merge into the lowest-slot partial stack when
stackCap > 1, otherwise allocate the lowest free slot (e.g. v1.150
elixirs with maxStack 1). Returns null when the bag rows cannot account
for the server's grant (wrong identity, over-take, or cube full).
================
*/
export function applyMagicCubePushLocal(
	cube: InventoryItem,
	elixir: InventoryItem,
	quantity: number,
	stackCap = 0,
	authoredCapacity = 0
): { cube: InventoryItem; elixirLeft: InventoryItem | null; } | null {
	if ( quantity < 1 || elixir.quantity < quantity ) return null;
	const capacity = magicCubeCapacity( cube, authoredCapacity );
	if ( capacity < 1 ) return null;
	const cap = stackCap > 0 ? stackCap : 1;
	const rows = [ ...(cube.linkedStorage?.rows ?? []) ].sort( ( a, b ) => a.slot - b.slot );
	const mergeAt = cap > 1 ?
		rows.findIndex( row => row.refObjId === elixir.refObjId && row.quantity < cap ) :
		-1;
	if ( mergeAt >= 0 ) {
		const existing = rows[mergeAt]!;
		if ( existing.quantity + quantity > cap ) return null;
		rows[mergeAt] = { ...existing, quantity: existing.quantity + quantity };
	} else {
		if ( quantity > cap || rows.length >= capacity ) return null;
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

Withdraw quantity of the named elixir RefObjID from the lowest matching
linked-storage slot into a bag destination row (or a fresh empty slot),
matching the server's linkedSlotByRef order.
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
	const cap = stackCap > 0 ? stackCap : 1;
	const rows = [ ...cube.linkedStorage.rows ].sort( ( a, b ) => a.slot - b.slot );
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
		if ( destination.quantity + quantity > cap ) return null;
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
applyMagicCubeConsumeLocal

Deduct quantity of the named elixir RefObjID from the lowest matching
linked-storage slot (0xB06D 0x29 from Alchemy Reinforce; v1.696 client
0x7f7480). The cube stays in its bag slot.
================
*/
export function applyMagicCubeConsumeLocal(
	cube: InventoryItem,
	refObjId: number,
	quantity: number
): InventoryItem | null {
	if ( quantity < 1 || !cube.linkedStorage ) return null;
	const rows = [ ...cube.linkedStorage.rows ].sort( ( a, b ) => a.slot - b.slot );
	const at = rows.findIndex( row => row.refObjId === refObjId && row.quantity >= quantity );
	if ( at < 0 ) return null;
	const source = rows[at]!;
	const left = source.quantity - quantity;
	if ( left > 0 ) rows[at] = { ...source, quantity: left };
	else rows.splice( at, 1 );
	return {
		...cube,
		quantity: magicCubeStoredCount( rows ),
		linkedStorage: { capacity: cube.linkedStorage.capacity, rows }
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
