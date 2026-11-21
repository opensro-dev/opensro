/*
===========================================================================

skillrecall_test.go - the skill reset scroll's progression half

Port-only, not native. Every paid skill is forgotten and every mastery
goes back to 0, the SP their ranks cost coming back; the free basic
skills stay; a reset with nothing to return changes nothing.

===========================================================================
*/
package progression

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
skillRecallCase

A character with a three-rank paid skill (2+7+13 SP, needing mastery 257
at 3), a skill learned on top of it, a free basic skill, mastery 257 at 6
(ranks 2..6 cost 1+1+1+2+2 = 7 SP in testLevels) and 100 SP, a quick slot
on each skill.
================
*/
func skillRecallCase(t *testing.T) (*Runtime, *enterworld.Character) {
	t.Helper()
	skills := withdrawalTestCatalog{staticSkills{
		10: {ID: 10, Group: 100, Level: 1, SPCost: 2, Masteries: [2]enterworld.SkillRequirement{{ID: 257, Level: 1}}},
		11: {ID: 11, Group: 100, Level: 2, SPCost: 7, Masteries: [2]enterworld.SkillRequirement{{ID: 257, Level: 2}}},
		12: {ID: 12, Group: 100, Level: 3, SPCost: 13, Masteries: [2]enterworld.SkillRequirement{{ID: 257, Level: 3}}},
		20: {ID: 20, Group: 200, Level: 1, SPCost: 5, Prerequisites: [3]enterworld.SkillRequirement{{ID: 100, Level: 2}}},
		1:  {ID: 1, Group: 1, Level: 1},
	}}
	c := testCharacter()
	c.Skills = []uint32{12, 20, 1}
	c.Masteries = []enterworld.CharacterMastery{{ID: 257, Level: 6}, {ID: 258, Level: 0}}
	c.QuickSlots = []enterworld.QuickSlotBinding{{Slot: 0, Kind: 0x49, Payload: 12}, {Slot: 1, Kind: 0x49, Payload: 1},
		{Slot: 2, Kind: 0x49, Payload: 20}}
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {c}},
		Items:      emptyItemRefs{},
		Levels:     testLevels(),
		Skills:     skills,
	}
	var doorMu sync.Mutex
	deps.MutateCharacter = func(_ *enterworld.Character, _ string, fn func()) {
		doorMu.Lock()
		defer doorMu.Unlock()
		if fn != nil {
			fn()
		}
	}
	rt := NewRuntime(deps)
	rt.BaseStats = func(*enterworld.Character) (wire.BaseStats, error) { return wire.BaseStats{}, nil }
	return rt, c
}

/*
================
TestSkillRecallRefundsEveryPaidRankAndMastery
================
*/
func TestSkillRecallRefundsEveryPaidRankAndMastery(t *testing.T) {
	rt, c := skillRecallCase(t)
	recall, ok := rt.SkillRecallUpdater()(c)
	if !ok {
		t.Fatal("the recall was refused")
	}
	// 2+7+13 for the three ranks, 5 for the dependent skill, 7 for the mastery.
	if *c.SkillPoints != 100+22+5+7 {
		t.Fatalf("SP = %d, want %d", *c.SkillPoints, 100+22+5+7)
	}
	if !reflect.DeepEqual(c.Skills, []uint32{1}) {
		t.Fatalf("skills = %v, want the free basic skill alone", c.Skills)
	}
	if !reflect.DeepEqual(c.Masteries, []enterworld.CharacterMastery{{ID: 257, Level: 0}, {ID: 258, Level: 0}}) {
		t.Fatalf("masteries = %+v, want every mastery at 0", c.Masteries)
	}
	if !reflect.DeepEqual(recall.Removed, []uint32{12, 20}) || !reflect.DeepEqual(recall.RemovedGroups, []uint32{100, 200}) {
		t.Fatalf("removed = %v groups %v, want [12 20] groups [100 200]", recall.Removed, recall.RemovedGroups)
	}
}

