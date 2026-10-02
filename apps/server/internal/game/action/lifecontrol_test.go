/*
===========================================================================

lifecontrol_test.go - Wizard Life Control through the timed self-effect owner

The buff trades half of the maximum HP for magical attack and damage rate,
and retires every contribution together.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestLifeControlTradesMaximumHPForMagicalDamage

apau raises both magical bounds, dru the magical damage rates and pmhp halves
the maximum HP. Expiry restores every parameter.
================
*/
func TestLifeControlTradesMaximumHPForMagicalDamage(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, "SKILL_EU_WIZARD_MENTALA_DAMAGEUP_A_01")
	if !skill.TimedEffect.Pinned {
		t.Fatalf("Life Control not admitted: %q", skill.OffenseRefusal)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	// reqi 6 11: the Wizard's staff.
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 11
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	base, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}

	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	out = assertAndSeparateActionSession(t, out)
	assertOpcodes(t, out.Frames, wire.OpSkillCastResult)
	released := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
	rt.advanceProjectileCasts(released)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatal("Life Control was not installed")
	}
	buffed, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	baseHP, _ := base.Param(3)
	buffedHP, _ := buffed.Param(3)
	if buffedHP != baseHP*0.5 {
		t.Errorf("maximum HP %v, want half of %v", buffedHP, baseHP)
	}
	for _, parameter := range []uint16{0x0f, 0x10} {
		before, _ := base.Param(parameter)
		after, _ := buffed.Param(parameter)
		if after != before+59 {
			t.Errorf("magical attack %x = %v, want %v", parameter, after, before+59)
		}
	}
	if buffed.MagicalBasicRate != base.MagicalBasicRate+25 || buffed.MagicalSkillRate != base.MagicalSkillRate+25 ||
		buffed.PhysicalSkillRate != base.PhysicalSkillRate {
		t.Errorf("damage rates %v/%v/%v, base %v/%v/%v", buffed.MagicalBasicRate, buffed.MagicalSkillRate,
			buffed.PhysicalSkillRate, base.MagicalBasicRate, base.MagicalSkillRate, base.PhysicalSkillRate)
	}

	rt.effects.Expire(released + int64(skill.EffectDurationMs) + 1)
	rt.drainStoppedCharacterEffects()
	after, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, parameter := range []uint16{3, 0x0f, 0x10} {
		want, _ := base.Param(parameter)
		got, _ := after.Param(parameter)
		if got != want {
			t.Errorf("retired parameter %x = %v, want %v", parameter, got, want)
		}
	}
	if after.MagicalSkillRate != base.MagicalSkillRate {
		t.Errorf("retired magical rate %v, want %v", after.MagicalSkillRate, base.MagicalSkillRate)
	}
}
