/*
===========================================================================

materialdrops_test.go - alchemy material drops off: every drop path picks
nothing

prepareSelectedDrop is where every monster drop path turns a pick into a
ground item, so the switch is pinned there with shipped item references.

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
TestAlchemyMaterialDropsOffPicksNothing
================
*/
func TestAlchemyMaterialDropsOffPicksNothing(t *testing.T) {
	clientdata.RequireGameData(t)
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	materials := []string{"ITEM_ETC_ARCHEMY_MATERIAL_CH_MANGNYANG1"}
	others := []string{"ITEM_ETC_HP_POTION_01", "ITEM_ETC_ARCHEMY_MAGICTABLET_STR_01", "ITEM_ETC_ARCHEMY_ELEMENT_EARTH_01"}
	for _, env := range []string{"off", "on"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(progression.EnvAlchemyMaterialDropsOff, env)
			rt, _, c, target := newCombatTestRuntime(t, 1)
			rt.MaterialDrops = progression.MaterialDropsFromEnv()
			at := simulation.Spawn{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z}
			for _, code := range append(append([]string{}, materials...), others...) {
				ref, ok := items.ItemRefByCodename(code)
				if !ok {
					t.Fatalf("missing shipped ref %s", code)
				}
				rt.deps.ItemReferences().(staticItemSource)[code] = ref
			}
			for _, code := range materials {
				if _, planned := rt.prepareSelectedDrop(loot.DropItem{Codename: code, Count: 1}, at, c.Name, rt.Now()); planned == (env == "on") {
					t.Fatalf("%s: material %s planned %v", env, code, planned)
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
