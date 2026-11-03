package quest

import (
	"encoding/json"
	"opensro.online/server/internal/testsupport/clientdata"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

func TestFiniteRepeatLimitSurvivesRestartAndLegacyCompletion(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	// Isolated definition: production specifications are immutable.
	root, _ := rt.Defs.ByCodename("QNO_CH_SOLDIER_EA1_1")
	copyDef := *root
	copyDef.MaxCompletions, copyDef.KillCount = 3, 1
	rt.Defs.byCodename[root.Codename], rt.Defs.byRefID[root.RefID] = &copyDef, &copyDef
	for i, def := range rt.Defs.ordered {
		if def.RefID == root.RefID {
			rt.Defs.ordered[i] = &copyDef
		}
	}
	c := questCharacter()
	// An old save with the unique completed ID represents at least one paid run.
	c.CompletedQuestIds = []uint32{root.RefID}
	for completed := uint32(1); completed < 3; completed++ {
		if _, err := rt.StartQuest(c, root.Codename); err != nil {
			t.Fatal(err)
		}
		if c.ActiveQuests[0].U08 != uint8(0x30|(completed+1)) {
			t.Fatal("native repeat title lost current/limit")
		}
		rt.KillUpdater()(c, copyDef.KillMonsterCodenames[0], 0)
		if c.ActiveQuests[0].U08 != uint8(0x30|(completed+1)) {
			t.Fatal("kill reset repeat title")
		}
		if _, err := rt.AdvanceNpcQuest(c, root.Codename, root.EndNpcCodename); err != nil {
			t.Fatal(err)
		}
		if completionCount(c, root.RefID) != completed+1 {
			t.Fatal("lost repeat count")
		}
		if len(c.CompletedQuestIds) != 1 {
			t.Fatal("duplicate completed ID would waste native wire capacity")
		}
		b, _ := json.Marshal(c)
		c = new(enterworld.Character)
		if err := json.Unmarshal(b, c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := rt.StartQuest(c, root.Codename); err == nil {
		t.Fatal("fourth reward available after restart")
	}
	for _, option := range rt.OptionsForNpc(c, root.StartNpcCodename) {
		if option.Codename == root.Codename {
			t.Fatal("exhausted repeat offered")
		}
	}
}
