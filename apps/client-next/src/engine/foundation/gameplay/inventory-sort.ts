/*
===========================================================================

inventory-sort.ts - the bag's sort order and the next move toward it

Port-only, not native (the server's SRO_INVENTORY_SORT, off unless the
shard turns it on). The original bag has no sort: the player drags items one by one.
This module only plans. It reads the bag as the client holds it and
names ONE ordinary bag move (the 0x706D movement type 0 a drag sends)
that brings the bag closer to the sorted layout; the caller sends it,
waits for the server's answer and asks again from the bag the answer
left. The server validates and applies every move exactly as it does a
drag (stack merge, swap or plain move), so a sort can neither lose nor
invent an item, and a refused move simply ends the sort.

Two phases, in order:

1. Merge: two partial stacks with the same stack identity
   (sameStackIdentity, the rule the server merges by) merge, the later
   slot onto the earlier one.
2. Place: the sorted items fill the bag from its first slot, with no
   gap. Each move puts the item that belongs in the first wrong slot
   there, from a later slot; an occupant swaps back into the vacated
   slot. A slot whose occupant compares equal to what belongs there is
   already right, so two stacks of one identity are never dropped on
   each other (the server would merge or trade their counts).

Every move fixes one slot for good or removes one partial stack, so a
sort ends in at most (bag items + partial stacks) moves.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import { sameStackIdentity, stackable } from "./container-transfer";
import { sealTier } from "./item-seal";

/*
================
InventorySortMove

One ordinary bag move: the whole source stack onto the destination.
================
*/
export interface InventorySortMove {
	readonly source: number;
	readonly destination: number;
	readonly quantity: number;
}

/*
================
InventorySortBag

The bag band of the wire slots: [first, end). The equipment sockets
below first are never read or moved.
================
*/
export interface InventorySortBag {
	readonly first: number;
	readonly end: number;
}

// The owner's order (2026-11-21), first to last.
export const SORT_HP_POTION = 0;
export const SORT_MP_POTION = 1;
export const SORT_CONSUMABLE = 2;
export const SORT_SCROLL = 3;
export const SORT_WEAPON = 4;
export const SORT_ARMOR = 5;
export const SORT_ACCESSORY = 6;
export const SORT_ALCHEMY = 7;
export const SORT_MATERIAL = 8;
export const SORT_PET = 9;
export const SORT_QUEST_EVENT = 10;
export const SORT_OTHER = 11;

// The RefItemData type word (typeFlags): TID1 bits 2-4, TID2 bits 5-6,
// TID3 bits 7-10, TID4 bits 11-15 (server inventory/slots.go reads the same).
const TID1_ITEM = 3;
const TID2_EQUIPMENT = 1;
const TID2_COS = 2;
const TID2_ETC = 3;

// TID 3.1.x equipment families (server EquipSocketForTypeFlags).
const EQUIP_SHIELD = 4;
const EQUIP_WEAPON = 6;
const EQUIP_JOB_SUIT = 7;
const EQUIP_AVATAR = 13;
const EQUIP_DEVIL_SPIRIT = 14;
const EQUIP_CH_ACCESSORY = 5;
const EQUIP_EU_ACCESSORY = 12;
const BODY_ARMOR_TID3: readonly number[] = [ 1, 2, 3, 9, 10, 11 ] as const;
// Armour TID4 (1 head, 2 shoulders, 3 chest, 4 legs, 5 hands, 6 feet) in
// the owner's order: head, chest, legs, hands, shoulders, feet.
const ARMOR_PART_ORDER: readonly number[] = [ 1, 3, 4, 5, 2, 6 ] as const;
// Accessory TID4 (1 earring, 2 necklace, 3 ring): necklace, earrings, rings.
const ACCESSORY_PART_ORDER: readonly number[] = [ 2, 1, 3 ] as const;

