/*
===========================================================================

rewardchoice.go - selection rewards offered as NPC completion rows

_RefQuestReward SelectionCnt quests grant one of several item rewards. The
v1.150 client ships no reward-selection window (its CIFQuestReward only
gives up or completes, 5C1FA0), so the completing NPC lists one row per
choice, titled by the choice's own symbol (QNO_EU_EASTEU_5's _09 Str
Scroll and _10 Int Scroll rows), and the row picked names the reward.

===========================================================================
*/
package quest

import (
	"fmt"
	"strconv"
	"strings"
)

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
rewardChoiceOptions

One completion row per choice; every row opens the quest's own complete
prompt.
================
*/
func rewardChoiceOptions(def *Definition) []NpcOption {
	out := make([]NpcOption, 0, len(def.RewardChoices))
	for i, choice := range def.RewardChoices {
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
