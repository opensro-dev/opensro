package quest

import (
	"bytes"
	"encoding/json"
	"opensro.online/server/internal/testsupport/clientdata"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

func TestIdenticalMissionCaptionsKeepNativeTagsAcrossReload(t *testing.T) {
	d := &Definition{QuestSpec: QuestSpec{Objective: ObjectiveParallel, Objectives: []MissionSpec{
		{ContentsSymbol: "SAME", Objective: ObjectiveKill, KillMonsterCodenames: []string{"A"}, KillCount: 1},
		{ContentsSymbol: "SAME", Objective: ObjectiveKill, KillMonsterCodenames: []string{"B"}, KillCount: 2},
	}}, RefID: 900}
	if err := loadMissions(d, []string{"SAME"}, nil); err != nil {
		t.Fatal(err)
	}
	c := questCharacter()
	previous := BuildActiveQuestRecord(d, 0)
	if previous.Contents[0].Tag != 1 || previous.Contents[1].Tag != 2 {
		t.Fatal("mission index omitted from native tag")
	}
	next, changed := refreshMissions(c, d, previous, "B", 0)
	if !changed {
		t.Fatal("second counter not updated")
	}
	b, _ := json.Marshal(next)
	var restored enterworld.ActiveQuestRecord
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	if recordProgress(missionRecord(restored, missionDefinition(d, 0))) != 0 || recordProgress(missionRecord(restored, missionDefinition(d, 1))) != 1 {
		t.Fatal("equal captions cross-credited after reload")
	}
	finished, _ := refreshMissions(c, d, restored, "A", 0)
	if finished.Contents[0].Kind != 0 || finished.Contents[1].Kind != 1 {
		t.Fatal("completion latched the wrong mission")
	}
	edge := finished
	edge.Contents = append([]enterworld.ActiveQuestContentsNode(nil), finished.Contents...)
	edge.Contents[0].Kind = 2
	if !bytes.Equal(encodeMissionProgress(restored, finished), EncodeQuestUpdateUpdate(edge)) {
		t.Fatal("completion publication matched by caption")
	}
	legacy := BuildActiveQuestRecord(d, 0)
	legacy.Contents[0].Description = "FIRST"
	legacy.Contents[1].Description = "SECOND"
	legacy.Contents[1].Tag = 1
	legacy.Contents[1].ObjectiveValues[0] = 7
	if i := missionNodeIndex(legacy.Contents, 2, "SECOND"); i != 1 {
		t.Fatal("unique legacy caption not migrated")
	}
	d.Objectives[0].ContentsSymbol = "FIRST"
	d.Objectives[1].ContentsSymbol = "SECOND"
	rt := &Runtime{Defs: &Definitions{byRefID: map[uint32]*Definition{900: d}}}
	c.ActiveQuests = []enterworld.ActiveQuestRecord{legacy}
	if err := rt.NormalizeEntryRecords(c); err != nil {
		t.Fatal(err)
	}
	if c.ActiveQuests[0].Contents[1].Tag != 2 || c.ActiveQuests[0].Contents[1].ObjectiveValues[0] != 7 {
		t.Fatal("entry migration lost the second mission")
	}
	c.ActiveQuests[0].Contents[1].Description = "FIRST"
	if err := rt.NormalizeEntryRecords(c); err == nil {
		t.Fatal("ambiguous saved state silently discarded")
	}
}

func TestMixedMissionsPreserveIndependentCountersAndEnvelope(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	root, _ := rt.Defs.ByCodename("QNO_CH_CHEF_1")
	d := *root
	d.Objective = ObjectiveParallel
	d.CollectItemCodename = ""
	d.CollectCount = 0
	d.MonsterDrop = nil
	d.Objectives = []MissionSpec{
		{ContentsSymbol: root.ContentsSymbol, Objective: ObjectiveCollect, CollectItemCodename: root.CollectItemCodename, CollectCount: 1, collectRef: root.CollectItemRefID},
		{ContentsSymbol: "HUNT", Objective: ObjectiveKill, KillMonsterCodenames: []string{"MOB_TEST"}, KillCount: 2},
	}
	rt.Defs.byRefID[d.RefID] = &d
	rt.Defs.byCodename[d.Codename] = &d
	if _, err := rt.StartQuest(c, d.Codename); err != nil {
		t.Fatal(err)
	}
	r := &c.ActiveQuests[0]
	r.U08 = 0x32
	r.TargetIds = []uint32{17}
	r.Flags |= 0x40
	if _, changed := rt.KillUpdater()(c, "MOB_TEST", 0); !changed {
		t.Fatal("kill not counted")
	}
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: root.CollectItemRefID, Codename: root.CollectItemCodename, StackCount: 1}}
	if _, changed := rt.InventoryUpdater()(c); !changed {
		t.Fatal("pickup not counted")
	}
	// Persist/reload, reorder definition, then lose and regain collection.
	b, _ := json.Marshal(c.ActiveQuests)
	c.ActiveQuests = nil
	if err := json.Unmarshal(b, &c.ActiveQuests); err != nil {
		t.Fatal(err)
	}
	d.Objectives[0], d.Objectives[1] = d.Objectives[1], d.Objectives[0]
	c.MissionInventory = nil
	rt.InventoryUpdater()(c)
	if recordProgress(missionRecord(c.ActiveQuests[0], missionDefinition(&d, 0))) != 1 {
		t.Fatal("inventory update/reordering erased kill")
	}
	if _, err := rt.CompleteNpcQuest(c, d.Codename); err == nil {
		t.Fatal("partial parallel quest completed")
	}
	rt.KillUpdater()(c, "MOB_TEST", 0)
	before, _ := json.Marshal(c)
	if _, err := rt.CompleteNpcQuest(c, d.Codename); err == nil {
		t.Fatal("missing item completed")
	}
	after, _ := json.Marshal(c)
	if string(before) != string(after) {
		t.Fatal("refusal mutated state")
	}
	r = &c.ActiveQuests[0]
	if r.U08 != 0x32 || r.Flags&0x40 == 0 || !reflect.DeepEqual(r.TargetIds, []uint32{17}) {
		t.Fatal("objective update erased envelope")
	}
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: root.CollectItemRefID, Codename: root.CollectItemCodename, StackCount: 1}}
	rt.InventoryUpdater()(c)
	if _, err := rt.CompleteNpcQuest(c, d.Codename); err != nil {
		t.Fatal(err)
	}
	if len(c.MissionInventory) != 0 || len(c.ActiveQuests) != 0 || *c.Gold != 475 {
		t.Fatal("parallel turn-in was not atomic")
	}
	if _, err := rt.CompleteNpcQuest(c, d.Codename); err == nil {
		t.Fatal("paid twice")
	}
}

