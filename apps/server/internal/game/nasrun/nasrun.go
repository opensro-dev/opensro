/*
===========================================================================

nasrun.go - the Devil's Spirit (Nasrun) switch and its avatar socket

Port-only, not native (#3348, #3456). v1.150 knows avatars of type 3/1/13
only; the later clients' Devil's Spirit is a wearable of type 3/1/14 whose
TID4 is 1, the hat's. Socketing it by TID4, as every v1.150 avatar is,
would put it in the hat socket, so the Spirit is keyed on its TID3 and
owns a fifth socket of its own, storage slot 4 (the spawn block's
capacity byte has always said 5). A dress and a Spirit can be worn
together, as in v1.188 (its GDR_AVATAR_SLOT_05 has no dress dependency).

SRO_NASRUN is also the build switch that imports the Spirit's item rows
(scripts/nasrun_import.py). The GameWorld reads it for the socket: off,
nothing can be put in slot 4 and the avatar rules are v1.150's.

===========================================================================
*/
package nasrun

import (
	"strings"

	"opensro.online/server/internal/game/item/wire"
)

const (
	Env = "SRO_NASRUN"

	// typeKindMask keeps TID1..TID3 of an item's packed type word
	// (tid1<<2 | tid2<<5 | tid3<<7 | tid4<<11), dropping bit 0.
	typeKindMask uint16 = 0x7fe
	// avatarKind is 3/1/13, a v1.150 avatar (a Spirit, 3/1/14, is
	// wire.IsDevilSpirit).
	avatarKind uint16 = 0x6ac

	tid4Shift = 11

	// Sockets 1..4 are the v1.150 avatar sockets, numbered by TID4 (hat,
	// dress, attachment, flag). SpiritSocket is the Spirit's own.
	FirstAvatarSocket = 1
	LastAvatarSocket  = 4
	SpiritSocket      = 5

	// AvatarStorageSlots is the v1.150 avatar storage (slots 0..3);
	// SpiritSlot is the fifth storage slot the Spirit alone may hold.
	AvatarStorageSlots       = 4
	SpiritSlot         uint8 = 4
	StorageSlots             = 5
)

/*
================
Enabled - port-only, not native; absent and unknown values stay native.
================
*/
func Enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on":
		return true
	}
	return false
}

/*
================
IsSpirit - the item is a Devil's Spirit wearable (3/1/14).
================
*/
func IsSpirit(typeFlags uint16) bool { return wire.IsDevilSpirit(typeFlags) }

/*
================
MallCopy

Port-only, not native (#4459): a Spirit the Item Mall delivered. The
token shop, the unique drops and the mall hand out the same codenames,
and the import strips their rows bound (#3456: nasrun_import.py clears
CanTrade, CanSell, CanBorrow and CanDrop), so only the delivery's own
marks tell a mall copy apart. Every mall delivery since #4133 is either
sealed (forgeBound) or carries the earned-Silk mark; no token, drop or
GM grant carries the earned mark, and only /FORGE seals outside the
mall. A forged Spirit therefore counts as a mall copy too, and its seal
keeps refusing what the seal refuses.

A mall copy follows the later client's own row instead: VSRO
Media.pk2 itemdata_25000.txt gives every ITEM_MALL_AVATAR_*_NASRUN
CanSell 1, CanBorrow 223 or 192 (both carry the warehouse bit 0x80)
and CanDrop 0, with SellPrice 0. Callers read StorableMallCopy and
SellableMallCopy rather than this.
================
*/
func MallCopy(typeFlags uint16, forgeBound, earnedBought bool) bool {
	return IsSpirit(typeFlags) && (forgeBound || earnedBought)
}

/*
================
StorableMallCopy

A mall copy enters the warehouse, sealed or not: the seal never refused
the owner's own room (npcguildstorage.go keeps it out of the guild's).
================
*/
func StorableMallCopy(typeFlags uint16, forgeBound, earnedBought bool) bool {
	return MallCopy(typeFlags, forgeBound, earnedBought)
}

/*
================
SellableMallCopy

A merchant takes a Spirit only when it is a mall copy and the seal does
not refuse the sale: an earned-Silk purchase (#4133). Its sell price is
0, so the sale never turns tokens, or Silk, into gold.
================
*/
func SellableMallCopy(typeFlags uint16, forgeBound, earnedBought bool) bool {
	return MallCopy(typeFlags, forgeBound, earnedBought) && !forgeBound
}

/*
================
Socket

The avatar socket an item is worn in: TID4 for a v1.150 avatar (1..4),
SpiritSocket for a Spirit, and false for anything else. Callers gate the
Spirit on the switch; Socket only classifies.
================
*/
func Socket(typeFlags uint16) (int, bool) {
	if IsSpirit(typeFlags) {
		return SpiritSocket, true
	}
	if typeFlags&typeKindMask != avatarKind {
		return 0, false
	}
	socket := int(typeFlags >> tid4Shift)
	if socket < FirstAvatarSocket || socket > LastAvatarSocket {
		return 0, false
	}
	return socket, true
}
