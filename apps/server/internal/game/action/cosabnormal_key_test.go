/*
===========================================================================

cosabnormal_key_test.go - the COS abnormal key identity tests

The judged pet tick used to rebuild its pet's committed-block map key
as a folded string on every read (#2033). These tests pin the identity
the folded struct key keeps: case-insensitive division and owner name,
the exact summoned gid (a replacement pet inherits no status), and a
session fold that only turns when the binding changes.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
)

/*
================
TestCosAbnormalKeyForIdentity

The folded key must partition exactly as the old built string did:
division and owner name compare case-folded, the summoned gid and the
name's letters compare exactly.
================
*/
func TestCosAbnormalKeyForIdentity(t *testing.T) {
	base := cosAbnormalKeyFor("Capital", "SoYeon", 77)
	if folded := cosAbnormalKeyFor("capital", "soyeon", 77); folded != base {
		t.Fatalf("case variants folded apart: %+v != %+v", folded, base)
	}
	if other := cosAbnormalKeyFor("capital", "soyeon", 78); other == base {
		t.Fatal("a replacement pet (gid 78) shares the key of gid 77")
	}
	if other := cosAbnormalKeyFor("capital", "soyeon2", 77); other == base {
		t.Fatal("a different owner name shares the key")
	}
	if other := cosAbnormalKeyFor("other", "soyeon", 77); other == base {
		t.Fatal("a different division shares the key")
	}
}

/*
================
TestCosAbnormalStoreKeyedFold

A block stored under one casing is the block read under any other,
through both the raw triple and a session's folded key; the
replacement pet reads nothing.
================
*/
func TestCosAbnormalStoreKeyedFold(t *testing.T) {
	rt := &Runtime{}
	rt.storeCosAbnormal("Capital", "SoYeon", 77, &abnormal.Block{Mask: abnormal.Poison.Bit()})

	state := &petSession{}
	key := state.judgedCosAbnormalKey("capital", "soyeon", 77)
	if got := rt.cosAbnormalKeyed(key); got == nil || got.Mask != abnormal.Poison.Bit() {
		t.Fatalf("keyed read under folded case = %+v, want the stored poison block", got)
	}
	if got := rt.cosAbnormal("CAPITAL", "SOYEON", 77); got == nil {
		t.Fatal("raw read under another casing missed the stored block")
	}
	if got := rt.cosAbnormalKeyed(state.judgedCosAbnormalKey("capital", "soyeon", 78)); got != nil {
		t.Fatalf("replacement pet read a block: %+v", got)
	}

	rt.storeCosAbnormal("capital", "soyeon", 77, nil)
	if got := rt.cosAbnormal("capital", "soyeon", 77); got != nil {
		t.Fatalf("cleared block still reads: %+v", got)
	}
}

/*
================
TestPetSessionJudgedCosAbnormalKeyReuse

The judged tick's fold happens once per binding: repeated weighs of
one binding allocate nothing, a new gid folds the new identity, and a
case-only change folds to the same key as before.
================
*/
func TestPetSessionJudgedCosAbnormalKeyReuse(t *testing.T) {
	state := &petSession{}
	first := state.judgedCosAbnormalKey("Capital", "SoYeon", 77)
	if allocs := testing.AllocsPerRun(100, func() {
		state.judgedCosAbnormalKey("Capital", "SoYeon", 77)
	}); allocs != 0 {
		t.Fatalf("repeat weigh allocated %v per run, want 0", allocs)
	}
	if same := state.judgedCosAbnormalKey("capital", "soyeon", 77); same != first {
		t.Fatalf("case-only change folded to a new key: %+v != %+v", same, first)
	}
	next := state.judgedCosAbnormalKey("capital", "soyeon", 78)
	if next == first || next != cosAbnormalKeyFor("capital", "soyeon", 78) {
		t.Fatalf("replacement binding folded wrong: %+v", next)
	}
}
