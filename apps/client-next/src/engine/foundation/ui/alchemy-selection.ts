/*
===========================================================================

alchemy-selection.ts - CIFAlchemy slot admission and reagent mode selection

Owns the client-side slot-selection rules for CIFAlchemyReinforce (0x0062B6D0),
CIFEquipReinforce (0x00621100) and CIFEquipAlchemy (0x006245A0).

===========================================================================
*/

import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { AlchemyMode } from "@/engine/contracts/item-process";

/*
================
isAlchemyEquipment

SRO_Client.exe 0x0062B6D0 (CIFAlchemyReinforce_BindItemSlot), 0x00621100
(CIFEquipReinforce_BindItemSlot) and 0x006245A0 (CIFEquipAlchemy_BindItemSlot):
equipment requires TypeID1=3, TypeID2=1 (0x004FB2D0 CItemData_IsEquipment at
0x0062B794, 0x0062B82A, 0x00624CFD and inlined in 0x00621100 at
0x00621195..0x006211B2 / 0x0062135C..0x00621371: (flags & 0x7e) === 0x2c)
and excludes job suits (0x00550410: TypeID3=7, (flags & 0x780) === 0x380),
avatar items (0x00550940: TypeID3=13, (flags & 0x780) === 0x680) and fortress
hammers (0x005504C0: TypeID3=6, TypeID4=16, flags === 0x832c).
================
*/
export function isAlchemyEquipment( typeFlags: number ): boolean {
	const flags = typeFlags & 0xfffe;
	return (flags & 0x7e) === 0x2c && (flags & 0x780) !== 0x380 && (flags & 0x780) !== 0x680 && flags !== 0x832c;
}

/*
================
alchemySlotCapacity
================
*/
export function alchemySlotCapacity( mode: AlchemyMode ) {
	return mode === "compound" ? 9 : mode === "advanced" ? 5 : mode === "reinforce" ? 3 : 2;
}

// Stone equipment-family word bits. Mirrors the stoneFamilyWords table in
// the server's enterworld/stonefamily.go, which publishes a stone's mask as
// the numeric stoneFamilyMask tooltip field; change both together.
export const STONE_FAMILY_BITS = {
	weapon: 1,
	shield: 2,
	accessory: 4,
	earring: 8,
	necklace: 16,
	ring: 32,
	armor: 64,
	helm: 128,
	shoulderguard: 256,
	mail: 512,
	pants: 1024,
	gauntlet: 2048,
	boots: 4096,
	honor: 8192
} as const;

// The armour slot bits (helm..boots for TypeID4 1..6) and the accessory
// subtype bits (earring, necklace, ring for TypeID4 1..3) are consecutive
// powers of two, so the candidate bit is a shift of the first one.

