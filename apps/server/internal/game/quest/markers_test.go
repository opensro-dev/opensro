package quest

import (
	"encoding/binary"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestMarkerStatesFollowAuthoritativeQuestLifecycle(t *testing.T) {
	licensed.RequireGameData(t)
	rt, err := NewRuntime(&enterworld.Deps{}, loadTestDefinitions(t), func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) { return nil, true })
	if err != nil {
		t.Fatal(err)
	}
	race, level := int64(enterworld.RaceEurope), int64(1)
	c := &enterworld.Character{Name: "Marker", RaceIndex: &race, Level: &level}
	if m := rt.MarkerStates(c)[143]; m.State != 1 || m.Codename != "NPC_EU_ADVICE" {
		t.Fatal(m)
	}
	if _, err := rt.StartQuest(c, "QNO_EU_TUTORIAL_1"); err != nil {
		t.Fatal(err)
	}
	if m := rt.MarkerStates(c)[143]; m.State != 3 {
		t.Fatal(m)
	}
	if _, err := rt.CompleteTalkQuest(c, "QNO_EU_TUTORIAL_1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := rt.MarkerStates(c)[143]; ok {
		t.Fatal("completed quest remains offered")
	}
	c.DeletePending = true
	if len(rt.MarkerStates(c)) != 0 {
		t.Fatal("deleted character has quest markers")
	}
}
func TestMarkerPublicationReplacementRemovalAndReconnect(t *testing.T) {
	var pub MarkerPublication
	p := EncodeNpcMarker(7, 200001, 1, 257, 10, -20, 30)
	rows := map[uint32][18]byte{7: p}
	f := pub.Update(rows)
	if len(f) != 1 || f[0].Opcode != 0x3498 || len(f[0].Payload) != 18 || binary.LittleEndian.Uint32(f[0].Payload[14:]) != 200001 {
		t.Fatal(f)
	}
	if len(pub.Update(map[uint32][18]byte{7: p})) != 0 {
		t.Fatal("unchanged markers retransmitted")
	}
	p[5] = 3
	if f = pub.Update(map[uint32][18]byte{7: p}); len(f) != 1 || f[0].Payload[5] != 3 {
		t.Fatal(f)
	}
	if f = pub.Update(nil); len(f) != 1 || f[0].Opcode != 0x30ea {
		t.Fatal(f)
	}
	if len(pub.Update(nil)) != 0 {
		t.Fatal("repeated removal")
	}
	pub = MarkerPublication{}
	if len(pub.Update(rows)) != 1 {
		t.Fatal("reconnect lost initial publication")
	}
}

func TestUnfinishedQuestConversationIsInformational(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	defs, err := LoadDefinitions(NewCatalog(dir), enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(&enterworld.Deps{}, defs, func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) { return nil, true })
	if err != nil {
		t.Fatal(err)
	}
	def, ok := rt.Defs.ByCodename("QNO_EU_CONS_1")
	if !ok {
		t.Fatal("missing generated quest")
	}
	race, level := int64(enterworld.RaceEurope), int64(100)
	c := &enterworld.Character{RaceIndex: &race, Level: &level, ActiveQuests: []enterworld.ActiveQuestRecord{BuildActiveQuestRecord(def, 0)}}
	find := func() NpcOption {
		for _, option := range rt.OptionsForNpc(c, def.EndNpcCodename) {
			if option.Codename == def.Codename {
				return option
			}
		}
		t.Fatal("missing conversation")
		return NpcOption{}
	}
	if row := find(); !row.Informational || row.Complete || row.PromptSymbol != "SN_TALK_QNO_EU_CONS_1_04" {
		t.Fatal(row)
	}
	c.ActiveQuests[0] = BuildActiveQuestRecord(def, objectiveRequired(def))
	if row := find(); row.Informational || !row.Complete || row.PromptSymbol != def.CompletePromptSymbol {
		t.Fatal(row)
	}
}

func TestMarkerGoingStateHonorsEquippedStagePrerequisite(t *testing.T) {
	licensed.RequireGameData(t)
	rt, err := NewRuntime(&enterworld.Deps{}, loadTestDefinitions(t), func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) { return nil, true })
	if err != nil {
		t.Fatal(err)
	}
	var root *Definition
	var at uint16
	for _, d := range rt.Defs.All() {
		for i, s := range d.Stages {
			if s.EquippedItem != "" {
				root = d
				at = uint16(i)
				break
			}
		}
		if root != nil {
			break
		}
	}
	if root == nil {
		t.Fatal("no equipped stage fixture")
	}
	stage, _ := definitionAtStage(root, at)
	level := int64(90)
	race := int64(enterworld.RaceChina)
	c := &enterworld.Character{Level: &level, RaceIndex: &race, ActiveQuests: []enterworld.ActiveQuestRecord{BuildActiveQuestRecord(stage, objectiveRequired(stage))}}
	c.ActiveQuests[0].Stage = at
	if m := rt.MarkerStates(c)[root.RefID]; m.State != 2 {
		t.Fatalf("unequipped stage = %+v", m)
	}
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 6, Codename: stage.requiredEquippedItem}}
	if m := rt.MarkerStates(c)[root.RefID]; m.State != 3 {
		t.Fatalf("equipped stage = %+v", m)
	}
}

func TestEveryQuestMarkerNpcHasAuthoredPlacement(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	defs, err := LoadDefinitions(NewCatalog(dir), enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	anchors := map[string]int{}
	for _, npc := range simulation.LoadNpcWorldRoster(dir) {
		anchors[npc.Codename]++
	}
	for _, def := range defs.All() {
		specs := []QuestSpec{def.QuestSpec}
		for _, stage := range def.Stages {
			specs = append(specs, stage.QuestSpec)
		}
		for _, spec := range specs {
			for _, code := range []string{spec.StartNpcCodename, spec.EndNpcCodename, spec.DeliveryNpcCodename} {
				if code != "" && anchors[code] != 1 {
					t.Errorf("%s: NPC %s has %d authored placements", def.Codename, code, anchors[code])
				}
			}
		}
	}
	t.Logf("%d executable definitions; %d authored NPC identities", defs.Len(), len(anchors))
}
