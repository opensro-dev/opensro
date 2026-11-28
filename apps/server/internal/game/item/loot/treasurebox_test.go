/*
===========================================================================

treasurebox_test.go - tests for treasurebox.go

===========================================================================
*/
package loot

import (
	"strings"
	"testing"
)

/*
================
TestTreasureBoxEquipmentPaysTheTopDegree

On the shipped catalogue the walk from degree 10 lands on degree 9: the
normal bucket holds every strength tier of it, the Sun bucket only Sun
pieces; refusing every candidate empties the bucket.
================
*/
func TestTreasureBoxEquipmentPaysTheTopDegree(t *testing.T) {
	normal := TreasureBoxEquipment(TreasureBoxTopDegree, 0, nil)
	if len(normal) == 0 {
		t.Fatal("no normal top-degree equipment")
	}
	tiers := map[byte]bool{}
	for _, code := range normal {
		if !strings.Contains(code, "_09_") {
			t.Fatalf("%s is not degree 9", code)
		}
		tiers[code[len(code)-1]] = true
	}
	if len(tiers) < 3 {
		t.Fatalf("normal bucket holds tiers %v, want A, B and C", tiers)
	}
	sun := TreasureBoxEquipment(TreasureBoxTopDegree, 3, nil)
	if len(sun) == 0 {
		t.Fatal("no Sun top-degree equipment")
	}
	for _, code := range sun {
		if !strings.Contains(code, "_09_") || !strings.HasSuffix(code, "_C_RARE") {
			t.Fatalf("%s is not a degree-9 Sun piece", code)
		}
	}
	if got := TreasureBoxEquipment(TreasureBoxTopDegree, 0, func(string) bool { return false }); got != nil {
		t.Fatal("a refused bucket returned items", got)
	}
	if TreasureBoxEquipment(TreasureBoxTopDegree, 4, nil) != nil {
		t.Fatal("seal 4 is not a tier")
	}
}
