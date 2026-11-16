/*
===========================================================================

clock_test.go - the awakening clock's rule

===========================================================================
*/
package nasrun

import "testing"

const (
	testNow     int64 = 1_800_000_000
	mallPeriod  int64 = 2_419_200 // Param1 of the mall Spirits: 28 days
	eventPeriod int64 = 604_800   // Param1 of the event pair: 7 days
)

/*
================
TestADormantSpiritIsNotAwake
================
*/
func TestADormantSpiritIsNotAwake(t *testing.T) {
	if Awake(0, testNow) {
		t.Fatal("a dormant Spirit reads awake; it must be used before it is worn")
	}
	if !Awake(uint32(testNow+1), testNow) || Awake(uint32(testNow), testNow) || Awake(uint32(testNow-1), testNow) {
		t.Fatal("awake must mean the expiry is still ahead of now")
	}
}

/*
================
TestUseWakesOnlyADormantSpirit
================
*/
func TestUseWakesOnlyADormantSpirit(t *testing.T) {
	next, woke := Wake(0, mallPeriod, testNow)
	if !woke || int64(next) != testNow+mallPeriod {
		t.Fatalf("wake = %d %v, want now + 28 days", next, woke)
	}
	if next, woke := Wake(0, eventPeriod, testNow); !woke || int64(next) != testNow+eventPeriod {
		t.Fatalf("event Spirit wake = %d %v, want now + 7 days", next, woke)
	}
	// Awake or lapsed, a second use changes nothing.
	for _, data := range []uint32{uint32(testNow + 10), uint32(testNow - 10)} {
		if again, woke := Wake(data, mallPeriod, testNow); woke || again != data {
			t.Fatalf("re-use of %d = %d %v, want unchanged", data, again, woke)
		}
	}
	if _, woke := Wake(0, 0, testNow); woke {
		t.Fatal("a row without a period woke")
	}
}

/*
================
TestExtensionCountsFromExpiryOrNow
================
*/
func TestExtensionCountsFromExpiryOrNow(t *testing.T) {
	awake := uint32(testNow + 3600)
	if next, ok := Extend(awake, mallPeriod, testNow); !ok || int64(next) != int64(awake)+mallPeriod {
		t.Fatalf("awake extension = %d %v, want expiry + 28 days", next, ok)
	}
	lapsed := uint32(testNow - 3600)
	if next, ok := Extend(lapsed, mallPeriod, testNow); !ok || int64(next) != testNow+mallPeriod {
		t.Fatalf("lapsed extension = %d %v, want now + 28 days", next, ok)
	}
	if _, ok := Extend(0, mallPeriod, testNow); ok {
		t.Fatal("a dormant Spirit took the extension; the gear needs a woken one")
	}
}

/*
================
TestClockStopsAtTheLastWord
================
*/
func TestClockStopsAtTheLastWord(t *testing.T) {
	if next, ok := Extend(^uint32(0)-5, mallPeriod, testNow); !ok || next != ^uint32(0) {
		t.Fatalf("extension past the u32 range = %d, want it held at the last second", next)
	}
}
