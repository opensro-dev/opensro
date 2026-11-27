package quest

import (
	"errors"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/clientdata"
	"testing"
)

func TestKillQuestCountsOnlyTargetsAndPaysNativeV150RewardOnce(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	character := questCharacter()
	level := int64(3)
	character.Level = &level
	const questCode = "QNO_CH_SOLDIER_EA1_1"
	level = 1
	if _, err := rt.StartQuest(character, questCode); err == nil {
		t.Fatal("under-level character accepted quest")
	}
	level = 3
	if _, err := rt.StartQuest(character, questCode); err != nil {
		t.Fatal(err)
	}
	update := rt.KillUpdater()
	if frames, changed := update(character, "MOB_CH_STONEGHOST", 0); changed || len(frames) != 0 {
		t.Fatal("unrelated monster advanced quest")
	}
	if _, err := rt.CompleteNpcQuest(character, questCode); err == nil {
		t.Fatal("incomplete quest paid reward")
	}
	for i := 0; i < 40; i++ {
		frames, changed := update(character, []string{"MOB_CH_GYO", "MOB_CH_GYO_CLON"}[i%2], 0)
		// The fortieth kill completes the objective and also carries the
		// script's ACHIEVED_NOW report banner, once.
		want := 1
		if i == 39 {
			want = 2
		}
		if !changed || len(frames) != want || frames[0].Opcode != OpQuestUpdate {
			t.Fatalf("kill %d: %v/%v", i, frames, changed)
		}
		if want == 2 && (frames[1].Opcode != questNotificationOpcode || string(frames[1].Payload[2:]) != "SN_TALK_QNO_CH_SOLDIER_EA1_1_06") {
			t.Fatalf("completion banner: %v", frames[1])
		}
	}
	if frames, changed := update(character, "MOB_CH_GYO", 0); changed || len(frames) != 0 {
		t.Fatal("finished counter overflowed")
	}
	options := rt.OptionsForNpc(character, "NPC_CH_SOLDIER_EA1")
	if len(options) != 1 || !options[0].Complete {
		t.Fatalf("turn-in unavailable: %+v", options)
	}
	result, err := rt.CompleteNpcQuest(character, questCode)
	if err != nil {
		t.Fatal(err)
	}
	if character.Gold == nil || *character.Gold != 1000 || len(character.ActiveQuests) != 0 {
		t.Fatal("reward and quest completion did not commit")
	}
	if len(result.Frames) != 3 || result.Frames[2].Opcode != wire.OpExpUpdate {
		t.Fatalf("missing reward burst: %v", result.Frames)
	}
	if _, err := rt.CompleteNpcQuest(character, questCode); err == nil {
		t.Fatal("duplicate completion paid twice")
	}
	if _, err := rt.StartQuest(character, questCode); err == nil {
		t.Fatal("non-repeatable quest accepted again")
	}
}

func TestRankedParallelKillCountersDoNotCrossCredit(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	root := &Definition{QuestSpec: QuestSpec{Codename: "RANKED", Objective: ObjectiveParallel, KindByte: 1, Objectives: []MissionSpec{
		{ContentsSymbol: "normal", Objective: ObjectiveKill, KillMonsterCodenames: []string{"CRAB"}, KillRanks: []uint8{0}, KillCount: 30},
		{ContentsSymbol: "champion", Objective: ObjectiveKill, KillMonsterCodenames: []string{"CRAB"}, KillRanks: []uint8{1}, KillCount: 10},
	}}, RefID: 900, CountryByte: 3}
	rt.Defs = &Definitions{byRefID: map[uint32]*Definition{900: root}, byCodename: map[string]*Definition{"RANKED": root}, ordered: []*Definition{root}}
	for rarity := 0; rarity < 256; rarity++ {
		c := questCharacter()
		c.ActiveQuests = []enterworld.ActiveQuestRecord{BuildActiveQuestRecord(root, 0)}
		rt.KillUpdater()(c, "CRAB", uint8(rarity))
		for i := 0; i < 2; i++ {
			got := recordProgress(missionRecord(c.ActiveQuests[0], missionDefinition(root, i)))
			want := uint32(0)
			if rarity&15 == i {
				want = 1
			}
			if got != want {
				t.Fatalf("rarity %x mission %d: %d != %d", rarity, i, got, want)
			}
		}
	}
	d := &Definition{QuestSpec: QuestSpec{Objective: ObjectiveKill, KillMonsterCodenames: []string{"A", "B"}, KillRanks: []uint8{0, 1}}}
	if killTargetMatches(d, "B", 0) || killTargetMatches(d, "A", 1) || !killTargetMatches(d, "B", 0x11) {
		t.Fatal("rank detached from matching species")
	}
	d.KillRanks = []uint8{0}
	if validateKillRanks(d.QuestSpec) == nil {
		t.Fatal("rank arity accepted")
	}
	d.KillRanks = []uint8{0, 16}
	if validateKillRanks(d.QuestSpec) == nil {
		t.Fatal("rank outside nibble accepted")
	}
}

