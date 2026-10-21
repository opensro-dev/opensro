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
equipment requires TypeID1=3, TypeID2=1 (0x005503E0: (flags & 0x7e) === 0x2c)
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
	item: InventoryItem
): { mode: AlchemyMode; slots: number[]; } | null {
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
		// branch, attribute stones (3/3/11/2 0x15ec) the attribute one.
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
