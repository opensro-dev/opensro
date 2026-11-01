/*
===========================================================================

equipmentreqi.go - equipment requirements (reqi / reqn)

The server's skill equipment-requirement check, for skills that carry reqi
pairs. Skills without pairs compare weapon kinds instead; see
action.skillEquipmentRefusal. The same walk decides whether a passive with
reqi contributes (the passive toggle switches its modifiers on and off).

===========================================================================
*/

package combat

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/durability"
)

// reqiCheck is the case the server's byte table picks for a pair's kind.
type reqiCheck uint8

const (
	reqiArmour       reqiCheck = iota // armour slot or armour set
	reqiSecondary                     // secondary slot (shield line)
	reqiPrimary                       // primary weapon slot
	reqiAvatarSocket                  // avatar socket 4
	reqiNothing                       // no test; falls through to the reqn count
)

// The secondary slot and the type a shield carries there (TID1 3 item,
// TID2 1 equipment, TID3 4 shield); ammunition shares the slot as 3.3.4.
const (
	reqiShieldSlot = 7
	reqiShieldTID1 = 3
	reqiShieldTID2 = 1
	reqiShieldTID3 = 4
)

// reqiArmourSlots is the server's armour-index table: index 1..6 to slot.
var reqiArmourSlots = [7]int64{-1, 0, 2, 1, 4, 3, 5}

/*
==================
ReqiRefusal

Returns 0 to admit, else the refusal code.

The walk keeps three pieces of native state: matched (EDI) for the pair
under test, count (esp+24) of matched pairs under reqn, and code (esp+1C),
the last refusal a case recorded. Not every case clears code when it
matches, so an admitted skill can still report an earlier 0x300F. That is
the native result and is kept.
==================
*/
func ReqiRefusal(c *enterworld.Character, items enterworld.ItemRefSource, req enterworld.SkillReqi) uint16 {
	eq := ReqiEquipment{C: c, Items: items}
	var code uint16
	matched := false
	count := 0

	i := 0
	for ; i < req.Count; i++ {
		// without reqn a match ends the walk
		if matched {
			break
		}
		matched, code = eq.testPair(req.Pairs[i], code)

		// under reqn every pair must match, and matches are counted
		if req.All {
			if !matched {
				return reqiFailure(code)
			}
			matched = false
			count++
		}
	}

	// The early-stop count test runs only when the walk stopped early. A full
	// five-pair walk skips it, so under reqn five matched pairs still fail.
	stoppedEarly := i < len(req.Pairs)
	if stoppedEarly && count != 0 && count == i {
		return code
	}
	if matched {
		return code
	}
	return reqiFailure(code)
}

// reqiFailure is the walk's failure exit: the recorded code, else 0x300D.
func reqiFailure(code uint16) uint16 {
	if code != 0 {
		return code
	}
	return 0x300d
}

// reqiCaseOf is the server's case byte table, indexed by kind - 1.
func reqiCaseOf(kind uint32) reqiCheck {
	table := [14]reqiCheck{
		reqiArmour, reqiArmour, reqiArmour, reqiSecondary, reqiNothing,
		reqiPrimary, reqiNothing, reqiNothing, reqiArmour, reqiArmour,
		reqiArmour, reqiNothing, reqiNothing, reqiAvatarSocket,
	}
	if kind == 0 || kind > 14 {
		return reqiNothing
	}
	return table[kind-1]
}

/*
===============================================================================

EQUIPMENT VIEW

===============================================================================
*/

// ReqiEquipment reads a character's equipped items for the reqi walk.
type ReqiEquipment struct {
	C     *enterworld.Character
	Items enterworld.ItemRefSource
}

/*
==================
testPair

One pair of the walk. Returns whether it matched and the recorded code,
which a case may clear, set to 0x300F or leave as it was.
==================
*/
func (e ReqiEquipment) testPair(pair enterworld.SkillReqiPair, code uint16) (bool, uint16) {
	switch reqiCaseOf(pair.Kind) {
	case reqiArmour:
		if pair.Value != 0 {
			return e.testArmourSlot(pair.Value, code)
		}
		return e.testArmourSet(pair.Kind, code)
	case reqiSecondary:
		return e.testShield(pair.Value, code)
	case reqiPrimary:
		return e.testWeapon(6, pair.Value, code)
	case reqiAvatarSocket:
		// The server reads avatar socket 4, but the v1.150 client has avatar
		// sockets 0..3 only, so socket 4 is always empty.
		return false, code
	}
	return false, code
}

