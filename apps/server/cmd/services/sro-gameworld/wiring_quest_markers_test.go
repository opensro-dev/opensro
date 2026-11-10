/*
===========================================================================

wiring_quest_markers_test.go - the marker derivation key proofs

A session's marker rows are derived once per (record, write epoch,
calendar hour) and served from the derivation while the key stands:
these tests pin every term of the key - a record swap, a write-epoch
move or an hour move re-derives, an unwired epoch never shares, and
rows under an unchanged key are the derived map itself.

===========================================================================
*/

package main

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
countedDerivation
================
*/
type countedDerivation struct {
	calls int
	rows  map[uint32][18]byte
}

/*
================
derive
================
*/
func (d *countedDerivation) derive() map[uint32][18]byte {
	d.calls++
	return d.rows
}

/*
================
markerRow
================
*/
func markerRow(fill byte) [18]byte {
	var row [18]byte
	for i := range row {
		row[i] = fill
	}
	return row
}

/*
================
TestQuestMarkerDerivationKey

Each term of the derivation key, flipped on its own, forces exactly
one re-derivation; an unchanged key keeps serving the same map.
================
*/
func TestQuestMarkerDerivationKey(t *testing.T) {
	character := &enterworld.Character{}
	other := &enterworld.Character{}
	derived := &questMarkerDerivations{}
	want := map[uint32][18]byte{7: markerRow(1)}
	derive := &countedDerivation{rows: want}

	got := derived.rows(42, character, 5, true, 20, derive.derive)
	if derive.calls != 1 || got[7] != want[7] {
		t.Fatalf("first derivation: calls=%d rows=%v", derive.calls, got)
	}
	// Unchanged key across many ticks: no re-derivation, same map.
	for tick := 0; tick < 100; tick++ {
		got = derived.rows(42, character, 5, true, 20, derive.derive)
	}
	if derive.calls != 1 {
		t.Fatalf("unchanged key re-derived: calls=%d", derive.calls)
	}
	if got[7] != want[7] {
		t.Fatalf("cached rows changed: %v", got)
	}
	// A write to the record moves the epoch: re-derive.
	derived.rows(42, character, 6, true, 20, derive.derive)
	if derive.calls != 2 {
		t.Fatalf("epoch move did not re-derive: calls=%d", derive.calls)
	}
	// The calendar hour moves: re-derive.
	derived.rows(42, character, 6, true, 21, derive.derive)
	if derive.calls != 3 {
		t.Fatalf("hour move did not re-derive: calls=%d", derive.calls)
	}
	// The session rebinds another record: re-derive.
	derived.rows(42, other, 6, true, 21, derive.derive)
	if derive.calls != 4 {
		t.Fatalf("record swap did not re-derive: calls=%d", derive.calls)
	}
	// Sessions hold separate derivations.
	derived.rows(43, character, 5, true, 20, derive.derive)
	if derive.calls != 5 {
		t.Fatalf("second session shared another's derivation: calls=%d", derive.calls)
	}
}

/*
================
TestQuestMarkerDerivationUnwiredEpoch

With no write epoch wired, no key proves the record stood still, so
every tick re-derives (the pre-cache behaviour).
================
*/
func TestQuestMarkerDerivationUnwiredEpoch(t *testing.T) {
	character := &enterworld.Character{}
	derived := &questMarkerDerivations{}
	derive := &countedDerivation{rows: map[uint32][18]byte{7: markerRow(1)}}
	for tick := 0; tick < 3; tick++ {
		derived.rows(42, character, 0, false, 20, derive.derive)
	}
	if derive.calls != 3 {
		t.Fatalf("unwired epoch shared a derivation: calls=%d", derive.calls)
	}
}

/*
================
TestQuestMarkerDerivationForget

Derivations of sessions that left are dropped; a returning session
derives fresh.
================
*/
func TestQuestMarkerDerivationForget(t *testing.T) {
	character := &enterworld.Character{}
	derived := &questMarkerDerivations{}
	derive := &countedDerivation{rows: map[uint32][18]byte{7: markerRow(1)}}
	derived.rows(42, character, 5, true, 20, derive.derive)
	derived.rows(43, character, 5, true, 20, derive.derive)
	derived.forget(map[uint64]bool{43: true})
	derived.rows(42, character, 5, true, 20, derive.derive)
	if derive.calls != 3 {
		t.Fatalf("forgotten session kept its derivation: calls=%d", derive.calls)
	}
	derived.rows(43, character, 5, true, 20, derive.derive)
	if derive.calls != 3 {
		t.Fatalf("live session lost its derivation: calls=%d", derive.calls)
	}
}