// TID 3.3.x expendables, read from the 1.150 itemdata.
const ETC_POTION = 1;
const ETC_CURE = 2;
const ETC_SCROLL = 3;
const ETC_AMMO = 4;
const ETC_REINFORCE = 10;
const ETC_ALCHEMY = 11;
const POTION_HP = 1;
const POTION_MP = 2;
// 3.3.1.4 is the pet Recovery Kit: a consumable, as the owner lists it.
const PET_POTIONS: readonly number[] = [ 6, 9 ] as const;
const PET_CURE = 7;
const ALCHEMY_MATERIAL = 4;
const ALCHEMY_ELEMENT = 5;

// Quest and event items share type words with potions and scrolls
// (ITEM_QNO_* rows sit in 3.3.1.8, 3.3.3.1, 3.3.13.x), so their codename
// decides first. Event items carry their event's date (ITEM_ETC_E051003_*).
const QUEST_PREFIXES: readonly string[] = [ "ITEM_QNO_", "ITEM_QSP_" ] as const;
const EVENT_PREFIX = "ITEM_ETC_E";
const EVENT_DATE_DIGITS = 6;
const EVENT_MARK = "_EVENT_";
const PET_PREFIXES: readonly string[] = [
	"ITEM_COS_P_",
	"ITEM_COS_T_",
	"ITEM_COS_C_",
	"ITEM_COS_PET_",
	"ITEM_MALL_PET_"
] as const;
const SCROLL_MARK = "SCROLL";

// A seal's rank, Sun first: sealTier is 0 Star, 1 Moon, 2 Sun.
const SEAL_RANK_NONE = 3;
const SEAL_CLASSES_PER_DEGREE = 3;

/*
================
isEventCodename

ITEM_ETC_E<yymmdd>_*, or any codename marked _EVENT_.
================
*/
function isEventCodename( codename: string ): boolean {
	if ( codename.includes( EVENT_MARK ) ) return true;
	if ( !codename.startsWith( EVENT_PREFIX ) ) return false;
	const date = codename.slice( EVENT_PREFIX.length, EVENT_PREFIX.length + EVENT_DATE_DIGITS );
	return date.length === EVENT_DATE_DIGITS && [ ...date ].every( c => c >= "0" && c <= "9" ) &&
		codename[EVENT_PREFIX.length + EVENT_DATE_DIGITS] === "_";
}

/*
================
typeIds

The four type ids of an item's type word.
================
*/
function typeIds( typeFlags: number ) {
	return {
		tid1: typeFlags >>> 2 & 7,
		tid2: typeFlags >>> 5 & 3,
		tid3: typeFlags >>> 7 & 15,
		tid4: typeFlags >>> 11 & 31
	};
}

/*
================
equipmentCategory
================
*/
function equipmentCategory( tid3: number ): number {
	if ( tid3 === EQUIP_WEAPON || tid3 === EQUIP_SHIELD ) return SORT_WEAPON;
	if (
		BODY_ARMOR_TID3.includes( tid3 ) || tid3 === EQUIP_JOB_SUIT || tid3 === EQUIP_AVATAR ||
		tid3 === EQUIP_DEVIL_SPIRIT
	) return SORT_ARMOR;
	if ( tid3 === EQUIP_CH_ACCESSORY || tid3 === EQUIP_EU_ACCESSORY ) return SORT_ACCESSORY;
	return SORT_OTHER;
}

/*
================
expendableCategory
================
*/
function expendableCategory( tid3: number, tid4: number, codename: string ): number {
	if ( tid3 === ETC_POTION ) {
		if ( tid4 === POTION_HP ) return SORT_HP_POTION;
		if ( tid4 === POTION_MP ) return SORT_MP_POTION;
		return PET_POTIONS.includes( tid4 ) ? SORT_PET : SORT_CONSUMABLE;
	}
	if ( tid3 === ETC_CURE ) return tid4 === PET_CURE ? SORT_PET : SORT_CONSUMABLE;
	if ( tid3 === ETC_AMMO ) return SORT_CONSUMABLE;
	if ( tid3 === ETC_SCROLL ) return SORT_SCROLL;
	if ( tid3 === ETC_REINFORCE ) return SORT_ALCHEMY;
	if ( tid3 === ETC_ALCHEMY ) {
		return tid4 === ALCHEMY_MATERIAL || tid4 === ALCHEMY_ELEMENT ? SORT_MATERIAL : SORT_ALCHEMY;
	}
	return codename.includes( SCROLL_MARK ) ? SORT_SCROLL : SORT_OTHER;
}

