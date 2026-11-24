/*
===========================================================================

rewardchoice.go - selection rewards picked in the reward window

_RefQuestReward SelectionCnt quests grant a pick of the listed items. The
later client's CIFQuestReward window lists the items with the reward row's
Gold/EXP/SP/SP EXP/AP/JOB EXP values; the player picks exactly
SelectionCnt and the pick rides the reward request as item ids (the
v1.188 server's reward-selection handler: quest id, pick count, the
picked item ids; it refuses a count other than SelectionCnt and answers
with a reason byte). v1.150 has no selection window - its 0x7515 opens
the guild storage - so the port keeps the v1.150 reward request (0x729A)
and composes the same pick body behind it (wire.go).

A selection quest claims in the window, never at the NPC: the completing
NPC offers no completion row for it (runtime.go), and the quest record
carries the reward-window kind byte (Definition.windowKindByte).

===========================================================================
*/
package quest

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
)

// countryEvery is itemdata Country 3: an item every character may use.
const countryEvery = 3

// The native refusal reasons the reward answer carries (the v1.188
// server's reward-selection answer: result 2, then one of these).
const (
	// rewardRefusalUnknown: no reward row for the quest, or a picked
	// item the quest does not offer this character.
	rewardRefusalUnknown uint8 = 1
	// rewardRefusalCount: the pick count is not the row's SelectionCnt.
	rewardRefusalCount uint8 = 2
)

// maxRewardPicks bounds a composed pick list. The widest shipped
// selection lists fourteen items; the count byte itself caps at 255.
const maxRewardPicks = 32

/*
================
resolveRewardChoices

Validates a spec's choices and resolves what the item data owns: the
title of an untitled choice (the item's name), the item's country and
its RefObjID. A choice holds exactly one item: the pick list the client
composes is items, never bundles. A kind-2 reward window or a stage
completion cannot carry a choice. Returns a fresh slice; the catalog
spec is never mutated.
================
*/
func resolveRewardChoices(spec QuestSpec, items enterworld.ItemRefSource) ([]RewardChoice, error) {
	if len(spec.RewardChoices) == 0 {
		if spec.RewardChoiceCheckCountry || spec.RewardSelectionCount != 0 {
			return nil, fmt.Errorf("quest %s checks reward country or count without choices", spec.Codename)
		}
		return nil, nil
	}
	if spec.KindByte == 2 || len(spec.Stages) > 0 || spec.EndNpcCodename == "" || items == nil {
		return nil, fmt.Errorf("quest %s invalid reward choice contract", spec.Codename)
	}
	if spec.RewardSelectionCount < 1 || spec.RewardSelectionCount > len(spec.RewardChoices) {
		return nil, fmt.Errorf("quest %s picks %d of %d reward choices", spec.Codename, spec.RewardSelectionCount, len(spec.RewardChoices))
	}
	out := make([]RewardChoice, 0, len(spec.RewardChoices))
	for _, choice := range spec.RewardChoices {
		if len(choice.Items) != 1 {
			return nil, fmt.Errorf("quest %s reward choice holds %d items, want one", spec.Codename, len(choice.Items))
		}
		ref, ok := items.ItemRefByCodename(choice.Items[0].ItemCodename)
		if !ok || ref == nil {
			return nil, fmt.Errorf("quest %s unresolved reward %s", spec.Codename, choice.Items[0].ItemCodename)
		}
		if choice.TitleSymbol == "" {
			choice.TitleSymbol = ref.NameStrID
		}
		choice.country = ref.Country
		choice.refID = ref.RefObjID
		choice.Items = append([]RewardItemLead(nil), choice.Items...)
		out = append(out, choice)
	}
	return out, nil
}

/*
================
rewardChoiceOffered

Whether the character may pick choice i. IsCheckCountry hides an item of
the other country; a country-3 item suits everyone. The v1.188 server
skips the IsCheck* columns because its client filters the selection
window; the port's window filters the same way, and the server re-checks
here because the pick is untrusted. IsCheckClass and IsCheckGender are
not applied: the v1.150 selection rows (QNO_CH_SHAMAN_1,
QNO_EU_CONS_11) set class but list every weapon of a country, and set no
gender check.
================
*/
func rewardChoiceOffered(def *Definition, country int, i int) bool {
	if i < 0 || i >= len(def.RewardChoices) {
		return false
	}
	c := def.RewardChoices[i].country
	return !def.RewardChoiceCheckCountry || c == countryEvery || c == int64(country)
}

/*
================
offeredChoiceIndices

The choices this character may pick, in row order.
================
*/
func offeredChoiceIndices(def *Definition, country int) []int {
	out := make([]int, 0, len(def.RewardChoices))
	for i := range def.RewardChoices {
		if rewardChoiceOffered(def, country, i) {
			out = append(out, i)
		}
	}
	return out
}

/*
================
firstOfferedChoices

The first SelectionCnt offered choices: the GM force completion's pick
(gm.go), the same rows the window would preselect.
================
*/
func firstOfferedChoices(def *Definition, country int) []int {
	offered := offeredChoiceIndices(def, country)
	if len(offered) > def.RewardSelectionCount {
		offered = offered[:def.RewardSelectionCount]
	}
	return offered
}

/*
================
resolveRewardPicks

Maps a composed pick list (item RefObjIDs) to choice indices for this
character. The count must be the row's SelectionCnt, every pick an
offered choice's item, and no item twice - the client composes exactly
that, so anything else is refused with the native reason the answer
carries (rewardRefusalCount / rewardRefusalUnknown).
================
*/
func resolveRewardPicks(def *Definition, country int, picked []uint32) ([]int, uint8, bool) {
	if len(picked) != def.RewardSelectionCount {
		return nil, rewardRefusalCount, false
	}
	out := make([]int, 0, len(picked))
	seen := make(map[uint32]bool, len(picked))
	for _, id := range picked {
		if seen[id] {
			return nil, rewardRefusalCount, false
		}
		seen[id] = true
		found := -1
		for i := range def.RewardChoices {
			if def.RewardChoices[i].refID == id && rewardChoiceOffered(def, country, i) {
				found = i
				break
			}
		}
		if found < 0 {
			return nil, rewardRefusalUnknown, false
		}
		out = append(out, found)
	}
	return out, 0, true
}

/*
================
rewardItemsWithChoices

The fixed items plus the picked choices'.
================
*/
func rewardItemsWithChoices(def *Definition, picks []int) []RewardItemLead {
	items := append([]RewardItemLead(nil), def.RewardItems...)
	for _, pick := range picks {
		if pick >= 0 && pick < len(def.RewardChoices) {
			items = append(items, def.RewardChoices[pick].Items...)
		}
	}
	return items
}
