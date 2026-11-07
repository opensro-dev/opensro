package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestElementalWalkFactsAgreeWithTheCopy

The eligibility walk's door-read refusal (#2030) must answer exactly
what the copy path answers. Each case runs twice on one runtime: once
with the facts live, and once behind the forced-copy seam, which makes
the facts abstain and the admitted copy judge as before. A refusal the
facts invent - or miss - shows up as a verdict split.
================
*/
func TestElementalWalkFactsAgreeWithTheCopy(t *testing.T) {
	verdict := func(t *testing.T, rt *Runtime, c *enterworld.Character, q elementalQuery, now int64) bool {
		t.Helper()
		_, factsOK := rt.elementalCompanionMatching(testDivision, c, q, now)
		rt.companionCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			return character.SnapshotCompanion()
		}
		_, copyOK := rt.elementalCompanionMatching(testDivision, c, q, now)
		rt.companionCopy = nil
		if factsOK != copyOK {
			t.Fatalf("verdict split: facts %v, copy %v", factsOK, copyOK)
		}
		return factsOK
	}

	// A wolf query against a raven: the record filter refuses.
	t.Run("kind refusal", func(t *testing.T) {
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		q := elementalQuery{gid: c.ActiveCOS.GID, kind: "Wolf"}
		if verdict(t, rt, c, q, clock.NowMs()) {
			t.Fatal("wolf query admitted a raven")
		}
	})

	// A crow without the passive slotted: the passive filter refuses.
	t.Run("passive refusal", func(t *testing.T) {
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		q := elementalQuery{gid: c.ActiveCOS.GID, kind: "Crow", passive: "Tailwind"}
		if verdict(t, rt, c, q, clock.NowMs()) {
			t.Fatal("crow without Tailwind admitted")
		}
	})

	// A dead pet: the life filter refuses.
	t.Run("dead refusal", func(t *testing.T) {
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		c.ActiveCOS.CurrentHP = 0
		q := elementalQuery{gid: c.ActiveCOS.GID, kind: "Crow"}
		if verdict(t, rt, c, q, clock.NowMs()) {
			t.Fatal("dead crow admitted")
		}
	})

	// An eligible crow: both paths admit, and the facts must not
	// refuse what the copy selects.
	t.Run("eligible agrees", func(t *testing.T) {
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		q := elementalQuery{gid: c.ActiveCOS.GID, kind: "Crow"}
		if !verdict(t, rt, c, q, clock.NowMs()) {
			t.Fatal("eligible crow refused")
		}
	})
}
