/*
===========================================================================

skillcatalogue_chain_test.go - sequence roots in the client catalogue

===========================================================================
*/

package enterworld

import "testing"

/*
================
TestCatalogueMarksChainRoots

Every projected sequence root (ChainNext set, not a stage) carries chain,
and no other row does: the client holds a chain root's cast until the
server closes it, past the root row's own action lifetime.
================
*/
func TestCatalogueMarksChainRoots(t *testing.T) {
	skills := sharedShippedSkills(t)
	roots := 0
	for _, row := range skills.SpawnSkillRows() {
		if row.UI == nil {
			continue
		}
		source, ok := skills.SkillByID(row.ID)
		if !ok {
			t.Fatalf("projected row %d has no source", row.ID)
		}
		want := source.ChainNext != 0 && !source.ChainSub
		if row.UI.Chain != want {
			t.Fatalf("row %d chain %v, chain next %d sub %v", row.ID, row.UI.Chain, source.ChainNext, source.ChainSub)
		}
		if want {
			roots++
		}
	}
	if roots == 0 {
		t.Fatal("no shipped player row roots a chain")
	}
}
