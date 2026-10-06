/*
===========================================================================

autopotion_catalog_test.go - every authored item reachable from auto-potion

Client 5503A0 accepts recovery and cure families. Enumerate the complete
licensed v1.150 catalog rather than inferring coverage from potion names.
Each reference must reach a supported server family with valid parameters.

===========================================================================
*/
package action

import (
	"testing"
)

/*
================
TestAutoPotionEntireShippedCatalog
================
*/
func TestAutoPotionEntireShippedCatalog(t *testing.T) {
	items := shippedItems(t)
	want := map[[2]int64]int{
		{1, 1}: 12, {1, 2}: 12, {1, 3}: 17, {1, 4}: 3,
		{1, 6}: 2, {1, 8}: 3, {1, 9}: 1, {1, 10}: 3,
		{2, 1}: 5, {2, 6}: 4, {2, 7}: 2,
	}
	counts := make(map[[2]int64]int)
	for _, candidate := range items.ItemCommandReferences() {
		ref, found := items.ItemRefByID(candidate.RefObjID)
		if !found {
			t.Fatalf("reference %d disappeared", candidate.RefObjID)
		}
		id := ref.TypeIDs
		if id[0] != 3 || id[1] != 3 || (id[2] != 1 && id[2] != 2) {
			continue
		}
		counts[[2]int64{id[2], id[3]}]++
		t.Run(ref.Codename, func(t *testing.T) {
			family := admittedItemUseFamily(ref)
			if family == itemUseUnsupported {
				t.Fatal("authored auto-potion reference has no server handler")
			}
			if ref.NativeFields.Get("canUse") == 0 {
				t.Fatal("authored reference requires a new client permission branch")
			}
			if family == itemUseRecovery {
				for _, level := range []uint8{1, 5, 20, 90} {
					amount, valid := computePotionAmount(ref, level, 20, 20, 1000, 1000)
					if !valid || (amount.hp <= 0 && amount.mp <= 0) {
						t.Fatalf("level %d has no valid authored recovery: %+v", level, amount)
					}
				}
			}
		})
	}
	for family, count := range want {
		if counts[family] != count {
			t.Errorf("family %v: got %d references, audited %d", family, counts[family], count)
		}
	}
	for family := range counts {
		if _, found := want[family]; !found {
			t.Errorf("unaudited authored family %v", family)
		}
	}
}
