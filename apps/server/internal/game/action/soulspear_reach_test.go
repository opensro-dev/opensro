/*
===========================================================================

soulspear_reach_test.go - a chained stage runs without another range test

#bug-reports: Soul Spear Emperor (SKILL_CH_SPEAR_STUN_D) lost its D2 stage
"about every 10 uses" against a Shauer. Natively the linked row goes from
action state 3 straight to state 2 (4AECE7), which executes it; only state 1
measures range and pursues. 58D8F0's target validation (58CC70) asks for a
live target on the same plane in an adjacent sector, never for reach. So a
target that stands beyond reach once the root lands still takes D2.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// chainStageGap separates caster and target well beyond a spear's reach,
// and well inside one sector.
const chainStageGap = 120

/*
================
soulSpearStageAfter

Presses the root, applies move to the caster's spawn once it lands, and
returns D2's cast result code: 1 when it ran, the refusal code when it was
refused, 0 when it never answered.
================
*/
func soulSpearStageAfter(t *testing.T, move func(*simulation.Spawn)) uint16 {
	t.Helper()
	rt, targets := areaFixture(t, 1000000)
	ch := rt.findCharacter(testDivision, "asd2")
	root := shippedOffense(t, "SKILL_CH_SPEAR_STUN_D_06")
	second := shippedOffense(t, "SKILL_CH_SPEAR_STUN_D2_06")
	skills := rt.deps.SkillData().(staticSkillSource)
	skills[root.ID], skills[second.ID] = root, second
	ch.Skills = append(ch.Skills, root.ID)
	ch.Masteries = append(ch.Masteries, enterworld.CharacterMastery{ID: 258, Level: 90})
	ch.Level, ch.MaxLevel = testInt64(90), testInt64(90)
	ch.Strength, ch.Intellect = testInt64(2000), testInt64(2000)
	ch.CurrentMP = testInt64(100000)
	items := rt.deps.ItemReferences().(staticItemSource)
	spear := *items[ch.MissionInventory[0].Codename]
	spear.Codename, spear.RefObjID, spear.TypeIDs[3] = "ITEM_CH_SPEAR_01_A", 73, 4
	items[spear.Codename] = &spear
	ch.MissionInventory[0].Codename, ch.MissionInventory[0].RefObjID, ch.MissionInventory[0].TypeFlags = spear.Codename, spear.RefObjID, spear.TypeFlags()
	rt.CombatRoll = func() (uint32, error) { return 16000, nil }
	start := rt.HandleTargetInteract(testDivision, ch, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	if len(start.Frames) == 0 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
		t.Fatalf("root refused %s %+v", start.DiagnosticRefusal, start.Frames)
	}
	// Moving the caster's spawn gives the separation without driving the
	// monster's AI.
	key := simulation.WorldKey(testDivision, ch.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(ch) }, func(w *simulation.WorldState) {
		move(&w.Spawn)
	})
	base := rt.Now()
	for tick := 1; tick <= 60; tick++ {
		now := base.Add(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()
		for _, burst := range append(rt.MonsterActionTickHook()(now), rt.TickHook()(now)...) {
			for _, frame := range burst.Frames {
				if frame.Opcode != wire.OpSkillCastResult || len(frame.Payload) < 2 {
					continue
				}
				if frame.Payload[0] == 1 && len(frame.Payload) >= 6 && binary.LittleEndian.Uint32(frame.Payload[2:6]) == second.ID {
					return 1
				}
				if frame.Payload[0] == 2 && len(frame.Payload) >= 3 {
					return binary.LittleEndian.Uint16(frame.Payload[1:3])
				}
			}
		}
	}
	return 0
}

/*
================
TestSoulSpearSecondStageBeyondReach

A target beyond reach in the same sector still takes D2.
================
*/
func TestSoulSpearSecondStageBeyondReach(t *testing.T) {
	if got := soulSpearStageAfter(t, func(at *simulation.Spawn) { at.X += chainStageGap }); got != 1 {
		t.Fatalf("D2 beyond reach answered %#x, want it executed", got)
	}
}

/*
================
TestSoulSpearSecondStageTwoSectorsAway

58CC70 never admits a target more than one sector away, so dropping the
range test does not let D2 strike across the world.
================
*/
func TestSoulSpearSecondStageTwoSectorsAway(t *testing.T) {
	if got := soulSpearStageAfter(t, func(at *simulation.Spawn) { at.RegionID += 2 }); got == 1 {
		t.Fatal("D2 struck a target two sectors away")
	}
}
