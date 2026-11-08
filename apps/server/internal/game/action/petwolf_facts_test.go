package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
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

/*
================
TestArmedElementalWalkFactsAgree

The armed walk (#2030) must answer exactly what the unarmed walk
answers. Each case runs the same query twice on one runtime: once
with nothing armed, and once behind armElementalFacts, the capture
advancePetInDivision makes for the tailwind, crow-kit, plume and
wolf lanes. A refusal the facts invent - or miss - shows up as a
verdict split.
================
*/
func TestArmedElementalWalkFactsAgree(t *testing.T) {
	verdict := func(t *testing.T, rt *Runtime, c *enterworld.Character, q elementalQuery, now int64) bool {
		t.Helper()
		_, plainOK := rt.elementalCompanionMatching(testDivision, c, q, now)
		key := petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}
		armed := rt.armElementalFacts(key, now)
		_, armedOK := rt.elementalCompanionMatching(testDivision, c, q, now)
		rt.clearElementalFacts(armed)
		if plainOK != armedOK {
			t.Fatalf("verdict split: unarmed %v, armed %v", plainOK, armedOK)
		}
		return plainOK
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

	// An eligible crow: both paths admit.
	t.Run("eligible crow agrees", func(t *testing.T) {
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		q := elementalQuery{gid: c.ActiveCOS.GID, kind: "Crow"}
		if !verdict(t, rt, c, q, clock.NowMs()) {
			t.Fatal("eligible crow refused")
		}
	})

	// An eligible wolf: both paths admit.
	t.Run("eligible wolf agrees", func(t *testing.T) {
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_WOLF_040", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		q := elementalQuery{gid: c.ActiveCOS.GID, kind: "Wolf"}
		if !verdict(t, rt, c, q, clock.NowMs()) {
			t.Fatal("eligible wolf refused")
		}
	})

	// No world entry at arm time: the town verdict cannot be judged,
	// so the armed walk must abstain and fall through to the walk
	// that seeds and judges, not invent a verdict.
	t.Run("missing world abstains", func(t *testing.T) {
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		rt.Worlds.Forget(simulation.WorldKey(testDivision, c.Name))
		now := clock.NowMs()
		key := petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}
		armed := rt.armElementalFacts(key, now)
		facts, ok := rt.armedElementalFacts(testDivision, c, now)
		if !ok || facts.townJudged {
			t.Fatal("armed facts judged a town with no world entry")
		}
		rt.clearElementalFacts(armed)
		q := elementalQuery{gid: c.ActiveCOS.GID, kind: "Crow"}
		verdict(t, rt, c, q, now)
	})

	// A passive the capture never folded: the armed walk abstains
	// and the unarmed walk judges, instead of the facts inventing
	// a refusal.
	t.Run("unknown passive abstains", func(t *testing.T) {
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		now := clock.NowMs()
		q := elementalQuery{gid: c.ActiveCOS.GID, kind: "Crow", passive: "NoSuchPassive"}
		key := petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}
		armed := rt.armElementalFacts(key, now)
		facts, ok := rt.armedElementalFacts(testDivision, c, now)
		if !ok {
			t.Fatal("facts did not arm")
		}
		if _, answered := facts.walkRefused(q); answered {
			t.Fatal("facts answered a passive they never folded")
		}
		rt.clearElementalFacts(armed)
		if verdict(t, rt, c, q, now) {
			t.Fatal("unknown passive admitted")
		}
	})
}

/*
================
TestArmedElementalWalkServesTheGearProcShape

strikePetTargets fires the gear element and proc walks (petarea.go)
synchronously behind a committed strike, inside the armed window -
not from a lane head. Those walks call elementalCompanion with the
striking pet's own kind; behind one arm they must judge what the
unarmed walk judges, off the same one snapshot, even after the
strike's side of the record (here the pet's MP) moved.
================
*/
func TestArmedElementalWalkServesTheGearProcShape(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	rt.AttackPetExpansion = true
	roarField(t, rt, c, clock)
	now := clock.NowMs()
	gid := c.ActiveCOS.GID

	plain, ok := rt.elementalCompanion(testDivision, c, gid, "Bear", now)
	if !ok {
		t.Fatal("eligible bear refused unarmed")
	}
	key := petOwnerKey{division: testDivision, name: c.Name, gid: gid}
	armed := rt.armElementalFacts(key, now)
	if armed.division == "" {
		t.Fatal("facts did not arm")
	}
	defer rt.clearElementalFacts(armed)
	// The committed strike's record weather: fields the walk never
	// judges move inside the window.
	c.ActiveCOS.CurrentMP = 1
	served, ok := rt.elementalCompanion(testDivision, c, gid, "Bear", now)
	if !ok {
		t.Fatal("gear walk refused inside the window")
	}
	again, ok := rt.elementalCompanion(testDivision, c, gid, "Bear", now)
	if !ok || again.snapshot != served.snapshot || served.snapshot == plain.snapshot {
		t.Fatal("window walks must share the arm's one snapshot")
	}
}

