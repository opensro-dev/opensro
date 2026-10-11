/*
===========================================================================

resident_references_test.go - the default resident catalogues behave as bounded

Production keeps the loaded skill and item rows resident (no
UseBoundedCache) unless SRO_BOUNDED_CATALOGUE=1. A resident catalogue must
close as a no-op, iterate the same rows, and carry the stack-size raise.

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestResidentReferencesMatchBoundedIteration
================
*/
func TestResidentReferencesMatchBoundedIteration(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	resident, bounded := NewTextdataItems(dir), NewTextdataItems(dir)
	sizes := StackSizes{"potion": 2000, "elixir": 50}
	for _, items := range []*TextdataItems{resident, bounded} {
		if raised, err := items.ApplyStackSizes(sizes); err != nil || raised < 40 {
			t.Fatalf("raise: %d rows, %v", raised, err)
		}
	}
	if err := bounded.UseBoundedCache(16); err != nil {
		t.Fatal(err)
	}
	defer bounded.Close()
	if err := resident.Close(); err != nil {
		t.Fatalf("resident item close: %v", err)
	}

	// The same rows, by codename, in both modes; the raise reached both.
	seen := make(map[string]float64)
	for row := range resident.itemRows() {
		seen[row.Codename] = row.NativeFields.Get("maxStack")
	}
	count := 0
	for row := range bounded.itemRows() {
		count++
		if cap, ok := seen[row.Codename]; !ok || cap != row.NativeFields.Get("maxStack") {
			t.Fatalf("%s: resident %v/%v, bounded %v", row.Codename, cap, ok, row.NativeFields.Get("maxStack"))
		}
	}
	if count != len(seen) || resident.Len() != bounded.Len() {
		t.Fatalf("resident %d rows (%d), bounded %d (%d)", len(seen), resident.Len(), count, bounded.Len())
	}
	potion, ok := resident.ItemRefByCodename("ITEM_ETC_HP_POTION_01")
	if !ok || potion.NativeFields.Get("maxStack") != 2000 || potion.NativeStackCap() != 50 {
		t.Fatal("the resident potion lost its raised cap or its native one")
	}

	skills := NewTextdataSkills(dir)
	if err := skills.Load(); err != nil {
		t.Fatal(err)
	}
	if err := skills.Close(); err != nil {
		t.Fatalf("resident skill close: %v", err)
	}
	if _, ok := skills.SkillByID(1); !ok && len(skills.rows.hot) == 0 {
		t.Fatal("resident skills hold no rows")
	}
}
