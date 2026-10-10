/*
===========================================================================

illusion_test.go - the Warlock's Illusion through the timed self owner

msch 3 has no server work (594AA4 skips modes 3 and 4): the cast installs
its instance on the caster for the row's duration, the client draws the
disguise, and skc's event mask 2 ends it on the caster's next cast.

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
TestIllusionInstallsOnTheCasterUntilItsNextCast
================
*/
func TestIllusionInstallsOnTheCasterUntilItsNextCast(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, "SKILL_EU_WARLOCK_CONFUSIONA_ILLUSION_A_01")
	if !skill.TimedEffect.Pinned || !skill.TimedEffect.Disguise || skill.TimedEffect.Targeted {
		t.Fatalf("Illusion not admitted: %+v", skill.TimedEffect)
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

	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 {
		t.Fatalf("Illusion refused: %q %+v", out.DiagnosticRefusal, out.Frames)
	}
	now := clock.NowMs()
	rt.advanceProjectileCasts(now + int64(skill.ActionCastingTimeMs) + 1)
	effects := rt.effects.Snapshot(testDivision, c.Name)
	if len(effects) != 1 || effects[0].SkillID != skill.ID || effects[0].EventCancelMask != effectEventSkillCast {
		t.Fatalf("Illusion was not installed on its caster: %+v", effects)
	}
	// A move does not end it; the next cast does.
	rt.retireEffectsOnEvent(testDivision, c, effectEventMove, now)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatal("a move ended the Illusion")
	}
	rt.retireEffectsOnEvent(testDivision, c, effectEventSkillCast, now)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("the next cast left the Illusion in place")
	}
}
