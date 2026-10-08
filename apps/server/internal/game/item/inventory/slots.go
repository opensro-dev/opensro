// Package inventory holds the server-authoritative inventory rules for the
// Silkroad Online v1.150 item plane: slot bands, equipment socket matching,
// stack arithmetic and the gold rules.
//
// Everything here is a pure function or a method on an in-memory value, so the
// rules can be exercised without a session, a database or a socket. The wire
// encoding lives in the sibling wire package; this package decides what is
// allowed and what the resulting rows look like, and returns the native
// 0xB06D error code when it refuses.
//
// The rules mirror the (retired) Node launcher-api's server.mjs (formerly
// rebuild/apps/launcher-api, deleted at the Go cutover; its state is archived
// at temp/retired/launcher-api/), which was the reference implementation the
// browser client was developed against.
package inventory

import "opensro.online/server/internal/domain"

// Wire slot bands. The 0x32B3 local-player entry block ships the character's
// inventory capacity byte (45 at creation, domain.InventoryCapacity), split
// into a raw equipment band and a biased bag band.
const (
	// EquipmentSlotEnd is one past the last equipment socket: wire slots
	// 0..12 address equipment directly, with no bias.
	EquipmentSlotEnd uint8 = 13
	// MaxBagEnd is the largest capacity byte any character holds. Request
	// decoders bound wire slots by it; the character's own BagEnd decides.
	MaxBagEnd = domain.MaxInventorySize
)

/*
================
BagEnd

One past the character's last bag slot: its capacity byte. Wire slots
13..BagEnd-1 are the bag.
================
*/
func BagEnd(character *domain.Character) uint8 {
	return character.InventoryCapacity()
}

// Equipment sockets, as the native CIFEquipment TID->socket map assigns them
// (sub_550490 / sub_5932c0 family).
const (
	SocketHead     uint8 = 0
	SocketBody     uint8 = 1
	SocketShoulder uint8 = 2
	SocketArm      uint8 = 3
	SocketLeg      uint8 = 4
	SocketFoot     uint8 = 5
	SocketWeapon   uint8 = 6
	SocketShield   uint8 = 7
	// SocketSpecialDress is the job/trade-suit socket (CICPlayer+0x850),
	// routed by the TID 3.1.7 arm of sub_594980.
	SocketSpecialDress uint8 = 8
	SocketEarring      uint8 = 9
	SocketNecklace     uint8 = 10
	SocketRing         uint8 = 11
	// SocketRingSecond is the second ring hand. A ring resolves to SocketRing
	// but is accepted in either.
	SocketRingSecond uint8 = 12
)

// TID word field extraction. The RefItemData TypeID word packs, low to high:
// bit 1 "not classifiable", bits 2-4 TID1, bits 5-6 TID2, bits 7-10 TID3,
// bits 11-15 TID4. Build one with wire.PackTypeFlags.

func typeFlagsTid3(typeFlags uint16) uint8 { return uint8(typeFlags >> 7 & 0x0F) }
func typeFlagsTid4(typeFlags uint16) uint8 { return uint8(typeFlags >> 11 & 0x1F) }

// isEquipmentClassWord is the shared "item class 3, equipment band" prefix of
// every equip-plane predicate: bit 1 clear, TID1 == 3, TID2 == 1.
func isEquipmentClassWord(typeFlags uint16) bool {
	return typeFlags&0x02 == 0 && typeFlags&0x1C == 0x0C && typeFlags&0x60 == 0x20
}

// isBodyArmorTid3 is the six-value body-armor TID3 set: CH garment(1),
// protector(2), armor(3) + the EU triple (9, 10, 11). Native sub_593270.
func isBodyArmorTid3(tid3 uint8) bool {
	return tid3 == 1 || tid3 == 2 || tid3 == 3 || tid3 == 9 || tid3 == 10 || tid3 == 11
}

