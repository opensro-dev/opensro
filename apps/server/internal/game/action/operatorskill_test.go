/*
===========================================================================

operatorskill_test.go - /GMSKILL's operator Harmony through the real cast

The client's /GMSKILL casts SKILL_OP_HARMONY_A_01 with an ordinary skill
press (enterworld/operatorskill.go). A GM who never learned it plants
its field; a player is refused at admission.

===========================================================================
*/
package action

import (
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

const operatorHarmonyCode = "SKILL_OP_HARMONY_A_01"

/*
================
operatorHarmonyFixture

The caster with the operator row in the runtime's table but not in its
learned list.
================
*/
func operatorHarmonyFixture(t *testing.T, gm bool) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow) {
	t.Helper()
	rt, clock, c := concealmentFixture(t)
	row := learnShipped(t, rt, c, shippedOffense(t, operatorHarmonyCode).ID)
	c.Skills = slices.DeleteFunc(c.Skills, func(id uint32) bool { return id == row.ID })
	c.GMPrivilege = gm
	if err := rt.admitPopulationSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	return rt, clock, c, row
}

/*
================
TestGmCastsOperatorHarmonyUnlearned
================
*/
func TestGmCastsOperatorHarmonyUnlearned(t *testing.T) {
	rt, clock, c, row := operatorHarmonyFixture(t, true)
	if result := castSelf(rt, c, row.ID); result.DiagnosticRefusal != "" {
		t.Fatalf("GM's operator Harmony refused: %+v", result)
	}
	if row.ActionCastingTimeMs > 0 {
		clock.Advance(time.Duration(row.ActionCastingTimeMs+1) * time.Millisecond)
		if len(rt.advanceProjectileCasts(clock.NowMs())) == 0 {
			t.Fatal("operator Harmony never released")
		}
	}
	fieldPass(rt, clock)
	if !hasSkillEffect(rt, c.Name, row.ID) {
		t.Fatal("the GM holds no operator Harmony after the field pass")
	}
}

/*
================
TestPlayerCannotCastOperatorHarmony
================
*/
func TestPlayerCannotCastOperatorHarmony(t *testing.T) {
	rt, clock, c, row := operatorHarmonyFixture(t, false)
	if result := castSelf(rt, c, row.ID); result.DiagnosticRefusal == "" {
		t.Fatalf("a player's operator Harmony was admitted: %+v", result)
	}
	clock.Advance(time.Duration(row.ActionCastingTimeMs+1) * time.Millisecond)
	rt.advanceProjectileCasts(clock.NowMs())
	fieldPass(rt, clock)
	if hasSkillEffect(rt, c.Name, row.ID) {
		t.Fatal("a player holds operator Harmony")
	}
}
