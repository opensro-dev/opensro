/*
===========================================================================

pharaohtomb_test.go - the tomb identity table stays the three tombs.

===========================================================================
*/
package pharaohtomb

import "testing"

/*
================
TestWorldsAreTheThreeTombs

The table is the one identity source for the monster catalogue and the
world catalogue: three worlds, shipped ids 57-59, in order, and no other
source world names a tomb.
================
*/
func TestWorldsAreTheThreeTombs(t *testing.T) {
	want := []World{
		{57, "INS_PHARAOH_TOMB_1"},
		{58, "INS_PHARAOH_TOMB_2"},
		{59, "INS_PHARAOH_TOMB_3"},
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
	for _, other := range []int{0, 1, 56, 60, 99} {
		if _, ok := CodeName(other); ok {
			t.Fatalf("world %d names a tomb", other)
		}
	}
}
