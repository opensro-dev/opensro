/*
===========================================================================

monsterabnormal_baseline_test.go - the struck monster's HP baseline

monsterImpactAbnormalFrames follows a cast result with the survivor's HP
while it carries a status. With the attack-pet expansion on (port-only),
every impact that took HP publishes it. These tests pin both settings and
where the frame stands in the burst.

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

// The baseline's layout: [u32 gid][u16 source][u8 mask][u32 hp][u32 status];
// its mask is HP plus the status word.
const (
	monsterBaselineSource = 0x100
	monsterBaselineMask   = vitalsUpdateMaskHP | 4
	monsterBaselineBytes  = 15
)

/*
================
struckMonsterBaseline

The HP and status mask of gid's baseline frame; fails the test when frame
is anything else.
================
*/
func struckMonsterBaseline(t *testing.T, frame wire.Frame, gid uint32) (hp, status uint32) {
	t.Helper()
	p := frame.Payload
	if frame.Opcode != simulation.OpVitalsUpdate || len(p) < monsterBaselineBytes ||
		binary.LittleEndian.Uint32(p) != gid ||
		binary.LittleEndian.Uint16(p[4:]) != monsterBaselineSource ||
		p[vitalsUpdateMaskOffset] != monsterBaselineMask {
		t.Fatalf("frame %04X % X is not monster %#x's HP baseline", frame.Opcode, p, gid)
	}
	return binary.LittleEndian.Uint32(p[7:]), binary.LittleEndian.Uint32(p[11:])
}

/*
================
TestStruckMonsterBaselineFollowsAStatusFreeHit

Port-only: with the attack-pet expansion on, a hit that leaves no status
still publishes the survivor's HP, behind the cast result, to the striker
and to every observer. The client binds the result to that frame, so a
pet's bonus hit (a delta-sourced HP frame) is measured from the HP this
hit left, not from an older one. Off, the burst is the native one: no HP
frame behind a status-free hit.
================
*/
func TestStruckMonsterBaselineFollowsAStatusFreeHit(t *testing.T) {
	for _, expansion := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "attack-pet expansion"}[expansion], func(t *testing.T) {
			rt, _, c, target := newCombatTestRuntime(t, 100000)
			rt.AttackPetExpansion = expansion
			result := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
			result = assertAndSeparateActionSession(t, result)
			if _, damage, fatal := assertSkillDamageOpen(t, result.Frames, 2, enterworld.ObjectIDForCharacter(c), target.Gid); damage == 0 || fatal {
				t.Fatalf("strike damage %d fatal %v, want a surviving damaged target", damage, fatal)
			}
			after, ok := rt.Monsters.Get(testDivision, target.Gid)
			if !ok || after.CurrentHP >= target.CurrentHP || after.AbnormalMask() != 0 {
				t.Fatalf("target after the strike: %+v", after)
			}
			for audience, frames := range map[string][]wire.Frame{"striker": result.Frames, "observers": result.Broadcast} {
				vitals := 0
				for _, frame := range frames {
					if frame.Opcode == simulation.OpVitalsUpdate {
						vitals++
					}
				}
				if !expansion {
					if vitals != 0 {
						t.Fatalf("%s frames = %04X, want no HP frame behind a status-free hit", audience, opcodesOf(frames))
					}
					continue
				}
				if len(frames) < 2 || frames[0].Opcode != wire.OpSkillCastResult || vitals != 1 {
					t.Fatalf("%s frames = %04X, want the cast result, then the target's baseline", audience, opcodesOf(frames))
				}
				hp, status := struckMonsterBaseline(t, frames[1], target.Gid)
				if hp != after.CurrentHP || status != 0 {
					t.Fatalf("%s baseline HP %d status %#x, want HP %d and no status", audience, hp, status, after.CurrentHP)
				}
			}
		})
	}
}

/*
================
TestMonsterImpactWithoutDamageOrStatusPublishesNoBaseline

The baseline answers an HP or status change. An impact that took no HP
from a status-free monster changed neither and publishes nothing, even
with the expansion on.
================
*/
func TestMonsterImpactWithoutDamageOrStatusPublishesNoBaseline(t *testing.T) {
	rt, _, _, target := newCombatTestRuntime(t, 100000)
	rt.AttackPetExpansion = true
	plan := simulation.MonsterDamagePlan{GID: target.Gid, ExpectedHP: target.CurrentHP}
	impacts := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, target.CurrentHP, []simulation.MonsterDamagePlan{plan})
	if len(impacts) != 1 || impacts[0].Applied != 0 {
		t.Fatalf("zero-damage impact = %+v", impacts)
	}
	if frames := rt.monsterImpactAbnormalFrames(testDivision, target.Gid, impacts); len(frames) != 0 {
		t.Fatalf("frames = %04X, want none", opcodesOf(frames))
	}
}
