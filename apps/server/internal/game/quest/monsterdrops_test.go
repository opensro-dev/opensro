package quest

import (
	"encoding/json"
	"errors"
	"math"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

func TestQuestMonsterDropsRequireAcceptedQuestTargetAndPickup(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	calls := 0
	roll := func() (uint32, error) {
		calls++
		if calls%2 == 1 {
			return 19000, nil
		}
		return 0, nil
	}
	if got := rt.MonsterDrops(c, "MOB_CH_BIGEYEGHOST", roll); len(got) != 0 || calls != 0 {
		t.Fatal("unaccepted quest rolled loot")
	}
	if _, err := rt.StartQuest(c, "QNO_CH_CHEF_1"); err != nil {
		t.Fatal(err)
	}
	before := c.ActiveQuests[0].Contents[0].ObjectiveValues[0]
	if got := rt.MonsterDrops(c, "MOB_CH_GYO", roll); len(got) != 0 || calls != 0 {
		t.Fatal("unrelated monster rolled loot")
	}
	for _, code := range []string{"MOB_CH_BIGEYEGHOST", "MOB_CH_BIGEYEGHOST_CLON"} {
		got := rt.MonsterDrops(c, code, roll)
		if len(got) != 1 || got[0].Codename != "ITEM_QNO_CH_CHEF_1" || got[0].Count != 1 {
			t.Fatalf("drop: %+v", got)
		}
	}
	if c.ActiveQuests[0].Contents[0].ObjectiveValues[0] != before || len(c.MissionInventory) != 0 {
		t.Fatal("planning loot advanced quest before pickup")
	}
	if _, err := rt.CompleteNpcQuest(c, "QNO_CH_CHEF_1"); err == nil {
		t.Fatal("uncollected shoe completed quest")
	}
	def, _ := rt.Defs.ByCodename("QNO_CH_CHEF_1")
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: def.CollectItemRefID, Codename: def.CollectItemCodename, StackCount: 1}}
	// The held shoe completes the only mission: the update and its
	// ACHIEVED_NOW banner.
	if frames := rt.NotifyInventoryChanged(c); len(frames) != 2 || frames[0].Opcode != OpQuestUpdate ||
		frames[1].Opcode != questNotificationOpcode || string(frames[1].Payload[2:]) != "SN_TALK_QNO_CH_CHEF_1_06" {
		t.Fatalf("pickup update: %v", frames)
	}
	calls = 0
	if got := rt.MonsterDrops(c, "MOB_CH_BIGEYEGHOST", roll); len(got) != 0 || calls != 0 {
		t.Fatal("held objective still rolled loot")
	}
	if _, err := rt.CompleteNpcQuest(c, def.Codename); err != nil {
		t.Fatal(err)
	}
	if len(c.MissionInventory) != 0 || c.Gold == nil || *c.Gold != 475 {
		t.Fatal("turn-in did not consume shoe and pay reward")
	}
}

