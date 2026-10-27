/*
===========================================================================

name-change.ts - the name-change scroll's identity, name shape and tails

Port-only, not native (#1689, owner decision 2026-10-27): the scroll is
a shipped mall item the v1.150 mall never sold (ITEM_MALL_REINFORCE_
WEAPON_INC_1_SCROLL, ref 3789), so the client knows it by its RefObjID
alone - its type word (3/3/13/11) is shared with the +2 scroll, which
stays an ordinary unusable item. Using it asks for the new name first;
the name rides the use request's tail as its raw ASCII bytes and the
server revalidates everything (the native 2..12 letter/digit/underscore
shape, the queue, the cooldown), so the checks here only keep an
obviously bad name off the wire.

The refusal bytes 0xF5..0xFD are the server's own (action/namechange.go);
the native client maps no text to them, so their lines live here.

===========================================================================
*/

// The donor item's shipped identity (see the file banner).
export const NAME_CHANGE_SCROLL_REF_OBJ_ID = 3789;

// The name entry's character limit: the native character-name bound.
export const NAME_CHANGE_MAX_LENGTH = 12;

/*
================
isNameChangeScrollItem

Whether a bag row is the name-change scroll. RefObjID, never the type
word: the +2 reinforce scroll shares the word and is not the scroll.
================
*/
export function isNameChangeScrollItem( refObjId: number ): boolean {
	return refObjId === NAME_CHANGE_SCROLL_REF_OBJ_ID;
}

/*
================
isValidRenameName

The native character-name shape (2..12 ASCII letters, digits and
underscores), mirrored so the entry box can refuse before sending. The
server remains authoritative: a name that passes here can still be
taken, reserved or on cooldown.
================
*/
export function isValidRenameName( name: string ): boolean {
	return /^[A-Za-z0-9_]{2,12}$/.test( name );
}

/*
================
nameChangeTail

The use request's tail: the name's raw ASCII bytes, nothing else. An
invalid name throws rather than sending bytes the server must refuse.
================
*/
export function nameChangeTail( name: string ): Uint8Array {
	if ( !isValidRenameName( name ) ) throw Error( "Enter a name of 2 to 12 letters, digits or underscores" );
	return new TextEncoder().encode( name );
}

/*
================
nameChangeFailureText

The line for one of the scroll's refusal bytes (0xB5BD result 2), or
undefined for any other byte (the native notice route owns those). A
switch, not a table: module-level state in a foundation file must stay
a scalar or an `as const` literal (verify-ownership).
================
*/
export function nameChangeFailureText( code: number ): string | undefined {
	switch ( code ) {
		case 0xF5:
			return "That name cannot be used. Names are 2 to 12 letters, digits or underscores.";
		case 0xF6:
			return "That name is already taken.";
		case 0xF7:
			return "That name is reserved by its previous owner.";
		case 0xF8:
			return "A name change is already waiting for your next login.";
		case 0xF9:
			return "You changed your name too recently. Name changes cool down for 30 days.";
		case 0xFA:
			return "Your guild is at war, so your name cannot change now.";
		case 0xFB:
			return "You hold a fortress position, so your name cannot change now.";
		case 0xFC:
			return "Take off your job suit to use the name-change scroll.";
		case 0xFD:
			return "Leave your party to use the name-change scroll.";
		default:
			return undefined;
	}
}

// Why a queued rename was refused at login (the character list's
// renameCancelled row field, spelled by the authority store).
export const RENAME_CANCELLED_REASONS = [
	"taken",
	"reserved",
	"guild-war",
	"invalid",
	"deleted"
] as const;

export type RenameCancelledReason = (typeof RENAME_CANCELLED_REASONS)[number];

/*
================
renameCancelledText

The character-select line for a refused queued rename, naming the
character it was queued for.
================
*/
export function renameCancelledText( reason: RenameCancelledReason, characterName: string ): string {
	switch ( reason ) {
		case "taken":
			return `The name change for ${characterName} was refused: that name was taken.`;
		case "reserved":
			return `The name change for ${characterName} was refused: that name is reserved.`;
		case "guild-war":
			return `The name change for ${characterName} was refused: your guild is at war.`;
		case "invalid":
			return `The name change for ${characterName} was refused: that name cannot be used.`;
		case "deleted":
			return `The name change for ${characterName} was refused.`;
	}
}
