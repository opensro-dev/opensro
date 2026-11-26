/*
===========================================================================

magiccube_test.go - Magic Cube mall listing table

===========================================================================
*/
package commerce

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

/*
================
TestMagicCubeMallGoods
================
*/
func TestMagicCubeMallGoods(t *testing.T) {
	if got := MagicCubeMallGoods(false); got != nil {
		t.Fatalf("off lists %v, want none", got)
	}
	got := MagicCubeMallGoods(true)
	if len(got) != len(MagicCubeListings) {
		t.Fatalf("listed %d, want %d", len(got), len(MagicCubeListings))
	}
	wantSilk := map[string]uint32{
		"ITEM_PRE_MALL_MAGIC_CUBE_WEAPON":       30,
		"ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10":    6,
		"ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_20":    12,
		"ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_30":    18,
		"ITEM_PRE_MALL_MAGIC_CUBE_ARMOR":        30,
		"ITEM_PRE_MALL_MAGIC_CUBE_ACCESSARY_10": 6,
		"ITEM_PRE_MALL_MAGIC_CUBE_SHIELD_20":    12,
		"ITEM_PRE_MALL_MAGIC_CUBE_SHIELD_30":    18,
	}
	for _, good := range got {
		if good.Tab != magicCubeMallTab {
			t.Fatalf("%s tab = %s, want %s", good.Item, good.Tab, magicCubeMallTab)
		}
		if good.Package == "" || good.Item == "" || good.Silk == 0 {
			t.Fatalf("incomplete listing %+v", good)
		}
		if silk, ok := wantSilk[good.Item]; ok && good.Silk != silk {
			t.Fatalf("%s silk = %d, want %d", good.Item, good.Silk, silk)
		}
	}
}

/*
================
TestMagicCubePackageContentAndGrant

Magic Cube mall packages (TID 3/2/3/1) must hydrate through packageContent
and grant cleanly into an inventory as non-stackable empty cubes.
================
*/
func TestMagicCubePackageContentAndGrant(t *testing.T) {
	refs := stubItemRefs{
		"ITEM_PRE_MALL_MAGIC_CUBE_WEAPON": {
			RefObjID: 51620,
			Codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON",
			TypeIDs:  [4]int64{3, 2, 3, 1},
		},
	}
	row := make([]string, 22)
	row[3] = "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON"
	for i := 4; i < 20; i++ {
		row[i] = "0"
	}
	row[21] = "xxx"

	content, err := packageContent(row, refs, rentTerms{})
	if err != nil {
		t.Fatalf("packageContent: %v", err)
	}
	if content.Stack != 1 || content.Data != 0 {
		t.Fatalf("content = %+v, want Stack=1 Data=0", content)
	}

	inv := inventory.New(nil, domain.DefaultInventorySize)
	slots, err := GrantPackage(inv, []Content{content}, 1, 32, GrantMarks{})
	if err != nil {
		t.Fatalf("GrantPackage: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("granted %d slots, want 1", len(slots))
	}
	item, ok := inv.At(slots[0])
	if !ok || item.Codename != "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON" || item.Quantity != 1 {
		t.Fatalf("granted item = %+v (ok=%v)", item, ok)
	}

	// Non-empty / unexpected template fields must still refuse.
	bad := append([]string(nil), row...)
	bad[6] = "5"
	if _, err := packageContent(bad, refs, rentTerms{}); err == nil {
		t.Fatal("packageContent accepted non-zero Data on Magic Cube template")
	}
}

type stubItemRefs map[string]*enterworld.ItemRef

func (s stubItemRefs) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	for _, ref := range s {
		if ref.RefObjID == id {
			return ref, true
		}
	}
	return nil, false
}

func (s stubItemRefs) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	ref, ok := s[codename]
	return ref, ok
}
