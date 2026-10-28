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
population codename; a world outside the bands names none.
================
*/
func TestTempleWorldsAreTheBands(t *testing.T) {
	worlds := Worlds()
	if len(worlds) != 22 || WorldCount != 22 {
		t.Fatalf("temple worlds = %d, want 22", len(worlds))
	}
	for index, world := range worlds {
		wantID := FirstWorldID + index
		if world.ID != wantID {
			t.Fatalf("temple world %d has id %d, want %d", index, world.ID, wantID)
		}
		code, ok := CodeName(world.ID)
		if !ok || code != world.CodeName || code == "" {
			t.Fatalf("temple world %d codename = %q, %v", world.ID, code, ok)
		}
	}
	for _, outside := range []int{0, 1, 62, 85, 100} {
		if _, ok := CodeName(outside); ok {
			t.Fatalf("world %d named a temple band", outside)
		}
	}
	// The table is a copy: scribbling on it must not move the leaf's.
	worlds[0].CodeName = "INS_TEMPLE_SCRIBBLED"
	if Worlds()[0].CodeName == "INS_TEMPLE_SCRIBBLED" {
		t.Fatal("Worlds leaks the identity table")
	}
}
