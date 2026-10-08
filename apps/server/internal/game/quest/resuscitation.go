/*
===========================================================================

resuscitation.go - the herbalist's native skill withdrawal service

The collection quest and withdrawal service share an NPC dialog, but opening
the skill window does not accept, complete or consume a quest. Actual spending
belongs to progression's atomic withdrawal transaction.

===========================================================================
*/
package quest

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const ResuscitationService = "QSP_ALL_POTION_1:withdraw"
const resuscitationMinimumLevel = 20
const resuscitationOpenOpcode = 0x3230
const resuscitationQuest = "QSP_ALL_POTION_1"
const resuscitationPotion = "ITEM_QSP_ALL_POTION_1_01"
const resuscitationPotionLimit = 30

/*
================
ResuscitationAvailable

88B2A0 registers five potion merchants; 88B760 requires level 20 for the
dialog, without requiring an active collection quest or a potion in advance.
================
*/
func ResuscitationAvailable(c *enterworld.Character, npc string) bool {
	if c == nil || c.DeletePending || c.Level == nil || *c.Level < resuscitationMinimumLevel {
		return false
	}
	return resuscitationMerchant(npc)
}

/*
================
resuscitationMerchant

88B2A0 registers the same collection and withdrawal services in all towns.
================
*/
func resuscitationMerchant(npc string) bool {
	switch npc {
	case "NPC_CH_POTION", "NPC_WC_POTION", "NPC_KT_POTION", "NPC_EU_POTION", "NPC_CA_POTION":
		return true
	default:
		return false
	}
}

/*
================
questNpcMatches

Ordinary quests bind one merchant. Resuscitation is a shared herbalist
service; offer and completion must accept the same five identities.
================
*/
func questNpcMatches(def *Definition, expected, actual string) bool {
	if actual == "" {
		return false
	}
	if def.Codename == resuscitationQuest {
		return resuscitationMerchant(actual)
	}
	return expected == actual
}

/*
================
resuscitationExchangeCount

88C980 exchanges every complete group of hearts, capped by the thirty
potions already held. Count inventory under the character transaction;
the journal's ten-heart progress bar is deliberately saturated.
================
*/
func resuscitationExchangeCount(c *enterworld.Character, def *Definition) (uint32, error) {
	if def.Codename != resuscitationQuest {
		return 1, nil
	}
	if def.CollectCount == 0 {
		return 0, fmt.Errorf("resuscitation exchange has no heart price")
	}
	var hearts, potions uint64
	for _, row := range c.MissionInventory {
		if !inventory.InBag(c, row.Slot) || row.StackCount <= 0 {
			continue
		}
		if row.RefObjID == def.CollectItemRefID {
			hearts = min(hearts+uint64(row.StackCount), uint64(def.CollectCount)*resuscitationPotionLimit)
		}
		if strings.EqualFold(row.Codename, resuscitationPotion) {
			potions = min(potions+uint64(row.StackCount), resuscitationPotionLimit)
		}
	}
	if potions >= resuscitationPotionLimit {
		return 0, fmt.Errorf("resuscitation potion limit reached")
	}
	count := min(hearts/uint64(def.CollectCount), resuscitationPotionLimit-potions)
	if count == 0 {
		return 0, fmt.Errorf("resuscitation exchange requires a complete group of hearts")
	}
	return uint32(count), nil
}

/*
================
OpenResuscitation

The selected-NPC owner validates distance and conversation identity before
calling this method. Re-read potion ownership under the character door; the
empty native 3230 response opens mode 2 and leaves inventory unchanged.
================
*/
func (rt *Runtime) OpenResuscitation(c *enterworld.Character, npc string) (OpResult, error) {
	if !ResuscitationAvailable(c, npc) {
		return OpResult{}, fmt.Errorf("resuscitation service unavailable")
	}
	result := OpResult{}
	rt.deps.Update(c, "resuscitation-open", func() bool {
		if !ResuscitationAvailable(c, npc) {
			return false
		}
		for _, row := range c.MissionInventory {
			if inventory.InBag(c, row.Slot) &&
				row.StackCount > 0 && strings.EqualFold(row.Codename, resuscitationPotion) {
				result.Frames = []wire.Frame{{Opcode: resuscitationOpenOpcode}}
				return false
			}
		}
		symbol := "SN_TALK_QSP_ALL_POTION_1_05"
		if npc == "NPC_KT_POTION" {
			symbol = "SN_TALK_QSP_ALL_POTION_1_17"
		} else if npc == "NPC_EU_POTION" || npc == "NPC_CA_POTION" {
			symbol = "SN_TALK_QSP_ALL_POTION_1_22"
		}
		result.Frames = []wire.Frame{{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(symbol)}}
		return false
	})
	if len(result.Frames) == 0 {
		return OpResult{}, fmt.Errorf("resuscitation character unavailable")
	}
	return result, nil
}
