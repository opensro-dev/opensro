/*
===========================================================================

gm_test.go - the GM's quest tools against the real quest catalog

===========================================================================
*/
package quest

import (
	"bytes"
	"slices"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/clientdata"
)

const (
	gmTutorialQuest = "QTUTORIAL_CH"
	gmTutorialRefID = 2
	gmSoldierQuest  = "QNO_CH_SOLDIER_EA1_1"
)

/*
================
gmAbandonFrames counts the abandon frames for one quest in a result.
================
*/
func gmAbandonFrames(result OpResult, refID uint32) int {
	count := 0
	for _, frame := range result.Frames {
		if frame.Opcode == OpQuestUpdate && bytes.Equal(frame.Payload, EncodeQuestUpdateAbandon(refID)) {
			count++
		}
	}
	return count
}

func TestGMRemoveQuestDropsTheActiveRecordAndKeepsHistory(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	if _, err := rt.StartQuest(c, gmTutorialQuest); err != nil {
		t.Fatal(err)
	}
	c.CompletedQuestIds = []uint32{77}

	result, err := rt.GMRemoveQuest(c, "qtutorial_ch")
	if err != nil {
		t.Fatalf("GMRemoveQuest: %v", err)
	}
	if len(c.ActiveQuests) != 0 || !slices.Equal(c.CompletedQuestIds, []uint32{77}) {
		t.Fatalf("active %v completed %v, want the record gone and history kept", c.ActiveQuests, c.CompletedQuestIds)
	}
	if gmAbandonFrames(result, gmTutorialRefID) != 1 {
		t.Fatalf("frames %+v, want one abandon", result.Frames)
	}
	if _, err := rt.GMRemoveQuest(c, gmTutorialQuest); err == nil {
		t.Fatal("removing a quest that is not active must refuse")
	}
	if _, err := rt.GMRemoveQuest(c, "QNO_NOT_CURATED"); err == nil {
		t.Fatal("an unknown codename must refuse")
	}
}

func TestGMRemoveQuestAllowsWhatTheGiveUpWindowRefuses(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	if _, err := rt.StartQuest(c, gmTutorialQuest); err != nil {
		t.Fatal(err)
	}
	c.ActiveQuests[0].Stage = 3
	if _, err := rt.HandleGiveUp(c, u32le(gmTutorialRefID)); err == nil {
		t.Fatal("the player's give-up must refuse a paid stage")
	}
	if _, err := rt.GMRemoveQuest(c, gmTutorialQuest); err != nil || len(c.ActiveQuests) != 0 {
		t.Fatalf("the GM tool must drop it: %v, active %v", err, c.ActiveQuests)
	}
}

func TestGMInitQuestForgetsEverythingThatRemembersTheQuest(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	c.CompletedQuestIds = []uint32{gmTutorialRefID, 77}
	c.EndedQuestIds = []uint32{gmTutorialRefID, 78}
	c.QuestCompletionCounts = map[uint32]uint32{gmTutorialRefID: 1, 77: 2}
	c.QuestSupplies = map[uint32]domain.QuestSupplyState{gmTutorialRefID: {Day: 4}, 77: {Day: 5}}
	if _, err := rt.StartQuest(c, gmTutorialQuest); err == nil {
		t.Fatal("a completed quest must not start before it is initialized")
	}

	if _, err := rt.GMInitQuest(c, gmTutorialQuest); err != nil {
		t.Fatalf("GMInitQuest: %v", err)
	}
	if !slices.Equal(c.CompletedQuestIds, []uint32{77}) || !slices.Equal(c.EndedQuestIds, []uint32{78}) {
		t.Fatalf("completed %v ended %v, want only the other quests left", c.CompletedQuestIds, c.EndedQuestIds)
	}
	if _, ok := c.QuestCompletionCounts[gmTutorialRefID]; ok || c.QuestCompletionCounts[77] != 2 {
		t.Fatalf("counts %v, want only the other quest's", c.QuestCompletionCounts)
	}
	if _, ok := c.QuestSupplies[gmTutorialRefID]; ok || len(c.QuestSupplies) != 1 {
		t.Fatalf("supplies %v, want only the other quest's", c.QuestSupplies)
	}
	if _, err := rt.StartQuest(c, gmTutorialQuest); err != nil {
		t.Fatalf("the initialized quest must start again: %v", err)
	}
}

