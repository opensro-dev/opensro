/*
===========================================================================

itemgrade_test.go - equipment degree and seal from itemClass and rarity

===========================================================================
*/
package enterworld

import "testing"

/*
================
TestEquipmentDegreeAndSeal

itemdata rows of the 1.150 data: ITEM_CH_SWORD_08_A_RARE is class 22,
rarity 2 (8th degree, Star); B and C are 23 and 24.
================
*/
func TestEquipmentDegreeAndSeal(t *testing.T) {
	sword := [4]int64{3, 1, 6, 2}
	ref := func(typeIDs [4]int64, class, rarity float64) *ItemRef {
		return &ItemRef{TypeIDs: typeIDs, NativeFields: NewNativeFields(map[string]float64{"itemClass": class, "rarity": rarity})}
	}
	cases := []struct {
		class, rarity float64
		degree, tier  int
		sealed        bool
	}{
		{22, 2, 8, SealStar, true},
		{23, 2, 8, SealMoon, true},
		{24, 2, 8, SealSun, true},
		{22, 0, 8, 0, false},
		{1, 0, 1, 0, false},
		{33, 2, 11, SealSun, true},
	}
	for _, c := range cases {
		r := ref(sword, c.class, c.rarity)
		tier, sealed := EquipmentSeal(r)
		if EquipmentDegree(r) != c.degree || tier != c.tier || sealed != c.sealed {
			t.Errorf("class %v rarity %v = %d/%d/%v, want %d/%d/%v", c.class, c.rarity, EquipmentDegree(r), tier, sealed, c.degree, c.tier, c.sealed)
		}
	}
	if EquipmentDegree(ref([4]int64{3, 3, 1, 1}, 5, 2)) != 0 {
		t.Error("an expendable is not equipment")
	}
	if _, sealed := EquipmentSeal(ref([4]int64{3, 3, 1, 1}, 5, 2)); sealed {
		t.Error("an expendable has no seal")
	}
	if EquipmentDegree(ref(sword, 0, 0)) != 0 || EquipmentDegree(nil) != 0 {
		t.Error("a class-0 row and a nil reference have no degree")
	}
}

/*
================
TestItemDegree

The server's degree rule: equipment counts three classes a degree,
anything else's class is its degree.
================
*/
func TestItemDegree(t *testing.T) {
	ref := func(typeIDs [4]int64, class float64) *ItemRef {
		return &ItemRef{TypeIDs: typeIDs, NativeFields: NewNativeFields(map[string]float64{"itemClass": class})}
	}
	if d := ItemDegree(ref([4]int64{3, 1, 6, 2}, 22)); d != 8 {
		t.Errorf("class-22 sword is degree %d, want 8", d)
	}
	if d := ItemDegree(ref([4]int64{3, 3, 10, 2}, 5)); d != 5 {
		t.Errorf("class-5 alchemy stone is degree %d, want 5", d)
	}
	if ItemDegree(ref([4]int64{3, 3, 1, 1}, 0)) != 0 || ItemDegree(nil) != 0 {
		t.Error("a class-0 row and a nil reference have no degree")
	}
}
