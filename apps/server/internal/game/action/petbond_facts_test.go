/*
===========================================================================

petbond_facts_test.go - the bond refusal judged on door-read values (#2030)

bondSource took the pet-owner copy before learning that the bear was
unsummoned, expansion-refused, or had no Guardian's Bond passive
slotted. The refusal now answers from door-read facts; an admitted
source still takes the copy. Each case runs twice on one runtime:
once with the facts live, and once behind the forced-copy seam, which
makes the facts abstain and the copy judge as before. A refusal the
facts invent - or miss - shows up as a verdict split.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestBondSourceFactsAgreeWithTheCopy
================
*/
func TestBondSourceFactsAgreeWithTheCopy(t *testing.T) {
	verdict := func(t *testing.T, rt *Runtime, key petOwnerKey, state *petSession) (bool, uint8, uint8) {
		t.Helper()
		_, _, factsSkill, factsLevel, factsOK := rt.bondSource(key, state)
		rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			return character.SnapshotCompanion()
		}
		_, _, copySkill, copyLevel, copyOK := rt.bondSource(key, state)
		rt.companionCopy = nil
		if factsOK != copyOK || factsSkill.ID != copySkill.ID || factsLevel != copyLevel {
			t.Fatalf("verdict split: facts ok=%v skill=%d level=%d, copy ok=%v skill=%d level=%d",
				factsOK, factsSkill.ID, factsLevel, copyOK, copySkill.ID, copyLevel)
		}
		return factsOK, factsSkill.ID, factsLevel
	}
	bear := func(t *testing.T) (*Runtime, *enterworld.Character, petOwnerKey, *petSession) {
		t.Helper()
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		step, ok := rt.elementalCompanion(testDivision, c, c.ActiveCOS.GID, "Bear", clock.NowMs())
		if !ok {
			t.Fatal("bear lost eligibility")
		}
		return rt, c, step.key, step.state
	}

	// A bear with no Guardian's Bond book: the passive scan refuses.
	t.Run("no passive refusal", func(t *testing.T) {
		rt, _, key, state := bear(t)
		if ok, _, _ := verdict(t, rt, key, state); ok {
			t.Fatal("bear without Guardian's Bond became a bond source")
		}
	})

	// A bear that is not summoned: the summoned gate refuses.
	t.Run("unsummoned refusal", func(t *testing.T) {
		rt, c, key, state := bear(t)
		skill := learnPetBook(t, c, 26)
		c.ActiveCOS.Training.Levels[skill.Index()] = 10
		c.ActiveCOS.Summoned = false
		if ok, _, _ := verdict(t, rt, key, state); ok {
			t.Fatal("unsummoned bear became a bond source")
		}
	})

	// A dead bear: the admission's life test refuses.
	t.Run("dead refusal", func(t *testing.T) {
		rt, c, key, state := bear(t)
		skill := learnPetBook(t, c, 26)
		c.ActiveCOS.Training.Levels[skill.Index()] = 10
		c.ActiveCOS.CurrentHP = 0
		if ok, _, _ := verdict(t, rt, key, state); ok {
			t.Fatal("dead bear became a bond source")
		}
	})

	// An owner pending deletion: the admission refuses.
	t.Run("delete-pending refusal", func(t *testing.T) {
		rt, c, key, state := bear(t)
		skill := learnPetBook(t, c, 26)
		c.ActiveCOS.Training.Levels[skill.Index()] = 10
		c.DeletePending = true
		if ok, _, _ := verdict(t, rt, key, state); ok {
			t.Fatal("deletion-pending owner became a bond source")
		}
	})

	// An eligible bear: both paths admit with the same skill and level,
	// and the facts must not refuse what the copy selects.
	t.Run("eligible agrees", func(t *testing.T) {
		rt, c, key, state := bear(t)
		skill := learnPetBook(t, c, 26)
		c.ActiveCOS.Training.Levels[skill.Index()] = 10
		ok, skillID, level := verdict(t, rt, key, state)
		if !ok || skillID != skill.ID || level == 0 {
			t.Fatalf("eligible bear refused: ok=%v skill=%d level=%d", ok, skillID, level)
		}
	})

	// The production shape: the bear's record rides in a bag row. The
	// facts' companion walk must find it there, exactly as the copy's
	// resolution does.
	t.Run("bag-row bear agrees", func(t *testing.T) {
		rt, c, key, state := bear(t)
		skill := learnPetBook(t, c, 26)
		c.ActiveCOS.Training.Levels[skill.Index()] = 10
		movePetToBagRow(c, 100)
		ok, skillID, _ := verdict(t, rt, key, state)
		if !ok || skillID != skill.ID {
			t.Fatalf("bag-row bear refused: ok=%v skill=%d", ok, skillID)
		}
	})
}

/*
================
BenchmarkAdvancePetBondRefusedOwner

The revival walk for an owner whose pet is no Guardian's Bond bear,
refused on door-read facts: what every pet owner's tick pays when no
bond can fire. Judging the refusal before bondSource's answer comes
home keeps it allocation-free; taking the SkillDef home first cost
160 B and one allocation per tick per owner (#2030).
================
*/
func BenchmarkAdvancePetBondRefusedOwner(b *testing.B) {
	owner := petTickBenchOwner(1)
	deps := &enterworld.Deps{Characters: enterworld.StaticCharacterSource{testDivision: {owner}}, Items: testItems()}
	rt := NewRuntime(deps, nil)
	rt.AttackPetExpansion = true
	rt.BindPetSession(testDivision, owner, 1)
	key := petOwnerKey{testDivision, owner.Name, owner.ActiveCOS.GID}
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if state == nil {
		b.Fatal("no pet session")
	}
	b.ReportAllocs()
	for b.Loop() {
		if frames := rt.advancePetBond(key, state, 1_000_000); frames != nil {
			b.Fatal("a refused owner produced bond frames")
		}
	}
}
