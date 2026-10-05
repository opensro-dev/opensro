/*
===========================================================================

skillhpchain_test.go - Dare Devil and Crutial Rush pay their HP once

Both European HP-ratio combos (SKILL_EU_WARRIOR_TWOHANDA_CRY_B and
SKILL_EU_WARRIOR_DUALA_WHIRLWIND_B) author their root's 10 % HP ratio
again on the second stage. The HP is consumed once, when the root starts;
the second stage strikes without charging it again.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
==================
TestHPRatioCombosChargeTheirHPOnce

Each shipped first tier is admitted, releases both stages on the target,
and leaves the caster at its HP less 10 % of it, charged once.
==================
*/
func TestHPRatioCombosChargeTheirHPOnce(t *testing.T) {
	for _, line := range []struct{ root, stage string }{
		{"SKILL_EU_WARRIOR_TWOHANDA_CRY_B_01", "SKILL_EU_WARRIOR_TWOHANDA_CRY_B2_01"},
		{"SKILL_EU_WARRIOR_DUALA_WHIRLWIND_B_01", "SKILL_EU_WARRIOR_DUALA_WHIRLWIND_B2_01"},
	} {
		t.Run(line.root, func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 1000000)
			skill := shippedOffense(t, line.root)
			stage := shippedOffense(t, line.stage)
			if skill.ChainNext != stage.ID || skill.Consumption.HPPercent != 10 || stage.Consumption.HPPercent != 10 {
				t.Fatalf("%s: chain %d stage %d consumption %+v / %+v", line.root, skill.ChainNext, stage.ID, skill.Consumption, stage.Consumption)
			}
			if _, ok := enterworld.OffensiveSequence(shippedSkills(t), skill.ID); !ok {
				t.Fatalf("%s not admitted as a two-stage offense", line.root)
			}
			for _, row := range []*enterworld.SkillRow{&skill, &stage} {
				row.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
				row.Reqi = enterworld.SkillReqi{}
				rt.deps.SkillData().(staticSkillSource)[row.ID] = *row
			}
			// The level-1 fixture cannot afford the authored MP; the HP ratio
			// is what is under test.
			skill.Consumption.MP = 10
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentMP = testInt64(150)
			hp := enterworld.CurrentHP(c)
			want := hp - int64(crtFtol(float64(int32(hp))*10/100))

			start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
			if len(start.Frames) == 0 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
				t.Fatalf("%s refused: %+v", line.root, start)
			}
			released := map[uint32]bool{}
			for tick := 1; tick <= 100; tick++ {
				for _, route := range rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
					for _, frame := range route.Frames {
						if frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) >= 6 && frame.Payload[0] == 1 {
							released[binary.LittleEndian.Uint32(frame.Payload[2:6])] = true
						}
					}
				}
			}
			if !released[stage.ID] {
				t.Fatalf("second stage %d never released: %v", stage.ID, released)
			}
			if got := enterworld.CurrentHP(c); got != want {
				t.Fatalf("caster HP %d, want %d (10 %% of %d, once)", got, want, hp)
			}
		})
	}
}
