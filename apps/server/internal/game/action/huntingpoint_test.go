/*
===========================================================================

huntingpoint_test.go - the Rogue's Tag Point marks a player for its caster

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// tagPointA1 is SKILL_EU_ROG_STEALTHA_POINT_A_01: nbuf bbuf, lnks 13 10000 1 1,
// dura 900000, hntp, reqi 6 12, reqi 6 13.
const tagPointA1 = 7949

/*
==================
TestTagPointReportsTheMarkOutOfSight

A cast on a hostile player (Tag Point is a hostile execution: 589EB6
reads hntp) installs the hunt link and sends the caster its private
B5ED naming the subject. While the subject is outside the caster's
published set, each change of its position reaches the caster as 0x30E3
for the subject's GID; an unchanged position or a subject in sight sends
nothing, and neither does a stopped mark.
==================
*/
func TestTagPointReportsTheMarkOutOfSight(t *testing.T) {
	rt, clock, c, marked := newPvpPair(t)
	learnShipped(t, rt, c, tagPointA1)
	skill, _ := rt.deps.SkillData().SkillByID(tagPointA1)
	if !skill.TimedEffect.Pinned || !skill.TimedEffect.Link.Hunt {
		t.Fatalf("Tag Point does not compile as a hunt link: %+v", skill.TimedEffect.Link)
	}
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = int64(skill.RequiredWeaponKinds[0])
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	marked.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	subject := enterworld.ObjectIDForCharacter(marked)

	r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: tagPointA1, HasTarget: true, TargetGid: subject}.Encode())
	if r.DiagnosticRefusal != "" || len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("Tag Point refused: %+v", r)
	}
	if _, ok := findFrame(r.Frames, wire.OpSourceEffect); !ok {
		t.Fatal("no B5ED for the hunter")
	}

	session := simulation.SessionView{DivisionID: testDivision, CharacterID: c.ID, PublishedObjects: []uint32{}}
	now := clock.NowMs()
	reports := rt.AdvanceHuntingPoints(now, []simulation.SessionView{session})
	if len(reports) != 1 || reports[0].OnlyCharacterID != c.ID || len(reports[0].Frames) != 1 ||
		reports[0].Frames[0].Opcode != wire.OpObjectSourceMove ||
		binary.LittleEndian.Uint32(reports[0].Frames[0].Payload[16:20]) != subject {
		t.Fatalf("out-of-sight report: %+v", reports)
	}
	if again := rt.AdvanceHuntingPoints(now, []simulation.SessionView{session}); len(again) != 0 {
		t.Fatalf("an unchanged position was reported again: %+v", again)
	}
	key := simulation.WorldKey(testDivision, marked.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(marked) }, func(w *simulation.WorldState) { w.Spawn.X += 500 })
	if moved := rt.AdvanceHuntingPoints(now, []simulation.SessionView{session}); len(moved) != 1 {
		t.Fatalf("a move was not reported: %+v", moved)
	}
	session.PublishedObjects = []uint32{subject}
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(marked) }, func(w *simulation.WorldState) { w.Spawn.X += 500 })
	if seen := rt.AdvanceHuntingPoints(now, []simulation.SessionView{session}); len(seen) != 0 {
		t.Fatalf("a subject in sight was reported: %+v", seen)
	}
	// A stopped mark reports nothing, before B6A0 retires its halves.
	hunts := rt.effects.HuntLinks(now)
	if len(hunts) != 1 {
		t.Fatalf("live hunt links %d, want 1", len(hunts))
	}
	rt.effects.StopLink(testDivision, hunts[0].SourceToken)
	session.PublishedObjects = []uint32{}
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(marked) }, func(w *simulation.WorldState) { w.Spawn.X += 500 })
	if stopped := rt.AdvanceHuntingPoints(now, []simulation.SessionView{session}); len(stopped) != 0 {
		t.Fatalf("a stopped mark was reported: %+v", stopped)
	}
}
