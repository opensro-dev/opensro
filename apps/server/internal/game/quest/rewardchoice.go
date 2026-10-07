/*
===========================================================================

rewardchoice.go - selection rewards offered as NPC completion rows

_RefQuestReward SelectionCnt quests grant one of several item rewards. The
v1.150 client ships no reward-selection window (its CIFQuestReward only
gives up or completes, 5C1FA0), so the completing NPC lists one row per
choice, titled by the choice's own symbol (QNO_EU_EASTEU_5's _09 Str
Scroll and _10 Int Scroll rows) or by its item's name, and the row picked
names the reward. v1.188's selection packet 0x7515 (SR_GameServer
51B370: quest id, SelectionCnt, the picked indices) has no v1.150
counterpart: there 0x7515 opens the guild storage (client 701770).

===========================================================================
*/
package quest

import (
	"fmt"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// countryEvery is itemdata Country 3: an item every character may use.
const countryEvery = 3

// noRewardChoice completes a quest that offers no choice.
const noRewardChoice = -1

const rewardChoiceSeparator = "#"

/*
================
rewardChoiceToken
================
*/
func rewardChoiceToken(code string, choice int) string {
	return fmt.Sprintf("%s%s%d", code, rewardChoiceSeparator, choice)
}

/*
================
parseRewardChoiceToken
================
*/
func parseRewardChoiceToken(token string) (string, int, bool) {
	code, suffix, found := strings.Cut(token, rewardChoiceSeparator)
	if !found {
		return token, noRewardChoice, false
	}
	n, err := strconv.Atoi(suffix)
	if err != nil || n < 0 {
		return token, noRewardChoice, false
	}
	return code, n, true
}

/*
================
resolveRewardChoices

Validates a spec's choices and resolves what the item data owns: the
title of an untitled single-item choice (the item's name) and the item's
country. A kind-2 reward window or a stage completion cannot carry a
choice. Returns a fresh slice; the catalog spec is never mutated.
================
*/
func resolveRewardChoices(spec QuestSpec, items enterworld.ItemRefSource) ([]RewardChoice, error) {
	if len(spec.RewardChoices) == 0 {
		if spec.RewardChoiceCheckCountry {
			return nil, fmt.Errorf("quest %s checks reward country without choices", spec.Codename)
		}
		return nil, nil
	}
	if spec.KindByte == 2 || len(spec.Stages) > 0 || spec.EndNpcCodename == "" || items == nil {
		return nil, fmt.Errorf("quest %s invalid reward choice contract", spec.Codename)
	}
	out := make([]RewardChoice, 0, len(spec.RewardChoices))
	for _, choice := range spec.RewardChoices {
		choice.country = countryEvery
		if len(choice.Items) == 0 {
			return nil, fmt.Errorf("quest %s invalid reward choice contract", spec.Codename)
		}
		if len(choice.Items) == 1 {
			ref, ok := items.ItemRefByCodename(choice.Items[0].ItemCodename)
			if !ok || ref == nil {
				return nil, fmt.Errorf("quest %s unresolved reward %s", spec.Codename, choice.Items[0].ItemCodename)
			}
			if choice.TitleSymbol == "" {
				choice.TitleSymbol = ref.NameStrID
			}
			choice.country = ref.Country
		} else if spec.RewardChoiceCheckCountry {
			return nil, fmt.Errorf("quest %s country-checked choice holds several items", spec.Codename)
		}
		if choice.TitleSymbol == "" {
			return nil, fmt.Errorf("quest %s invalid reward choice contract", spec.Codename)
		}
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
window; the v1.150 NPC rows have no client filter, so the server applies
the one check the item data can answer. IsCheckClass and IsCheckGender
are not applied: the v1.150 selection rows (QNO_CH_SHAMAN_1,
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
rewardChoiceOptions

One completion row per choice the character may pick; every row opens the
quest's own complete prompt.
================
*/
func rewardChoiceOptions(def *Definition, country int) []NpcOption {
	out := make([]NpcOption, 0, len(def.RewardChoices))
	for i, choice := range def.RewardChoices {
		if !rewardChoiceOffered(def, country, i) {
			continue
		}
		out = append(out, NpcOption{
			Codename: rewardChoiceToken(def.Codename, i), TitleSymbol: choice.TitleSymbol,
			PromptSymbol: def.CompletePromptSymbol, Complete: true,
		})
	}
	return out
}

/*
================
rewardItemsWithChoice

The fixed items plus the picked choice's.
================
*/
func rewardItemsWithChoice(def *Definition, choice int) []RewardItemLead {
	items := append([]RewardItemLead(nil), def.RewardItems...)
	if choice >= 0 && choice < len(def.RewardChoices) {
		items = append(items, def.RewardChoices[choice].Items...)
	}
	return items
}
