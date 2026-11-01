/*
===========================================================================

character_encode_test.go - the commit's borrowed encode is Marshal's bytes

The commit path encodes every dirty character through the store's
shared scratch (encodeCharacterBorrowed) instead of json.Marshal.
What lands in the characters table must stay byte-identical: the
record is the character's persisted form, read back by loaders and
compared by tests. These tests pin the borrowed bytes against
json.Marshal directly, against the row a real commit persists, and
keep the allocation saving honest.

===========================================================================
*/

package store

import (
	"bytes"
	"encoding/json"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"opensro.online/server/internal/platform/jsonscratch"
)

/*
================
TestEncodeCharacterBorrowedMatchesMarshal
================
*/
func TestEncodeCharacterBorrowedMatchesMarshal(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := seededCharacter()
	c.Aggressions = map[uint32]uint32{300: 3, 20: 2, 1: 1}
	if err := s.CreateCharacter(testDivision, "encode-account", c); err != nil {
		t.Fatal(err)
	}

	owned, err := encodeCharacter(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := encodeCharacterBorrowed(&s.encodeScratch, testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(borrowed, owned) {
		t.Fatalf("borrowed encode differs from Marshal:\nborrowed: %s\nowned:    %s", borrowed, owned)
	}

	// End to end: the row a commit persists is Marshal's bytes for the
	// live record, through the borrowed encode and the statement copy.
	c.Gold = int64Ptr(777)
	s.Mutate("encode-borrowed-test", nil)
	raw := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name))
	want, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if raw != string(want) {
		t.Fatalf("persisted record differs from Marshal:\npersisted: %s\nmarshal:   %s", raw, want)
	}
}

/*
================
TestEncodeCharacterBorrowedAllocatesLess

The encoder's working allocations are shared by both paths; the
borrowed encode must never allocate more than the Marshal it replaces
(and in bytes moved it is far cheaper - BenchmarkEncodeCharacter and
BenchmarkEncodeCharacterBorrowed carry the numbers). Plain builds only:
the race detector's instrumentation allocates on its own.
================
*/
func TestEncodeCharacterBorrowedAllocatesLess(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector's instrumentation allocates on its own")
	}
	c := seededCharacter()
	c.Aggressions = map[uint32]uint32{300: 3, 20: 2, 1: 1}
	var scratch jsonscratch.Scratch
	if _, err := encodeCharacterBorrowed(&scratch, testDivision, c); err != nil {
		t.Fatal(err)
	}
	borrowed := fewestAllocsPerRun(func() {
		if _, err := encodeCharacterBorrowed(&scratch, testDivision, c); err != nil {
			t.Fatal(err)
		}
	})
	owned := fewestAllocsPerRun(func() {
		if _, err := encodeCharacter(testDivision, c); err != nil {
			t.Fatal(err)
		}
	})
	if borrowed > owned {
		t.Fatalf("borrowed encode allocated %.1f per call, Marshal %.1f", borrowed, owned)
	}
}

/*
================
fewestAllocsPerRun

The fewest allocations per run over a handful of trials. One
AllocsPerRun window counts whatever else the process allocates while
it runs - a collection clearing the sync.Pool the encoder borrows its
working state from, a background goroutine waking - and on a loaded
machine that noise once read borrowed 4.0 against Marshal's 3.0 and
failed a sound encoder. Noise only ever adds allocations, never
removes them, so the minimum over trials is the encoder's own floor,
and the floor is what the never-allocate-more contract compares.
================
*/
func fewestAllocsPerRun(f func()) float64 {
	// Warm the scratch and the encoder pool, collect, and warm again,
	// so every window starts from the same steady state; then hold
	// the collector off while measuring, because a mid-window
	// collection empties the pool both encodes borrow from.
	f()
	runtime.GC()
	f()
	defer debug.SetGCPercent(debug.SetGCPercent(-1))

	fewest := testing.AllocsPerRun(100, f)
	for range 4 {
		if got := testing.AllocsPerRun(100, f); got < fewest {
			fewest = got
		}
	}
	return fewest
}
