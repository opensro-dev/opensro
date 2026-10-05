/*
===========================================================================

skinchange.go - the character skin change scroll

ITEM_MALL_CHAR_SKIN_CHANGE_SCROLL (3/3/13/9) is 49C2B0 case 8: the v1.150
CIFChangePlayerModel window sends the item use with a tail of [u32 model]
[u8 scale] (CIFChangePlayerModel_OnConfirm 6D0650), and
CGObjPC_ChangeCharacterModel (4EFE50) admits it:

  - the model is a player character (type 1/1) of the same country;
  - a change of gender needs bare armour sockets 0..5, an empty job-suit
    socket 8 and an empty avatar inventory, else 0x1892
    (UIIT_MSG_CHAR_SKIN_ERR_ARMOR);
  - the model and scale are written and the character reloads in place
    under teleport mode 3 after one second (4EFFC0,
    CGObjChar_SetTeleportModeAndScheduleRestore). The reload's entry
    packets carry the new body to the player and its observers.

INFERENCE: v1.188 writes the scale byte unchecked; the window's two
scrollbars run 0..4 (CIFChangePlayerModel_RefreshScaleScrollBar 6D0980),
so a nibble above 4 is refused here.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// errCodeSkinArmour is the low byte of 4EFE50's 0x1892.
	errCodeSkinArmour uint8 = 0x92
	// skinReloadMs is 4EFFC0's 1.0 s restore delay.
	skinReloadMs = 1000
	// skinTeleportMode is the channel-11 mode 4EFFC0 sets.
	skinTeleportMode uint8 = 3
	// skinMaxShapeStep is the window's scrollbar limit for each nibble.
	skinMaxShapeStep = 4
	// skinJobSuitSlot is the equipment socket 4EFE50 checks after 0..5.
	skinJobSuitSlot = 8
	// skinArmourSlots counts the armour sockets 0..5.
	skinArmourSlots = 6
)

/*
================
skinModels

The roster facet the skin change reads.
================
*/
type skinModels interface {
	CharacterModelRef(*enterworld.Character) uint32
	PlayableModel(refObjID uint32) (string, bool)
}

/*
================
skinBodyBound

True when a gender change must refuse: a worn armour piece, a job suit or
any avatar.
================
*/
func skinBodyBound(c *enterworld.Character) bool {
	for _, row := range c.MissionInventory {
		if row.Slot >= 0 && row.Slot < skinArmourSlots || row.Slot == skinJobSuitSlot {
			return true
		}
	}
	return c.AvatarInventory != nil && len(c.AvatarInventory.Rows) != 0
}

/*
================
useSkinChangeScroll

Runs inside the item use's character Update.
================
*/
func (rt *Runtime) useSkinChangeScroll(use skillItemUse, c *enterworld.Character, tail []byte, result *OpResult) bool {
	r := wire.NewReader(tail)
	model, e := r.U32()
	shape, e2 := r.U8()
	if e != nil || e2 != nil || r.Done() != nil || shape&0xf > skinMaxShapeStep || shape>>4 > skinMaxShapeStep {
		result.DiagnosticRefusal = "item-use: malformed skin change"
		return false
	}
	models, ok := rt.deps.(skinModels)
	if !ok {
		return false
	}
	codename, ok := models.PlayableModel(model)
	currentCodename, currentOK := models.PlayableModel(models.CharacterModelRef(c))
	if !ok || !currentOK {
		result.DiagnosticRefusal = "item-use: skin change names no player model"
		return false
	}
	next, current := &enterworld.Character{ModelCodename: codename}, &enterworld.Character{ModelCodename: currentCodename}
	if enterworld.ResolveCharacterRaceKey(next) != enterworld.ResolveCharacterRaceKey(current) {
		result.DiagnosticRefusal = "item-use: skin change crosses countries"
		return false
	}
	gender := enterworld.ResolveCharacterGenderIndex(next)
	if gender != enterworld.ResolveCharacterGenderIndex(current) && skinBodyBound(c) {
		*result = itemUseFailure(errCodeSkinArmour)
		return false
	}
	key := simulation.WorldKey(use.division, c.Name)
	here := rt.liveSpawn(key, c, use.nowMs)
	if !rt.startReturnCast(returnCast{division: use.division, character: c, row: use.row, slot: use.request.Slot,
		typeWord: use.request.TypeWord, duration: skinReloadMs, destination: &here, now: use.nowMs,
		mode: skinTeleportMode}, result) {
		return false
	}
	ref, body := int64(model), int64(shape)
	height, volume := int64(shape&0xf), int64(shape>>4)
	c.ModelRef, c.ModelCodename, c.Gender = &ref, codename, &gender
	// The port packs height low and volume high (agent/api/characters.go).
	c.BodyShapeByte, c.HeightIndex, c.VolumeIndex = &body, &height, &volume
	c.HeightScale, c.VolumeScale = nil, nil
	return true
}
