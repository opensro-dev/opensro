/*
===========================================================================

jobhonor_test.go - the job honor ledger (#3530)

The account cap is shared by its characters and survives a restart, the
honor day opens at 05:00 UTC, the tiers follow the owner's cuts, and the
change at the boundary snapshots the rolling window once.

===========================================================================
*/
package jobhonor

import (
	"testing"
	"time"
)

/*
================
clock

A settable test clock.
================
*/
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

/*
================
openAt
================
*/
func openAt(t *testing.T, dir string, c *clock) *Service {
	t.Helper()
	s, err := Open(dir, c.now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHonorDayOpensAtFiveUTC(t *testing.T) {
	before := time.Date(2026, 11, 16, 4, 59, 0, 0, time.UTC)
	after := time.Date(2026, 11, 16, 5, 0, 0, 0, time.UTC)
	if got := DayStart(before); !got.Equal(time.Date(2026, 11, 15, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("04:59 belongs to the day that opened yesterday at 05:00, got %v", got)
	}
	if got := DayStart(after); !got.Equal(after) {
		t.Fatalf("05:00 opens a day, got %v", got)
	}
	if got := NextChange(before); !got.Equal(after) {
		t.Fatalf("the next change after 04:59 is 05:00, got %v", got)
	}
}

func TestTheCapIsSharedByAnAccountsCharacters(t *testing.T) {
	c := &clock{at: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	s := openAt(t, t.TempDir(), c)
	defer s.Close()
	if got := s.Award(Award{Account: "alt", CharacterID: 1, Job: JobTrader, Points: 700, At: c.at}); got != 700 {
		t.Fatalf("first character kept %d of 700", got)
	}
	if got := s.Award(Award{Account: "alt", CharacterID: 2, Job: JobThief, Points: 700, At: c.at}); got != DailyCap-700 {
		t.Fatalf("second character of the account kept %d, want the %d the cap leaves", got, DailyCap-700)
	}
	if got := s.Award(Award{Account: "alt", CharacterID: 3, Job: JobTrader, Points: 1, At: c.at}); got != 0 {
		t.Fatalf("a spent cap kept %d", got)
	}
	if got := s.Award(Award{Account: "other", CharacterID: 4, Job: JobTrader, Points: 50, At: c.at}); got != 50 {
		t.Fatalf("another account kept %d of 50", got)
	}
	next := c.at.Add(24 * time.Hour)
	if got := s.Award(Award{Account: "alt", CharacterID: 1, Job: JobTrader, Points: 10, At: next}); got != 10 {
		t.Fatalf("the next honor day kept %d of 10", got)
	}
}

func TestTheCapSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	c := &clock{at: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	s := openAt(t, dir, c)
	s.Award(Award{Account: "alt", CharacterID: 1, Job: JobTrader, Points: 900, At: c.at})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openAt(t, dir, c)
	defer s.Close()
	if got := s.Award(Award{Account: "alt", CharacterID: 2, Job: JobTrader, Points: 500, At: c.at}); got != 100 {
		t.Fatalf("after a restart the account kept %d, want 100", got)
	}
}

func TestAwardRefusesWhatIsNotAJobEvent(t *testing.T) {
	c := &clock{at: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	s := openAt(t, t.TempDir(), c)
	defer s.Close()
	for _, a := range []Award{
		{Account: "", CharacterID: 1, Job: JobTrader, Points: 5, At: c.at},
		{Account: "a", CharacterID: 0, Job: JobTrader, Points: 5, At: c.at},
		{Account: "a", CharacterID: 1, Job: 0, Points: 5, At: c.at},
		{Account: "a", CharacterID: 1, Job: JobTrader, Points: -5, At: c.at},
		{Account: "a", CharacterID: 1, Job: JobTrader, Points: 5, At: c.at.Add(-48 * time.Hour)},
	} {
		if got := s.Award(a); got != 0 {
			t.Fatalf("%+v kept %d", a, got)
		}
	}
}

func TestTiersFollowTheOwnersCuts(t *testing.T) {
	var standings []Standing
	for id := int64(1); id <= 20; id++ {
		standings = append(standings, Standing{CharacterID: id, Job: JobTrader, Points: 1000 - id})
	}
	standings = append(standings, Standing{CharacterID: 99, Job: JobThief, Points: 1})
	AssignTiers(standings)
	want := map[int64]int{1: TierKing, 2: TierGold, 3: TierSilver, 6: TierSilver, 7: TierNone, 20: TierNone, 99: TierKing}
	for _, row := range standings {
		if tier, named := want[row.CharacterID]; named && row.Tier != tier {
			t.Fatalf("character %d (rank %d of its job) got tier %d, want %d", row.CharacterID, row.Rank, row.Tier, tier)
		}
	}
}

func TestTheChangeSnapshotsTheWindowOnce(t *testing.T) {
	c := &clock{at: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	s := openAt(t, t.TempDir(), c)
	defer s.Close()
	s.Award(Award{Account: "a", CharacterID: 1, Job: JobTrader, Points: 300, At: c.at})
	s.Award(Award{Account: "b", CharacterID: 2, Job: JobTrader, Points: 100, At: c.at})
	current, err := s.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 2 || current[0].CharacterID != 1 || current[0].Tier != TierKing || current[0].Held != TierNone {
		t.Fatalf("before any change: %+v", current)
	}
	// The first pass writes the snapshot for today's boundary (05:00),
	// which ranks the window before it: nothing earned yet.
	if wrote, err := s.Recompute(); err != nil || !wrote {
		t.Fatalf("first change: wrote=%v err=%v", wrote, err)
	}
	if wrote, _ := s.Recompute(); wrote {
		t.Fatal("the same boundary changed twice")
	}
	c.at = time.Date(2026, 11, 17, 5, 0, 30, 0, time.UTC)
	if wrote, err := s.Recompute(); err != nil || !wrote {
		t.Fatalf("next boundary: wrote=%v err=%v", wrote, err)
	}
	previous, period, err := s.Previous()
	if err != nil {
		t.Fatal(err)
	}
	if !period.Equal(time.Date(2026, 11, 17, 5, 0, 0, 0, time.UTC)) || len(previous) != 2 || previous[0].Tier != TierKing {
		t.Fatalf("snapshot %v: %+v", period, previous)
	}
	current, _ = s.Current()
	if current[0].Held != TierKing {
		t.Fatalf("the snapshot's tier is not held now: %+v", current[0])
	}
	// Eight days later the points fell out of the window.
	c.at = time.Date(2026, 11, 25, 6, 0, 0, 0, time.UTC)
	if current, _ = s.Current(); len(current) != 0 {
		t.Fatalf("points past the window still ranked: %+v", current)
	}
}
