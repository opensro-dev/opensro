package quest

import (
	"encoding/hex"
	"encoding/json"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// The same accepted production frames drive client-next's closed-journal probe.
func TestGraespKillProducerMatchesClientFeedbackFixture(t *testing.T) {
	licensed.RequireGameData(t)
	data, err := os.ReadFile("graesp_wire_fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Frames []struct {
			Count      uint32
			PayloadHex string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	textdata := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(textdata)
	defs, err := LoadDefinitions(NewCatalog(textdata), items)
	if err != nil {
		t.Fatal(err)
	}
	deps := &enterworld.Deps{Items: items}
	rt, err := NewRuntime(deps, defs, func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) { return nil, true })
	if err != nil {
		t.Fatal(err)
	}
	rt.PlanInventory = action.NewRuntime(deps, nil).PlanQuestInventory
	level, gold := int64(3), int64(0)
	c := &enterworld.Character{ID: 3, Name: "questfixture", ModelCodename: "CHAR_EU_MAN_NOBLE", Level: &level, Gold: &gold}
	inserted, err := rt.StartQuest(c, "QNO_EU_CONS_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Frames) != 21 {
		t.Fatal("incomplete kill sequence")
	}
	for i, row := range fixture.Frames {
		frames := inserted.Frames
		if i > 0 {
			var changed bool
			frames, changed = rt.KillUpdater()(c, "MOB_EU_EDENP_CLON", 0)
			if !changed {
				t.Fatalf("kill %d lost", i)
			}
		}
		if len(frames) != 1 || frames[0].Opcode != OpQuestUpdate || hex.EncodeToString(frames[0].Payload) != row.PayloadHex {
			t.Fatalf("count %d wire mismatch: %+v", i, frames)
		}
		if c.ActiveQuests[0].Contents[0].ObjectiveValues[0] != row.Count {
			t.Fatal("persisted count differs")
		}
		if _, changed := rt.KillUpdater()(c, "MOB_UNRELATED", 0); changed {
			t.Fatal("unrelated kill credited")
		}
	}
	if _, changed := rt.KillUpdater()(c, "MOB_EU_EDENP_CLON", 0); changed {
		t.Fatal("completed counter overflow")
	}
	if node := c.ActiveQuests[0].Contents[0]; node.Kind != 0 || !node.CompletionReached {
		t.Fatal("transient completion polluted resident state")
	}
	if p := EncodeQuestUpdateInsert(c.ActiveQuests[0]); p[11] != 0 {
		t.Fatal("login replays first completion")
	}
	if _, err := rt.CompleteNpcQuest(c, "QNO_EU_CONS_1"); err != nil {
		t.Fatal(err)
	}
	if len(c.ActiveQuests) != 0 {
		t.Fatal("completed quest retained")
	}
	if _, err := rt.CompleteNpcQuest(c, "QNO_EU_CONS_1"); err == nil {
		t.Fatal("duplicate reward")
	}
}

func TestCollectionCompletionLatchSurvivesReloadLossAndReacquisition(t *testing.T) {
	licensed.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	if _, err := rt.StartQuest(c, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}
	def, _ := rt.Defs.ByCodename("QSP_ALL_POTION_1")
	update := func(count int64, want byte) {
		t.Helper()
		c.MissionInventory = potionInventory(count)
		frames, changed := rt.InventoryUpdater()(c)
		if !changed || len(frames) != 1 || frames[0].Payload[11] != want {
			t.Fatalf("count %d expected kind %d: %+v", count, want, frames)
		}
	}
	update(int64(def.CollectCount), 2)
	if !c.ActiveQuests[0].Contents[0].CompletionReached {
		t.Fatal("completion latch was not committed")
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	restored := new(enterworld.Character)
	if err := json.Unmarshal(b, restored); err != nil {
		t.Fatal(err)
	}
	c = restored
	update(0, 1)
	if !c.ActiveQuests[0].Contents[0].CompletionReached {
		t.Fatal("item loss cleared completion latch")
	}
	update(int64(def.CollectCount), 0)
	if _, changed := rt.InventoryUpdater()(c); changed {
		t.Fatal("unchanged inventory emitted feedback")
	}
	// A legacy complete record has the same latch even without the new field.
	c.ActiveQuests[0].Contents[0].CompletionReached = false
	update(0, 1)
	update(int64(def.CollectCount), 0)
	// Fresh acceptance starts a fresh objective; no latch leaks across abandon.
	c.ActiveQuests = nil
	c.MissionInventory = nil
	if _, err := rt.StartQuest(c, "QSP_ALL_POTION_1"); err != nil {
		t.Fatal(err)
	}
	update(int64(def.CollectCount), 2)
}