// EquipSocketForTypeFlags resolves the equipment socket from the RefItemData
// TypeID word exactly as native sub_594980 /
// CIFEquipment_ComposeEquipMoveForDroppedItem routes it (cross-checked against
// the independent visual-side mapper sub_868a80). The second result is false
// for "no socket class", which is the native SILENT reject: sub_594980 falls
// off its end with no packet and no message.
//
// This is the AUTHORITATIVE resolver - the word is on every inventory row -
// and it deliberately carries no gender/race gate: the only requirement check
// sub_594980 performs is the required-sex test inside the job/trade-suit
// branch, so a blanket gate here would be stricter than retail.
func EquipSocketForTypeFlags(typeFlags uint16) (uint8, bool) {
	if typeFlags&0x02 != 0 || typeFlags&0x1C != 0x0C {
		return 0, false
	}
	equipment := typeFlags&0x60 == 0x20
	etc := typeFlags&0x60 == 0x60
	tid3 := typeFlagsTid3(typeFlags)
	tid4 := typeFlagsTid4(typeFlags)

	switch {
	// TID 3.1.7 job/trade suit -> the special-dress socket.
	case equipment && tid3 == 7:
		return SocketSpecialDress, true
	// TID 3.1.6 weapons.
	case equipment && tid3 == 6:
		return SocketWeapon, true
	// TID 3.1.4.{1,2} shields (CH/EU).
	case equipment && tid3 == 4 && (tid4 == 1 || tid4 == 2):
		return SocketShield, true
	// TID 3.3.4 arrow/bolt ammo -> the SAME secondary socket. Slot 7 holds a
	// shield for melee and a quiver/bolt case for bow/crossbow.
	case etc && tid3 == 4:
		return SocketShield, true
	// TID 3.1.{1,2,3,9,10,11} armor sets -> the TID4 table jump_table_594d7c.
	case equipment && isBodyArmorTid3(tid3):
		switch tid4 {
		case 1:
			return SocketHead, true
		case 2:
			return SocketShoulder, true
		case 3:
			return SocketBody, true
		case 4:
			return SocketLeg, true
		case 5:
			return SocketArm, true
		case 6:
			return SocketFoot, true
		}
		return 0, false
	// TID 3.1.{5,12} accessories (CH/EU): earring, necklace, ring. Rings
	// resolve to SocketRing; SocketAccepts admits either hand.
	case equipment && (tid3 == 5 || tid3 == 12):
		switch tid4 {
		case 1:
			return SocketEarring, true
		case 2:
			return SocketNecklace, true
		case 3:
			return SocketRing, true
		}
		return 0, false
	}
	return 0, false
}

// IsEquipmentSlot reports whether a wire slot addresses an equipment socket.
func IsEquipmentSlot(wireSlot uint8) bool {
	return wireSlot < EquipmentSlotEnd
}

/*
================
InBag

Whether a persisted row's slot lies in the character's bag.
================
*/
func InBag(character *domain.Character, slot int64) bool {
	return slot >= int64(EquipmentSlotEnd) && slot < int64(BagEnd(character))
}

// IsBagSlot reports whether a wire slot addresses a bag slot of a bag that
// ends at bagEnd (BagEnd).
func IsBagSlot(wireSlot, bagEnd uint8) bool {
	return wireSlot >= EquipmentSlotEnd && wireSlot < bagEnd
}

// WireSlotFromBagIndex converts a zero-based bag index into the wire slot the
// composer sends, which is biased by EquipmentSlotEnd.
func WireSlotFromBagIndex(bagIndex uint8) uint8 {
	return bagIndex + EquipmentSlotEnd
}

// BagIndexFromWireSlot converts a bag wire slot back to its zero-based index.
// The second result is false when the slot is not in the bag band.
func BagIndexFromWireSlot(wireSlot, bagEnd uint8) (uint8, bool) {
	if !IsBagSlot(wireSlot, bagEnd) {
		return 0, false
	}
	return wireSlot - EquipmentSlotEnd, true
}

// SocketAccepts reports whether an item whose natural socket is itemSocket may
// be placed into destSlot. Rings fit either hand; everything else must land in
// its own socket.
func SocketAccepts(itemSocket, destSlot uint8) bool {
	if itemSocket == destSlot {
		return true
	}
	return itemSocket == SocketRing && destSlot == SocketRingSecond
}
