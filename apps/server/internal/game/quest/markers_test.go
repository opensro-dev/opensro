package quest

import (
	"encoding/binary"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"slices"
	"testing"
)

func TestMarkerStatesFollowAuthoritativeQuestLifecycle(t *testing.T) {
	licensed.RequireGameData(t)
	defs, items := loadShippedDefinitions(t)
	rt, err := NewRuntime(&enterworld.Deps{Items: items}, defs, func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) { return nil, true })
	if err != nil {
		t.Fatal(err)
	}
	race, level := int64(enterworld.RaceEurope), int64(1)
	c := &enterworld.Character{Name: "Marker", RaceIndex: &race, Level: &level}
	// Lipria offers the European tutorial; the superseded chain shows nothing.
	tutorial, _ := rt.Defs.ByCodename("QTUTORIAL_EU")
	if m := rt.MarkerStates(c)[tutorial.RefID]; m.State != 1 || m.Codename != "NPC_EU_ADVICE" {
		t.Fatal(m)
	}
	if _, ok := rt.MarkerStates(c)[143]; ok {
		t.Fatal("superseded QNO_EU_TUTORIAL_1 still offered")
	}
	if _, err := rt.StartQuest(c, "QTUTORIAL_EU"); err != nil {
		t.Fatal(err)
	}
	// Stage 1 is a talk with Lipria herself: ready to report at once.
	if m := rt.MarkerStates(c)[tutorial.RefID]; m.State != 3 || m.Codename != "NPC_EU_ADVICE" {
		t.Fatal(m)
	}
	if _, err := rt.AdvanceNpcQuest(c, stageToken("QTUTORIAL_EU", 0), "NPC_EU_ADVICE"); err != nil {
		t.Fatal(err)
	}
	// Stage 2 moves the marker to Jatomo.
	if m := rt.MarkerStates(c)[tutorial.RefID]; m.Codename != "NPC_EU_ARMOR" {
		t.Fatal(m)
	}
	c.DeletePending = true
	if len(rt.MarkerStates(c)) != 0 {
		t.Fatal("deleted character has quest markers")
	}
}

/*
================
TestMarkersByNpcShowsTheReportOverAnOffer

The client indexes the lowest key per NPC (787DB0), so the server must not
let a lower quest the NPC offers mask the quest ready to report there.
================
*/
func TestMarkersByNpcShowsTheReportOverAnOffer(t *testing.T) {
	states := map[uint32]NpcMarker{
		10: {Codename: "NPC_A", State: markerStateOffer},
		20: {Codename: "NPC_A", State: markerStateReport},
		30: {Codename: "NPC_A", State: markerStateInProgress},
		40: {Codename: "NPC_B", State: markerStateInProgress},
		50: {Codename: "NPC_B", State: markerStateOffer},
		60: {Codename: "NPC_C", State: markerStateOffer},
		70: {Codename: "NPC_C", State: markerStateOffer},
	}
	got := MarkersByNpc(states)
	want := map[uint32]NpcMarker{20: states[20], 50: states[50], 60: states[60]}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for id, m := range want {
		if got[id] != m {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

/*
================
TestJournalTargetsFollowTheQuestNpc

The journal names the NPC the quest sends the player to (SQuestInfo 0x40),
so the world map and minimap can place it; a stage advance moves it.
================
*/
func TestJournalTargetsFollowTheQuestNpc(t *testing.T) {
	licensed.RequireGameData(t)
	defs, items := loadShippedDefinitions(t)
	refs := map[string]uint32{"NPC_EU_ADVICE": 7526, "NPC_EU_ARMOR": 7527}
	if err := defs.ResolveJournalNpcs(func(code string) (uint32, bool) {
		if ref, ok := refs[code]; ok {
			return ref, true
		}
		return 1, true
	}); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(&enterworld.Deps{Items: items}, defs, func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) { return nil, true })
	if err != nil {
		t.Fatal(err)
	}
	race, level := int64(enterworld.RaceEurope), int64(1)
	c := &enterworld.Character{Name: "Journal", RaceIndex: &race, Level: &level}
	if _, err := rt.StartQuest(c, "QTUTORIAL_EU"); err != nil {
		t.Fatal(err)
	}
	if r := c.ActiveQuests[0]; r.Flags&questFlagTargets == 0 || !slices.Equal(r.TargetIds, []uint32{7526}) {
		t.Fatalf("accepted record targets %#x %v, want Lipria", r.Flags, r.TargetIds)
	}
	if _, err := rt.AdvanceNpcQuest(c, stageToken("QTUTORIAL_EU", 0), "NPC_EU_ADVICE"); err != nil {
		t.Fatal(err)
	}
	if r := c.ActiveQuests[0]; !slices.Equal(r.TargetIds, []uint32{7527}) {
		t.Fatalf("stage 2 targets %v, want Jatomo", r.TargetIds)
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

/*
================
TestOfferMarkerFollowsTheNativeLevelGap

925D20 / 40FE90: below the quest's level the NPC shows the red scroll;
within six levels above it the offer mark; further above, nothing.
================
*/
func TestOfferMarkerFollowsTheNativeLevelGap(t *testing.T) {
	for _, tc := range []struct {
		level, questLevel int64
		state             uint8
		shown             bool
	}{
		{1, 10, markerStateTooLow, true},
		{9, 10, markerStateTooLow, true},
		{10, 10, markerStateOffer, true},
		{16, 10, markerStateOffer, true},
		{17, 10, 0, false},
	} {
		if state, shown := offerMarkerState(tc.level, tc.questLevel); state != tc.state || shown != tc.shown {
			t.Fatalf("level %d quest %d = (%d, %v), want (%d, %v)", tc.level, tc.questLevel, state, shown, tc.state, tc.shown)
		}
	}
}
