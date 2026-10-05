/*
===========================================================================
damagetomp_test.go - all retail ranks, hit publication and effect lifetime
===========================================================================
*/
package action

import (
	"encoding/binary"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
	"time"
)

/*
================
damageToMPFixture
================
*/
func damageToMPFixture(t *testing.T, id uint32) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	t.Helper()
	rt, clock, c, mob := newCombatTestRuntime(t, 100000)
	row := learnShipped(t, rt, c, id)
	row.Consumption.MP = 1
	rt.deps.SkillData().(staticSkillSource)[id] = row
	c.CurrentMP = testInt64(200)
	result := castSelf(rt, c, id)
	if result.DiagnosticRefusal != "" {
		t.Fatal(result.DiagnosticRefusal)
	}
	clock.Advance(time.Duration(row.ActionCastingTimeMs+1) * time.Millisecond)
	rt.advanceProjectileCasts(clock.NowMs())
	clock.Advance(time.Duration(row.ActionDurationMs+1) * time.Millisecond)
	rt.drainSkillFinalizes(clock.NowMs())
	if !hasSkillEffect(rt, c.Name, id) {
		t.Fatal("effect not installed")
	}
	mob.Ref.DefaultSkillIDs[0] = 2
	return rt, clock, c, mob
}

/*
================
TestSnowShieldEveryPublishedRank
================
*/
func TestSnowShieldEveryPublishedRank(t *testing.T) {
	skills := shippedSkills(t)
	for id := uint32(19592); id <= 19613; id++ {
		row, ok := skills.SkillByID(id)
		if !ok || !row.TimedEffect.Pinned || !row.TimedEffect.DamageToMP {
			t.Fatalf("rank %d not admitted", id)
		}
		rt, _, c, _ := damageToMPFixture(t, id)
		if got := rt.effects.DamageToMPPercent(testDivision, c.Name); got != row.TimedEffect.DamageToMPPercent {
			t.Fatalf("rank %d: %d", id, got)
		}
	}
}

/*
================
TestDamageToMPHitSequenceAndPublication

Compare the same deterministic hit sequence without a shield. Each impact uses
remaining MP, publishes reduced HP damage, and never applies an MP debit twice.
================
*/
func TestDamageToMPHitSequenceAndPublication(t *testing.T) {
	for _, mp := range []int64{0, 1, 20, 200} {
		for _, flags := range []uint32{5, 9} {
			plain, plainClock, pc, mob := newCombatTestRuntime(t, 100000)
			mob.Ref.DefaultSkillIDs[0] = 2
			shield, clock, c, shieldMob := damageToMPFixture(t, 19592)
			for _, rt := range []*Runtime{plain, shield} {
				table := rt.deps.SkillData().(staticSkillSource)
				attack := table[2]
				attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent, attack.Attack.Flags = 30, 30, 100, flags
				table[2] = attack
			}
			pc.CurrentHP = testInt64(200)
			c.CurrentHP = testInt64(200)
			c.CurrentMP = testInt64(mp)
			baseline := plain.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(pc), 2, plainClock.NowMs())
			result := shield.MonsterBasicAttack(testDivision, shieldMob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
			if !baseline.Accepted || !result.Accepted {
				t.Fatal("hit refused")
			}
			p, q := baseline.Frames[0].Payload, result.Frames[0].Payload
			count := int(p[19])
			remainingMP := uint32(mp)
			hpDamage := uint32(0)
			for i := 0; i < count; i++ {
				at := 25 + i*9
				damage := binary.LittleEndian.Uint32(p[at+1:]) >> 8
				hp, spent := combat.DamageToMP(damage, remainingMP, 20)
				remainingMP -= spent
				hpDamage += hp
				if got := binary.LittleEndian.Uint32(q[at+1:]) >> 8; got != hp {
					t.Fatalf("MP %d flags %d impact %d: %d != %d", mp, flags, i, got, hp)
				}
			}
			if *c.CurrentMP != int64(remainingMP) || *c.CurrentHP != 200-int64(hpDamage) {
				t.Fatalf("wrong gauges HP %d MP %d", *c.CurrentHP, *c.CurrentMP)
			}
			updates := 0
			for _, f := range privateFramesOf(result) {
				if f.Opcode == simulation.OpVitalsUpdate {
					updates++
					if len(f.Payload) != 11 || f.Payload[6] != 2 || binary.LittleEndian.Uint16(f.Payload[4:]) != 4 || binary.LittleEndian.Uint32(f.Payload[7:]) != remainingMP {
						t.Fatalf("wrong MP update %x", f.Payload)
					}
				}
			}
			if (updates == 1) != (remainingMP < uint32(mp)) {
				t.Fatalf("MP updates %d", updates)
			}
			if !hasSkillEffect(shield, c.Name, 19592) {
				t.Fatal("MP exhaustion retired the buff")
			}
		}
	}
}

/*
================
TestDamageToMPRetirementAndOwnerIsolation
================
*/
func TestDamageToMPRetirementAndOwnerIsolation(t *testing.T) {
	rt, clock, c, _ := damageToMPFixture(t, 19592)
	if rt.effects.DamageToMPPercent(testDivision, "other") != 0 || rt.effects.DamageToMPPercent("other", c.Name) != 0 {
		t.Fatal("cross-owner shield")
	}
	clock.Advance(121 * time.Second)
	rt.effects.Expire(clock.NowMs())
	rt.effects.DrainStopRequested()
	if rt.effects.DamageToMPPercent(testDivision, c.Name) != 0 {
		t.Fatal("expired shield retained")
	}
}

