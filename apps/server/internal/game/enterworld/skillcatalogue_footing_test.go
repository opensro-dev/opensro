/*
===========================================================================

skillcatalogue_footing_test.go - the footing gate in the client catalogue

===========================================================================
*/

package enterworld

import "testing"

/*
================
TestCatalogueMarksFootingRows

Every shipped row whose cast gate is ao or pw (58E0BF refuses it to a rider
with 0x3009) is projected with needsFooting, and no other row is.
================
*/
func TestCatalogueMarksFootingRows(t *testing.T) {
	skills := sharedShippedSkills(t)
	footed := 0
	for _, row := range skills.SpawnSkillRows() {
		if row.UI == nil {
			continue
		}
		source, ok := skills.SkillByID(row.ID)
		if !ok {
			t.Fatalf("projected row %d has no source", row.ID)
		}
		want := source.CastGate.Ao || source.CastGate.Pw
		if row.UI.NeedsFooting != want {
			t.Fatalf("row %d needsFooting %v, cast gate ao %v pw %v", row.ID, row.UI.NeedsFooting, source.CastGate.Ao, source.CastGate.Pw)
		}
		if want {
			footed++
		}
	}
	if footed == 0 {
		t.Fatal("no shipped player row carries a footing gate")
	}
}