func TestGMInitQuestAbandonsAnActiveQuestAndIsIdempotent(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	if _, err := rt.StartQuest(c, gmTutorialQuest); err != nil {
		t.Fatal(err)
	}
	result, err := rt.GMInitQuest(c, gmTutorialQuest)
	if err != nil || len(c.ActiveQuests) != 0 || gmAbandonFrames(result, gmTutorialRefID) != 1 {
		t.Fatalf("init of an active quest: %v, active %v, frames %+v", err, c.ActiveQuests, result.Frames)
	}
	result, err = rt.GMInitQuest(c, gmTutorialQuest)
	if err != nil || len(result.Frames) != 0 {
		t.Fatalf("init of a quest in its initial state must succeed silently: %v, %+v", err, result.Frames)
	}
	if _, err := rt.GMInitQuest(c, "QNO_NOT_CURATED"); err == nil {
		t.Fatal("an unknown codename must refuse")
	}
}

func TestGMResetQuestsClearsTheJournalAndEveryHistory(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	level := int64(3)
	c.Level = &level
	for _, code := range []string{gmTutorialQuest, gmSoldierQuest} {
		if _, err := rt.StartQuest(c, code); err != nil {
			t.Fatalf("start %s: %v", code, err)
		}
	}
	c.CompletedQuestIds = []uint32{77}
	c.EndedQuestIds = []uint32{78}
	c.QuestCompletionCounts = map[uint32]uint32{77: 1}
	c.QuestSupplies = map[uint32]domain.QuestSupplyState{77: {Day: 5}}

	result, err := rt.GMResetQuests(c)
	if err != nil {
		t.Fatalf("GMResetQuests: %v", err)
	}
	if len(c.ActiveQuests) != 0 || len(c.CompletedQuestIds) != 0 || len(c.EndedQuestIds) != 0 ||
		len(c.QuestCompletionCounts) != 0 || len(c.QuestSupplies) != 0 {
		t.Fatalf("journal %v completed %v ended %v counts %v supplies %v, want all empty",
			c.ActiveQuests, c.CompletedQuestIds, c.EndedQuestIds, c.QuestCompletionCounts, c.QuestSupplies)
	}
	soldier, _ := rt.Defs.ByCodename(gmSoldierQuest)
	if gmAbandonFrames(result, gmTutorialRefID) != 1 || gmAbandonFrames(result, soldier.RefID) != 1 {
		t.Fatalf("frames %+v, want one abandon for each active quest", result.Frames)
	}
}

func TestGMResetQuestsDropsARecordNoDefinitionOwns(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	const orphan = 987654
	c.ActiveQuests = []enterworld.ActiveQuestRecord{{RefID: orphan}}

	result, err := rt.GMResetQuests(c)
	if err != nil || len(c.ActiveQuests) != 0 || gmAbandonFrames(result, orphan) != 1 {
		t.Fatalf("reset of an orphan record: %v, active %v, frames %+v", err, c.ActiveQuests, result.Frames)
	}
}

func TestGMCompleteQuestPaysTheRewardWithoutTheObjective(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	level := int64(3)
	c.Level = &level
	if _, err := rt.GMCompleteQuest(c, gmSoldierQuest); err == nil {
		t.Fatal("completing a quest that is not active must refuse")
	}
	if _, err := rt.StartQuest(c, gmSoldierQuest); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.CompleteNpcQuest(c, gmSoldierQuest); err == nil {
		t.Fatal("precondition: the ordinary turn-in must refuse an unfinished objective")
	}

	result, err := rt.GMCompleteQuest(c, gmSoldierQuest)
	if err != nil {
		t.Fatalf("GMCompleteQuest: %v", err)
	}
	soldier, _ := rt.Defs.ByCodename(gmSoldierQuest)
	if len(c.ActiveQuests) != 0 || !slices.Contains(c.CompletedQuestIds, soldier.RefID) {
		t.Fatalf("active %v completed %v, want a completed quest", c.ActiveQuests, c.CompletedQuestIds)
	}
	if c.Gold == nil || *c.Gold != 1000 {
		t.Fatalf("gold %v, want the quest's 1000", c.Gold)
	}
	if len(result.Frames) == 0 {
		t.Fatal("the reward frames must come back")
	}
	if _, err := rt.GMCompleteQuest(c, gmSoldierQuest); err == nil {
		t.Fatal("a completed quest must not pay twice")
	}
}

