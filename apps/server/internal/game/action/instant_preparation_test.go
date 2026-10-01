/*
===========================================================================

instant_preparation_test.go - prepared instant casts and resource admission tests

Exercise the production action owner and its native packet lifecycle.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// 586C92 validates before 586CE3 reads the clock. These tests distinguish
// cancellation during preparation from a release-only resource check.
/*
================
TestPreparingCastResourceShortageCancelsBeforeTimer
================
*/
func TestPreparingCastResourceShortageCancelsBeforeTimer(t *testing.T) {
	for _, kind := range []string{"self-recovery", "projectile"} {
		for _, checkAt := range []string{"before-boundary", "at-boundary", "after-boundary"} {
			t.Run(kind+"/"+checkAt, func(t *testing.T) {
				rt, c, target, skill, now := arrowFixture(t)
				cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}
				if kind == "self-recovery" {
					skill = shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
					rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
					c.Skills = append(c.Skills, skill.ID)
					c.CurrentHP = testInt64(1)
					cast = wire.SkillAction{ActionId: skill.ID}
				}
				start := rt.HandleTargetInteract(testDivision, c, cast.Encode())
				start = assertAndSeparateActionSession(t, start)
				if len(start.Frames) != 1 || len(rt.pendingProjectileCasts) != 1 {
					t.Fatal("preparation failed", start)
				}
				token := binary.LittleEndian.Uint32(start.Frames[0].Payload[10:])
				cooldown := c.OffensiveSkillCooldowns[skill.Group]
				before, _ := rt.Monsters.Get(testDivision, target)
				hp, ammo := enterworld.CurrentHP(c), c.MissionInventory[1].StackCount
				c.CurrentMP = testInt64(0)
				at := now + int64(skill.ActionCastingTimeMs)
				if checkAt == "before-boundary" {
					at = now + 1
				} else if checkAt == "after-boundary" {
					at++
				}
				frames := rt.advanceProjectileCasts(at)
				if len(frames) != 1 || len(frames[0].Frames) != 1 {
					t.Fatalf("shortage must cancel immediately with one broadcast: %+v", frames)
				}
				close := frames[0].Frames[0]
				if close.Opcode != wire.OpSkillEffectControl || len(close.Payload) != 6 || close.Payload[0] != 2 || close.Payload[1] != 0 || binary.LittleEndian.Uint32(close.Payload[2:]) != token {
					t.Fatalf("wrong cancellation: %x", close.Payload)
				}
				if _, owned := rt.currentSkillCommandFor(testDivision, c); owned || rt.hasOpenSkillCast(testDivision, c.Name) {
					t.Fatal("cancelled cast retained ownership")
				}
				after, _ := rt.Monsters.Get(testDivision, target)
				if before.CurrentHP != after.CurrentHP || enterworld.CurrentHP(c) != hp || enterworld.CurrentMP(c) != 0 || c.MissionInventory[1].StackCount != ammo || c.OffensiveSkillCooldowns[skill.Group] != cooldown {
					t.Fatal("cancel changed damage, resources or accepted cooldown")
				}
				c.CurrentMP = testInt64(100)
				if len(rt.advanceProjectileCasts(now+int64(skill.ActionCastingTimeMs)+100)) != 0 {
					t.Fatal("regeneration revived cancelled cast")
				}
			})
		}
	}
}

/*
================
TestPreparingInstantExactManaStillWaitsAtEquality
================
*/
func TestPreparingInstantExactManaStillWaitsAtEquality(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentHP = testInt64(1)
	c.CurrentMP = testInt64(int64(skill.Consumption.MP))
	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	start = assertAndSeparateActionSession(t, start)
	if len(start.Frames) != 1 {
		t.Fatal("preparation failed", start)
	}
	deadline := clock.NowMs() + int64(skill.ActionCastingTimeMs)
	if frames := rt.advanceProjectileCasts(deadline); len(frames) != 0 || len(rt.pendingProjectileCasts) != 1 {
		t.Fatal("exact resources must wait at equality", frames)
	}
	if len(rt.advanceProjectileCasts(deadline+1)) == 0 || enterworld.CurrentMP(c) != 0 || enterworld.CurrentHP(c) <= 1 {
		t.Fatal("valid preparation did not release once")
	}
}
