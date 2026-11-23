/*
===========================================================================

arabia_test.go - the Arabia identity table stays the one dungeon world.

===========================================================================
*/
package arabia

import "testing"

/*
================
TestWorldIsTheBaghdadDungeon

The table is the one identity source for the monster catalogue and the
world catalogue: the source's dungeon world 112 and no other; the
boss-lair, Petra and garden worlds (113-115) name no Arabia world
until a later slice sources their content.
================
*/
func TestWorldIsTheBaghdadDungeon(t *testing.T) {
	want := []World{
		{DungeonWorldID, "GROUP_ARABIA_DUNGEON"},
	}
	worlds := Worlds()
	if len(worlds) != len(want) {
		t.Fatalf("table has %d worlds, want %d", len(worlds), len(want))
	}
	for index, world := range want {
		if worlds[index] != world {
			t.Fatalf("world %d = %+v, want %+v", index, worlds[index], world)
		}
		if code, ok := CodeName(world.ID); !ok || code != world.CodeName {
			t.Fatalf("world %d codename = %q, %v; want %q", world.ID, code, ok, world.CodeName)
		}
	}
	for _, other := range []int{0, 1, 111, 113, 114, 115, 116} {
		if _, ok := CodeName(other); ok {
			t.Fatalf("world %d must not name an Arabia world", other)
		}
	}
	// The dungeon's own worlds ride their own switch (#4134): the
	// dungeon and its boss lair, never the continent's worlds or
	// Petra and the garden.
	for _, dungeon := range []int{DungeonWorldID, HideoutWorldID} {
		if !IsDungeonWorld(dungeon) {
			t.Fatalf("world %d must name a Baghdad dungeon world", dungeon)
		}
	}
	for _, other := range []int{0, 1, 111, 114, 115, 116} {
		if IsDungeonWorld(other) {
			t.Fatalf("world %d must not name a Baghdad dungeon world", other)
		}
	}
	worlds[0].CodeName = "MUTATED"
	if code, _ := CodeName(DungeonWorldID); code != "GROUP_ARABIA_DUNGEON" {
		t.Fatalf("Worlds leaked the table: %q", code)
	}
}
