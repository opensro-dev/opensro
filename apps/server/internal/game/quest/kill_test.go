package quest

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestKillQuestCountsOnlyTargetsAndPaysNativeV150RewardOnce(t *testing.T) {
	licensed.RequireGameData(t)
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
	licensed.RequireGameData(t)
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
