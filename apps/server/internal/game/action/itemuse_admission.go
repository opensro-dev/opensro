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
	itemUsePetFeed
	itemUseMonsterCapsule
	itemUseQuestTool
	itemUseComposite
	itemUsePersistentSummoner
	itemUsePetExtension
	itemUseResurrection
	itemUseStatRecall
	itemUseFirework
	itemUseRepairHammer
	itemUseWarehouseTicket
	itemUseSkinChange
	itemUseGenderTool
	itemUsePremiumTicket
	itemUseSkillTimeTicket
	itemUseStructureRepair
	itemUseMercenary
	itemUseGlobalChat
	itemUseStallDecoration
	itemUseReverseReturn
	itemUseNameChange
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
	// #1689 (port-only, not native): the name-change scroll reuses this
	// shipped mall item, which no native family claims (TypeIDs
	// 3/3/13/11), so admission is by codename and the family's handler
	// re-checks the SRO_NAME_CHANGE_SCROLL flag before any effect
	// (namechange.go). The classification alone grants nothing.
	if ref.Codename == NameChangeScrollItem {
		return itemUseNameChange
	}
	if ref.TypeIDs == [4]int64{3, 3, 12, 1} {
		return itemUseMercenary
	}
	// Native item use case 4 (type 4 = 5): the Global Chatting item (globalchat.go).
	if ref.TypeIDs == [4]int64{3, 3, 3, 5} {
		return itemUseGlobalChat
	}
	if ref.TypeIDs == [4]int64{3, 3, 9, 0} {
		return itemUseQuestTool
	}
	if ref.TypeIDs == [4]int64{3, 3, 3, 1} && (ref.ReturnDestination == "RESURRECT" || ref.ReturnDestination == "THIEFDEN") {
		return itemUseReturn
	}
	// The monster capsule (TID 3/2/2): the monster mask.
	if wire.IsMonsterCapsule(ref.TypeFlags()) {
		return itemUseMonsterCapsule
	}
	if ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 3 && ref.TypeIDs[2] == 13 && ref.TypeIDs[3] >= 1 && ref.TypeIDs[3] <= 3 {
		return itemUseSkill
	}
	if ref.TypeIDs == [4]int64{3, 3, 1, 10} {
		return itemUseStructureRepair
	}
	if ref.TypeIDs == [4]int64{3, 3, 1, 8} {
		return itemUseBerserk
	}
	if ref.TypeIDs == [4]int64{3, 3, 13, 12} {
		return itemUsePetExtension
	}
	// Native special consumable case 5 (type 4 = 6): the resurrection scroll, usable only dead.
	if ref.TypeIDs == [4]int64{3, 3, 13, 6} {
		return itemUseResurrection
	}
	// Native family 6: a firework only needs a living user; its whole effect
	// is the item visual every use publishes. Type 4 = 2 is the fortress shock
	// bomb, a siege item.
	if ref.TypeIDs == [4]int64{3, 3, 6, 1} {
		return itemUseFirework
	}
	// The reverse return scroll (reversereturn.go), spent from the bag.
	if ref.TypeIDs == [4]int64{3, 3, 3, 3} {
		return itemUseReverseReturn
	}
	// Native item use case 3 (type 4 = 4): a stall booth decoration, kept for good.
	if ref.TypeIDs == [4]int64{3, 3, 3, 4} {
		return itemUseStallDecoration
	}
	// Native special consumable case 6 (type 4 = 7): the repair hammer (npcrepair.go).
	if ref.TypeIDs == [4]int64{3, 3, 13, 7} {
		return itemUseRepairHammer
	}
	// Native special consumable cases 3 and 4 (type 4 = 4 and 5): the premium time tickets.
	if ref.TypeIDs == [4]int64{3, 3, 13, 4} {
		return itemUsePremiumTicket
	}
	if ref.TypeIDs == [4]int64{3, 3, 13, 5} {
		return itemUseSkillTimeTicket
	}
	// Native special consumable case 7 (type 4 = 8): the armour gender change (gendertool.go).
	if ref.TypeIDs == [4]int64{3, 3, 13, 8} {
		return itemUseGenderTool
	}
	// Native special consumable case 8 (type 4 = 9): the character skin change (skinchange.go).
	if ref.TypeIDs == [4]int64{3, 3, 13, 9} {
		return itemUseSkinChange
	}
	// Type 4 = 10: the remote warehouse ticket (storage.go).
	if ref.TypeIDs == [4]int64{3, 3, 13, 10} {
		return itemUseWarehouseTicket
	}
	// Type 4 = 13 splits on Param1 (itemdata column 29c): 1 is the Hwan
	// scroll, which fills the berserk gauge like the type 3/3/1/8 item
	// above (ITEM_MALL_HWAN 9259); 2 is the stat point recall
	// (ITEM_MALL_(STR|INT)_RECALL 9253..9258, progression/statrecall.go).
	// The family read Param1 or it sends a Hwan to the recall, which
	// spends it resetting a player's stat points.
	if ref.TypeIDs == [4]int64{3, 3, 13, 13} {
		if param1, _ := ref.NativeFields.Lookup("itemParam1_29c"); param1 == 1 {
			return itemUseBerserk
		}
		return itemUseStatRecall
	}
	// The composite scroll (UIU1 param jobs).
	if ref.TypeIDs == [4]int64{3, 3, 13, 14} {
		return itemUseComposite
	}
	// The native COS item use, and the v1.150 client appends the COS gid for TID4 4/5/7
	// (6963CF) and for the TID3 2 TID4 7 cure (696336). TID4 6 is the
	// revival scroll (696490 / server case 6).
	if ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 3 && ref.TypeIDs[2] == 1 {
		switch ref.TypeIDs[3] {
		case 4, 5, 7:
			return itemUsePetPotion
		case 6:
			return itemUsePetRevive
		case 9:
			return itemUsePetFeed
		}
	}
	if ref.TypeIDs == [4]int64{3, 3, 2, 7} {
		return itemUsePetCure
	}
	// TID3 2 is the player cure family; TID4 1 is the universal pill.
	// TID4 7 is the pet cure, which the native COS item use applies to the owner's COS, not
	// the player, so it never enters the player cure path.
	if ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 3 && ref.TypeIDs[2] == 2 && ref.TypeIDs[3] != 7 {
		return itemUseCure
	}
	if _, ok := potionType(ref.TypeIDs); ok {
		return itemUseRecovery
	}
	if wire.IsCosSummoner(ref.TypeFlags()) && (ref.TypeIDs[3] == 1 || ref.TypeIDs[3] == 2) {
		return itemUsePersistentSummoner
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

The reference server's recovery use starts the reuse lock from the recovery amount's
arm. Absolute potions lock 1.1 s in China and 15.1 s in Europe; percentage
potions lock 4.1 s in both. The native server guard outlasts the client
icon by 100 ms. The native reuse check tests independent category reuse bits, so changing
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

/*
================
ItemUseSupported

Whether the server has a use handler for ref's family: the catalogue
export reads this registry instead of keeping its own list.
================
*/
func ItemUseSupported(ref *enterworld.ItemRef) bool {
	return admittedItemUseFamily(ref) != itemUseUnsupported
}