func TestGMCompleteQuestWalksEveryStageOfAStagedQuest(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	if _, err := rt.StartQuest(c, gmTutorialQuest); err != nil {
		t.Fatal(err)
	}

	result, err := rt.GMCompleteQuest(c, gmTutorialQuest)
	if err != nil {
		t.Fatalf("GMCompleteQuest: %v (frames %+v)", err, result.Frames)
	}
	if len(c.ActiveQuests) != 0 || !slices.Contains(c.CompletedQuestIds, gmTutorialRefID) {
		t.Fatalf("active %v completed %v, want the tutorial finished", c.ActiveQuests, c.CompletedQuestIds)
	}
	if c.Gold == nil || *c.Gold != 200 {
		t.Fatalf("gold %v, want the errand stage's 200", c.Gold)
	}
}

func TestGMCompleteQuestTakesTheShortObjectiveItemsThatAreHeld(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	const code = "QNO_CH_SPECIAL_1"
	def, _ := rt.Defs.ByCodename(code)
	level := int64(def.Level)
	c.Level = &level
	c.CompletedQuestIds = append(c.CompletedQuestIds, def.RequiredQuestIDs...)
	if _, err := rt.StartQuest(c, code); err != nil {
		t.Fatal(err)
	}
	const held = 4
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: def.CollectItemRefID, Codename: def.CollectItemCodename, StackCount: held}}
	rt.NotifyInventoryChanged(c)
	if _, err := rt.CompleteNpcQuest(c, code); err == nil {
		t.Fatal("precondition: the ordinary turn-in must refuse a short collection")
	}

	if _, err := rt.GMCompleteQuest(c, code); err != nil {
		t.Fatalf("GMCompleteQuest: %v", err)
	}
	if len(c.ActiveQuests) != 0 || captureItemCount(c, def.CollectItemCodename) != 0 {
		t.Fatalf("active %v, held %d: the quest must close and take the %d held items",
			c.ActiveQuests, captureItemCount(c, def.CollectItemCodename), held)
	}
	if c.Gold == nil || *c.Gold != def.RewardGold {
		t.Fatalf("gold %v, want the quest's %d", c.Gold, def.RewardGold)
	}
}

func TestGMCompleteQuestStillRefusesTheResuscitationExchangeWithoutHearts(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	c.MissionInventory = potionInventory(4)
	if _, err := rt.StartQuest(c, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.GMCompleteQuest(c, "QSP_ALL_POTION_1"); err == nil || len(c.ActiveQuests) != 1 {
		t.Fatalf("the exchange needs a whole group of hearts: err %v, active %v", err, c.ActiveQuests)
	}
}

func TestGMCompleteQuestCompletesWhenTheExperienceIsCapped(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	rt.ApplyExperience = func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
		return nil, false
	}
	c := questCharacter()
	level := int64(3)
	c.Level = &level
	if _, err := rt.StartQuest(c, gmSoldierQuest); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.GMCompleteQuest(c, gmSoldierQuest); err != nil {
		t.Fatalf("a capped grant must not refuse the GM's completion: %v", err)
	}
	soldier, _ := rt.Defs.ByCodename(gmSoldierQuest)
	if len(c.ActiveQuests) != 0 || !slices.Contains(c.CompletedQuestIds, soldier.RefID) {
		t.Fatalf("active %v completed %v, want a completed quest", c.ActiveQuests, c.CompletedQuestIds)
	}
	if c.Gold == nil || *c.Gold != 1000 {
		t.Fatalf("gold %v, want the gold the quest still pays", c.Gold)
	}
}

func TestFirstOfferedChoiceSkipsTheOtherCountrysReward(t *testing.T) {
	def := &Definition{QuestSpec: QuestSpec{
		RewardChoiceCheckCountry: true,
		RewardChoices:            []RewardChoice{{country: 0}, {country: countryEvery}},
	}}
	if got := firstOfferedChoice(def, 1); got != 1 {
		t.Fatalf("choice %d, want the country-free reward", got)
	}
	def.RewardChoices[0].country = 1
	if got := firstOfferedChoice(def, 1); got != 0 {
		t.Fatalf("choice %d, want the first reward of the character's country", got)
	}
	if got := firstOfferedChoice(&Definition{}, 1); got != noRewardChoice {
		t.Fatalf("choice %d, want none for a quest without choices", got)
	}
}

func TestClampToHeldTakesOnlyWhatTheBagHolds(t *testing.T) {
	c := questCharacter()
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 20, Codename: "ITEM_A", StackCount: 3}}
	got := clampToHeld(c, []inventory.ItemAmount{{Codename: "ITEM_A", Count: 10}, {Codename: "ITEM_B", Count: 2}})
	if len(got) != 1 || got[0].Codename != "ITEM_A" || got[0].Count != 3 {
		t.Fatalf("clamped %+v, want only the 3 held ITEM_A", got)
	}
}
