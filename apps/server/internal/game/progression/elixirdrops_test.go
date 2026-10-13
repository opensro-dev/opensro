/*
===========================================================================

elixirdrops_test.go - the elixir drop switch and its shop guard

===========================================================================
*/
package progression

import (
	"strings"
	"testing"
)

/*
================
TestElixirDropsSwitch
================
*/
func TestElixirDropsSwitch(t *testing.T) {
	elixir := [4]int64{3, 3, 10, 1}
	powder := [4]int64{3, 3, 10, 2}
	for _, tc := range []struct {
		env  string
		want bool
	}{{"", false}, {"off", false}, {"0", false}, {"on", true}, {"TRUE", true}, {"1", true}} {
		t.Setenv(EnvElixirDropsOff, tc.env)
		drops := ElixirDropsFromEnv()
		if drops.Off() != tc.want || drops.Excludes(elixir) != tc.want {
			t.Fatalf("%q: off %v excludes %v, want %v", tc.env, drops.Off(), drops.Excludes(elixir), tc.want)
		}
		if drops.Excludes(powder) {
			t.Fatalf("%q: a lucky powder is not an elixir", tc.env)
		}
	}
}

/*
================
TestElixirDropsNeedTheShop
================
*/
func TestElixirDropsNeedTheShop(t *testing.T) {
	all := func(string) bool { return true }
	none := func(string) bool { return false }
	onlyB := func(codename string) bool { return strings.HasSuffix(codename, "_B") }
	if err := (ElixirDrops{}).CheckSource(none, "SRO_SHOP_EXTRA_GOODS"); err != nil {
		t.Fatalf("native drops need no shop: %v", err)
	}
	off := ElixirDrops{off: true}
	if err := off.CheckSource(all, "SRO_SHOP_EXTRA_GOODS"); err != nil {
		t.Fatalf("every elixir sold: %v", err)
	}
	for _, sold := range []func(string) bool{none, onlyB} {
		err := off.CheckSource(sold, "SRO_SHOP_EXTRA_GOODS")
		if err == nil || !strings.Contains(err.Error(), EnvElixirDropsOff) || !strings.Contains(err.Error(), "SRO_SHOP_EXTRA_GOODS") {
			t.Fatalf("an unsold elixir must stop the boot naming both settings: %v", err)
		}
	}
}
