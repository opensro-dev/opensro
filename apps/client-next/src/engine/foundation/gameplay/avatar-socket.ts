/*
===========================================================================

avatar-socket.ts - which avatar socket an item is worn in

v1.150 avatars are items of type 3/1/13, socketed by TID4 (hat, dress,
attachment, flag) into avatar storage slots 0..3 (594980). The Devil's
Spirit (Nasrun) is a later wearable, type 3/1/14 with the hat's TID4: it
is socketed by its TID3 into a fifth socket of its own, storage slot 4
(port-only, not native: SRO_NASRUN, #3456; the server's game/nasrun owns
the rule). The client follows what the server stores: a v1.150 build has
no 3/1/14 row at all, so with the switch off nothing here changes what a
native client shows (inference recorded, AGENTS.md scope rule).

===========================================================================
*/

// The packed type word is tid1<<2 | tid2<<5 | tid3<<7 | tid4<<11; this
// mask keeps TID1..TID3.
const TYPE_KIND_MASK = 0x7fe;
const AVATAR_KIND = 0x6ac;
const SPIRIT_KIND = 0x72c;
const TID4_SHIFT = 11;

export const FIRST_AVATAR_SOCKET = 1;
export const LAST_AVATAR_SOCKET = 4;
export const SPIRIT_SOCKET = 5;
// Slots 0..3 hold the v1.150 avatars; slot 4 is the Spirit's alone.
export const AVATAR_STORAGE_SLOTS = 4;
export const SPIRIT_SLOT = 4;
export const AVATAR_STORAGE_CAPACITY = 5;

/*
================
isAvatarItem

A v1.150 avatar or a Devil's Spirit: an item avatar storage can hold.
================
*/
export function isAvatarItem( typeFlags: number ): boolean {
	const kind = typeFlags & TYPE_KIND_MASK;
	return kind === AVATAR_KIND || kind === SPIRIT_KIND;
}

/*
================
avatarSocket

The socket an item is worn in: TID4 (1..4) for a v1.150 avatar,
SPIRIT_SOCKET for a Spirit, 0 for anything else.
================
*/
export function avatarSocket( typeFlags: number ): number {
	const kind = typeFlags & TYPE_KIND_MASK;
	if ( kind === SPIRIT_KIND ) return SPIRIT_SOCKET;
	if ( kind !== AVATAR_KIND ) return 0;
	const socket = typeFlags >>> TID4_SHIFT;
	return socket >= FIRST_AVATAR_SOCKET && socket <= LAST_AVATAR_SOCKET ? socket : 0;
}

/*
================
avatarSlotFits

Whether an avatar storage slot may hold the item: the Spirit only slot
4, a v1.150 avatar only 0..3.
================
*/
export function avatarSlotFits( typeFlags: number, slot: number ): boolean {
	if ( !Number.isInteger( slot ) ) return false;
	if ( avatarSocket( typeFlags ) === SPIRIT_SOCKET ) return slot === SPIRIT_SLOT;
	return slot >= 0 && slot < AVATAR_STORAGE_SLOTS;
}