// testArmourSlot is the armour-slot case: index value must hold TID4 == value.
func (e ReqiEquipment) testArmourSlot(value uint32, code uint16) (bool, uint16) {
	if uint32(e.armourTID(value)>>11) == value && e.armourUsable(value) {
		return true, 0
	}
	return false, code
}

/*
==================
testArmourSet

The armour-set case: armour index 1..6 in order must carry TID3 == kind. The first
mismatch stops the walk but keeps a match made before it, so in practice
only the head slot has to carry the family. A broken piece records 0x300F.
==================
*/
func (e ReqiEquipment) testArmourSet(kind uint32, code uint16) (bool, uint16) {
	matched := false
	for index := uint32(1); index <= 6; index++ {
		if uint32(e.armourTID(index)>>7&0xf) != kind {
			return matched, 0
		}
		if !e.armourUsable(index) {
			return false, 0x300f
		}
		matched = true
	}
	return matched, code
}

// testWeapon is the primary and secondary slot case: the slot's TID4 must
// equal value, and a matching but broken weapon records 0x300F.
func (e ReqiEquipment) testWeapon(slot int64, value uint32, code uint16) (bool, uint16) {
	if uint32(e.WeaponTID(slot)>>11) != value {
		return false, code
	}
	if !e.SlotUsable(slot) {
		return false, 0x300f
	}
	return true, 0
}

/*
==================
testShield

Kind 4 tests the secondary slot. The native case compares only the TID4 of
whatever the equipped-item read returns from slot 7, and ammunition sits there too: a
Chinese arrow stack (3.3.4.1) passes a Chinese shield's 4,1 and a European
bolt stack (3.3.4.2) a European shield's 4,2. Every shipped kind-4 pair is a
shield line (SKILL_CH_*_SHIELD*, SKILL_CH_SWORD_PASSIVE_A's shield block,
SKILL_EU_WARRIOR_ONEHANDA_SHIELD_*), so a bow user wearing arrows blocked
like a shield bearer (#bug-reports, "blocking without a shield"). Port-only,
not native: the slot must hold a shield (TID1 3, TID2 1, TID3 4) before its
TID4 is compared. press-admission.ts testPair mirrors this.
==================
*/
func (e ReqiEquipment) testShield(value uint32, code uint16) (bool, uint16) {
	tid := e.WeaponTID(reqiShieldSlot)
	if tid>>2&7 != reqiShieldTID1 || tid>>5&3 != reqiShieldTID2 || tid>>7&0xf != reqiShieldTID3 {
		return false, code
	}
	return e.testWeapon(reqiShieldSlot, value, code)
}

// item returns the equipped row's reference in one slot and whether it
// carries the item+0x190 broken mark (depleted durability).
func (e ReqiEquipment) Item(slot int64) (ref *enterworld.ItemRef, broken, ok bool) {
	for _, row := range e.C.MissionInventory {
		if row.Slot != slot {
			continue
		}
		ref, found := e.Items.ItemRefByCodename(row.Codename)
		if !found {
			return nil, false, false
		}
		return ref, durability.Broken(ref.TypeFlags(), row.Durability == 0), true
	}
	return nil, false, false
}

// weaponTID is the server's equipped-item TID read: the packed TID, or 0 when empty or broken.
func (e ReqiEquipment) WeaponTID(slot int64) uint16 {
	ref, broken, ok := e.Item(slot)
	if !ok || broken {
		return 0
	}
	return ref.TypeFlags()
}

// armourTID is the server's equipped-armour TID read for index 1..6.
func (e ReqiEquipment) armourTID(index uint32) uint16 {
	if index < 1 || index > 6 {
		return 0
	}
	return e.WeaponTID(reqiArmourSlots[index])
}

// slotUsable is the player's slot usability test (+0x5D0 / +0x63C): an empty slot
// passes, an equipped item must not be broken.
func (e ReqiEquipment) SlotUsable(slot int64) bool {
	_, broken, ok := e.Item(slot)
	return !ok || !broken
}

// armourUsable is the armour usability test (+0x640); an index past 6 fails.
func (e ReqiEquipment) armourUsable(index uint32) bool {
	if index < 1 || index > 6 {
		return false
	}
	return e.SlotUsable(reqiArmourSlots[index])
}
