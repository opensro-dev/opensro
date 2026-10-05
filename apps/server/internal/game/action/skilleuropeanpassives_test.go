/*
===========================================================================

skilleuropeanpassives_test.go - the European passives and their buffs

The Warrior's shield critical-down (dcri) and two-hand return (dmgr), the
Rogue's bow absorb (odar), Mad Bow and Dagger Up (pmdp) and the Warlock's
Soul Return (dmgr on a buff) were refused by their qualifiers. Each now
installs its native block; damage return strikes the attacker back.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestEuropeanPassivesQualify
================
*/
func TestEuropeanPassivesQualify(t *testing.T) {
	if p := shippedOffense(t, "SKILL_EU_WARRIOR_SHIELDP_CRITICALDOWN_A_01").PassiveParameters; !p.Pinned || p.CriticalEvasion == 0 {
		t.Fatalf("shield critical-down = %+v", p)
	}
	if p := shippedOffense(t, "SKILL_EU_ROG_BOWP_ABSORB_A_01").PassiveParameters; !p.Pinned || !p.IncomingReduction {
		t.Fatalf("bow absorb = %+v", p)
	}
	ret := shippedOffense(t, "SKILL_EU_WARRIOR_TWOHANDP_RETURN_A_01").PassiveParameters.DamageReturn
	if ret != (enterworld.SkillDamageReturn{Present: true, Chance: 20, Physical: 20, Magical: 0, Range: 100}) {
		t.Fatalf("two-hand return = %+v", ret)
	}
	for _, code := range []string{"SKILL_EU_ROG_BOWP_MAD_BOW_UP_A_01", "SKILL_EU_ROG_POIS_DAGP_DAGGAR_UP_A_01"} {
		if e := shippedOffense(t, code).TimedEffect; !e.Pinned || !e.Attributes.DefensePenalty {
			t.Fatalf("%s timed effect = %+v", code, e)
		}
	}
	for _, code := range []string{"SKILL_EU_WARLOCK_SOULA_RETURN_A_01", "SKILL_EU_WARLOCK_SOULA_RETURN_B_01"} {
		if e := shippedOffense(t, code).TimedEffect; !e.Pinned || !e.DamageReturn.Present {
			t.Fatalf("%s timed effect = %+v", code, e)
		}
	}
}

/*
================
TestDefensePenaltyLowersBothDefenses

594AC0 0x596252: pmdp lowers parameters 5 and 6 on the percent channel.
================
*/
func TestDefensePenaltyLowersBothDefenses(t *testing.T) {
	a := shippedOffense(t, "SKILL_EU_ROG_BOWP_MAD_BOW_UP_A_01").TimedEffect.Attributes
	lowered := map[uint16]bool{}
	for _, w := range combat.AttributeEffectWrites(a) {
		if (w.Parameter == 5 || w.Parameter == 6) && w.Value < 0 {
			lowered[w.Parameter] = true
		}
	}
	if a.PhysicalDefensePenalty != 0 && !lowered[5] || a.MagicalDefensePenalty != 0 && !lowered[6] {
		t.Fatalf("pmdp %+v lowered %v", a, lowered)
	}
}

/*
================
TestShieldPassiveEvadesCriticals

A learned dcri raises the defender's 0x39, and 58EB64 divides the
attacker's critical byte by (1 + 0x39 / 100).
================
*/
func TestShieldPassiveEvadesCriticals(t *testing.T) {
	rt, c, _, _, _ := arrowFixture(t)
	row := shippedOffense(t, "SKILL_EU_WARRIOR_SHIELDP_CRITICALDOWN_A_01")
	row.Reqi = enterworld.SkillReqi{}
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	evade, _ := stats.Param(0x39)
	if uint32(evade) != row.PassiveParameters.CriticalEvasion {
		t.Fatalf("0x39 = %v, want %d", evade, row.PassiveParameters.CriticalEvasion)
	}
	want := uint8(30 / (1 + float64(evade)/100))
	if got := combat.EvadedCriticalRate(30, stats); got != want {
		t.Fatalf("evaded rate = %d, want %d", got, want)
	}
}

/*
================
TestBowAbsorbLowersIncomingDamage
================
*/
func TestBowAbsorbLowersIncomingDamage(t *testing.T) {
	rt, c, _, _, _ := arrowFixture(t)
	before, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	// The row's reqi asks for a crossbow; the fixture wears a bow.
	row := shippedOffense(t, "SKILL_EU_ROG_BOWP_ABSORB_A_01")
	row.Reqi = enterworld.SkillReqi{}
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	after, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	cut := false
	was := [...]float32{before.PhysicalBasicTaken, before.PhysicalSkillTaken, before.MagicalBasicTaken, before.MagicalSkillTaken}
	now := [...]float32{after.PhysicalBasicTaken, after.PhysicalSkillTaken, after.MagicalBasicTaken, after.MagicalSkillTaken}
	for i := range was {
		cut = cut || now[i] != was[i]
	}
	if !cut {
		t.Fatal("bow absorb lowered no incoming-damage keeper")
	}
}