/*
================
inventorySortCategory

The owner's category of an item (SORT_*): its codename first for quest,
event and pet items, then its type word.
================
*/
export function inventorySortCategory( item: InventoryItem ): number {
	const codename = item.codename ?? "", { tid1, tid2, tid3, tid4 } = typeIds( item.typeFlags );
	if ( QUEST_PREFIXES.some( prefix => codename.startsWith( prefix ) ) ) return SORT_QUEST_EVENT;
	if ( isEventCodename( codename ) ) return SORT_QUEST_EVENT;
	if ( tid1 !== TID1_ITEM ) return SORT_OTHER;
	if ( tid2 === TID2_COS || PET_PREFIXES.some( prefix => codename.startsWith( prefix ) ) ) return SORT_PET;
	if ( tid2 === TID2_EQUIPMENT ) return equipmentCategory( tid3 );
	if ( tid2 === TID2_ETC ) return expendableCategory( tid3, tid4, codename );
	return SORT_OTHER;
}

/*
================
partRank

Where an equipment piece's TID4 falls in the owner's order.
================
*/
function partRank( category: number, tid3: number, tid4: number ): number {
	const order = category === SORT_ARMOR && BODY_ARMOR_TID3.includes( tid3 ) ?
		ARMOR_PART_ORDER :
		category === SORT_ACCESSORY ?
		ACCESSORY_PART_ORDER :
		null;
	if ( !order ) return tid4;
	const rank = order.indexOf( tid4 );
	return rank < 0 ? order.length + tid4 : rank;
}

/*
================
typeRank

The family order inside a category: weapons before shields, body armour
before job suits, avatars and devil spirits, then the TID3 itself.
================
*/
function typeRank( category: number, tid3: number ): number {
	if ( category === SORT_WEAPON ) return tid3 === EQUIP_WEAPON ? 0 : 1;
	if ( category === SORT_ARMOR && !BODY_ARMOR_TID3.includes( tid3 ) ) return 16 + tid3;
	return tid3;
}

/*
================
sortKey

The tuple an item sorts by, every field ascending. Quantity and slot are
left out on purpose: two stacks of one identity compare equal, so the
planner never drops one on the other.
================
*/
function sortKey( item: InventoryItem ): readonly (number | string)[] {
	const category = inventorySortCategory( item ), { tid3, tid4 } = typeIds( item.typeFlags );
	const fields = item.tooltip?.fields, itemClass = fields?.itemClass ?? 0, level = fields?.requiredLevel ?? 0;
	if ( category === SORT_WEAPON || category === SORT_ARMOR || category === SORT_ACCESSORY ) {
		// Degree descending, type, part, Sun > Moon > Star > normal, required
		// level descending, plus descending.
		const degree = itemClass > 0 ? Math.trunc( (itemClass - 1) / SEAL_CLASSES_PER_DEGREE ) + 1 : 0;
		const seal = sealTier( fields?.rarity, itemClass, true );
		return [
			category,
			-degree,
			typeRank( category, tid3 ),
			partRank( category, tid3, tid4 ),
			seal === undefined ? SEAL_RANK_NONE : 2 - seal,
			-level,
			-item.plus,
			item.refObjId
		];
	}
	// Grouped by type, then by level descending.
	return [
		category,
		tid3,
		tid4,
		-itemClass,
		-level,
		item.codename ?? "",
		item.refObjId,
		-item.plus,
		item.label ?? ""
	];
}

