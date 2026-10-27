/*
===========================================================================

soulspear_reach_test.go - a chained stage runs without another range test

#bug-reports: Soul Spear Emperor (SKILL_CH_SPEAR_STUN_D) lost its D2 stage
"about every 10 uses" against a Shauer. Natively the linked row goes from
action state 3 straight to state 2, which executes it; only state 1
measures range and pursues. The native target validation asks for a
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
	"opensro.online/server/internal/game/world/monster"
)

// chainStageGap separates caster and target well beyond a spear's reach,
// and well inside one sector.
const chainStageGap = 120

/*
================
soulSpearStageAfter

Presses the root, applies move to the target's pose once it lands, and
returns D2's cast result code: 1 when it ran, the refusal code when it was
refused, 0 when it never answered.
================
*/
func soulSpearStageAfter(t *testing.T, move func(*monster.Pose)) uint16 {
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
	// The target steps away mid-combo.
	mover, _ := rt.Monsters.Mover(testDivision, targets[0].Gid)
	move(&mover.Pose)
	rt.Monsters.CommitMover(testDivision, targets[0].Gid, mover)
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
				// A refusal carries the low byte of its 0x30xx code
				// (offensiveRefusal).
				if frame.Payload[0] == 2 {
					return 0x3000 | uint16(frame.Payload[1])
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
	if got := soulSpearStageAfter(t, func(at *monster.Pose) { at.X += chainStageGap }); got != 1 {
		t.Fatalf("D2 beyond reach answered %#x, want it executed", got)
	}
}

/*
================
TestSoulSpearSecondStageTwoSectorsAway

The native target validation refuses a target outside the same plane and adjacent sectors with
0x3006, so dropping the range test does not let D2 strike across the world.
================
*/
func TestSoulSpearSecondStageTwoSectorsAway(t *testing.T) {
	if got := soulSpearStageAfter(t, func(at *monster.Pose) { at.RegionID += 2 }); got != 0x3006 {
		t.Fatalf("D2 two sectors away answered %#x, want 0x3006", got)
	}
}
