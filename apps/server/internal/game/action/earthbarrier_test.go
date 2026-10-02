/*
===========================================================================

earthbarrier_test.go - Wizard Earth Barrier through the timed area owner

The caster-and-party area buff installs only its authored odar reduction
and retires it on expiry.

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
TestEarthBarrierReducesPhysicalDamageTaken

odar 4 30 normalizes to the physical lanes (0xAE, 0xAF) as a negated
percentage product (factor 0.7); the magical lanes stay unset.
================
*/
func TestEarthBarrierReducesPhysicalDamageTaken(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, "SKILL_EU_WIZARD_EARTHA_GUARD_A_01")
	if !skill.TimedEffect.Pinned || !skill.TimedEffect.Area.Present {
		t.Fatalf("Earth Barrier not admitted: %+v", skill.TimedEffect)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = int64(skill.RequiredWeaponKinds[0])
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	base, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}

	// An instant cast (column 12 is zero) installs at once.
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 {
		t.Fatalf("Earth Barrier refused: %+v", out)
	}
	now := clock.NowMs()
	rt.advanceProjectileCasts(now + int64(skill.ActionCastingTimeMs) + 1)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatalf("Earth Barrier was not installed on its caster: %+v", out)
	}
	buffed, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range []struct {
		parameter uint16
		reduced   bool
	}{{0xae, true}, {0xaf, true}, {0xb0, false}, {0xb1, false}} {
		before, _ := base.Param(lane.parameter)
		after, _ := buffed.Param(lane.parameter)
		// The keeper reports a product lane as its factor; zero means unset.
		want := before
		if lane.reduced {
			want = 0.7
		}
		if after != want {
			t.Errorf("parameter %x = %v, want %v (base %v)", lane.parameter, after, want, before)
		}
	}

	// The combat snapshot carries the factor into the damage formula.
	if buffed.PhysicalBasicTaken != 0.7 || buffed.PhysicalSkillTaken != 0.7 || buffed.MagicalSkillTaken != 0 {
		t.Errorf("taken factors %v/%v/%v", buffed.PhysicalBasicTaken, buffed.PhysicalSkillTaken, buffed.MagicalSkillTaken)
	}

	rt.effects.Expire(now + int64(skill.ActionCastingTimeMs) + 1 + int64(skill.EffectDurationMs) + 1)
	rt.drainStoppedCharacterEffects()
	after, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, parameter := range []uint16{0xae, 0xaf} {
		want, _ := base.Param(parameter)
		got, _ := after.Param(parameter)
		if got != want {
			t.Errorf("retired parameter %x = %v, want %v", parameter, got, want)
		}
	}
}