/*
================
TestSkillRecallClearsRemovedQuickSlots
================
*/
func TestSkillRecallClearsRemovedQuickSlots(t *testing.T) {
	rt, c := skillRecallCase(t)
	if _, ok := rt.SkillRecallUpdater()(c); !ok {
		t.Fatal("the recall was refused")
	}
	want := []enterworld.QuickSlotBinding{{Slot: 0}, {Slot: 1, Kind: 0x49, Payload: 1}, {Slot: 2}}
	if !reflect.DeepEqual(c.QuickSlots, want) {
		t.Fatalf("quick slots = %+v, want %+v", c.QuickSlots, want)
	}
}

/*
================
TestSkillRecallFrames

The SP update, the base stats, then the port-only control 21 naming the
kept skills and the masteries for the browser.
================
*/
func TestSkillRecallFrames(t *testing.T) {
	rt, c := skillRecallCase(t)
	recall, _ := rt.SkillRecallUpdater()(c)
	index := map[uint16]int{}
	for i, frame := range recall.Frames {
		index[frame.Opcode] = i
	}
	points, stats, control := index[wire.OpPointsUpdate], index[wire.OpBaseStats], index[SkillRecallControl]
	if len(recall.Frames) != 3 || !(points < stats && stats < control) {
		t.Fatalf("frames = %+v, want 0x30B3, 0x303D, then control %d", recall.Frames, SkillRecallControl)
	}
	if got := recall.Frames[points].Payload; !reflect.DeepEqual(got, wire.EncodePointsSkillUpdate(134, false)) {
		t.Fatalf("SP update = %v", got)
	}
	var body struct {
		Version   int                           `json:"version"`
		Skills    []uint32                      `json:"skills"`
		Masteries []enterworld.CharacterMastery `json:"masteries"`
	}
	if err := json.Unmarshal(recall.Frames[control].Payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != 1 || !reflect.DeepEqual(body.Skills, []uint32{1}) || len(body.Masteries) != 2 || body.Masteries[0].Level != 0 {
		t.Fatalf("control body = %+v", body)
	}
}

/*
================
TestSkillRecallNothingToReturnKeepsScroll
================
*/
func TestSkillRecallNothingToReturnKeepsScroll(t *testing.T) {
	rt, c := skillRecallCase(t)
	c.Skills = []uint32{1}
	c.Masteries = []enterworld.CharacterMastery{{ID: 257, Level: 0}}
	if _, ok := rt.SkillRecallUpdater()(c); ok || *c.SkillPoints != 100 {
		t.Fatalf("nothing spent: SP %d; want refused and unchanged", *c.SkillPoints)
	}
}

/*
================
TestSkillRecallRefusesSPOverflow
================
*/
func TestSkillRecallRefusesSPOverflow(t *testing.T) {
	rt, c := skillRecallCase(t)
	full := int64(math.MaxUint32 - 10)
	c.SkillPoints = &full
	if _, ok := rt.SkillRecallUpdater()(c); ok || len(c.Skills) != 3 || c.Masteries[0].Level != 6 {
		t.Fatalf("overflow: skills %v, masteries %+v; want refused and unchanged", c.Skills, c.Masteries)
	}
}

/*
================
TestSkillRecallSendsAnEmptyMasteryList

A record without masteries still sends a list, never null: the browser
ends the session on a malformed control.
================
*/
func TestSkillRecallSendsAnEmptyMasteryList(t *testing.T) {
	rt, c := skillRecallCase(t)
	c.Masteries = nil
	recall, ok := rt.SkillRecallUpdater()(c)
	if !ok {
		t.Fatal("the recall was refused")
	}
	if got := string(recall.Frames[len(recall.Frames)-1].Payload); !strings.Contains(got, `"masteries":[]`) {
		t.Fatalf("control body %s, want an empty mastery list", got)
	}
}

/*
================
TestSkillRecallRefusesAnUnknownSkill
================
*/
func TestSkillRecallRefusesAnUnknownSkill(t *testing.T) {
	rt, c := skillRecallCase(t)
	c.Skills = append(c.Skills, 999)
	if _, ok := rt.SkillRecallUpdater()(c); ok || len(c.Skills) != 4 {
		t.Fatalf("unknown skill: skills %v; want refused and unchanged", c.Skills)
	}
}
