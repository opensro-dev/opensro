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

import "strings"

const (
	Env = "SRO_NASRUN"

	// typeKindMask keeps TID1..TID3 of an item's packed type word
	// (tid1<<2 | tid2<<5 | tid3<<7 | tid4<<11), dropping bit 0.
	typeKindMask uint16 = 0x7fe
	// avatarKind is 3/1/13, a v1.150 avatar; spiritKind is 3/1/14.
	avatarKind uint16 = 0x6ac
	spiritKind uint16 = 0x72c
	tid4Shift         = 11

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
func IsSpirit(typeFlags uint16) bool { return typeFlags&typeKindMask == spiritKind }

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