func TestCursedHeartDropsContinuePastExchangeCountButStopAtNativeCap(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	if _, err := rt.StartQuest(c, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	roll := func() (uint32, error) {
		calls++
		if calls%2 == 1 {
			return 4000, nil
		}
		return 0, nil
	}
	for _, count := range []int64{0, 10, 299} {
		c.MissionInventory = potionInventory(count)
		if got := rt.MonsterDrops(c, "MOB_CH_GYO", roll); len(got) != 1 {
			t.Fatalf("held %d: %+v", count, got)
		}
	}
	c.MissionInventory = potionInventory(300)
	if got := rt.MonsterDrops(c, "MOB_CH_GYO", roll); len(got) != 0 {
		t.Fatal("300-heart cap exceeded")
	}
	c.MissionInventory = nil
	*c.Level = 19
	if got := rt.MonsterDrops(c, "MOB_CH_GYO", roll); len(got) != 0 {
		t.Fatal("under-level heart drop")
	}
	*c.Level = 20
	if got := rt.MonsterDrops(c, "MOB_CH_GYO", func() (uint32, error) { return 5, nil }); len(got) != 0 {
		t.Fatal("failed chance produced loot")
	}
	if got := rt.MonsterDrops(c, "MOB_CH_GYO", func() (uint32, error) { return 0, errors.New("rng unavailable") }); len(got) != 0 {
		t.Fatal("RNG failure produced loot")
	}
}

func TestMonsterDropContractsRejectInvalidDefinitions(t *testing.T) {
	base := QuestSpec{Codename: "TEST", Objective: ObjectiveCollect, CollectCount: 10}
	for _, rule := range []MonsterDropRule{
		{AnyMonster: true}, {AnyMonster: true, ChancePercent: 101},
		{ChancePercent: 10}, {AnyMonster: true, MonsterCodenames: []string{"MOB_A"}, ChancePercent: 10},
		{AnyMonster: true, ChancePercent: 10, MaxHeld: 9},
		{MonsterCodenames: []string{"MOB_A", "MOB_A"}, ChancePercent: 10},
		{MonsterCodenames: []string{""}, ChancePercent: 10},
		{AnyMonster: true, ChancePercent: float32(math.NaN())},
		{MonsterCodenames: []string{"MOB_A"}, SpeciesChancePercent: []float32{1, 2}},
		{MonsterCodenames: []string{"MOB_A"}, ChancePercent: 1, SpeciesChancePercent: []float32{2}},
	} {
		base.MonsterDrop = &rule
		if validateMonsterDrop(base) == nil {
			t.Fatalf("accepted %+v", rule)
		}
	}
	base.MonsterDrop = &MonsterDropRule{AnyMonster: true, ChancePercent: 5, MaxHeld: 300}
	before := *base.MonsterDrop
	if err := validateMonsterDrop(base); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, *base.MonsterDrop) {
		t.Fatal("validation mutated definition")
	}
}

func TestNativeQuestDropFractionalAndFloatBoundary(t *testing.T) {
	for _, tc := range []struct {
		rate   float32
		sample uint32
		want   bool
	}{
		{100, 0, false}, {100, 1, true}, {100, 999999, true},
		{0.5, 4999, true}, {0.5, 5000, true}, {0.5, 5001, false},
		{1.3, 12999, true}, {1.3, 13000, false},
		{25, 250000, true}, {25, 250001, false},
	} {
		if got := nativeQuestDropChance(tc.rate, tc.sample&0x7fff, tc.sample>>15); got != tc.want {
			t.Fatalf("rate=%g sample=%d got=%v", tc.rate, tc.sample, got)
		}
	}
}

func TestQuestDropMatchesExecutedNativeMachineCases(t *testing.T) {
	data, err := os.ReadFile("testdata/native-drop-probability.json")
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Format string
		Cases  []struct {
			Rate          float32
			First, Second uint32
			Accepted      bool
		}
	}
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Format != "sro-native-quest-probability-v1" || len(evidence.Cases) != 51 {
		t.Fatal("missing native oracle cases")
	}
	for _, c := range evidence.Cases {
		if got := nativeQuestDropChance(c.Rate, c.First, c.Second); got != c.Accepted {
			t.Fatalf("native mismatch: %+v port=%v", c, got)
		}
	}
}

func TestQuestDropSpeciesRatesAndSecondRngFailure(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	if _, err := rt.StartQuest(c, "QNO_CH_CHEF_1"); err != nil {
		t.Fatal(err)
	}
	root, _ := rt.Defs.ByCodename("QNO_CH_CHEF_1")
	def := *root
	def.MonsterDrop = &MonsterDropRule{MonsterCodenames: []string{"MOB_A", "MOB_B"}, SpeciesChancePercent: []float32{0.5, 1.3}}
	for _, tc := range []struct {
		monster string
		want    int
	}{{"MOB_A", 0}, {"MOB_B", 1}} {
		calls := 0
		got := missionMonsterDrops(c, &def, tc.monster, func() (uint32, error) {
			calls++
			if calls == 1 {
				return 10000, nil
			}
			return 0, nil
		})
		if len(got) != tc.want || calls != 2 {
			t.Fatalf("%s drops=%v calls=%d", tc.monster, got, calls)
		}
	}
	calls := 0
	got := missionMonsterDrops(c, &def, "MOB_B", func() (uint32, error) {
		calls++
		if calls == 1 {
			return 1, nil
		}
		return 0, errors.New("second RNG failed")
	})
	if len(got) != 0 || calls != 2 {
		t.Fatal("second RNG failure was ignored")
	}
}
