/*
===========================================================================

elixirdrops_test.go - elixir drops off: every drop path picks nothing

prepareSelectedDrop is where assigned drops, the consumable mix, a
unique's recipe stage and equipment all turn a pick into a ground item, so
the switch is pinned there with shipped item references.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestElixirDropsOffPicksNothing
================
*/
func TestElixirDropsOffPicksNothing(t *testing.T) {
	clientdata.RequireGameData(t)
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	elixirs := []string{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A", "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_B"}
	others := []string{"ITEM_ETC_HP_POTION_01", "ITEM_ETC_ARCHEMY_MAGICSTONE_STR_01"}
	for _, env := range []string{"off", "on"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(progression.EnvElixirDropsOff, env)
			rt, _, c, target := newCombatTestRuntime(t, 1)
			rt.ElixirDrops = progression.ElixirDropsFromEnv()
			at := simulation.Spawn{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z}
			for _, code := range append(append([]string{}, elixirs...), others...) {
				ref, ok := items.ItemRefByCodename(code)
				if !ok {
					t.Fatalf("missing shipped ref %s", code)
				}
				rt.deps.ItemReferences().(staticItemSource)[code] = ref
			}
			for _, code := range elixirs {
				if _, planned := rt.prepareSelectedDrop(loot.DropItem{Codename: code, Count: 1}, at, c.Name, rt.Now()); planned == (env == "on") {
					t.Fatalf("%s: elixir %s planned %v", env, code, planned)
				}
			}
			for _, code := range others {
				if _, planned := rt.prepareSelectedDrop(loot.DropItem{Codename: code, Count: 1}, at, c.Name, rt.Now()); !planned {
					t.Fatalf("%s: %s must still drop", env, code)
				}
			}
		})
	}
}
