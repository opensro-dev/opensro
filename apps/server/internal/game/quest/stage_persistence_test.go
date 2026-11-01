package quest

import (
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/clientdata"
	"testing"
)

func TestPaidTutorialStageSurvivesAuthorityReopen(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := t.TempDir()
	open := func() *store.Store {
		s, err := store.Open(dir, store.Options{DefaultSkills: rewardTestSkillSeeder})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	authority := open()
	t.Cleanup(func() { authority.Close() })
	defs := loadTestDefinitions(t)
	bind := func() *Runtime {
		deps := &enterworld.Deps{Items: fakeItems{}, Characters: authority.Characters(), UpdateCharacter: authority.UpdateCharacter, MutateCharacter: authority.MutateCharacter}
		rt, err := NewRuntime(deps, defs, func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) { return nil, true })
		if err != nil {
			t.Fatal(err)
		}
		rt.PlanInventory = action.NewRuntime(deps, nil).PlanQuestInventory
		return rt
	}
	c := questCharacter()
	if err := authority.CreateCharacter("global-official", "stage-fixture", c); err != nil {
		t.Fatal(err)
	}
	c = authority.Characters().CharactersForDivision("global-official")[0]
	rt := bind()
	if _, err := rt.StartQuest(c, "QTUTORIAL_CH"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, "QTUTORIAL_CH@0", "NPC_CH_GENARAL"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.AdvanceNpcQuest(c, "QTUTORIAL_CH@1", "NPC_CH_ARMOR"); err != nil {
		t.Fatal(err)
	}
	authority.UpdateCharacter(c, "test-repeat-checkpoint", func() bool { recordCompletion(c, 5); return true })
	authority.Close()
	authority = open()
	rt = bind()
	c = authority.Characters().CharactersForDivision("global-official")[0]
	if len(c.ActiveQuests) != 1 || c.ActiveQuests[0].Stage != 2 || c.QuestCompletionCounts[5] != 1 {
		t.Fatal("quest checkpoint lost on disk")
	}
	if len(c.MissionInventory) != 1 || c.MissionInventory[0].Codename != "ITEM_CH_M_LIGHT_01_AA_A" {
		t.Fatal("stage gift lost on disk")
	}
	if _, err := rt.AdvanceNpcQuest(c, "QTUTORIAL_CH@1", "NPC_CH_ARMOR"); err == nil {
		t.Fatal("paid stage replayed after server restart")
	}
	if _, err := rt.AdvanceNpcQuest(c, "QTUTORIAL_CH@2", "NPC_CH_GENARAL"); err != nil {
		t.Fatal(err)
	}
}