/*
================
compareInventorySort
================
*/
export function compareInventorySort( a: InventoryItem, b: InventoryItem ): number {
	const x = sortKey( a ), y = sortKey( b );
	for ( let i = 0; i < x.length; i++ ) {
		const p = x[i]!, q = y[i]!;
		if ( p === q ) continue;
		return p < q ? -1 : 1;
	}
	return 0;
}

/*
================
stackCapOf

The stack limit the client holds for an item (its reference's MaxStack);
1 for anything that does not stack.
================
*/
function stackCapOf( item: InventoryItem ): number {
	if ( !stackable( item ) ) return 1;
	const cap = item.tooltip?.fields.maxStack;
	return cap !== undefined && Number.isInteger( cap ) && cap > 1 ? cap : 1;
}

/*
================
nextMerge

The later of the first two partial stacks that share a stack identity,
onto the earlier one.
================
*/
function nextMerge( items: readonly InventoryItem[] ): InventorySortMove | null {
	const partial = items.filter( item => item.quantity < stackCapOf( item ) );
	for ( let i = 0; i < partial.length; i++ ) {
		const a = partial[i]!;
		const b = partial.slice( i + 1 ).find( row => sameStackIdentity( a, row ) );
		if ( b ) return { source: b.slot, destination: a.slot, quantity: b.quantity };
	}
	return null;
}

/*
================
samePlace

Whether a slot holding occupant already holds what belongs there: equal
in the sort order, or a stack the server would merge with it. The second
test keeps the planner from ever dropping one stack of an identity on
another even if the two disagreed on a reference field.
================
*/
function samePlace( occupant: InventoryItem, want: InventoryItem ): boolean {
	if ( compareInventorySort( occupant, want ) === 0 ) return true;
	return stackable( occupant ) && stackCapOf( occupant ) > 1 && sameStackIdentity( occupant, want );
}

/*
================
nextPlacement

The move that fills the first slot whose occupant is not what the sorted
bag holds there, from the first later slot holding an equal item.
================
*/
function nextPlacement( items: readonly InventoryItem[], bag: InventorySortBag ): InventorySortMove | null {
	const sorted = [ ...items ].sort( ( a, b ) => compareInventorySort( a, b ) || a.slot - b.slot );
	const bySlot = new Map( items.map( item => [ item.slot, item ] ) );
	for ( let i = 0; i < sorted.length; i++ ) {
		const slot = bag.first + i, want = sorted[i]!, occupant = bySlot.get( slot );
		if ( occupant && samePlace( occupant, want ) ) continue;
		const source = items
			.filter( item => item.slot > slot && samePlace( item, want ) )
			.sort( ( a, b ) => a.slot - b.slot )[0];
		// The later slots hold exactly the items still to place, so an equal
		// one is always there; guard anyway rather than loop.
		if ( !source ) return null;
		return { source: source.slot, destination: slot, quantity: source.quantity };
	}
	return null;
}

/*
================
nextInventorySortMove

The next move of a sort, or null when the bag is sorted. Only items in
the bag band are read.
================
*/
export function nextInventorySortMove(
	items: readonly InventoryItem[],
	bag: InventorySortBag
): InventorySortMove | null {
	const rows = items.filter( item => item.slot >= bag.first && item.slot < bag.end )
		.sort( ( a, b ) => a.slot - b.slot );
	return nextMerge( rows ) ?? nextPlacement( rows, bag );
}

/*
================
inventorySortSignature

A bag layout's identity (slot, reference, count), so a driver can tell a
sort that stopped making progress from one that still does.
================
*/
export function inventorySortSignature( items: readonly InventoryItem[], bag: InventorySortBag ): string {
	return items.filter( item => item.slot >= bag.first && item.slot < bag.end )
		.sort( ( a, b ) => a.slot - b.slot )
		.map( item => `${item.slot}:${item.refObjId}:${item.quantity}:${item.plus}` )
		.join( "," );
}