/*
================
TestDamageToMPDoesNotAffectAbnormalDamage
================
*/
func TestDamageToMPDoesNotAffectAbnormalDamage(t *testing.T) {
	rt, clock, c, _ := damageToMPFixture(t, 19592)
	c.CurrentHP = testInt64(100)
	c.CurrentMP = testInt64(100)
	owner := rt.newPlayerAbnormalOwner(testDivision, c, clock.NowMs())
	owner.Hit(0, false, 25, 2, 0)
	if *c.CurrentHP != 75 || *c.CurrentMP != 100 {
		t.Fatal("abnormal damage entered dgmp")
	}
}

/*
================
TestDamageToMPVoluntaryCancellationAndDeath
================
*/
func TestDamageToMPVoluntaryCancellationAndDeath(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		rt, _, c, _ := damageToMPFixture(t, 19592)
		if _, ok := rt.effects.RequestVoluntaryStop(testDivision, c.Name, 19592, 0); !ok {
			t.Fatal("cancel refused")
		}
		if rt.effects.DamageToMPPercent(testDivision, c.Name) != 20 {
			t.Fatal("stop request removed contribution before retirement")
		}
		rt.drainStoppedCharacterEffects()
		if rt.effects.DamageToMPPercent(testDivision, c.Name) != 0 {
			t.Fatal("cancel retained contribution")
		}
	})
	t.Run("death", func(t *testing.T) {
		rt, clock, c, mob := damageToMPFixture(t, 19592)
		c.CurrentHP = testInt64(1)
		c.CurrentMP = testInt64(0)
		table := rt.deps.SkillData().(staticSkillSource)
		attack := table[2]
		attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent = 100, 100, 100
		table[2] = attack
		result := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
		if !result.Accepted || result.TargetAlive || rt.effects.DamageToMPPercent(testDivision, c.Name) != 0 {
			t.Fatal("fatal hit retained shield", result)
		}
	})
}

/*
================
TestDamageToMPAfterWallAbsorption
================
*/
func TestDamageToMPAfterWallAbsorption(t *testing.T) {
	rt, clock, c, mob := damageToMPFixture(t, 19592)
	learnWall(t, rt, c, crystalWallA1)
	if result := castSelf(rt, c, crystalWallA1); result.DiagnosticRefusal != "" || !hasSkillEffect(rt, c.Name, crystalWallA1) {
		t.Fatal(result.DiagnosticRefusal)
	}
	clock.Advance(2 * time.Second)
	rt.drainSkillFinalizes(clock.NowMs())
	table := rt.deps.SkillData().(staticSkillSource)
	attack := table[2]
	attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent, attack.Attack.Flags = 30, 30, 100, 5
	table[2] = attack
	before := *c.CurrentMP
	player, _ := wallHit(t, rt, clock, c, mob)
	for _, damage := range player {
		if damage != 0 {
			t.Fatal("wall leaked HP damage")
		}
	}
	if *c.CurrentMP != before {
		t.Fatal("fully absorbed hit charged MP")
	}
}

/*
================
TestDamageToMPDoesNotStackAndFreezesDescriptor

Native packed casting states refuse a second Snow Shield while one is active.
After cancellation the new descriptor owns the contribution until retirement.
================
*/
func TestDamageToMPDoesNotStackAndFreezesDescriptor(t *testing.T) {
	rt, clock, c, _ := damageToMPFixture(t, 19592)
	row := learnShipped(t, rt, c, 19613)
	row.Consumption.MP = 1
	table := rt.deps.SkillData().(staticSkillSource)
	table[row.ID] = row
	result := castSelf(rt, c, row.ID)
	if len(result.Frames) != 1 || result.Frames[0].Opcode != 0xb245 || len(result.Frames[0].Payload) != 2 || result.Frames[0].Payload[0] != 2 {
		t.Fatal("active shield allowed stacking", result)
	}
	if rt.effects.DamageToMPPercent(testDivision, c.Name) != 20 {
		t.Fatal("refusal changed contribution")
	}
	if _, ok := rt.effects.RequestVoluntaryStop(testDivision, c.Name, 19592, 0); !ok {
		t.Fatal("cancel refused")
	}
	rt.drainStoppedCharacterEffects()
	clock.Advance(181 * time.Second)
	result = castSelf(rt, c, row.ID)
	clock.Advance(2 * time.Second)
	rt.advanceProjectileCasts(clock.NowMs())
	rt.drainSkillFinalizes(clock.NowMs())
	if got := rt.effects.DamageToMPPercent(testDivision, c.Name); got != 53 {
		t.Fatalf("next rank percent %d: %+v", got, result)
	}
	changed := table[row.ID]
	changed.TimedEffect.DamageToMPPercent = 1
	table[row.ID] = changed
	if rt.effects.DamageToMPPercent(testDivision, c.Name) != 53 {
		t.Fatal("live contribution reread mutable catalog")
	}
	rt.effects.Forget(testDivision, c.Name)
	if rt.effects.DamageToMPPercent(testDivision, c.Name) != 0 {
		t.Fatal("logout retained shield")
	}
}
