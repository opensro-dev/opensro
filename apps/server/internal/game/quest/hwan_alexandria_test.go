/*
===========================================================================

hwan_alexandria_test.go - the Alexandria Blue Berserk quests

The table's shape needs no game data; the gating, the Blue Berserk level
reward and the drop knob run over the shipped data when the build has the
cap-110 quest tables (they skip otherwise, like the other catalog tests).

===========================================================================
*/
package quest

import (
	"slices"
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func TestHwanAlexandriaTableIsTheWholeChain(t *testing.T) {
	specs := hwanAlexandriaSpecs()
	if len(specs) != 24 {
		t.Fatalf("%d specs, want the 24 quests QNO_SD_HW_001 to 024", len(specs))
	}
	known := map[string]QuestSpec{}
	for _, spec := range specs {
		if _, dup := known[spec.Codename]; dup {
			t.Fatalf("%s is authored twice", spec.Codename)
		}
		known[spec.Codename] = spec
	}
	for number := 1; number <= 24; number++ {
		if _, ok := known[hwanQuestCodename(number)]; !ok {
			t.Errorf("%s is missing", hwanQuestCodename(number))
		}
	}
	external := map[string]bool{hwanChineseLastOfChain: true, hwanEuropeanLastOfChain: true}
	hwanLevels := map[string]uint8{}
	for _, spec := range specs {
		for _, need := range append(append([]string(nil), spec.RequiredQuests...), spec.RequiredAnyQuests...) {
			if _, ok := known[need]; !ok && !external[need] {
				t.Errorf("%s requires %s, which is neither authored here nor the end of the 100 chain", spec.Codename, need)
			}
		}
		if spec.MinLevel != 105 && spec.MinLevel != 110 {
			t.Errorf("%s level %d", spec.Codename, spec.MinLevel)
		}
		if spec.MaxCompletions != 1 || spec.RewardExp <= 0 || spec.RewardGold <= 0 || spec.RewardSkillExp <= 0 {
			t.Errorf("%s is not a once-only paying quest: %+v", spec.Codename, spec)
		}
		if spec.RewardHwanLevel > 0 {
			hwanLevels[spec.Codename] = spec.RewardHwanLevel
		}
	}
	want := map[string]uint8{"QNO_SD_HW_018": 3, "QNO_SD_HW_022": 3, "QNO_SD_HW_020": 4, "QNO_SD_HW_024": 4}
	if len(hwanLevels) != len(want) {
		t.Fatalf("Blue Berserk level rewards %v, want %v", hwanLevels, want)
	}
	for code, level := range want {
		if hwanLevels[code] != level {
			t.Errorf("%s grants level %d, want %d", code, hwanLevels[code], level)
		}
	}
	// The chain is entered from the 100 chain only, one race at a time.
	if !slices.Equal(known["QNO_SD_HW_017"].RequiredQuests, []string{"QNO_CH_HWAN_2_6"}) ||
		!slices.Equal(known["QNO_SD_HW_021"].RequiredQuests, []string{"QNO_EU_HWAN_2_6"}) {
		t.Error("the 105 chain does not start from the end of the 100 chain")
	}
	// 110 starts only after the 105 inscription.
	if !slices.Equal(known["QNO_SD_HW_019"].RequiredQuests, []string{"QNO_SD_HW_018"}) ||
		!slices.Equal(known["QNO_SD_HW_023"].RequiredQuests, []string{"QNO_SD_HW_022"}) {
		t.Error("the 110 chain does not start from the 105 inscription")
	}
}

func TestHwanOstraconDropsAreHundredPercentFromTheTombOfTheirLevel(t *testing.T) {
	for _, spec := range hwanAlexandriaSpecs() {
		if spec.Objective != ObjectiveCollect {
			continue
		}
		suffix := "_2"
		if spec.MinLevel == 110 {
			suffix = "_3"
		}
		rule := spec.MonsterDrop
		if rule == nil || rule.ChancePercent != 100 || !rule.FastDrop {
			t.Fatalf("%s: drop rule %+v", spec.Codename, rule)
		}
		for _, monster := range rule.MonsterCodenames {
			if monster[len(monster)-2:] != suffix {
				t.Errorf("%s drops from %s, not a %s monster", spec.Codename, monster, suffix)
			}
		}
	}
}

// shippedHwanRuntime loads the shipped catalog; it skips without the cap-110 quest tables.
func shippedHwanRuntime(t *testing.T) (*Runtime, *Definitions) {
	t.Helper()
	defs, items := loadShippedDefinitions(t)
	if _, ok := defs.ByCodename("QNO_SD_HW_018"); !ok {
		t.Skip("the built data has no cap-110 quest tables (SRO_CAP110_QUESTS)")
	}
	rt, err := NewRuntime(&enterworld.Deps{}, defs, func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
		return nil, true
	})
	if err != nil {
		t.Fatal(err)
	}
	rt.PlanInventory = action.NewRuntime(&enterworld.Deps{Items: items}, nil).PlanQuestInventory
	return rt, defs
}

