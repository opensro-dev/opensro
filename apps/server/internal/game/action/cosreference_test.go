/*
===========================================================================

cosreference_test.go - the satiety verdict read keeps the shared catalog row

The verdict cosReferenceVerdict resolves validity, band and satiety
minutes off the immutable catalog row; cosReference's MaxHP-adjusted
copies belong to the stats readers and must not leak into the tick's
per-pet read (#2033).

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

var (
	verdictSinkRef   *enterworld.CharacterRef
	verdictSinkValid bool
)

/*
================
TestCosReferenceVerdictSkipsTheMaxHPCopies

Across every pet shape the two reads agree on the verdict and on the
fields the satiety tick reads, while only cosReference ever leaves the
shared row: its expansion, brood and resolve branches each return a
private copy with an adjusted MaxHP.
================
*/
func TestCosReferenceVerdictSkipsTheMaxHPCopies(t *testing.T) {
	items := shippedItems(t)
	c := testCharacter()
	rt, _ := newTestRuntime(c, items)
	equipShippedPet(t, rt, c, items, "COS_P_WOLF_002")
	source, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		t.Fatal("test runtime has no character reference source")
	}
	catalog, found := source.CharacterRefByCodename(c.ActiveCOS.Codename)
	if !found || catalog == nil {
		t.Fatal("shipped wolf row missing")
	}
	base := c.ActiveCOS

	brood := *base
	brood.BroodStage = 2
	brood.BroodMaxHP = catalog.MaxHP + 12345

	resolved := *base
	resolved.ResolveStacks = 3

	stale := *base
	stale.RefObjID = catalog.RefObjID + 1

	unknown := *base
	unknown.Codename = "COS_P_NOPE_999"

	shapes := []struct {
		name      string
		expansion bool
		pet       *enterworld.CharacterCOS
		wantCopy  bool
	}{
		{"plain", false, base, false},
		{"expansion", true, base, true},
		{"brood", false, &brood, true},
		{"resolve", true, &resolved, true},
		{"stale identity", false, &stale, false},
		{"unknown codename", false, &unknown, false},
		{"no pet", false, nil, false},
	}
	for _, shape := range shapes {
		rt.AttackPetExpansion = shape.expansion
		verdict, verdictValid := rt.cosReferenceVerdict(shape.pet)
		full, fullValid := rt.cosReference(shape.pet)
		if verdictValid != fullValid {
			t.Fatalf("%s: verdict validity %v, full read %v", shape.name, verdictValid, fullValid)
		}
		if !verdictValid {
			if verdict != full {
				t.Fatalf("%s: invalid verdict returned %p, full read %p", shape.name, verdict, full)
			}
			continue
		}
		if verdict != catalog {
			t.Fatalf("%s: verdict left the shared row: %p, catalog %p", shape.name, verdict, catalog)
		}
		if verdict.TidWord != full.TidWord || verdict.SatietyMinutes != full.SatietyMinutes {
			t.Fatalf("%s: verdict fields (tid %d, minutes %d) disagree with full read (tid %d, minutes %d)",
				shape.name, verdict.TidWord, verdict.SatietyMinutes, full.TidWord, full.SatietyMinutes)
		}
		if shape.wantCopy && full == catalog {
			t.Fatalf("%s: full read returned the shared row where a MaxHP copy is owed", shape.name)
		}
		if !shape.wantCopy && full != catalog {
			t.Fatalf("%s: full read copied the row where the shared row is owed", shape.name)
		}
	}
	rt.AttackPetExpansion = false
	if broodRef, valid := rt.cosReference(&brood); !valid || broodRef.MaxHP != brood.BroodMaxHP {
		t.Fatalf("brood copy lost its stored max: %+v (valid %v)", broodRef, valid)
	}
	if got := testing.AllocsPerRun(100, func() {
		verdictSinkRef, verdictSinkValid = rt.cosReferenceVerdict(base)
	}); got != 0 {
		t.Fatalf("verdict read allocated %v per call, want 0", got)
	}
}
