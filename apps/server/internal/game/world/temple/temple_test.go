/*
===========================================================================

temple_test.go - the temple identity table holds exactly the 22 bands.

===========================================================================
*/
package temple

import "testing"

/*
================
TestTempleWorldsAreTheBands

The database's instanced worlds 63-84, in id order, each with its own
native codename, its group's level band and its per-layer resident
limit; a world outside the bands names none.
================
*/
func TestTempleWorldsAreTheBands(t *testing.T) {
	worlds := Worlds()
	if len(worlds) != 22 || WorldCount != 22 {
		t.Fatalf("temple worlds = %d, want 22", len(worlds))
	}
	// The full source table, pinned per world: codename, band, limit.
	want := []World{
		{63, "INS_GOD_TOGUI_35_50_LEVEL_1", 35, 50, 4},
		{64, "INS_GOD_TOGUI_35_50_LEVEL_2", 35, 50, 4},
		{65, "INS_GOD_TOGUI_51_60_LEVEL_1", 51, 60, 4},
		{66, "INS_GOD_TOGUI_51_60_LEVEL_2", 51, 60, 4},
		{67, "INS_GOD_TOGUI_61_70_LEVEL_1", 61, 70, 4},
		{68, "INS_GOD_TOGUI_61_70_LEVEL_2", 61, 70, 4},
		{69, "INS_GOD_FLAME_71_80_LEVEL_1", 71, 80, 4},
		{70, "INS_GOD_FLAME_71_80_LEVEL_2", 71, 80, 4},
		{71, "INS_GOD_FLAME_71_80_LEVEL_3", 71, 80, 8},
		{72, "INS_GOD_FLAME_71_80_LEVEL_4", 71, 80, 8},
		{73, "INS_GOD_FLAME_81_90_LEVEL_1", 81, 90, 4},
		{74, "INS_GOD_FLAME_81_90_LEVEL_2", 81, 90, 4},
		{75, "INS_GOD_FLAME_81_90_LEVEL_3", 81, 90, 8},
		{76, "INS_GOD_FLAME_81_90_LEVEL_4", 81, 90, 8},
		{77, "INS_GOD_WRECK_IN_91_100_LEVEL_1", 91, 100, 4},
		{78, "INS_GOD_WRECK_IN_91_100_LEVEL_2", 91, 100, 4},
		{79, "INS_GOD_WRECK_OUT_91_100_LEVEL_3", 91, 100, 8},
		{80, "INS_GOD_WRECK_OUT_91_100_LEVEL_4", 91, 100, 8},
		{81, "INS_GOD_WRECK_IN_100_110_LEVEL_1", 101, 110, 4},
		{82, "INS_GOD_WRECK_IN_100_110_LEVEL_2", 101, 110, 4},
		{83, "INS_GOD_WRECK_OUT_100_110_LEVEL_3", 101, 110, 8},
		{84, "INS_GOD_WRECK_OUT_100_110_LEVEL_4", 101, 110, 8},
	}
	for index, world := range worlds {
		if world != want[index] {
			t.Fatalf("temple world %d = %+v, want %+v", index, world, want[index])
		}
		code, ok := CodeName(world.ID)
		if !ok || code != world.CodeName {
			t.Fatalf("temple world %d codename = %q, %v", world.ID, code, ok)
		}
		min, max, ok := Band(world.ID)
		if !ok || min != world.MinLevel || max != world.MaxLevel {
			t.Fatalf("temple world %d band = %d..%d, %v", world.ID, min, max, ok)
		}
	}
	// The top band's floor is the config's 101, not the codename's 100.
	if min, max, _ := Band(81); min != 101 || max != 110 {
		t.Fatalf("top band = %d..%d, want 101..110", min, max)
	}
	for _, outside := range []int{0, 1, 62, 85, 100} {
		if _, ok := CodeName(outside); ok {
			t.Fatalf("world %d named a temple band", outside)
		}
		if _, _, ok := Band(outside); ok {
			t.Fatalf("world %d has a temple band", outside)
		}
	}
	// The table is a copy: scribbling on it must not move the leaf's.
	worlds[0].CodeName = "INS_TEMPLE_SCRIBBLED"
	if Worlds()[0].CodeName == "INS_TEMPLE_SCRIBBLED" {
		t.Fatal("Worlds leaks the identity table")
	}
}
