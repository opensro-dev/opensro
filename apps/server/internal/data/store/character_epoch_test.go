/*
===========================================================================

character_epoch_test.go - the write epoch behind shared record copies

The tick snapshot cache (action tick_snapshots.go, #3099) serves a
detached record copy again only while CharacterWriteEpoch stands, so
the epoch must move on every write a record can receive - scoped or
unscoped, immediate or deferred - and on nothing else.

===========================================================================
*/

package store

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestCharacterWriteEpochTracksScopedDoors
================
*/
func TestCharacterWriteEpochTracksScopedDoors(t *testing.T) {
	s, a, b := batchFixture(t)
	epochA, epochB := s.CharacterWriteEpoch(a), s.CharacterWriteEpoch(b)

	s.MutateCharacter(a, "epoch-test", func() { *a.Gold = 101 })
	if got := s.CharacterWriteEpoch(a); got == epochA {
		t.Fatal("a scoped mutate left the record's epoch standing")
	}
	if got := s.CharacterWriteEpoch(b); got != epochB {
		t.Fatal("a scoped mutate moved a sibling record's epoch")
	}
	epochA = s.CharacterWriteEpoch(a)

	if changed := s.UpdateCharacter(a, "epoch-test", func() bool { return false }); changed {
		t.Fatal("a refused update reported a change")
	}
	if got := s.CharacterWriteEpoch(a); got != epochA {
		t.Fatal("a refused update moved the record's epoch")
	}

	// A deferred door mutates at call time; only its commit waits, so
	// the epoch moves before any flush.
	s.BeginTickBatch()
	if changed := s.UpdateCharacterDeferred(a, "epoch-test", func() bool {
		*a.Gold = 102
		return true
	}); !changed {
		t.Fatal("the deferred update refused")
	}
	if got := s.CharacterWriteEpoch(a); got == epochA {
		t.Fatal("a deferred update left the record's epoch standing until its commit")
	}
	s.FlushTickBatch()

	s.MutateCharacters([]*domain.Character{b}, "epoch-test", func() { *b.Gold = 103 })
	if got := s.CharacterWriteEpoch(b); got == epochB {
		t.Fatal("a multi-record mutate left its record's epoch standing")
	}
}

/*
================
TestCharacterWriteEpochRosterWrites
================
*/
func TestCharacterWriteEpochRosterWrites(t *testing.T) {
	s, a, b := batchFixture(t)
	epochA, epochB := s.CharacterWriteEpoch(a), s.CharacterWriteEpoch(b)

	s.Mutate("epoch-test", func() {})
	if s.CharacterWriteEpoch(a) == epochA || s.CharacterWriteEpoch(b) == epochB {
		t.Fatal("the unscoped door left a record's epoch standing")
	}
	epochA, epochB = s.CharacterWriteEpoch(a), s.CharacterWriteEpoch(b)

	// A create changes the roster, not the records already on it:
	// their epochs - and so their cached copies - stand.
	third := &enterworld.Character{Name: "BatchC", Gold: int64Ptr(100)}
	if err := s.CreateCharacter(testDivision, "batch-account", third); err != nil {
		t.Fatal(err)
	}
	if s.CharacterWriteEpoch(a) != epochA || s.CharacterWriteEpoch(b) != epochB {
		t.Fatal("a create moved an existing record's epoch")
	}
}
