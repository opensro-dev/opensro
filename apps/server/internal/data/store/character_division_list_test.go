/*
===========================================================================

character_division_list_test.go - the shared division roster list

CharactersForDivision hands every caller the one roster copy the
lookup rebuild published at the division's last roster change, so
per-tick readers share the list instead of copying it per call. The
published copy is never written after publication: a retained slice
stays stable (merely stale) across creates and the reaper's in-place
compaction of the live array.

===========================================================================
*/
package store

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestCharactersForDivisionSharesPublishedList

Two calls share one backing array; a create publishes a new list; a
retained list keeps its membership across the reap.
================
*/
func TestCharactersForDivisionSharesPublishedList(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)

	doomed := &enterworld.Character{Name: "listdoomed"}
	keeper := &enterworld.Character{Name: "listkeeper"}
	for _, c := range []*enterworld.Character{doomed, keeper} {
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}

	first := s.Characters().CharactersForDivision(testDivision)
	second := s.Characters().CharactersForDivision(testDivision)
	if len(first) != 2 || first[0] != doomed || first[1] != keeper {
		t.Fatalf("first = %v, want the two live records by pointer identity", first)
	}
	if &first[0] != &second[0] {
		t.Fatal("two calls do not share the published list (per-call copy is back)")
	}

	late := &enterworld.Character{Name: "listlate"}
	if err := s.CreateCharacter(testDivision, "test-account", late); err != nil {
		t.Fatal(err)
	}
	third := s.Characters().CharactersForDivision(testDivision)
	if len(third) != 3 || third[2] != late {
		t.Fatalf("third = %v, want the new record appended by the create", third)
	}
	if &third[0] == &first[0] {
		t.Fatal("the create did not publish a new list")
	}
	if len(first) != 2 {
		t.Fatalf("retained list grew to %d across the create", len(first))
	}

	s.MutateCharacter(doomed, "delete-reserve", func() {
		doomed.DeletePending = true
		doomed.DeleteReservedAt = clock.Now().UTC().Format(time.RFC3339)
	})
	clock.Advance(DeleteReservationWindow + time.Hour)
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("reaped %v, want exactly the doomed record", reaped)
	}
	if first[0] != doomed || first[1] != keeper {
		t.Fatal("the retained list was rewritten by the reap's compaction")
	}
	live := s.Characters().CharactersForDivision(testDivision)
	if len(live) != 2 || live[0] != keeper || live[1] != late {
		t.Fatalf("live after reap = %v, want keeper and late", live)
	}
}
