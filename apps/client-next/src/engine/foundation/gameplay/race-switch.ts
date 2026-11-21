/*
===========================================================================

race-switch.ts - the armor and accessory switch scrolls' identity and tail

Port-only, not native (owner, 2026-11-21): two scrolls sold in the Item
Mall while the server runs SRO_ARMOR_SWITCH_SCROLL or
SRO_ACCESSORY_SWITCH_SCROLL turn a bag item into the same item of the
other race, its plus and blue options kept. The armor scroll takes armor
(robe <-> garment, light <-> protector, heavy <-> armor) and shields, the
accessory scroll earrings, necklaces and rings. They are items of
the variant build stages as items of their own (ITEM_MALL_ARMOR_SWITCH_
SCROLL, ITEM_MALL_ACCESSORY_SWITCH_SCROLL, scripts/switch_scroll_import.py)
under build-assigned RefObjIDs, so the client knows them by codename
alone: their type word (3/3/13/11) is shared with the reinforce,
name-change and weapon switch scrolls.

Using one arms the item-target cursor (cos-hud.ts); a bag item the scroll
switches opens a Yes/No confirmation, and Yes sends the use with
[u8 target slot] after the type word. The server
(action/raceswitch.go) revalidates everything and owns the refusal bytes
0xF5, 0xF6 and 0xF8, whose lines live here.

===========================================================================
*/

// The scrolls' items (see the file banner).
export const ARMOR_SWITCH_SCROLL_CODENAME = "ITEM_MALL_ARMOR_SWITCH_SCROLL";
export const ACCESSORY_SWITCH_SCROLL_CODENAME = "ITEM_MALL_ACCESSORY_SWITCH_SCROLL";

export type RaceSwitchKind = "armor" | "accessory";

// Equipment's type word under the TypeID 1..2 mask: 3/1, that is
// 3 << 2 | 1 << 5.
const EQUIPMENT_MASK = 0x7c;
const EQUIPMENT_WORD = 0x2c;

// The third TypeIDs each scroll switches; the shield (4) also needs a
// race in its fourth (1 Chinese, 2 European).
const ARMOR_TYPES: readonly number[] = [ 1, 2, 3, 9, 10, 11 ] as const;
const ACCESSORY_TYPES: readonly number[] = [ 5, 12 ] as const;
const SHIELD_TYPE = 4;

// The lowest bag slot; 0..12 are the equipment slots.
const FIRST_BAG_SLOT = 13;

/*
================
raceSwitchKindOf

What a bag row's scroll switches, or null when it is no race switch
scroll. Its codename, never the type word.
================
*/
export function raceSwitchKindOf( codename: string | undefined ): RaceSwitchKind | null {
	if ( codename === ARMOR_SWITCH_SCROLL_CODENAME ) return "armor";
	if ( codename === ACCESSORY_SWITCH_SCROLL_CODENAME ) return "accessory";
	return null;
}

/*
================
raceSwitchAccepts

Whether a scroll switches an item of this type word. The server decides
whether the other race makes a twin.
================
*/
export function raceSwitchAccepts( kind: RaceSwitchKind, typeFlags: number ): boolean {
	if ( (typeFlags & EQUIPMENT_MASK) !== EQUIPMENT_WORD ) return false;
	const type = typeFlags >>> 7 & 15, sub = typeFlags >>> 11 & 31;
	if ( kind === "accessory" ) return ACCESSORY_TYPES.includes( type );
	return ARMOR_TYPES.includes( type ) || type === SHIELD_TYPE && (sub === 1 || sub === 2);
}

/*
================
raceSwitchTail

The use request's tail: the target bag slot.
================
*/
export function raceSwitchTail( targetSlot: number | undefined ): Uint8Array {
	if ( targetSlot === undefined ) throw Error( "Choose the item to switch" );
	if ( !Number.isInteger( targetSlot ) || targetSlot < FIRST_BAG_SLOT || targetSlot > 255 ) {
		throw Error( "Invalid item slot" );
	}
	return Uint8Array.of( targetSlot );
}

/*
================
raceSwitchFailureText

The line for one of a scroll's refusal bytes (0xB5BD result 2), or
undefined for any other byte (the native notice route owns those).
================
*/
export function raceSwitchFailureText( kind: RaceSwitchKind, code: number ): string | undefined {
	switch ( code ) {
		case 0xF5:
			return kind === "armor" ?
				"The armor switch scroll works only on armor or a shield in your bag." :
				"The accessory switch scroll works only on an earring, necklace or ring in your bag.";
		case 0xF6:
			return "This item's blue options cannot follow it to the other race.";
		case 0xF8:
			return "No item of the other race matches this one.";
		default:
			return undefined;
	}
}
