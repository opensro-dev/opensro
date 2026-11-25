/*
===========================================================================

weapon-switch.ts - the weapon switch scroll's identity, kinds and tail

Port-only, not native (owner, 2026-11-04; 2026-11-21): a scroll sold in
the Item Mall while the server runs SRO_WEAPON_SWITCH_SCROLL turns a bag
weapon into another kind of the same degree, grade and seal, its plus
and blue options kept. It is an item of its own the variant build stages
(ITEM_MALL_WEAPON_SWITCH_SCROLL, scripts/switch_scroll_import.py), a copy
of a reinforce scroll's row under a build-assigned RefObjID, so the
client knows it by its codename alone: its type word (3/3/13/11) is
shared with the reinforce scrolls and the name-change scroll
(name-change.ts).

Using it arms the item-target cursor (cos-hud.ts); the clicked bag weapon
opens a box that asks the race (Chinese or European), then the kind of
that race, the weapon's own kind excepted; the picked kind rides the
use request's tail after the target slot, [u8 target slot][u8 kind], the
kind being the weapon's fourth TypeID. The server (action/weaponswitch.go)
revalidates everything and owns the refusal bytes 0xF5..0xF8, whose lines
live here.

===========================================================================
*/

// The scroll's item (see the file banner).
export const WEAPON_SWITCH_SCROLL_CODENAME = "ITEM_MALL_WEAPON_SWITCH_SCROLL";

// A wielded weapon's type word under the TypeID 1..3 mask: 3/1/6, that is
// 3 << 2 | 1 << 5 | 6 << 7.
const WEAPON_TYPE_MASK = 0x7fc;
const WEAPON_TYPE_WORD = 0x32c;

// The races the box offers, itemdata's Country.
export const WEAPON_RACE_CHINESE = 0;
export const WEAPON_RACE_EUROPEAN = 1;
// The two rows in box order: Chinese, European.
export const WEAPON_RACES: readonly number[] = [ 0, 1 ] as const;

// The weapon kinds (fourth TypeID) of each race; 16, the siege weapon,
// is none a player wields.
const CHINESE_FIRST_KIND = 2;
const CHINESE_LAST_KIND = 6;
const EUROPEAN_FIRST_KIND = 7;
const EUROPEAN_LAST_KIND = 15;

/*
================
isWeaponSwitchScroll

Whether a bag row's codename is the scroll's; never the type word.
================
*/
export function isWeaponSwitchScroll( codename: string | undefined ): boolean {
	return codename === WEAPON_SWITCH_SCROLL_CODENAME;
}

/*
================
weaponKind

A wielded weapon's kind (its fourth TypeID), or 0 for anything else,
the shield and the siege weapon included.
================
*/
export function weaponKind( typeFlags: number ): number {
	if ( (typeFlags & WEAPON_TYPE_MASK) !== WEAPON_TYPE_WORD ) return 0;
	const kind = typeFlags >>> 11 & 31;
	return kind >= CHINESE_FIRST_KIND && kind <= EUROPEAN_LAST_KIND ? kind : 0;
}

/*
================
weaponSwitchKinds

The kinds the box offers for a weapon under one race, in TypeID order,
the weapon's own kind excepted. Empty for anything that is no weapon or
an unknown race. The server decides whether a twin of the picked kind
exists.
================
*/
export function weaponSwitchKinds( typeFlags: number, race: number ): number[] {
	const kind = weaponKind( typeFlags );
	if ( kind === 0 || !WEAPON_RACES.includes( race ) ) return [];
	const [first, last] = race === WEAPON_RACE_CHINESE ?
		[ CHINESE_FIRST_KIND, CHINESE_LAST_KIND ] :
		[ EUROPEAN_FIRST_KIND, EUROPEAN_LAST_KIND ];
	const kinds: number[] = [];
	for ( let next = first; next <= last; next++ ) {
		if ( next !== kind ) kinds.push( next );
	}
	return kinds;
}

/*
================
weaponRaceName

The race row's English label in the box.
================
*/
export function weaponRaceName( race: number ): string {
	return race === WEAPON_RACE_CHINESE ? "Chinese" : "European";
}

/*
================
weaponSwitchTail

The use request's tail: the target bag slot and the picked kind.
================
*/
export function weaponSwitchTail( targetSlot: number | undefined, kind: number | undefined ): Uint8Array {
	if ( targetSlot === undefined || kind === undefined ) throw Error( "Choose the weapon and its new kind" );
	if ( !Number.isInteger( targetSlot ) || targetSlot < 13 || targetSlot > 255 ) throw Error( "Invalid weapon slot" );
	if ( !Number.isInteger( kind ) || kind < CHINESE_FIRST_KIND || kind > EUROPEAN_LAST_KIND ) {
		throw Error( "Invalid weapon kind" );
	}
	return Uint8Array.of( targetSlot, kind );
}

/*
================
weaponSwitchFailureText

The line for one of the scroll's refusal bytes (0xB5BD result 2), or
undefined for any other byte (the native notice route owns those).
================
*/
export function weaponSwitchFailureText( code: number ): string | undefined {
	switch ( code ) {
		case 0xF5:
			return "The weapon switch scroll works only on a weapon in your bag.";
		case 0xF6:
			return "This item's blue options cannot follow it to that kind.";
		case 0xF7:
			return "That weapon cannot be switched to this kind.";
		case 0xF8:
			return "No weapon of that kind matches this one.";
		default:
			return undefined;
	}
}
