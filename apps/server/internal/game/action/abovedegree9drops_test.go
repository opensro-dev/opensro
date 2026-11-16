/*
===========================================================================

abovedegree9drops_test.go - drops above degree 9 off: the pick drops nothing

prepareSelectedDrop is where every monster drop path turns a pick into a
ground item, unique kills included, so the switch is pinned there. The
shipped v1.150 data holds no degree-10 rows (they come from the cap-110
import), so a shipped degree-9 sword and Lucky Powder stand in with their
ItemClass raised one degree; everything else about the reference is real.
The picks are assigned rewards, so the stand-in codename skips the
authored magic table, and the roll is seeded so equipment variance runs.

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

// The ItemClass one step past degree 9 for equipment (three seal tiers per
// degree) and for alchemy goods (ItemClass is the degree).
const (
	degree10EquipmentClass = 28
	degree10AlchemyClass   = 10
)

/*
================
TestAboveDegree9DropsOffPicksNothing
================
*/
func TestAboveDegree9DropsOffPicksNothing(t *testing.T) {
	clientdata.RequireGameData(t)
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	shipped := func(code string) *enterworld.ItemRef {
		ref, ok := items.ItemRefByCodename(code)
		if !ok {
			t.Fatalf("missing shipped ref %s", code)
		}
		return ref
	}
	raised := func(code, as string, class float64) *enterworld.ItemRef {
		ref := *shipped(code)
		ref.Codename = as
		ref.NativeFields = ref.NativeFields.With("itemClass", class)
		return &ref
	}
	above := map[string]*enterworld.ItemRef{
		"DEGREE10_SWORD":        raised("ITEM_CH_SWORD_09_A", "DEGREE10_SWORD", degree10EquipmentClass),
		"DEGREE10_LUCKY_POWDER": raised("ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09", "DEGREE10_LUCKY_POWDER", degree10AlchemyClass),
	}
	kept := []string{"ITEM_CH_SWORD_09_A", "ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09", "ITEM_ETC_HP_POTION_01"}
	for _, ref := range above {
		if enterworld.ItemDegree(ref) != 10 {
			t.Fatalf("%s: degree %d, want 10", ref.Codename, enterworld.ItemDegree(ref))
		}
	}
	for _, env := range []string{"off", "on"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(progression.EnvAboveDegree9DropsOff, env)
			rt, _, c, target := newCombatTestRuntime(t, 1)
			rt.AboveDegree9Drops = progression.AboveDegree9DropsFromEnv()
			rt.DropRoll = constantDropRoll(0)
			at := simulation.Spawn{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z}
			source := rt.deps.ItemReferences().(staticItemSource)
			for code, ref := range above {
				source[code] = ref
			}
			for _, code := range kept {
				source[code] = shipped(code)
			}
			for code := range above {
				for _, fromUnique := range []bool{false, true} {
					pick := loot.DropItem{Codename: code, Count: 1, Assigned: true, FromUnique: fromUnique}
					if _, planned := rt.prepareSelectedDrop(pick, at, c.Name, rt.Now()); planned == (env == "on") {
						t.Fatalf("%s: %s (unique %v) planned %v", env, code, fromUnique, planned)
					}
				}
			}
			for _, code := range kept {
				pick := loot.DropItem{Codename: code, Count: 1, Assigned: true}
				if _, planned := rt.prepareSelectedDrop(pick, at, c.Name, rt.Now()); !planned {
					t.Fatalf("%s: %s must still drop", env, code)
				}
			}
		})
	}
}
