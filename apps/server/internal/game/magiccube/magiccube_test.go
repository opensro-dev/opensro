/*
===========================================================================

magiccube_test.go - switch, capacity, filter and non-empty predicates

===========================================================================
*/
package magiccube

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestEnabled
================
*/
func TestEnabled(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"", false},
		{"0", false},
		{"off", false},
		{"1", true},
		{"ON", true},
		{"true", true},
	} {
		if got := Enabled(tc.in); got != tc.want {
			t.Fatalf("Enabled(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

/*
================
TestCapacityAndFilter
================
*/
func TestCapacityAndFilter(t *testing.T) {
	if Capacity(50) != 50 || Capacity(11) != 0 {
		t.Fatal("Capacity must admit only 10/20/30/50")
	}
	if Filter(FilterWeapon) != FilterWeapon || Filter(3) != 0 {
		t.Fatal("Filter must admit only 1/2/4/8")
	}
}

/*
================
TestIsCubeMatchesWire
================
*/
func TestIsCubeMatchesWire(t *testing.T) {
	if !IsCube(wire.PackTypeFlags(3, 2, 3, 1)) || IsCube(wire.PackTypeFlags(3, 2, 2, 0)) {
		t.Fatal("IsCube must match TID 3.2.3 only")
	}
	if NonEmpty(0) || !NonEmpty(1) {
		t.Fatal("NonEmpty must follow StoredItemCount")
	}
}

/*
================
TestElixirFilter
================
*/
func TestElixirFilter(t *testing.T) {
	if ElixirFilter("ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A") != FilterWeapon {
		t.Fatal("weapon elixir")
	}
	if ElixirFilter("ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_B") != FilterArmor {
		t.Fatal("armor elixir")
	}
	if ElixirFilter("ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_A") != FilterAccessary {
		t.Fatal("accessary elixir")
	}
	if ElixirFilter("ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_B") != FilterShield {
		t.Fatal("shield elixir")
	}
	if ElixirFilter("ITEM_ETC_HP_POTION_01") != 0 {
		t.Fatal("potion must not match")
	}
	if !MatchesFilter(FilterWeapon, "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A") ||
		MatchesFilter(FilterWeapon, "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_A") {
		t.Fatal("MatchesFilter")
	}
}
