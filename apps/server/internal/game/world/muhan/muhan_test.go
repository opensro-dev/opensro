/*
===========================================================================

muhan_test.go - the Muhan identity table's contract.

The eight banded worlds, their bands and solo marks; the unbanded
shells and anything else report no world.

===========================================================================
*/
package muhan

import "testing"

/*
================
TestWorldsCarryTheEightBandedArenas
================
*/
func TestWorldsCarryTheEightBandedArenas(t *testing.T) {
	worlds := Worlds()
	if len(worlds) != 8 {
		t.Fatalf("worlds: got %d, want 8", len(worlds))
	}
	bands := map[int]int{}
	for i, world := range worlds {
		if i > 0 && world.ID <= worlds[i-1].ID {
			t.Fatalf("worlds not in id order: %+v", worlds)
		}
		if world.CodeName == "" || world.Band == 0 {
			t.Fatalf("world %+v has no codename or band", world)
		}
		bands[world.Band]++
		found, ok := Lookup(world.ID)
		if !ok || found != world {
			t.Fatalf("Lookup(%d) = %+v, %v", world.ID, found, ok)
		}
		if code, ok := CodeName(world.ID); !ok || code != world.CodeName {
			t.Fatalf("CodeName(%d) = %q, %v", world.ID, code, ok)
		}
	}
	// Two arenas per band: one solo, one party.
	for _, band := range []int{75, 85, 95, 105} {
		if bands[band] != 2 {
			t.Fatalf("band %d has %d worlds, want 2", band, bands[band])
		}
	}
	solo := 0
	for _, world := range worlds {
		if world.Solo {
			solo++
		}
	}
	if solo != 4 {
		t.Fatalf("solo worlds: got %d, want 4", solo)
	}
	// The unbanded shells (99-102) and any other id are not wave worlds.
	for _, id := range []int{0, 99, 100, 101, 102, 107, 112} {
		if _, ok := Lookup(id); ok {
			t.Fatalf("Lookup(%d) found a wave world", id)
		}
	}
}