/*
================
TestKillTakersAndSharedQuestCredit

The party share's census and credit: a character's quests that take a
kill (an unfilled kill mission, or a gather mission dropping from it),
and a credit restricted to the named quests advancing only those.
================
*/
func TestKillTakersAndSharedQuestCredit(t *testing.T) {
	killDef := &Definition{QuestSpec: QuestSpec{Codename: "QA", Objective: ObjectiveKill, KindByte: 1,
		KillMonsterCodenames: []string{"MOB_TEST"}, KillCount: 3}, RefID: 901, CountryByte: 3}
	gatherDef := &Definition{QuestSpec: QuestSpec{Codename: "QB", Objective: ObjectiveCollect, KindByte: 2,
		CollectItemCodename: "ITEM_TEST", CollectCount: 5, DivideType: 5,
		MonsterDrop: &MonsterDropRule{ItemCodename: "ITEM_TEST", MonsterCodenames: []string{"MOB_TEST"}, ChancePercent: 100}}, RefID: 902, CountryByte: 3}
	defs := &Definitions{byRefID: map[uint32]*Definition{901: killDef, 902: gatherDef},
		byCodename: map[string]*Definition{"QA": killDef, "QB": gatherDef}, ordered: []*Definition{killDef, gatherDef}}
	// Hand-built definitions: the census reads no catalog, so the test
	// runs where no game data is built (the shared fixture loader's do).
	rt, err := NewRuntime(&enterworld.Deps{}, defs, nil)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	c := questCharacter()
	c.ActiveQuests = []enterworld.ActiveQuestRecord{BuildActiveQuestRecord(killDef, 0), BuildActiveQuestRecord(gatherDef, 0)}
	takers := rt.KillTakers(c, "MOB_TEST", 0)
	if len(takers) != 2 || takers[0] != (KillTaker{RefID: 901}) || takers[1] != (KillTaker{RefID: 902, DivideType: 5, Drop: true}) {
		t.Fatalf("takers %+v", takers)
	}
	if got := rt.KillTakers(c, "MOB_OTHER", 0); len(got) != 0 {
		t.Fatalf("another monster's takers %+v", got)
	}
	progress := func() uint32 { return recordProgress(missionRecord(c.ActiveQuests[0], missionDefinition(killDef, 0))) }
	rt.CreditKillQuests(c, "MOB_TEST", 0, map[uint32]bool{902: true})
	if progress() != 0 {
		t.Fatalf("a gather-only credit advanced the kill quest: %d", progress())
	}
	rt.CreditKillQuests(c, "MOB_TEST", 0, map[uint32]bool{901: true})
	if progress() != 1 {
		t.Fatalf("the shared credit counted %d", progress())
	}
	rt.CreditKillQuests(c, "MOB_TEST", 0, nil)
	rt.CreditKillQuests(c, "MOB_TEST", 0, map[uint32]bool{901: true})
	if progress() != 3 {
		t.Fatalf("the counter overran its goal: %d", progress())
	}
	if got := rt.KillTakers(c, "MOB_TEST", 0); len(got) != 1 || got[0].RefID != 902 {
		t.Fatalf("a full quest still takes kills: %+v", got)
	}
}

/*
================
TestSharedKillRunsTheMemberExchange

A shared kill that counts none of a member's missions (an empty, non-nil
credit set) still runs his kill exchange, as the solo event does: the
server's handler tests the exchange after its mission loop, whichever
quests the share credited.
================
*/
func TestSharedKillRunsTheMemberExchange(t *testing.T) {
	def := &Definition{QuestSpec: QuestSpec{Codename: "QX", Objective: ObjectiveKill, KindByte: 1,
		KillMonsterCodenames: []string{"MOB_TEST"}, KillCount: 1,
		KillExchange: &KillExchange{TakeItem: "ITEM_TAKE", GrantItem: "ITEM_GRANT"}}, RefID: 903, CountryByte: 3}
	defs := &Definitions{byRefID: map[uint32]*Definition{903: def},
		byCodename: map[string]*Definition{"QX": def}, ordered: []*Definition{def}}
	rt, err := NewRuntime(&enterworld.Deps{}, defs, nil)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	c := questCharacter()
	c.ActiveQuests = []enterworld.ActiveQuestRecord{BuildActiveQuestRecord(def, 0)}
	// Fill the one kill mission while no inventory planner is wired.
	rt.CreditKillQuests(c, "MOB_TEST", 0, nil)
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 20, Codename: "ITEM_TAKE", StackCount: 2}}
	planned := 0
	rt.PlanInventory = func(*enterworld.Character, []inventory.ItemAmount, []inventory.ItemAmount) ([]enterworld.InventoryRow, []wire.Frame, error) {
		planned++
		return nil, nil, errors.New("test: plan only")
	}
	rt.CreditKillQuests(c, "MOB_TEST", 0, map[uint32]bool{})
	if planned != 1 {
		t.Fatalf("the shared kill planned the exchange %d times, want 1", planned)
	}
}