func TestRepeatOfferUsesPersistedCompletionAndStopsAtLimit(t *testing.T) {
	clientdata.RequireGameData(t)
	rt := testRuntime(t)
	c := questCharacter()
	d, _ := rt.Defs.ByCodename("QNO_CH_CHEF_1")
	d.MaxCompletions = 3
	d.RepeatOfferPromptSymbol = "REPEAT_OFFER"
	for run := uint32(0); run < 3; run++ {
		found := false
		for _, o := range rt.OptionsForNpc(c, d.StartNpcCodename) {
			if o.Codename != d.Codename {
				continue
			}
			want := d.OfferPromptSymbol
			if run > 0 {
				want = d.RepeatOfferPromptSymbol
			}
			if o.PromptSymbol != want {
				t.Fatalf("run %d wrong offer %s", run, o.PromptSymbol)
			}
			found = true
		}
		if !found {
			t.Fatal("missing offer")
		}
		if _, err := rt.StartQuest(c, d.Codename); err != nil {
			t.Fatal(err)
		}
		c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: d.CollectItemRefID, Codename: d.CollectItemCodename, StackCount: 1}}
		if _, err := rt.CompleteNpcQuest(c, d.Codename); err != nil {
			t.Fatal(err)
		}
		bytes, _ := json.Marshal(c)
		if err := json.Unmarshal(bytes, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range rt.OptionsForNpc(c, d.StartNpcCodename) {
		if o.Codename == d.Codename {
			t.Fatal("offer beyond repeat limit")
		}
	}
	if _, err := rt.StartQuest(c, d.Codename); err == nil {
		t.Fatal("accepted beyond repeat limit")
	}
}
