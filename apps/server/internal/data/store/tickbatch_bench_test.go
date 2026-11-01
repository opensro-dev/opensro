/*
===========================================================================

tickbatch_bench_test.go - what a tick-shaped burst of character doors costs

One benchmark operation is one burst of door calls over a division's
characters, the shape the simulation tick produces: the immediate doors
commit (and encode the whole dirty set) once per call, the deferred
doors mark their rows and let the tick-end flush encode each row once.

===========================================================================
*/
package store

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/platform/jsonscratch"
)

// benchBurstCalls is the door calls in one benchmarked burst;
// benchBurstCharacters is the division it spreads over.
const (
	benchBurstCalls      = 8
	benchBurstCharacters = 16
)

/*
================
benchBurstFixture

A store holding benchBurstCharacters characters with 100 gold each.
================
*/
func benchBurstFixture(b *testing.B) (*Store, []*enterworld.Character) {
	b.Helper()
	s, err := Open(b.TempDir(), Options{Now: newTestClock().Now, DefaultSkills: testSkillSeeder})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(s.Close)
	chars := make([]*enterworld.Character, 0, benchBurstCharacters)
	for i := 0; i < benchBurstCharacters; i++ {
		c := &enterworld.Character{Name: fmt.Sprintf("Bench%02d", i), Gold: int64Ptr(100)}
		if err := s.CreateCharacter(testDivision, fmt.Sprintf("bench-account-%d", i/4), c); err != nil {
			b.Fatal(err)
		}
		chars = append(chars, c)
	}
	return s, chars
}

/*
================
benchDoorBurst

benchBurstCalls updates spread over the fixture's characters; each says
its character changed, so every call dirties a row.
================
*/
func benchDoorBurst(chars []*enterworld.Character, door func(c *enterworld.Character, update func() bool) bool) {
	for i := 0; i < benchBurstCalls; i++ {
		c := chars[i%len(chars)]
		door(c, func() bool {
			c.Gold = int64Ptr(*c.Gold + 1)
			return true
		})
	}
}

/*
================
BenchmarkTickDoorBurst

The same burst through the immediate doors (each call a commit) and
through the deferred doors inside one tick batch (one commit at the
flush).
================
*/
func BenchmarkTickDoorBurst(b *testing.B) {
	s, chars := benchBurstFixture(b)
	b.Run("Immediate", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchDoorBurst(chars, func(c *enterworld.Character, update func() bool) bool {
				return s.UpdateCharacter(c, "bench", update)
			})
		}
	})
	b.Run("Deferred", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			s.BeginTickBatch()
			benchDoorBurst(chars, func(c *enterworld.Character, update func() bool) bool {
				return s.UpdateCharacterDeferred(c, "bench", update)
			})
			if stats := s.FlushTickBatch(); stats.Err != nil {
				b.Fatal(stats.Err)
			}
		}
	})
}

/*
================
BenchmarkTickUnchangedBurst

The deferred burst with touches that change nothing: the doors mark
every touched character dirty, so before the digest skip each flush
rewrote the whole set; the commit now writes none of them. The
changing burst above is the control.
================
*/
func BenchmarkTickUnchangedBurst(b *testing.B) {
	s, chars := benchBurstFixture(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s.BeginTickBatch()
		for j := 0; j < benchBurstCalls; j++ {
			c := chars[j%len(chars)]
			s.MutateCharacterDeferred(c, "bench", func() {})
		}
		if stats := s.FlushTickBatch(); stats.Err != nil {
			b.Fatal(stats.Err)
		}
	}
}

/*
================
BenchmarkEncodeCharacter

One character row's JSON: the unit the commit repeats per dirty row.
================
*/
func BenchmarkEncodeCharacter(b *testing.B) {
	_, chars := benchBurstFixture(b)
	c := chars[0]
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		record, err := encodeCharacter(testDivision, c)
		if err != nil {
			b.Fatal(err)
		}
		_ = record
	}
}

/*
================
BenchmarkEncodeCharacterBorrowed

The commit path's unit after the scratch swap: the same character row
encoded into the store's shared buffer. BenchmarkEncodeCharacter is
the before column.
================
*/
func BenchmarkEncodeCharacterBorrowed(b *testing.B) {
	_, chars := benchBurstFixture(b)
	c := chars[0]
	var scratch jsonscratch.Scratch
	if _, err := encodeCharacterBorrowed(&scratch, testDivision, c); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		record, err := encodeCharacterBorrowed(&scratch, testDivision, c)
		if err != nil {
			b.Fatal(err)
		}
		_ = record
	}
}
