package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
)

/*
================
TestOperatorGrantPerfectMagicFillsCatalogBlues

A grant with PerfectMagic carries the strongest catalog-valid blues for the
equipment, from the same catalog monster drops use. A grant without the flag
stays plain.
================
*/
func TestOperatorGrantPerfectMagicFillsCatalogBlues(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	rows, _, err := rt.PlanQuestInventory(c, nil, []inventory.ItemAmount{{
		Codename: "ITEM_CH_SWORD_01_A_RARE", Count: 1, PerfectMagic: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var granted *enterworld.InventoryRow
	for i := range rows {
		if rows[i].Codename == "ITEM_CH_SWORD_01_A_RARE" {
			granted = &rows[i]
		}
	}
	if granted == nil {
		t.Fatal("no granted sword")
	}
	want := loot.BestEquipmentMagic("ITEM_CH_SWORD_01_A_RARE")
	if len(want) == 0 {
		t.Fatal("catalog carries no blues for the sword")
	}
	if len(granted.MagicOptions) != len(want) {
		t.Fatalf("granted %d blues, want %d", len(granted.MagicOptions), len(want))
	}
	for i := range want {
		if granted.MagicOptions[i] != want[i] {
			t.Fatalf("blue %d = %d, want %d", i, granted.MagicOptions[i], want[i])
		}
	}

	plain := testCharacter()
	rt2, _ := newTestRuntime(plain, testItems())
	rows2, _, err := rt2.PlanQuestInventory(plain, nil, []inventory.ItemAmount{{
		Codename: "ITEM_CH_SWORD_01_A_RARE", Count: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range rows2 {
		if rows2[i].Codename == "ITEM_CH_SWORD_01_A_RARE" && len(rows2[i].MagicOptions) != 0 {
			t.Fatalf("plain grant carried %d blues", len(rows2[i].MagicOptions))
		}
	}
}
