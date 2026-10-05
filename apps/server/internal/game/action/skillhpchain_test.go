/*
===========================================================================

skillhpchain_test.go - Dare Devil and Crutial Rush pay their HP at each stage

Both European HP-ratio combos (SKILL_EU_WARRIOR_TWOHANDA_CRY_B and
SKILL_EU_WARRIOR_DUALA_WHIRLWIND_B) author their root's 10 % HP ratio
again on the second stage. 4AECE7 returns the next row to admission; 5867DC prepares its own
resource snapshot, so the second stage also consumes its authored HP.

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
TestHPRatioCombosChargeEachStage

Each shipped first tier is admitted, releases both stages on the target,
and consumes 10 % of the current HP at each stage.
==================
*/
func TestHPRatioCombosChargeEachStage(t *testing.T) {
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
			want -= int64(crtFtol(float64(int32(want)) * 10 / 100))

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
				t.Fatalf("caster HP %d, want %d (10 %% of current HP at both stages, starting %d)", got, want, hp)
			}
		})
	}
}

/*
================
TestHPRatioChainRechecksHPBeforeSecondStage

A hit between stages can leave too little HP for the next row's admission
price, which uses maximum HP even though execution charges current HP.
================
*/
func TestHPRatioChainRechecksHPBeforeSecondStage(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	root := shippedOffense(t, "SKILL_EU_WARRIOR_TWOHANDA_CRY_B_01")
	stage := shippedOffense(t, "SKILL_EU_WARRIOR_TWOHANDA_CRY_B2_01")
	for _, row := range []*enterworld.SkillRow{&root, &stage} {
		row.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
		row.Reqi = enterworld.SkillReqi{}
		row.Consumption.MP = 0
		rt.deps.SkillData().(staticSkillSource)[row.ID] = *row
	}
	c.Skills = append(c.Skills, root.ID)
	before := enterworld.CurrentHP(c)
	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(start.Frames) == 0 || start.Frames[0].Payload[0] != 1 {
		t.Fatalf("root refused: %+v", start)
	}
	// Wait for the first HP debit, then model damage before the chain advances.
	depleted := enterworld.CurrentHP(c) < before
	if depleted {
		c.CurrentHP = testInt64(1)
	}
	for tick := 1; tick <= 100; tick++ {
		for _, route := range rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
			for _, frame := range route.Frames {
				if frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) >= 6 && frame.Payload[0] == 1 && binary.LittleEndian.Uint32(frame.Payload[2:6]) == stage.ID {
					t.Fatal("second stage started without enough HP")
				}
			}
		}
		if !depleted && enterworld.CurrentHP(c) < before {
			c.CurrentHP = testInt64(1)
			depleted = true
		}
	}
	if !depleted {
		t.Fatal("first stage never paid its HP cost")
	}
	if got := enterworld.CurrentHP(c); got != 1 {
		t.Fatalf("refused stage changed HP: %d", got)
	}
}

/*
================
TestPreparedHPCostUsesNativeHandlerArithmetic

50 * 58 / 100 is 29 in the instant integer path; persistent x87 first
forms 58 / 100, whose double product truncates to 28.
================
*/
func TestPreparedHPCostUsesNativeHandlerArithmetic(t *testing.T) {
	rt, _, caster, _ := newCombatTestRuntime(t, 100000)
	caster.CurrentHP = testInt64(50)
	row := enterworld.SkillRow{Consumption: enterworld.SkillConsumption{HPPercent: 58, Pinned: true}}
	if got := rt.preparedExecutionHPCost(testDivision, caster, row); got != 29 {
		t.Fatalf("instant HP price %d, want 29", got)
	}
	row.TimedEffect.Pinned = true
	if got := rt.preparedExecutionHPCost(testDivision, caster, row); got != 28 {
		t.Fatalf("persistent HP price %d, want 28", got)
	}
}