func hwanCharacter(model string, level int64) *enterworld.Character {
	return &enterworld.Character{ID: 5, Name: "blue", ModelCodename: model, Level: &level}
}

func TestInscriptionDeliveryRaisesTheBlueBerserkLevel(t *testing.T) {
	rt, defs := shippedHwanRuntime(t)
	announced := 0
	rt.HwanLevelFrames = func(*enterworld.Character) []wire.Frame {
		announced++
		return []wire.Frame{{Opcode: 0x3FC0}}
	}
	last, _ := defs.ByCodename("QNO_SD_HW_008")
	c := hwanCharacter("CHAR_CH_MAN_ADVENTURER", 105)
	c.CompletedQuestIds = []uint32{last.RefID}
	if _, err := rt.StartQuest(c, "QNO_SD_HW_022"); err == nil {
		t.Fatal("a Chinese character took the European inscription quest")
	}
	low := hwanCharacter("CHAR_CH_MAN_ADVENTURER", 104)
	low.CompletedQuestIds = []uint32{last.RefID}
	if _, err := rt.StartQuest(low, "QNO_SD_HW_018"); err == nil {
		t.Fatal("level 104 took a level 105 quest")
	}
	if _, err := rt.StartQuest(c, "QNO_SD_HW_018"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, "QNO_SD_HW_018", hwanDoctor); err == nil {
		t.Fatal("the inscription was delivered to the wrong NPC")
	}
	result, err := rt.AdvanceNpcQuest(c, "QNO_SD_HW_018", hwanGeneralChinese)
	if err != nil {
		t.Fatal(err)
	}
	if rt.HwanLevel(c) != 3 || announced != 1 || result.Frames[len(result.Frames)-1].Opcode != 0x3FC0 {
		t.Fatalf("level %d announced %d frames %v", rt.HwanLevel(c), announced, result.Frames)
	}
}

func TestOstraconQuestNeedsItsPredecessorAndTheDropKnobOnlyScalesTheStack(t *testing.T) {
	rt, defs := shippedHwanRuntime(t)
	first, _ := defs.ByCodename("QNO_SD_HW_001")
	c := hwanCharacter("CHAR_EU_MAN_NOBLE", 105)
	if _, err := rt.StartQuest(c, "QNO_SD_HW_002"); err == nil {
		t.Fatal("002 was taken before 001")
	}
	c.CompletedQuestIds = []uint32{first.RefID}
	if _, err := rt.StartQuest(c, "QNO_SD_HW_002"); err != nil {
		t.Fatal(err)
	}
	root, _ := defs.ByCodename("QNO_SD_HW_002")
	always := func() (uint32, error) { return 1, nil } // a zero sample never drops
	count := func(fast uint16) uint32 {
		drops := missionMonsterDrops(c, root, "MOB_SD_DEMONBUG_2", 0, fast, always)
		if len(drops) != 1 || drops[0].Codename != "ITEM_QNO_SD_HW_002_1" {
			t.Fatalf("drops %v", drops)
		}
		return drops[0].Count
	}
	if got := count(1); got != 1 {
		t.Errorf("knob off drops %d, want 1", got)
	}
	if got := count(FastDropCount); got != FastDropCount {
		t.Errorf("knob on drops %d, want %d", got, FastDropCount)
	}
	if drops := missionMonsterDrops(c, root, "MOB_SD_DEMONBUG_3", 0, 1, always); len(drops) != 0 {
		t.Errorf("a level 110 monster dropped a level 105 ostracon: %v", drops)
	}
	// With 95 held, the knob drops only the 5 still needed.
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 20, RefObjID: root.CollectItemRefID,
		Codename: "ITEM_QNO_SD_HW_002_1", StackCount: 95})
	if got := count(FastDropCount); got != 5 {
		t.Errorf("knob on with 95 held drops %d, want the remaining 5", got)
	}
}

func TestGMLevelOverridesTheQuestsLevelUntilRestart(t *testing.T) {
	rt, defs := shippedHwanRuntime(t)
	last, _ := defs.ByCodename("QNO_CH_HWAN_2_6")
	c := hwanCharacter("CHAR_CH_MAN_ADVENTURER", 100)
	c.CompletedQuestIds = []uint32{last.RefID}
	if rt.HwanLevel(c) != 2 {
		t.Fatalf("quest level %d, want 2", rt.HwanLevel(c))
	}
	if !rt.SetGMHwanLevel(c, 6) || rt.HwanLevel(c) != 6 {
		t.Fatalf("GM level %d, want 6", rt.HwanLevel(c))
	}
	if !rt.SetGMHwanLevel(c, 0) || rt.HwanLevel(c) != 0 {
		t.Fatal("a GM level 0 did not clear the quests' level")
	}
	if rt.SetGMHwanLevel(c, 7) || rt.HwanLevel(c) != 0 {
		t.Fatal("level 7 was accepted")
	}
	other := hwanCharacter("CHAR_CH_MAN_ADVENTURER", 100)
	other.ID = 99
	other.CompletedQuestIds = []uint32{last.RefID}
	if rt.HwanLevel(other) != 2 {
		t.Fatal("one character's override leaked to another")
	}
}
