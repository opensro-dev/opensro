/*
===========================================================================

avatar.go - the smith's avatar magic option grant

A smith's 0x2F row opens CIFGrantMagicAttributeWnd; the player drops an
avatar hat, dress or attachment and picks one of the options
magicoptionassign.txt assigns to that avatar part. v1.188 grants it through
CBless_AvatarItemWithNPC (handler 5079C0): the success ratio is a fixed 100
(504D00), there is no fee and no material, and the value comes from the
option's own generator. The client fills its option list from the same
table (78C720), keyed by the item's TID3/TID4.

===========================================================================
*/

package alchemy

import (
	"fmt"
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

const (
	avatarTypeID1    = 3
	avatarTypeID2    = 1
	avatarTypeID3    = 13
	avatarFlagTypeID = 4 // TID4 4, the avatar flag: refused by both sides (6EA1B0)

	// 5079C0 accepts inventory slots 0x0D..0x6F.
	avatarSlotFirst = 0x0d
	avatarSlotLast  = 0x6f

	// 505810 clamps every generated value to 1..0x6A4.
	avatarValueMax = 0x6a4

	// 505810 stacks the protection tags by one up to these ceilings.
	avatarStackMax  = 6
	avatarRepairMax = 7
)

// Avatar grant answers (0x32D9 [2][code], category 0x20 notices).
const (
	AvatarNoItem    Refusal = 3    // 5079C0: no item in the slot
	AvatarFull      Refusal = 6    // UIIT_MSG_AVATAR_MAGICOPTION_ADD_ERORR
	AvatarFailed    Refusal = 7    // UIIT_STT_AVATAR_MAGICOPTION_ADD_FAIL (5056E0's failed roll)
	AvatarNotAvatar Refusal = 8    // UIIT_STT_AVATAR_MAGICOPTION_ONLY_AVATAR
	AvatarBadSlot   Refusal = 9    // 5079C0: slot outside the bag
	AvatarWrong     Refusal = 0x0a // UIIT_STT_AVATAR_MAGICOPTION_WRONG
	AvatarNoDegree  Refusal = 0x0b // no option row at the item's degree
)

/*
================
loadAvatarOptions

The TID3 13 rows of magicoptionassign.txt: column 0 service, 1 country,
2 TID3, 3 TID4, then the option codenames up to "xxx". The client's lookup
(7D4600) keys on TID3/TID4 alone, so a second row for the same part would be
unreachable; it is refused here.
================
*/
func (c *Catalog) loadAvatarOptions(dir string) error {
	c.AvatarOptions = map[uint8][]string{}
	for _, a := range enterworld.ReadTextdataFile(filepath.Join(dir, "magicoptionassign.txt")) {
		if len(a) < 5 || a[0] != "1" || a[2] != strconv.Itoa(avatarTypeID3) {
			continue
		}
		part, err := strconv.ParseUint(a[3], 10, 5)
		if err != nil {
			return fmt.Errorf("alchemy: magicoptionassign avatar part %q", a[3])
		}
		if _, exists := c.AvatarOptions[uint8(part)]; exists {
			return fmt.Errorf("alchemy: duplicate avatar assignment for part %d", part)
		}
		names := []string{}
		for _, name := range a[4:] {
			if name == "xxx" || name == "" {
				break
			}
			names = append(names, name)
		}
		c.AvatarOptions[uint8(part)] = names
	}
	if len(c.AvatarOptions) == 0 {
		return fmt.Errorf("alchemy: magicoptionassign.txt has no avatar rows")
	}
	return nil
}

/*
================
avatarPart

The TID4 of a grantable avatar part, or false. Bionic rows and the avatar
flag are refused as the client's drop check (6EB570) refuses them.
================
*/
func avatarPart(flags uint16) (uint8, bool) {
	tid1, tid2, tid3, tid4 := flags>>2&7, flags>>5&3, flags>>7&15, uint8(flags>>11&31)
	if flags&2 != 0 || tid1 != avatarTypeID1 || tid2 != avatarTypeID2 || tid3 != avatarTypeID3 || tid4 == avatarFlagTypeID {
		return 0, false
	}
	return tid4, true
}

/*
================
avatarAssigned

RefData_IsMagicOptionAssignedToItem (727EF0): the codename is on the part's
magicoptionassign row.
================
*/
func (c *Catalog) avatarAssigned(part uint8, codename string) bool {
	for _, name := range c.AvatarOptions[part] {
		if name == codename {
			return true
		}
	}
	return false
}

/*
================
BlessAvatar

CBless_AvatarItemWithNPC_BlessItem (505810) over a detached bag. An option
the item already carries is rewritten in place (same param id, new value)
and costs no slot; a new one needs a free slot. The grant always succeeds
(504D00 returns 100).
================
*/
func (c *Catalog) BlessAvatar(items []inventory.Item, slot uint8, codename string, roll Roll) (Outcome, error) {
	var zero Outcome
	if slot < avatarSlotFirst || slot > avatarSlotLast {
		return zero, AvatarBadSlot
	}
	out := clone(items)
	target := -1
	for i := range out {
		if out[i].Slot == slot {
			target = i
			break
		}
	}
	if target < 0 {
		return zero, AvatarNoItem
	}
	item := &out[target]
	ref, ok := c.Items[item.Codename]
	if !ok || ref.ID != item.RefObjID || ref.Flags != item.TypeFlags || item.Quantity == 0 {
		return zero, AvatarNoItem
	}
	part, ok := avatarPart(item.TypeFlags)
	if !ok {
		return zero, AvatarNotAvatar
	}
	limit := c.magicLimit(*item)
	if len(item.MagicOptions) > limit {
		return zero, AvatarFull
	}
	if codename == "" || !c.knownOption(codename) || !c.avatarAssigned(part, codename) {
		return zero, AvatarWrong
	}
	option, ok := c.Option(codename, ref.Degree())
	if !ok {
		return zero, AvatarNoDegree
	}
	index, old := c.findMagic(*item, option.Tag)
	id := option.ID
	if index >= 0 {
		// 505810 keeps the carried param id (CGItemEquip_FindMagicParam).
		id = uint16(item.MagicOptions[index])
	}
	var value uint32
	switch option.Tag {
	case 0x61746861, 0x6c75636b, 0x736f6c69, 0x61737472:
		if value = old + 1; value > avatarStackMax {
			return zero, AvatarFull
		}
	case 0x726570:
		if value = old + 1; value > avatarRepairMax {
			return zero, AvatarFull
		}
	default:
		var err error
		if value, err = RollSingleRange(option, roll); err != nil {
			return zero, err
		}
	}
	value = min(max(value, 1), avatarValueMax)
	encoded := uint64(value)<<32 | uint64(id)
	switch {
	case index >= 0:
		item.MagicOptions[index] = encoded
	case len(item.MagicOptions) < limit:
		item.MagicOptions = append(item.MagicOptions, encoded)
	default:
		// INFERENCE: 505810 answers 2 here, which the v1.150 client has no
		// text for; it refuses the same full item before sending with
		// ADD_ERORR, so the server names that notice too.
		return zero, AvatarFull
	}
	return Outcome{Items: out, Target: slot, Success: true}, nil
}

/*
================
knownOption

RefData_FindMagicOptionByCodename (727DB0): any row carries the codename.
================
*/
func (c *Catalog) knownOption(codename string) bool {
	for _, m := range c.Magic {
		if m.Name == codename {
			return true
		}
	}
	return false
}
