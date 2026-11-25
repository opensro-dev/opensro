/*
===========================================================================

magiccube_test.go - Magic Cube mall listing table

===========================================================================
*/
package commerce

import "testing"

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