/*
================
alchemyStoneRefusal

0x0062B6D0 stone placement refusal, checked when a magic or attribute
stone is bound onto the slotted target item. The degree gate runs first:
the stone's degree (tooltip field itemParam1_29c, the itemdata Param1 at
+0x29C) must equal the target degree class trunc((itemClass - 1) / 3) + 1
with signed C truncation, so a target class of 0 still yields class 1.
Behind it, the family gate admits the stone when any candidate word for
the target is in the stone's published stoneFamilyMask; a magic stone
whose option kind or descriptors admit nothing carries mask 0 and refuses
every target. A target carrying MATTR_REPAIR is searched as 'honor' for a
repair stone (its mask carries the honor bit) and as 'weapon' otherwise.
Missing tooltip data (a client older than the published field) binds as
before and lets the server fuse refuse; the refusal itself is purely
local and never sends a packet.
================
*/
export function alchemyStoneRefusal( target: InventoryItem, stone: InventoryItem ): string | null {
	const targetFields = target.tooltip?.fields,
		stoneFields = stone.tooltip?.fields;
	if ( !targetFields || !stoneFields ) return null;
	const stoneDegree = stoneFields["itemParam1_29c"],
		targetClass = targetFields["itemClass"];
	if ( stoneDegree !== undefined && targetClass !== undefined ) {
		if ( stoneDegree !== Math.trunc( (targetClass - 1) / 3 ) + 1 ) {
			return "UIIT_MSG_ENCHANT_EQUIPCLASS_MISMATCH";
		}
	}
	const mask = stoneFields["stoneFamilyMask"];
	if ( mask === undefined ) return null;
	let candidates: readonly number[];
	if ( target.magicReferences?.some( reference => reference.optionName === "MATTR_REPAIR" ) ) {
		candidates = [ mask & STONE_FAMILY_BITS.honor ? STONE_FAMILY_BITS.honor : STONE_FAMILY_BITS.weapon ];
	} else {
		const tid3 = (target.typeFlags >>> 7) & 15,
			tid4 = (target.typeFlags >>> 11) & 31;
		if ( tid3 === 6 ) candidates = [ STONE_FAMILY_BITS.weapon ];
		else if ( tid3 === 4 ) candidates = [ STONE_FAMILY_BITS.shield ];
		else if ( tid3 === 5 || tid3 === 12 ) {
			candidates = tid4 >= 1 && tid4 <= 3 ?
				[ STONE_FAMILY_BITS.accessory, STONE_FAMILY_BITS.earring << (tid4 - 1) ] :
				[ STONE_FAMILY_BITS.accessory ];
		} else if ( tid3 === 1 || tid3 === 2 || tid3 === 3 || tid3 === 9 || tid3 === 10 || tid3 === 11 ) {
			candidates = tid4 >= 1 && tid4 <= 6 ?
				[ STONE_FAMILY_BITS.armor, STONE_FAMILY_BITS.helm << (tid4 - 1) ] :
				[ STONE_FAMILY_BITS.armor ];
		} else return null;
	}
	return candidates.some( bit => (mask & bit) !== 0 ) ? null : "UIIT_STRGERR_CANNOT_BE_USED_TO_TARGET";
}

/*
================
alchemySelection

62B6D0 -> 6252B0/550600/550730/550660: the reagent selects the
operation on the reinforcement page; 7023C0 maps modes 1/2 to wire 2/3.
================
*/
export function alchemySelection(
	mode: AlchemyMode,
	slots: readonly number[],
	item: InventoryItem,
	target?: InventoryItem
): { mode: AlchemyMode; slots: number[]; refusal?: string; } | null {
	const index = slots.indexOf( item.slot );
	if ( index >= 0 ) return { mode, slots: index === 0 ? [] : slots.filter( s => s !== item.slot ) };
	if ( item.slot < 13 ) return null;
	const flags = item.typeFlags & 0xfffe,
		equipment = isAlchemyEquipment( flags ),
		process = [ "compound", "advanced", "dissolve" ].includes( mode );
	if ( !slots.length ) {
		if ( process ) {
			if ( equipment ) mode = "dissolve";
			else if ( flags === 0x1dec ) mode = "advanced";
			else if ( flags === 0x25ec || flags === 0x35ec ) mode = "compound";
			else return null;
		} else if ( !equipment ) return null;
	} else if ( !process ) {
		// 62B6D0: magic stones (3/3/11/1 0x0dec, 3/3/11/7 0x3dec) take the magic-option
		// branch, attribute stones (3/3/11/2 0x15ec) the attribute one. A stone
		// bound onto the slotted target passes the degree and family gates
		// first; a refusal binds nothing and sends nothing.
		if ( target && (flags === 0x0dec || flags === 0x3dec || flags === 0x15ec) ) {
			const refusal = alchemyStoneRefusal( target, item );
			if ( refusal ) return { mode, slots: [ ...slots ], refusal };
		}
		if ( flags === 0x0dec || flags === 0x3dec ) mode = "magic";
		else if ( flags === 0x15ec ) mode = "attribute";
		else if ( flags === 0x0d6c ) mode = "reinforce";
		else if ( flags !== 0x156c || mode !== "reinforce" ) return null;
		if ( slots.length > 1 && flags !== 0x156c ) return { mode, slots: [ slots[0]!, item.slot ] };
	} else if (
		mode === "compound" && flags !== 0x25ec && flags !== 0x35ec ||
		mode === "advanced" && flags !== 0x2dec ||
		mode === "dissolve" && flags !== 0x35ec
	) return null;
	const capacity = alchemySlotCapacity( mode );
	return slots.length >= capacity ? null : { mode, slots: [ ...slots, item.slot ] };
}
