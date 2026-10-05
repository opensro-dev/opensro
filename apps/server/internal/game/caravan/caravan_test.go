/*
===========================================================================

caravan_test.go - the native caravan formulas and registry timer

Each expectation is worked from the native constants (60BC80..60C6C0), not
read back from the implementation.

===========================================================================
*/

package caravan

import "testing"

/*
================
sequence

A scripted rand() source; it fails the test when exhausted.
================
*/
func sequence(t *testing.T, values ...uint32) Roll {
	t.Helper()
	index := 0
	return func() (uint32, error) {
		if index >= len(values) {
			t.Fatalf("roll sequence exhausted after %d draws", index)
		}
		value := values[index]
		index++
		return value, nil
	}
}

/*
================
TestDifficultyTierUsesTheTradeScaleThresholds
================
*/
func TestDifficultyTierUsesTheTradeScaleThresholds(t *testing.T) {
	for value, want := range map[uint32]uint8{0: 0, 1: 1, 408: 1, 409: 2, 918: 2, 919: 3, 1428: 3, 2142: 4, 2143: 5, 1 << 30: 5} {
		if got := DifficultyTier(value); got != want {
			t.Fatalf("tier(%d)=%d want %d", value, got, want)
		}
	}
}

/*
================
TestStarRatingDividesByTheTradeFactor
================
*/
func TestStarRatingDividesByTheTradeFactor(t *testing.T) {
	cases := []struct {
		value   uint32
		special bool
		want    uint32
	}{
		{0, false, 0}, {1, false, 1}, {408, false, 1}, {409, false, 2}, {612, false, 2}, {613, false, 3},
		// 3.3.8.2 goods: trunc((v * 1.55 - 1) / 306 + 1).
		{200, true, 2}, {198, true, 1}, {1000, true, 6},
	}
	for _, c := range cases {
		if got := StarRating(c.value, c.special); got != c.want {
			t.Fatalf("stars(%d,%v)=%d want %d", c.value, c.special, got, c.want)
		}
	}
}

/*
================
TestCargoValueScalesGoodsByLevelAndCapacity

40 goods in a 40-slot transport at a basis of 348: 40 x 348 / 348 = 40.
A tiny load still counts as 1; no goods is 0.
================
*/
func TestCargoValueScalesGoodsByLevelAndCapacity(t *testing.T) {
	if got := CargoValue(40, 40, 348); got != 40 {
		t.Fatalf("value=%d want 40", got)
	}
	if got := CargoValue(40, 80, 348); got != 80 {
		t.Fatalf("double capacity value=%d want 80", got)
	}
	if got := CargoValue(1, 40, 100000); got != 1 {
		t.Fatalf("fractional value=%d want 1", got)
	}
	if got := CargoValue(0, 40, 348); got != 0 {
		t.Fatal("an empty transport has value")
	}
	if CargoLevel(5) != 20 || CargoLevel(200) != 140 || CargoLevel(77) != 77 {
		t.Fatal("cargo level clamp")
	}
	if CargoCapacity(1, 99) != 40 || CargoCapacity(2, 99) != 30 || CargoCapacity(0, 99) != 99 {
		t.Fatal("cargo capacity")
	}
	if BaseDeathExp(100) != 125 {
		t.Fatal("base death exp")
	}
}

/*
================
TestSpawnCountAddsOneOnHighDrawAndDoublesRarely
================
*/
func TestSpawnCountAddsOneOnHighDrawAndDoublesRarely(t *testing.T) {
	cases := []struct {
		stars uint32
		draws []uint32
		want  int
	}{
		{0, []uint32{0, 99}, 1},
		{3, []uint32{16384, 99}, 4},
		{3, []uint32{16383, 1}, 6},
	}
	for _, c := range cases {
		got, err := SpawnCount(c.stars, sequence(t, c.draws...))
		if err != nil || got != c.want {
			t.Fatalf("count(%d,%v)=%d/%v want %d", c.stars, c.draws, got, err, c.want)
		}
	}
}

/*
================
TestBanditLevelFollowsTheTraderAndTier

Level 50, mastery 60: base 56; tier 3 adds 2; a full float adds 3.
================
*/
func TestBanditLevelFollowsTheTraderAndTier(t *testing.T) {
	got, err := BanditLevel(3, 50, 60, sequence(t, 50, 32767))
	if err != nil || got != 56+4+3-1 {
		t.Fatalf("level=%d/%v want 62", got, err)
	}
	// A low level floors at 16; tier 0 counts as 1, doubled on a 2% draw.
	got, err = BanditLevel(0, 5, 0, sequence(t, 1, 0))
	if err != nil || got != 16+0+2-1 {
		t.Fatalf("floored level=%d/%v want 17", got, err)
	}
}

/*
================
TestTacticsNumbersFollowJobAndParity
================
*/
func TestTacticsNumbersFollowJobAndParity(t *testing.T) {
	cases := []struct {
		job   uint8
		draws []uint32
		want  uint32
	}{
		{1, []uint32{2}, 2004},
		{1, []uint32{3, 6}, 2003},
		{3, []uint32{0}, 2014},
		{2, []uint32{1, 4}, 2011},
	}
	for _, c := range cases {
		got, err := TacticsID(c.job, sequence(t, c.draws...))
		if err != nil || got != c.want {
			t.Fatalf("tactics(%d,%v)=%d want %d", c.job, c.draws, got, c.want)
		}
	}
	if !Thieves(1) || Thieves(2) || Thieves(3) {
		t.Fatal("thieves ambush traders only")
	}
}

/*
================
TestRegistryKeepsATimerAndFiresWhenDue

The timer is 60..120 s; a re-registration keeps the running timer.
================
*/
func TestRegistryKeepsATimerAndFiresWhenDue(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("d", "trader", sequence(t, 0)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("d", "trader", sequence(t)); err != nil {
		t.Fatal(err)
	}
	due, err := registry.Due(59999, sequence(t))
	if err != nil || len(due) != 0 {
		t.Fatalf("fired early: %v/%v", due, err)
	}
	due, err = registry.Due(1, sequence(t, 32767))
	if err != nil || len(due) != 1 || due[0].Character != "trader" || due[0].NextMs != 120000 || due[0].ElapsedMs != 0 {
		t.Fatalf("due=%+v/%v", due, err)
	}
	registry.Remove("d", "trader")
	if registry.Len() != 0 {
		t.Fatal("removal kept the caravan")
	}
}
