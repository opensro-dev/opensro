/*
===========================================================================

rewardchoice_test.go - selection picks by item id in the reward window

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
		RewardItems:          []RewardItemLead{{ItemCodename: "ITEM_FIXED", Count: 2}},
		RewardSelectionCount: 1,
		RewardChoices: []RewardChoice{
			{TitleSymbol: "SN_TALK_QNO_EU_EASTEU_5_09", Items: []RewardItemLead{{ItemCodename: "ITEM_QNO_EU_EASTEU_5_02", Count: 1}}, refID: 101},
			{TitleSymbol: "SN_TALK_QNO_EU_EASTEU_5_10", Items: []RewardItemLead{{ItemCodename: "ITEM_QNO_EU_EASTEU_5_03", Count: 1}}, refID: 102},
		},
	}}
}

/*
================
TestRewardPicksResolveByItemID

The pick names items by RefObjID: it resolves to the choice index, the
count must be the row's SelectionCnt, an unknown id refuses with the
unknown reason and a doubled id with the count reason. The grant is the
fixed items plus the picked choice's.
================
*/
func TestRewardPicksResolveByItemID(t *testing.T) {
	def := choiceDefinition()
	picks, _, ok := resolveRewardPicks(def, 1, []uint32{102})
	if !ok || len(picks) != 1 || picks[0] != 1 {
		t.Fatalf("picks %+v ok %v", picks, ok)
	}
	if _, reason, ok := resolveRewardPicks(def, 1, nil); ok || reason != rewardRefusalCount {
		t.Fatalf("no pick: reason %d ok %v", reason, ok)
	}
	if _, reason, ok := resolveRewardPicks(def, 1, []uint32{101, 102}); ok || reason != rewardRefusalCount {
		t.Fatalf("two picks: reason %d ok %v", reason, ok)
	}
	if _, reason, ok := resolveRewardPicks(def, 1, []uint32{999}); ok || reason != rewardRefusalUnknown {
		t.Fatalf("unknown item: reason %d ok %v", reason, ok)
	}
	dup := &Definition{QuestSpec: QuestSpec{RewardSelectionCount: 2, RewardChoices: def.RewardChoices}}
	if _, reason, ok := resolveRewardPicks(dup, 1, []uint32{101, 101}); ok || reason != rewardRefusalCount {
		t.Fatalf("doubled pick: reason %d ok %v", reason, ok)
	}
	items := rewardItemsWithChoices(def, []int{1})
	if len(items) != 2 || items[0].ItemCodename != "ITEM_FIXED" || items[1].ItemCodename != "ITEM_QNO_EU_EASTEU_5_03" {
		t.Fatalf("items %+v", items)
	}
	if len(rewardItemsWithChoices(def, nil)) != 1 {
		t.Fatal("no pick granted a choice item")
	}
}

/*
================
TestRewardPicksRequiredForSelectionQuests

A completion without picks, or with an unoffered pick, cannot finish a
selection quest; a quest without choices refuses picks.
================
*/
func TestRewardPicksRequiredForSelectionQuests(t *testing.T) {
	rt := &Runtime{}
	if _, err := rt.completeRewardPicks(nil, choiceDefinition(), nil); err == nil || !strings.Contains(err.Error(), "reward choices") {
		t.Fatalf("a selection quest completed without picks: %v", err)
	}
	if _, err := rt.completeRewardPicks(nil, choiceDefinition(), []int{2}); err == nil {
		t.Fatal("an out-of-range pick was admitted")
	}
	plain := &Definition{QuestSpec: QuestSpec{Codename: "QNO_PLAIN"}}
	if _, err := rt.completeRewardPicks(nil, plain, []int{0}); err == nil || !strings.Contains(err.Error(), "offers no reward choice") {
		t.Fatalf("a plain quest took a pick: %v", err)
	}
}

/*
================
TestSelectionRewardsOfferTheCharactersCountry

QNO_CH_SHAMAN_1 (Exorcist Miaoryeong) lists five Chinese and nine European
weapons with SelectionCnt 1 and IsCheckCountry set. A character may pick
only its own country's weapons, each pick named by the item's RefObjID,
and a pick of the other country's weapon settles nothing.
================
*/
func TestSelectionRewardsOfferTheCharactersCountry(t *testing.T) {
	defs, items := loadShippedDefinitions(t)
	def, ok := defs.ByCodename("QNO_CH_SHAMAN_1")
	if !ok {
		t.Fatal("QNO_CH_SHAMAN_1 is not loaded")
	}
	if len(def.RewardChoices) != 14 || !def.RewardChoiceCheckCountry || len(def.RewardItems) != 0 || def.RewardSelectionCount != 1 {
		t.Fatalf("selection contract = %d choices, country check %v, %d fixed items, pick %d", len(def.RewardChoices), def.RewardChoiceCheckCountry, len(def.RewardItems), def.RewardSelectionCount)
	}
	for country, want := range map[int]int{0: 5, 1: 9} {
		offered := offeredChoiceIndices(def, country)
		if len(offered) != want {
			t.Fatalf("country %d offered %d picks, want %d", country, len(offered), want)
		}
		for _, i := range offered {
			choice := def.RewardChoices[i]
			ref, _ := items.ItemRefByCodename(choice.Items[0].ItemCodename)
			if choice.refID != ref.RefObjID || choice.TitleSymbol != ref.NameStrID || ref.Country != int64(country) {
				t.Fatalf("country %d pick %+v names %s (RefObjID %d, country %d)", country, choice, choice.Items[0].ItemCodename, ref.RefObjID, ref.Country)
			}
		}
	}
	european := &enterworld.Character{ModelCodename: "CHAR_EU_MAN_NOBLE"}
	chinese := -1
	for i, choice := range def.RewardChoices {
		if strings.HasPrefix(choice.Items[0].ItemCodename, "ITEM_CH_") {
			chinese = i
			break
		}
	}
	if chinese < 0 {
		t.Fatal("QNO_CH_SHAMAN_1 offers no Chinese weapon")
	}
	if _, reason, ok := resolveRewardPicks(def, 1, []uint32{def.RewardChoices[chinese].refID}); ok || reason != rewardRefusalUnknown {
		t.Fatalf("a European picked a Chinese weapon: reason %d ok %v", reason, ok)
	}
	if _, err := (&Runtime{}).completeRewardPicks(european, def, []int{chinese}); err == nil || !strings.Contains(err.Error(), "reward choices") {
		t.Fatalf("a European completed with a Chinese weapon: %v", err)
	}
}
