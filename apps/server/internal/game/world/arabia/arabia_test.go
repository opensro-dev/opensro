/*
===========================================================================

arabia_test.go - the Arabia dungeon world's identity table.

===========================================================================
*/
package arabia

import "testing"

/*
================
TestDungeonWorldIsTheBaghdadDungeon

World 112 is the one Arabia world the leaf carries; the empty worlds
(113-115: boss lair, Petra, Garden) stay unregistered.
================
*/
func TestDungeonWorldIsTheBaghdadDungeon(t *testing.T) {
	world := DungeonWorld()
	if world.ID != 112 || world.CodeName != "GROUP_ARABIA_DUNGEON" {
		t.Fatalf("dungeon world = %+v", world)
	}
	if code, ok := CodeName(112); !ok || code != "GROUP_ARABIA_DUNGEON" {
		t.Fatalf("CodeName(112) = %q, %v", code, ok)
	}
	for _, id := range []int{0, 1, 111, 113, 114, 115} {
		if _, ok := Lookup(id); ok {
			t.Fatalf("Lookup(%d) resolved an unregistered world", id)
		}
	}
}
