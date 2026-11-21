/*
===========================================================================

skillrecall_test.go - the skill reset scroll's use

Port-only, not native. The scroll (3/3/13/13, Param1 5) is served only
while its switch is on; a reset spends one scroll, answers with the use's
success then the progression frames, and retires the forgotten skills'
resident effects; a reset with nothing to return keeps the scroll.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
)

/*
================
skillRecallUseCase

A character holding one skill reset scroll, with a resident effect of a
skill the reset forgets (77) and one of a skill it keeps (88).
================
*/
func skillRecallUseCase(t *testing.T, on bool, recalls bool) (*Runtime, *enterworld.Character) {
	t.Helper()
	ref := &enterworld.ItemRef{RefObjID: 51601, Codename: "ITEM_MALL_SKILL_POINT_RECALL_A", TypeIDs: [4]int64{3, 3, 13, 13},
		ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 5})}
	c := testCharacter()
	items := testItems()
	items[ref.Codename] = ref
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID,
		Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1}}
	rt, _ := newTestRuntime(c, items)
	rt.ResetScrolls = map[commerce.ResetScrollKind]bool{commerce.SkillReset: on}
	rt.RecallSkills = func(*enterworld.Character) (progression.SkillRecall, bool) {
		if !recalls {
			return progression.SkillRecall{}, false
		}
		return progression.SkillRecall{Removed: []uint32{77}, Frames: []wire.Frame{
			{Opcode: progression.SkillRecallControl, Payload: []byte(`{"version":1}`)}}}, true
	}
	for _, id := range []uint32{77, 88} {
		if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name,
			SkillID: id, SkillGroup: id, InstanceToken: id, State: statuseffect.StateActive}) {
			t.Fatalf("failed to install effect %d", id)
		}
	}
	return rt, c
}

/*
================
residentSkills
================
*/
func residentSkills(rt *Runtime, c *enterworld.Character) map[uint32]bool {
	resident := map[uint32]bool{}
	for _, effect := range rt.effects.Snapshot(testDivision, c.Name) {
		resident[effect.SkillID] = true
	}
	return resident
}

/*
================
TestSkillRecallUseRetiresForgottenEffects
================
*/
func TestSkillRecallUseRetiresForgottenEffects(t *testing.T) {
	rt, c := skillRecallUseCase(t, true, true)
	r := useSlot21(rt, c)
	if len(c.MissionInventory) != 0 {
		t.Fatalf("bag %+v; want the scroll spent", c.MissionInventory)
	}
	if len(r.Frames) == 0 || r.Frames[0].Opcode != wire.OpItemUseResponse {
		t.Fatalf("frames %+v; want the use's success first", r.Frames)
	}
	control := false
	for _, frame := range r.Frames {
		control = control || frame.Opcode == progression.SkillRecallControl
	}
	if !control {
		t.Fatalf("frames %+v; want the recall's control", r.Frames)
	}
	if resident := residentSkills(rt, c); resident[77] || !resident[88] {
		t.Fatalf("resident effects %v; want 77 retired and 88 kept", resident)
	}
}

/*
================
TestSkillRecallOffIsRefused
================
*/
func TestSkillRecallOffIsRefused(t *testing.T) {
	rt, c := skillRecallUseCase(t, false, true)
	r := useSlot21(rt, c)
	if r.DiagnosticRefusal == "" || len(c.MissionInventory) != 1 || !residentSkills(rt, c)[77] {
		t.Fatalf("switch off: refusal %q, bag %+v; want refused, kept and nothing retired", r.DiagnosticRefusal, c.MissionInventory)
	}
}

/*
================
TestSkillRecallNothingToReturnKeepsTheScroll
================
*/
func TestSkillRecallNothingToReturnKeepsTheScroll(t *testing.T) {
	rt, c := skillRecallUseCase(t, true, false)
	r := useSlot21(rt, c)
	if r.DiagnosticRefusal == "" || len(c.MissionInventory) != 1 || !residentSkills(rt, c)[77] {
		t.Fatalf("nothing back: refusal %q, bag %+v; want refused and kept", r.DiagnosticRefusal, c.MissionInventory)
	}
}
