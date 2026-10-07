/*
===========================================================================

rewardchoice_test.go - selection rewards as NPC completion rows

===========================================================================
*/
package quest

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
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
	rows := rewardChoiceOptions(def, 1)
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

/*
================
TestSelectionRewardsOfferTheCharactersCountry

QNO_CH_SHAMAN_1 (Exorcist Miaoryeong) lists five Chinese and nine European
weapons with SelectionCnt 1 and IsCheckCountry set. Each row is titled by
its weapon's name, a character sees only its own country's weapons, and a
pick of the other country's weapon is refused before any grant.
================
*/
func TestSelectionRewardsOfferTheCharactersCountry(t *testing.T) {
	defs, items := loadShippedDefinitions(t)
	def, ok := defs.ByCodename("QNO_CH_SHAMAN_1")
	if !ok {
		t.Fatal("QNO_CH_SHAMAN_1 is not loaded")
	}
	if len(def.RewardChoices) != 14 || !def.RewardChoiceCheckCountry || len(def.RewardItems) != 0 {
		t.Fatalf("selection contract = %d choices, country check %v, %d fixed items", len(def.RewardChoices), def.RewardChoiceCheckCountry, len(def.RewardItems))
	}
	for country, want := range map[int]int{0: 5, 1: 9} {
		rows := rewardChoiceOptions(def, country)
		if len(rows) != want {
			t.Fatalf("country %d offered %d rows, want %d", country, len(rows), want)
		}
		for _, row := range rows {
			_, choice, picked := parseRewardChoiceToken(row.Codename)
			item := def.RewardChoices[choice].Items[0].ItemCodename
			ref, _ := items.ItemRefByCodename(item)
			if !picked || row.TitleSymbol != ref.NameStrID || ref.Country != int64(country) {
				t.Fatalf("country %d row %+v names %s (country %d)", country, row, item, ref.Country)
			}
		}
	}
	european := &enterworld.Character{ModelCodename: "CHAR_EU_MAN_NOBLE"}
	chinese := 0
	for i, choice := range def.RewardChoices {
		if strings.HasPrefix(choice.Items[0].ItemCodename, "ITEM_CH_") {
			chinese = i
			break
		}
	}
	if _, err := (&Runtime{}).completeRewardChoice(european, def, nil, def.EndNpcCodename, chinese); err == nil || !strings.Contains(err.Error(), "reward choices") {
		t.Fatalf("a European completed with a Chinese weapon: %v", err)
	}
}
