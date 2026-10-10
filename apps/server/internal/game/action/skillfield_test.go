/*
===========================================================================

skillfield_test.go - Harmony therapy's buff field through the real owners

Drive the production cast, release and skill-object tick with the shipped
SKILL_CH_WATER_HARMONY_A_01 row: the field holds everyone it selects while
they stand in it and lets go of whoever leaves or outlives it.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

const harmonyFirstCode = "SKILL_CH_WATER_HARMONY_A_01"

/*
================
harmonyFixture

The caster out of battle with Harmony therapy learned, in an admitted
population so the field can stand.
================
*/
func harmonyFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow) {
	t.Helper()
	row := shippedOffense(t, harmonyFirstCode)
	rt, clock, c := concealmentFixture(t)
	row = learnShipped(t, rt, c, row.ID)
	if !row.TimedEffect.Pinned || !row.TimedEffect.Field.Present {
		t.Fatalf("Harmony therapy not admitted as a field: %+v", row.TimedEffect)
	}
	if err := rt.admitPopulationSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	return rt, clock, c, row
}

/*
================
releaseHarmony

Cast and, when the row prepares, release at the prepared boundary.
================
*/
func releaseHarmony(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character, row enterworld.SkillRow) {
	t.Helper()
	if result := castSelf(rt, c, row.ID); result.DiagnosticRefusal != "" {
		t.Fatalf("Harmony therapy refused: %+v", result)
	}
	if row.ActionCastingTimeMs > 0 {
		clock.Advance(time.Duration(row.ActionCastingTimeMs+1) * time.Millisecond)
		if len(rt.advanceProjectileCasts(clock.NowMs())) == 0 {
			t.Fatal("Harmony therapy never released")
		}
	}
}

/*
================
fieldPass

Advance to the next 48CEA0 pass and run the object tick.
================
*/
func fieldPass(rt *Runtime, clock *fakeClock) {
	clock.Advance(enterworld.SkillFieldScanMs * time.Millisecond)
	rt.AdvanceSkillObjects(clock.NowMs(), nil)
}

/*
================
TestHarmonyFieldHoldsItsRecipients

The release plants the object and installs nothing on the caster. The first
pass admits the caster, a party member and a non-hostile stranger in the
radius (select 7), never one past it; the instance carries irgc on the
recovery parameters. A recipient who steps out loses it on the next pass
and regains it on stepping back in; the field's end takes it from all.
================
*/
func TestHarmonyFieldHoldsItsRecipients(t *testing.T) {
	rt, clock, c, row := harmonyFixture(t)
	radius := float64(row.TimedEffect.Field.Radius)
	mate := nearbyCharacter(rt, c, 12, "harmony-mate", radius-1)
	stranger := nearbyCharacter(rt, c, 13, "harmony-stranger", 1)
	outside := nearbyCharacter(rt, c, 14, "harmony-outside", radius+1)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(mate)}}}
	}

	releaseHarmony(t, rt, clock, c, row)
	objects := rt.SkillObjects.Snapshot()
	if len(objects) != 1 || !objects[0].Program.Field || objects[0].Program.Radius != row.TimedEffect.Field.Radius {
		t.Fatalf("no field planted: %+v", objects)
	}
	if hasSkillEffect(rt, c.Name, row.ID) {
		t.Fatal("the release installed on the caster")
	}

	fieldPass(rt, clock)
	for name, want := range map[string]bool{c.Name: true, mate.Name: true, stranger.Name: true, outside.Name: false} {
		if hasSkillEffect(rt, name, row.ID) != want {
			t.Errorf("%s holds Harmony = %v, want %v", name, !want, want)
		}
	}
	recovery := false
	for _, w := range rt.effects.ModifierWrites(testDivision, mate.Name) {
		recovery = recovery || w.Parameter == itemParamHPRecovery && w.Channel == paramkeeper.PercentSum &&
			w.Value == float32(row.TimedEffect.Recovery.HP)
	}
	if !recovery {
		t.Fatalf("irgc missing from the recipient's writes: %+v", rt.effects.ModifierWrites(testDivision, mate.Name))
	}

	moveCharacter(rt, mate, c, radius+1)
	fieldPass(rt, clock)
	if hasSkillEffect(rt, mate.Name, row.ID) || !hasSkillEffect(rt, stranger.Name, row.ID) {
		t.Fatal("leaving the field did not end only the leaver's instance")
	}
	moveCharacter(rt, mate, c, 1)
	fieldPass(rt, clock)
	if !hasSkillEffect(rt, mate.Name, row.ID) {
		t.Fatal("returning to the field did not readmit")
	}

	clock.Advance(time.Duration(row.EffectDurationMs) * time.Millisecond)
	rt.AdvanceSkillObjects(clock.NowMs(), nil)
	if len(rt.SkillObjects.Snapshot()) != 0 {
		t.Fatal("field outlived its duration")
	}
	for _, name := range []string{c.Name, mate.Name, stranger.Name} {
		if hasSkillEffect(rt, name, row.ID) {
			t.Errorf("%s kept Harmony after the field retired", name)
		}
	}
}
