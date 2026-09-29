/*
===========================================================================

consumables_test.go - loot behavior and lifecycle verification

===========================================================================
*/

package loot

import (
	"encoding/json"
	"errors"
	"testing"
)

/*
================
zeroRoll
================
*/
func zeroRoll() (uint32, error) { return 0, nil }

/*
================
TestAlchemyAndInferredConsumableSelections
================
*/
func TestAlchemyAndInferredConsumableSelections(t *testing.T) {
	for _, family := range []int{8, 9, 10} {
		for _, level := range []uint8{20, 50, 80, 90} {
			r, ok := SelectConsumable(family, level, 0, zeroRoll)
			if !ok || r.Codename == "" || r.Count != 1 {
				t.Fatalf("family %d level %d: %+v/%v", family, level, r, ok)
			}
			if _, ok := SelectConsumable(family, level, 999999, zeroRoll); ok {
				t.Fatal("class miss produced item")
			}
		}
	}
	for _, family := range []int{2, 3, 6} {
		for level := uint8(1); level <= 90; level++ {
			if _, ok := SelectConsumable(family, level, 0, zeroRoll); !ok {
				t.Fatal("inferred family missing at level", family, level)
			}
		}
	}
}

/*
================
TestPotionSelectionUsesAuthoredClassAndQuantity
================
*/
func TestPotionSelectionUsesAuthoredClassAndQuantity(t *testing.T) {
	var source consumableSource
	if err := json.Unmarshal(consumablesJSON, &source); err != nil {
		t.Fatal(err)
	}
	// This detached override must not change the generated reconstruction.
	clear(source.Classes[2][19])
	source.Classes[2][19][0] = 0.5
	c, err := compileConsumables(source)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := c.selectConsumable(2, 20, 0, zeroRoll)
	if !ok || r.Codename != "ITEM_ETC_HP_POTION_01" || r.Count != 1 {
		t.Fatalf("potion selection: %+v/%v", r, ok)
	}
	if _, ok := c.selectConsumable(2, 20, 500001, zeroRoll); ok {
		t.Fatal("class upper boundary")
	}
	if _, ok := c.selectConsumable(2, 20, 0, func() (uint32, error) { return 0, errors.New("entropy") }); ok {
		t.Fatal("RNG failure admitted")
	}
	if row, ok := SelectConsumable(2, 20, 0, zeroRoll); !ok || row.Codename != "ITEM_ETC_HP_POTION_02" {
		t.Fatal("test fixture mutated global catalog")
	}
}

/*
================
TestUniqueSpecificDropsAndCaps
================
*/
func TestUniqueSpecificDropsAndCaps(t *testing.T) {
	rows := AssignedDrops("MOB_CH_TIGERWOMAN", 60, zeroRoll)
	if len(rows) != 3 || rows[0].Codename != "ITEM_MALL_GLOBAL_CHATTING" || rows[1].Codename != "ITEM_MALL_REVERSE_RETURN_SCROLL" {
		t.Fatalf("Tiger Girl assigned loot: %+v", rows)
	}
	if len(AssignedDrops("MOB_CH_TIGER", 60, zeroRoll)) != 0 {
		t.Fatal("ordinary tiger inherited unique table")
	}
	if len(AssignedDrops("MOB_CH_TIGERWOMAN", 1, zeroRoll)) != 1 {
		t.Fatal("assigned capacity exceeded")
	}
	for code, rows := range consumables.fixed {
		n := 0
		for _, r := range rows {
			n += int(r.Min)
		}
		if got := AssignedDrops(code, 250, zeroRoll); len(got) != n {
			t.Fatalf("%s assigned copies=%d want %d", code, len(got), n)
		}
	}
}

/*
================
TestRandomAssignedGroupDistinctnessAndExhaustion
================
*/
func TestRandomAssignedGroupDistinctnessAndExhaustion(t *testing.T) {
	c := consumableCatalog{groups: map[int][]groupDrop{1: {{"A", 1}, {"B", 1}}}, random: map[string][]assignedRandom{"M": {{Monster: "M", Group: 1, Distinct: true, Min: 2, Max: 2, Probability: 1}}}}
	got := c.assigned("M", 8, zeroRoll)
	if len(got) != 2 || got[0].Codename != "A" || got[1].Codename != "B" {
		t.Fatalf("distinct draw: %+v", got)
	}
	if len(c.groups[1]) != 2 {
		t.Fatal("draw mutated shared group")
	}
	c.random["M"][0].Distinct = false
	c.random["M"][0].Min, c.random["M"][0].Max = 3, 3
	got = c.assigned("M", 8, zeroRoll)
	if len(got) != 3 || got[2].Codename != "A" {
		t.Fatal("repeatable group lost copies")
	}
	c.groups[1] = []groupDrop{{"A", 0.0000001}}
	got = c.assigned("M", 8, zeroRoll)
	if len(got) != 3 {
		t.Fatal("rare admitted group lost rewards", got)
	}
}

/*
================
TestGradeDropBudgets
================
*/
func TestGradeDropBudgets(t *testing.T) {
	for _, tc := range []struct {
		grade                 uint8
		code                  string
		cap, passes, attempts int
	}{
		{0, "M", 8, 1, 1}, {1, "M", 9, 2, 1}, {3, "M", 60, 10, 30}, {4, "M", 30, 5, 4},
		{5, "M", 60, 60, 1}, {6, "M", 20, 4, 1}, {7, "M", 30, 8, 1}, {8, "M", 60, 10, 30},
		{0x14, "M", 30, 5, 36}, {3, "MOB_RM_ROC", 250, 10, 30}, {3, "MOB_TQ_WHITESNAKE", 60, 10, 60},
	} {
		a, b, c := MonsterDropBudget(tc.grade, tc.code)
		if a != tc.cap || b != tc.passes || c != tc.attempts {
			t.Fatalf("grade %x: %d/%d/%d", tc.grade, a, b, c)
		}
	}
}

/*
================
TestCatalogRejectsDuplicateAssignmentAndInvalidProbability
================
*/
func TestCatalogRejectsDuplicateAssignmentAndInvalidProbability(t *testing.T) {
	var source consumableSource
	json.Unmarshal(consumablesJSON, &source)
	source.Items = append(source.Items, source.Items[0])
	if _, err := compileConsumables(source); err == nil {
		t.Fatal("duplicate assignment accepted")
	}
	json.Unmarshal(consumablesJSON, &source)
	source.Classes[2][0][0] = -1
	if _, err := compileConsumables(source); err == nil {
		t.Fatal("negative probability accepted")
	}
}
