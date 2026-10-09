/*
===========================================================================

capture_supply.go - native trap supply grants and daily NPC reservations

Initial acceptance and refills use the inventory planner. The selected NPC
opens one confirmation token after spending the native game-day allowance;
only that pending confirmation can commit the grant.

===========================================================================
*/
package quest

import (
	"fmt"
	"strconv"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const captureSupplyCount = 5
const captureSupplyPrefix = "capture-supply:"

/*
================
captureSupply

The Ivy trap is awarded by its material quest and has a different refill
handler. The other handlers grant their items at quest acceptance. count is
the grant size; spendAtGrant spends the day only when the grant lands, as
Cerberus 1's 8B2420 stamps the day after its scissors are given. unlimited
handlers keep no allowance: Hidden Treasure 5's 8C7260 refills whenever
no key is held.
================
*/
type captureSupply struct {
	quest, item, title, prompt, success, exhausted, full string
	// held, when set, answers a player who still holds the item (8AB070's
	// _11) instead of offering nothing.
	held                                     string
	count                                    int
	afterCompletion, spendAtGrant, unlimited bool
	// grantOnlyStamps leaves the day unstamped at completion: 8AB070
	// stamps record +6 only when it grants, unlike Ivy 2's supply.
	grantOnlyStamps bool
}

var captureSupplies = []captureSupply{
	{
		quest: ivyMaterialQuest, item: "ITEM_QNO_EU_IVY_2_01", count: captureSupplyCount, afterCompletion: true,
		title: "SN_TALK_QNO_EU_IVY_2_11", prompt: "SN_TALK_QNO_EU_IVY_2_05",
		success: "SN_TALK_QNO_EU_IVY_2_12", exhausted: "SN_TALK_QNO_EU_IVY_2_16",
		full: "SN_TALK_QNO_EU_IVY_2_14",
	},
	{
		quest: "QNO_EU_EASTEU_14_1", item: "ITEM_QNO_EU_EASTEU_14_02", count: captureSupplyCount,
		title: "SN_TALK_QNO_EU_EASTEU_14_1_12", prompt: "SN_TALK_QNO_EU_EASTEU_14_1_05",
		success: "SN_TALK_QNO_EU_EASTEU_14_1_13", exhausted: "SN_TALK_QNO_EU_EASTEU_14_1_14",
		full: "SN_TALK_QNO_EU_EASTEU_14_1_06",
	},
	{
		quest: "QNO_EU_GENERAL_1", item: "ITEM_QNO_EU_GENERAL_1_01", count: captureSupplyCount,
		title: "SN_TALK_QNO_EU_GENERAL_1_06", prompt: "SN_TALK_QNO_EU_GENERAL_1_05",
		success: "SN_TALK_QNO_EU_GENERAL_1_07", exhausted: "SN_TALK_QNO_EU_GENERAL_1_08",
		full: "SN_TALK_QNO_EU_GENERAL_1_09",
	},
	{
		quest: "QNO_CA_GORIA_6", item: "ITEM_QNO_CA_GORIA_6_01", count: captureSupplyCount,
		title: "SN_TALK_QNO_CA_GORIA_6_06", prompt: "SN_TALK_QNO_CA_GORIA_6_05",
		success: "SN_TALK_QNO_CA_GORIA_6_07", exhausted: "SN_TALK_QNO_CA_GORIA_6_08",
		full: "SN_TALK_QNO_CA_GORIA_6_09",
	},
	{
		// 8B2420 grants thirty Long Scissors (mission +0x1D) at acceptance
		// and again once a day: _07 with the _08 row, _09 on the grant, _10
		// when today's scissors are spent, _14 when the bag is full.
		quest: cerberusQuest, item: cerberusScissors, count: 30, spendAtGrant: true,
		title: "SN_TALK_QNO_EU_EASTEU_19_08", prompt: "SN_TALK_QNO_EU_EASTEU_19_07",
		success: "SN_TALK_QNO_EU_EASTEU_19_09", exhausted: "SN_TALK_QNO_EU_EASTEU_19_10",
		full: "SN_TALK_QNO_EU_EASTEU_19_14",
	},
	{
		// Uvetino (8AB070): once QNO_EU_EASTEU_3 is done, ten holy water a
		// day for the Sunset Witch's stable: _05 with the _09 row, _10 on
		// the grant, _12 when today's water is given, _11 while some is
		// still held, _06 when the bag is full.
		quest: "QNO_EU_EASTEU_3", item: holyWater, count: 10, afterCompletion: true, grantOnlyStamps: true,
		title: "SN_TALK_QNO_EU_EASTEU_3_09", prompt: "SN_TALK_QNO_EU_EASTEU_3_05",
		success: "SN_TALK_QNO_EU_EASTEU_3_10", exhausted: "SN_TALK_QNO_EU_EASTEU_3_12",
		full: "SN_TALK_QNO_EU_EASTEU_3_06", held: "SN_TALK_QNO_EU_EASTEU_3_11",
	},
	{
		// 8C7260 grants five Seal Keys (mission +0x1D) at acceptance and,
		// with none held, again on every visit: _05 with the _06 row, _07 on
		// the grant, _08 when the bag is full.
		quest: treasureQuest, item: treasureKey, count: treasureKeyCount, spendAtGrant: true, unlimited: true,
		title: "SN_TALK_QNO_CA_TREASURE_5_06", prompt: "SN_TALK_QNO_CA_TREASURE_5_05",
		success: "SN_TALK_QNO_CA_TREASURE_5_07", full: "SN_TALK_QNO_CA_TREASURE_5_08",
	},
}

/*
================
captureSupplyForQuest
================
*/
func captureSupplyForQuest(code string) (captureSupply, bool) {
	for _, supply := range captureSupplies {
		if supply.quest == code {
			return supply, true
		}
	}
	return captureSupply{}, false
}

/*
================
captureSupplyCleanup

Native completion and abandonment remove unused mission traps as well as
the captured item. Reward items are never included in this cleanup plan.
================
*/
func captureSupplyCleanup(c *enterworld.Character, def *Definition) []inventory.ItemAmount {
	supply, found := captureSupplyForQuest(def.Codename)
	if !found || supply.afterCompletion {
		return nil
	}
	count := captureItemCount(c, supply.item)
	if count == 0 {
		return nil
	}
	return []inventory.ItemAmount{{Codename: supply.item, Count: count}}
}

/*
================
captureSupplyOption

Read-only projection. The allowance is spent when the row is selected, not
when the NPC's list is rendered (8B843D..8B84C9).
================
*/
func (rt *Runtime) captureSupplyOption(c *enterworld.Character, def *Definition, npc string) (NpcOption, bool) {
	supply, found := captureSupplyForQuest(def.Codename)
	if !found || npc != def.StartNpcCodename {
		return NpcOption{}, false
	}
	if captureItemCount(c, supply.item) > 0 {
		if supply.held == "" || !supply.afterCompletion || !questCompleted(c, def.RefID) || activeQuestIndex(c, def.RefID) >= 0 ||
			!prerequisitesMet(c, def) {
			return NpcOption{}, false
		}
		return NpcOption{Codename: def.Codename, TitleSymbol: def.TitleSymbol, PromptSymbol: supply.held, Informational: true}, true
	}
	if supply.afterCompletion {
		if !questCompleted(c, def.RefID) || !prerequisitesMet(c, def) {
			return NpcOption{}, false
		}
	} else if activeQuestIndex(c, def.RefID) < 0 {
		return NpcOption{}, false
	}
	// 8B2420 offers no refill once the objective stands complete.
	if def.Objective == ObjectiveCollect && !supply.afterCompletion && heldCollectCount(c, def) >= def.CollectCount {
		return NpcOption{}, false
	}
	rule, capture := captureRuleForQuest(def.Codename)
	if capture && captureItemCount(c, rule.item) > 0 {
		return NpcOption{}, false
	}
	option := NpcOption{Codename: captureSupplyPrefix + def.Codename, TitleSymbol: supply.title,
		PromptSymbol: supply.prompt, AcceptResponseSymbol: supply.success, Complete: true}
	if state, exists := c.QuestSupplies[def.RefID]; exists && !supply.unlimited && state.Day == rt.CalendarNow().Day {
		option.Informational = true
		option.PromptSymbol = supply.exhausted
	}
	return option, true
}

/*
================
setCaptureSupply

Map presence matters on world day zero. The caller owns character mutation.
================
*/
func setCaptureSupply(c *enterworld.Character, refID uint32, day uint16, pending bool) {
	if c.QuestSupplies == nil {
		c.QuestSupplies = make(map[uint32]domain.QuestSupplyState)
	}
	c.QuestSupplies[refID] = domain.QuestSupplyState{Day: day, Pending: pending, ReservedDay: day}
}

/*
================
PrepareNpcQuest

Ordinary options pass through unchanged. A supply row reserves today's quota
and returns the token retained only by the selected NPC conversation.
================
*/
func (rt *Runtime) PrepareNpcQuest(c *enterworld.Character, code, npc string) (string, error) {
	if !strings.HasPrefix(code, captureSupplyPrefix) {
		return code, nil
	}
	def, found := rt.Defs.ByCodename(strings.TrimPrefix(code, captureSupplyPrefix))
	if !found {
		return "", fmt.Errorf("unknown capture supply")
	}
	var refusal error
	var day uint16
	changed := rt.deps.Update(c, "quest-supply-reserve", func() bool {
		if c == nil || c.DeletePending {
			return false
		}
		option, available := rt.captureSupplyOption(c, def, npc)
		if !available || option.Informational {
			refusal = fmt.Errorf("capture supply is unavailable")
			if available {
				refusal = &dialogueRefusal{refusal, option.PromptSymbol}
			}
			return false
		}
		day = rt.CalendarNow().Day
		supply, _ := captureSupplyForQuest(def.Codename)
		if supply.afterCompletion || supply.spendAtGrant {
			// 8BAF60 (Ivy) and 8B2420 (Cerberus) spend the allowance at the
			// grant, unlike the direct capture quests that spend it when
			// opening the prompt.
			state, exists := c.QuestSupplies[def.RefID]
			if !exists {
				state.Day = day - 1
			}
			setCaptureSupply(c, def.RefID, state.Day, true)
			state = c.QuestSupplies[def.RefID]
			state.ReservedDay = day
			c.QuestSupplies[def.RefID] = state
		} else {
			setCaptureSupply(c, def.RefID, day, true)
		}
		return true
	})
	if refusal != nil {
		return "", refusal
	}
	if !changed {
		return "", fmt.Errorf("capture supply character unavailable")
	}
	return code + ":" + strconv.FormatUint(uint64(day), 10), nil
}

/*
================
finishCaptureSupply

Recheck the reservation, active quest, NPC and held items before granting.
Inventory failure spends the reserved day but cannot leak a partial grant.
================
*/
func (rt *Runtime) finishCaptureSupply(c *enterworld.Character, token, npc string) (OpResult, error) {
	parts := strings.Split(strings.TrimPrefix(token, captureSupplyPrefix), ":")
	if len(parts) != 2 || rt.PlanInventory == nil {
		return OpResult{}, fmt.Errorf("invalid capture supply confirmation")
	}
	day, err := strconv.ParseUint(parts[1], 10, 16)
	supply, found := captureSupplyForQuest(parts[0])
	def, defined := rt.Defs.ByCodename(parts[0])
	if err != nil || !found || !defined || npc != def.StartNpcCodename {
		return OpResult{}, fmt.Errorf("capture supply confirmation mismatch")
	}
	var refusal error
	var frames []wire.Frame
	changed := rt.deps.Update(c, "quest-supply-grant", func() bool {
		if c == nil || c.DeletePending {
			return false
		}
		if supply.afterCompletion {
			if !questCompleted(c, def.RefID) || !prerequisitesMet(c, def) {
				return false
			}
		} else if activeQuestIndex(c, def.RefID) < 0 {
			return false
		}
		state, exists := c.QuestSupplies[def.RefID]
		rule, _ := captureRuleForQuest(def.Codename)
		if !exists || !state.Pending || state.ReservedDay != uint16(day) ||
			captureItemCount(c, supply.item) > 0 || captureItemCount(c, rule.item) > 0 {
			return false
		}
		rows, updates, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: supply.item, Count: uint32(supply.count)}})
		if err != nil {
			state.Pending = false
			c.QuestSupplies[def.RefID] = state
			refusal = &dialogueRefusal{err, supply.full}
			return true
		}
		setCaptureSupply(c, def.RefID, rt.CalendarNow().Day, false)
		c.MissionInventory = rows
		objectives, _ := rt.applyInventoryChange(c)
		frames = append(updates, objectives...)
		return true
	})
	if refusal != nil {
		return OpResult{}, refusal
	}
	if !changed {
		return OpResult{}, fmt.Errorf("capture supply confirmation is stale")
	}
	return OpResult{Frames: frames}, nil
}
