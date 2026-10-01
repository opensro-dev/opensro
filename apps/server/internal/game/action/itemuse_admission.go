/*
===========================================================================

itemuse_admission.go - which item families may be used at all

===========================================================================
*/

package action

import (
	"math"
	"opensro.online/server/internal/game/item/wire"

	"opensro.online/server/internal/game/enterworld"
)

/*
==================
itemUseFamily

New usable families must enter this closed dispatch only after their effect,
prerequisites, cooldown and native receipt are implemented together. A name,
recovery-looking parameter or client type word never grants use permission.
==================
*/
type itemUseFamily uint8

const (
	itemUseUnsupported itemUseFamily = iota
	itemUseRecovery
	itemUseSummoner
	itemUsePetSkill
	itemUseSkill
	itemUseReturn
	itemUseBerserk
	itemUseCure
	itemUsePetPotion
	itemUsePetCure
	itemUsePetRevive
	itemUseMonsterCapsule
	itemUseQuestTool
	itemUseComposite
)

/*
================
admittedItemUseFamily

Classify the reference family only. Quest tools still require a matching
active native handler before inventory consumption can be authorized.
================
*/
func admittedItemUseFamily(ref *enterworld.ItemRef) itemUseFamily {
	if ref == nil {
		return itemUseUnsupported
	}
	if ref.TypeIDs == [4]int64{3, 3, 9, 0} {
		return itemUseQuestTool
	}
	if ref.TypeIDs == [4]int64{3, 3, 3, 1} && ref.ReturnDestination == "RESURRECT" {
		return itemUseReturn
	}
	// CGItemMonsterCapsule (sub_42E750, TID 3/2/2): the monster mask.
	if wire.IsMonsterCapsule(ref.TypeFlags()) {
		return itemUseMonsterCapsule
	}
	if ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 3 && ref.TypeIDs[2] == 13 && ref.TypeIDs[3] >= 1 && ref.TypeIDs[3] <= 3 {
		return itemUseSkill
	}
	if ref.TypeIDs == [4]int64{3, 3, 1, 8} {
		return itemUseBerserk
	}
	// 49F590: the composite scroll (UIU1 param jobs).
	if ref.TypeIDs == [4]int64{3, 3, 13, 14} {
		return itemUseComposite
	}
	// 49D240, and the v1.150 client appends the COS gid for TID4 4/5/7
	// (6963CF) and for the TID3 2 TID4 7 cure (696336). TID4 6 is the
	// revival scroll (696490 / server case 6).
	if ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 3 && ref.TypeIDs[2] == 1 {
		switch ref.TypeIDs[3] {
		case 4, 5, 7:
			return itemUsePetPotion
		case 6:
			return itemUsePetRevive
		}
	}
	if ref.TypeIDs == [4]int64{3, 3, 2, 7} {
		return itemUsePetCure
	}
	// 49B710: TID3 2 is the player cure family; TID4 1 is the universal pill.
	// TID4 7 is the pet cure, which 49D240 applies to the owner's COS, not
	// the player, so it never enters the player cure path.
	if ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 3 && ref.TypeIDs[2] == 2 && ref.TypeIDs[3] != 7 {
		return itemUseCure
	}
	if _, ok := potionType(ref.TypeIDs); ok {
		return itemUseRecovery
	}
	if cosSummonerType(ref.TypeIDs) {
		return itemUseSummoner
	}
	if petSkillItemType(ref.TypeIDs) {
		return itemUsePetSkill
	}
	return itemUseUnsupported
}

/*
================
itemUseRequirements

The reference's use flag, country and typed requirements admit every family.
================
*/
func itemUseRequirements(character *enterworld.Character, ref *enterworld.ItemRef) uint8 {
	permission, present := ref.NativeFields.Lookup("canUse")
	if !present || math.IsNaN(permission) || math.IsInf(permission, 0) ||
		permission < 0 || permission > 255 || math.Trunc(permission) != permission || uint8(permission)&1 == 0 {
		return wire.ErrCodeInvalidRequest
	}
	if ref.Country != 3 && ref.Country != int64(enterworld.NativeCountryByte9C(character)) {
		return wire.ErrCodeCountryMismatch
	}
	if ref.RequiredStr < 0 || ref.RequiredInt < 0 {
		return wire.ErrCodeInvalidRequest
	}
	if enterworld.CharacterStrength(character) < ref.RequiredStr {
		return wire.ErrCodeStrengthRequired
	}
	if enterworld.CharacterIntellect(character) < ref.RequiredInt {
		return wire.ErrCodeIntellectRequired
	}
	// Reuse the native typed requirement walk rather than testing only the
	// first pair or mistaking a mastery ID for a character level.
	requirements := characterEquipRequirements{character: character}
	if quadWalk(ref, func(kind int64) (int64, bool) {
		if kind == 1 {
			return requirements.characterLevel(), true
		}
		return 0, false
	}) {
		return wire.ErrCodeItemUseLevelRequired
	}
	if quadWalk(ref, func(kind int64) (int64, bool) {
		if kind <= 10 || kind > math.MaxUint32 {
			return 0, false
		}
		level, ok := enterworld.MasteryLevel(character, uint32(kind))
		return clampToU8(level), ok
	}) {
		return wire.ErrCodeInvalidRequest
	}
	return 0
}

/*
==================
recoveryCooldownDuration

SR_GameServer 49B94B..49B993 -> 4E0AB0: the reuse lock follows 49AA70's
arm. Absolute potions lock 1.1 s in China and 15.1 s in Europe; percentage
potions lock 4.1 s in both. The native server guard outlasts the client
icon by 100 ms. 4EB410 tests independent category reuse bits, so changing
stack/grade cannot evade a lane, and HP must not block MP.
==================
*/
func recoveryCooldownDuration(character *enterworld.Character, absolute bool) (int64, bool) {
	if !absolute {
		return 4100, true
	}
	switch enterworld.NativeCountryByte9C(character) {
	case 0:
		return 1100, true
	case 1:
		return 15100, true
	default:
		return 0, false
	}
}

/*
================
petSkillWindowHasCapacity

Expiry belongs to the sweep; admission cannot silently evict a live window.
================
*/
func petSkillWindowHasCapacity(character *enterworld.Character, ref *enterworld.ItemRef) bool {
	// The sweep owns expiry and its removal receipt. Do not silently discard
	// expired entries here: the client board still needs that publication.
	if len(character.PetSkillWindows) < petSkillWindowCapacity {
		return true
	}
	if len(character.PetSkillWindows) > petSkillWindowCapacity {
		return false
	}
	for _, window := range character.PetSkillWindows {
		if window.ItemRefObjID == ref.RefObjID {
			return true
		}
	}
	return false
}