/*
================
TestArmedElementalFactsShareOneSnapshot

Behind one arm the admitted lanes share the facts' owner snapshot -
the pointer is the proof: unarmed walks copy per call, armed walks
hand back the same copy - and clearing the arm makes the next walk
copy fresh and judge the record as it now stands.
================
*/
func TestArmedElementalFactsShareOneSnapshot(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
	rt.AttackPetExpansion = true
	roarField(t, rt, c, clock)
	now := clock.NowMs()
	q := elementalQuery{gid: c.ActiveCOS.GID, kind: "Crow"}

	first, ok := rt.elementalCompanionMatching(testDivision, c, q, now)
	if !ok {
		t.Fatal("eligible crow refused unarmed")
	}
	second, ok := rt.elementalCompanionMatching(testDivision, c, q, now)
	if !ok || second.snapshot == first.snapshot {
		t.Fatal("unarmed walks must copy per call")
	}

	key := petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}
	armed := rt.armElementalFacts(key, now)
	if armed.division == "" {
		t.Fatal("facts did not arm")
	}
	third, ok := rt.elementalCompanionMatching(testDivision, c, q, now)
	if !ok {
		t.Fatal("eligible crow refused armed")
	}
	anyCrow := elementalQuery{kind: "Crow"}
	fourth, ok := rt.elementalCompanionMatching(testDivision, c, anyCrow, now)
	if !ok || fourth.snapshot != third.snapshot {
		t.Fatal("armed lanes must share the one snapshot")
	}

	// The arm judges the captured record; clearing it hands the walk
	// back to the record as it now stands.
	c.ActiveCOS.CurrentHP = 0
	rt.clearElementalFacts(armed)
	if _, ok := rt.elementalCompanionMatching(testDivision, c, q, now); ok {
		t.Fatal("cleared arm must judge the record fresh")
	}
}

/*
================
TestArmedElementalFactsRearmJudgesFresh

A cleared capture may serve a later arm (armElementalFacts recycles
it), so a re-arm must judge the record as it now stands: a pet that
died between arms folds no row, the earlier tick's arm refuses, and
the shared snapshot is rebuilt instead of handing back the first
arm's. A stale row or snapshot leaking through the recycle shows up
here as an admission the record no longer earns.
================
*/
func TestArmedElementalFactsRearmJudgesFresh(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
	rt.AttackPetExpansion = true
	roarField(t, rt, c, clock)
	q := elementalQuery{gid: c.ActiveCOS.GID, kind: "Crow"}
	key := petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}

	first := clock.NowMs()
	armed := rt.armElementalFacts(key, first)
	served, ok := rt.elementalCompanionMatching(testDivision, c, q, first)
	if !ok {
		t.Fatal("eligible crow refused armed")
	}
	rt.clearElementalFacts(armed)

	// The pet dies; the re-arm folds the record as it now stands.
	c.ActiveCOS.CurrentHP = 0
	second := first + 100
	armed = rt.armElementalFacts(key, second)
	if _, ok := rt.armedElementalFacts(testDivision, c, first); ok {
		t.Fatal("stale arm answered for an earlier tick")
	}
	facts, ok := rt.armedElementalFacts(testDivision, c, second)
	if !ok {
		t.Fatal("re-armed facts missing")
	}
	if len(facts.pets) != 0 {
		t.Fatalf("re-armed facts carry %d stale pets", len(facts.pets))
	}
	if _, ok := rt.elementalCompanionMatching(testDivision, c, q, second); ok {
		t.Fatal("re-armed walk admitted a dead crow")
	}
	rt.clearElementalFacts(armed)

	// Revived, the pet is judged again - off a snapshot built for
	// this arm, not the first arm's.
	c.ActiveCOS.CurrentHP = 5000
	third := second + 100
	armed = rt.armElementalFacts(key, third)
	defer rt.clearElementalFacts(armed)
	fresh, ok := rt.elementalCompanionMatching(testDivision, c, q, third)
	if !ok {
		t.Fatal("revived crow refused armed")
	}
	if fresh.snapshot == served.snapshot {
		t.Fatal("re-armed walk handed back the first arm's snapshot")
	}
}
