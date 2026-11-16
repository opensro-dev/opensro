/*
===========================================================================

event_groups_test.go - running events join an entry's event groups

===========================================================================
*/
package enterworld

import (
	"slices"
	"testing"
)

/*
================
TestWithEventGroupsAddsRunningGroupsOnce

The character's own groups stay first and in order, a running group the
character already lists is not repeated, and the character's slice is
never written through.
================
*/
func TestWithEventGroupsAddsRunningGroupsOnce(t *testing.T) {
	own := make([]uint32, 2, 8)
	own[0], own[1] = 1, 5
	got := withEventGroups(own, []uint32{5, 9})
	if !slices.Equal(got, []uint32{1, 5, 9}) {
		t.Fatalf("got %v", got)
	}
	if spare := own[:3]; spare[2] != 0 {
		t.Fatal("the character's backing array was written")
	}
	if got := withEventGroups(own, nil); !slices.Equal(got, own) {
		t.Fatalf("no running group changed the list: %v", got)
	}
	if got := withEventGroups(nil, []uint32{5}); !slices.Equal(got, []uint32{5}) {
		t.Fatalf("got %v", got)
	}
}
