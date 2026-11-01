/*
===========================================================================

namekey_test.go - the fold is byte-identical and allocates by contract

The session bindings and the live-world store key on Lower's output, so
the cheaper fold must produce exactly the strings strings.ToLower did:
these tests pin the bytes over mixed-case and non-ASCII names, pin the
steady-state allocation counts the tick path pays per lookup (the
replaced composition runs alongside as the before picture), and keep
that composition around as the benchmark baseline.

===========================================================================
*/

package namekey

import (
	"strings"
	"testing"
)

var sink string

/*
================
TestLowerMatchesStringsToLower
================
*/
func TestLowerMatchesStringsToLower(t *testing.T) {
	for _, name := range []string{
		"", "a", "z", "already-lower", "digits-0123_and.dots",
		"MixedCase", "ALL_CAPS_123", "TigerGirl", "xXShadowXx",
		"ÄFFIN", "İSTANBUL", "ΣΊΣΥΦΟΣ", "홍길동", "Hong길동A", "AİB", "ﬀ",
	} {
		if got, want := Lower(name), strings.ToLower(name); got != want {
			t.Errorf("Lower(%q) = %q, want %q (strings.ToLower)", name, got, want)
		}
		if got, want := Key("division", name), "division:"+strings.ToLower(name); got != want {
			t.Errorf("Key(division, %q) = %q, want %q", name, got, want)
		}
	}
}

/*
================
TestLowerAllocationCounts

The tick path folds the same online names every tick, so what matters
is the steady state: a repeated lookup must pay no fold allocation at
all - only Key's concatenation. Under the replaced composition every
mixed-case lookup paid the fold as well (two allocations); these pins
fail if a fold ever starts allocating per call again.
================
*/
func TestLowerAllocationCounts(t *testing.T) {
	if got := testing.AllocsPerRun(100, func() { sink = Lower("already-lower") }); got != 0 {
		t.Errorf("Lower of a folded name: %v allocations per run, want 0", got)
	}
	// The first run folds and memoizes; the remaining 99 are memo hits,
	// so the average sits at the cold fold's hundredth.
	if got := testing.AllocsPerRun(100, func() { sink = Lower("SteadyStateMixed") }); got > 0.05 {
		t.Errorf("Lower of a repeated mixed-case name: %v allocations per run, want a memo hit (<= 0.05)", got)
	}
	if got := testing.AllocsPerRun(100, func() { sink = Key("division", "already-lower") }); got != 1 {
		t.Errorf("Key of a folded name: %v allocations per run, want 1 (the concatenation)", got)
	}
	if got := testing.AllocsPerRun(100, func() { sink = Key("division", "SteadyStateMixed") }); got != 1 {
		t.Errorf("Key of a repeated mixed-case name: %v allocations per run, want 1 (the concatenation)", got)
	}
	if got := testing.AllocsPerRun(100, func() { sink = baselineKey("division", "SteadyStateMixed") }); got != 2 {
		t.Errorf("baseline Key of a mixed-case name: %v allocations per run, want 2 (the before picture)", got)
	}
}

// baselineKey is the composition this package replaced
// (division + ":" + strings.ToLower(name)), kept as the before picture
// for the allocation pins and the benchmarks.
func baselineKey(divisionID, characterName string) string {
	return divisionID + ":" + strings.ToLower(characterName)
}

/*
================
BenchmarkLower
================
*/
func BenchmarkLower(b *testing.B) {
	b.Run("already folded", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sink = Lower("already-lower")
		}
	})
	b.Run("mixed case steady", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sink = Lower("TigerGirl")
		}
	})
	b.Run("key mixed case steady", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sink = Key("division", "TigerGirl")
		}
	})
	b.Run("baseline fold mixed case", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sink = strings.ToLower("TigerGirl")
		}
	})
	b.Run("baseline key mixed case", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sink = baselineKey("division", "TigerGirl")
		}
	})
}