/*
================
TestReturnedDamageArithmetic

5A0C2D: each lane share truncates on its own, and range must exceed the
distance.
================
*/
func TestReturnedDamageArithmetic(t *testing.T) {
	rule := enterworld.SkillDamageReturn{Present: true, Chance: 20, Physical: 95, Magical: 30, Range: 100}
	hit := combat.Result{Damage: 150, PhysicalDamage: 101, MagicalDamage: 49}
	if got := combat.ReturnedDamage(rule, hit); got != 95+14 {
		t.Fatalf("returned = %d, want 109", got)
	}
	if combat.DamageReturnInRange(rule, 100) || !combat.DamageReturnInRange(rule, 99.5) {
		t.Fatal("range must strictly exceed the distance")
	}
	if combat.DamageReturnApplies(rule, combat.Result{Blocked: true}) {
		t.Fatal("a blocked hit returns nothing")
	}
}

/*
================
damageReturnHit

A monster strikes c once; returns the monster's HP before and after and
the strike's frames.
================
*/
func damageReturnHit(t *testing.T, install func(*Runtime, *enterworld.Character)) (uint32, uint32, bool) {
	t.Helper()
	rt, clock, c, mob := newCombatTestRuntime(t, 5000)
	mob.Ref.DefaultSkillIDs[0] = 2
	c.CurrentHP = testInt64(100000)
	skills := rt.deps.SkillData().(staticSkillSource)
	strike := skills[2]
	strike.Attack.Min, strike.Attack.Max, strike.Attack.Percent = 100, 100, 100
	skills[2] = strike
	install(rt, c)
	before, _ := rt.Monsters.Get(testDivision, mob.Gid)
	result := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !result.Accepted {
		t.Fatal("monster attack refused")
	}
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	pulsed := false
	for _, f := range result.Frames {
		pulsed = pulsed || f.Opcode == wire.OpSkillPulse
	}
	return before.CurrentHP, after.CurrentHP, pulsed
}

/*
================
TestTwoHandReturnStrikesTheAttacker
================
*/
func TestTwoHandReturnStrikesTheAttacker(t *testing.T) {
	learn := func(rule enterworld.SkillDamageReturn) func(*Runtime, *enterworld.Character) {
		return func(rt *Runtime, c *enterworld.Character) {
			row := shippedOffense(t, "SKILL_EU_WARRIOR_TWOHANDP_RETURN_A_01")
			row.Reqi = enterworld.SkillReqi{}
			row.PassiveParameters.DamageReturn = rule
			rt.deps.SkillData().(staticSkillSource)[row.ID] = row
			c.Skills = append(c.Skills, row.ID)
		}
	}
	before, after, pulsed := damageReturnHit(t, learn(enterworld.SkillDamageReturn{Present: true, Chance: 100, Physical: 50, Range: 1000}))
	if after >= before || !pulsed {
		t.Fatalf("monster HP %d -> %d, pulse %v: the return did not land", before, after, pulsed)
	}
	before, after, pulsed = damageReturnHit(t, learn(enterworld.SkillDamageReturn{Present: true, Chance: 100, Physical: 50, Range: 1}))
	if after != before || pulsed {
		t.Fatalf("out of range: monster HP %d -> %d, pulse %v", before, after, pulsed)
	}
}

/*
================
TestSoulReturnBuffStrikesTheAttacker

Without an enabled passive, 5A0C2D falls back to the buff's rule (+0x208).
================
*/
func TestSoulReturnBuffStrikesTheAttacker(t *testing.T) {
	before, after, pulsed := damageReturnHit(t, func(rt *Runtime, c *enterworld.Character) {
		row := shippedOffense(t, "SKILL_EU_WARLOCK_SOULA_RETURN_A_01")
		row.TimedEffect.DamageReturn.Chance = 100
		rt.deps.SkillData().(staticSkillSource)[row.ID] = row
		if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: row.ID,
			SkillGroup: row.Group, InstanceToken: 7, State: statuseffect.StateActive}) {
			t.Fatal("Soul Return did not install")
		}
	})
	if after >= before || !pulsed {
		t.Fatalf("monster HP %d -> %d, pulse %v: the buff did not return", before, after, pulsed)
	}
}
