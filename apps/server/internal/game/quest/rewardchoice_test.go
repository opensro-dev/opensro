/*
===========================================================================

rewardchoice_test.go - selection rewards as NPC completion rows

===========================================================================
*/
package quest

import (
	"strings"
	"testing"
)

/*
================
choiceDefinition
================
*/
func choiceDefinition() *Definition {
	return &Definition{QuestSpec: QuestSpec{
		Codename: "QNO_EU_EASTEU_5", CompletePromptSymbol: "SN_TALK_QNO_EU_EASTEU_5_08",
		RewardItems: []RewardItemLead{{ItemCodename: "ITEM_FIXED", Count: 2}},
		RewardChoices: []RewardChoice{
			{TitleSymbol: "SN_TALK_QNO_EU_EASTEU_5_09", Items: []RewardItemLead{{ItemCodename: "ITEM_QNO_EU_EASTEU_5_02", Count: 1}}},
			{TitleSymbol: "SN_TALK_QNO_EU_EASTEU_5_10", Items: []RewardItemLead{{ItemCodename: "ITEM_QNO_EU_EASTEU_5_03", Count: 1}}},
		},
	}}
}

/*
================
TestRewardChoiceRowsNameTheirItems

Each choice is its own completion row with its title and the quest's
complete prompt; its token resolves back to the quest and the pick, and
the grant is the fixed items plus the picked choice's.
================
*/
func TestRewardChoiceRowsNameTheirItems(t *testing.T) {
	def := choiceDefinition()
	rows := rewardChoiceOptions(def)
	if len(rows) != 2 || rows[1].TitleSymbol != "SN_TALK_QNO_EU_EASTEU_5_10" || !rows[1].Complete || rows[1].PromptSymbol != def.CompletePromptSymbol {
		t.Fatalf("rows %+v", rows)
	}
	code, choice, picked := parseRewardChoiceToken(rows[1].Codename)
	if !picked || code != def.Codename || choice != 1 {
		t.Fatalf("token %q -> %q %d %v", rows[1].Codename, code, choice, picked)
	}
	for _, bad := range []string{def.Codename, def.Codename + "#x", def.Codename + "#-1"} {
		if _, _, picked := parseRewardChoiceToken(bad); picked {
			t.Fatalf("%q parsed as a choice", bad)
		}
	}
	items := rewardItemsWithChoice(def, 1)
	if len(items) != 2 || items[0].ItemCodename != "ITEM_FIXED" || items[1].ItemCodename != "ITEM_QNO_EU_EASTEU_5_03" {
		t.Fatalf("items %+v", items)
	}
	if len(rewardItemsWithChoice(def, noRewardChoice)) != 1 {
		t.Fatal("no choice granted a choice item")
	}
}

/*
================
TestRewardChoiceRequiredForSelectionQuests

The kind-2 reward window and a plain completion carry no choice; they
cannot finish a selection quest, nor pick one for a quest without choices.
================
*/
func TestRewardChoiceRequiredForSelectionQuests(t *testing.T) {
	rt := &Runtime{}
	if _, err := rt.completeRewardChoice(nil, choiceDefinition(), nil, "", noRewardChoice); err == nil || !strings.Contains(err.Error(), "reward choices") {
		t.Fatalf("a choice quest completed without a choice: %v", err)
	}
	if _, err := rt.completeRewardChoice(nil, choiceDefinition(), nil, "", 2); err == nil {
		t.Fatal("an out-of-range choice was admitted")
	}
	plain := &Definition{QuestSpec: QuestSpec{Codename: "QNO_PLAIN"}}
	if _, err := rt.completeRewardChoice(nil, plain, nil, "", 0); err == nil || !strings.Contains(err.Error(), "offers no reward choice") {
		t.Fatalf("a plain quest took a choice: %v", err)
	}
}
