/*
===========================================================================

itemrefs_cure_test.go - shipped cure items load their parameters

===========================================================================
*/

package enterworld

import "testing"

// The shipped rows pin the cure params (the curable-type, cure-level and
// cure-chance labels and the six cure-point columns): Param1..6 are
// columns 118..128.
func TestShippedCureItemsLoadTheirParams(t *testing.T) {
	items := sharedShippedItems(t)
	pill, ok := items.ItemRefByCodename("ITEM_ETC_CURE_RANDOM_01")
	if !ok {
		t.Fatal("universal pill row missing")
	}
	if pill.CureMask != 25145280 || pill.CureGradeSub != 3 || pill.CureChance != 100 {
		t.Fatalf("pill mask %d grade %d chance %d", pill.CureMask, pill.CureGradeSub, pill.CureChance)
	}
	all, ok := items.ItemRefByCodename("ITEM_ETC_CURE_ALL_01")
	if !ok {
		t.Fatal("level cure row missing")
	}
	if all.CureLevels != [6]int64{36, 36, 36, 36, 36, 36} {
		t.Fatalf("level cure points %v", all.CureLevels)
	}
}
