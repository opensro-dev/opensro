package loot

import "testing"

func TestEquipmentCatalogCountriesAndRoster(t *testing.T) {
	requireTables(t)
	if !HasEquipmentCountry(0) || !HasEquipmentCountry(1) || HasEquipmentCountry(2) {
		t.Fatal("equipment country buckets")
	}
	first := MonsterDropRefItemCodenames()
	if len(first) != 318 {
		t.Fatalf("starter seed = %d", len(first))
	}
	seen := map[string]bool{}
	for _, code := range first {
		if seen[code] {
			t.Fatal("duplicate starter reference")
		}
		seen[code] = true
	}
	want := first[0]
	first[0] = "mutated"
	if MonsterDropRefItemCodenames()[0] != want {
		t.Fatal("mutable catalog escaped")
	}
}
