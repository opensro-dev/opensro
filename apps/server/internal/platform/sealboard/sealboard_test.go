/*
===========================================================================

sealboard_test.go - the weekly seal-drop counter: weeks, tiers, flush

===========================================================================
*/
package sealboard

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// monday is 2026-10-26, a Monday; the week it opens runs to 2026-11-02.
var monday = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

/*
================
fixedClock
================
*/
func fixedClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

/*
================
TestWeekStartOpensMonday
================
*/
func TestWeekStartOpensMonday(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   time.Time
		want time.Time
	}{
		{"monday midnight", monday, monday},
		{"monday late", monday.Add(23*time.Hour + 59*time.Minute), monday},
		{"wednesday", monday.AddDate(0, 0, 2).Add(5 * time.Hour), monday},
		{"sunday is last week", monday.Add(-time.Second), monday.AddDate(0, 0, -7)},
		{"sunday ends the week", monday.AddDate(0, 0, 6).Add(23 * time.Hour), monday},
		{"next monday opens next", monday.AddDate(0, 0, 7), monday.AddDate(0, 0, 7)},
	} {
		if got := WeekStart(tc.at); !got.Equal(tc.want) {
			t.Fatalf("%s: WeekStart(%v) = %v, want %v", tc.name, tc.at, got, tc.want)
		}
	}
}

/*
================
TestNoteRejectsInvalidIdentityAndTier
================
*/
func TestNoteRejectsInvalidIdentityAndTier(t *testing.T) {
	service, err := Open(t.TempDir(), fixedClock(monday.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = service.Close() }()
	service.NoteSealDrop(0, TierSun, monday)
	service.NoteSealDrop(-3, TierSun, monday)
	service.NoteSealDrop(7, -1, monday)
	service.NoteSealDrop(7, Tiers, monday)
	counts, err := service.Counts(monday)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Fatalf("invalid notes counted: %+v", counts)
	}
}

/*
================
TestCountsMergePendingAndFlushed
================
*/
func TestCountsMergePendingAndFlushed(t *testing.T) {
	service, err := Open(t.TempDir(), fixedClock(monday.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = service.Close() }()
	at := monday.Add(2 * time.Hour)
	service.NoteSealDrop(9, TierSun, at)
	service.NoteSealDrop(9, TierSun, at)
	service.NoteSealDrop(9, TierStar, at)
	service.NoteSealDrop(4, TierMoon, at)
	if err := service.Flush(); err != nil {
		t.Fatal(err)
	}
	// One more Sun after the flush: the read merges both halves.
	service.NoteSealDrop(9, TierSun, at)
	counts, err := service.Counts(monday)
	if err != nil {
		t.Fatal(err)
	}
	want := []Count{
		{CharacterID: 4, Tier: TierMoon, Drops: 1},
		{CharacterID: 9, Tier: TierStar, Drops: 1},
		{CharacterID: 9, Tier: TierSun, Drops: 3},
	}
	if len(counts) != len(want) {
		t.Fatalf("counts %+v, want %+v", counts, want)
	}
	for i := range want {
		if counts[i] != want[i] {
			t.Fatalf("counts[%d] %+v, want %+v", i, counts[i], want[i])
		}
	}
}

/*
================
TestCountsSurviveReopen
================
*/
func TestCountsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	opened := monday.Add(3 * time.Hour)
	service, err := Open(dir, fixedClock(opened))
	if err != nil {
		t.Fatal(err)
	}
	service.NoteSealDrop(9, TierMoon, monday.Add(4*time.Hour))
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir, fixedClock(monday.Add(5*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	counts, err := reopened.Counts(monday)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0] != (Count{CharacterID: 9, Tier: TierMoon, Drops: 1}) {
		t.Fatalf("counts after reopen %+v", counts)
	}
	// The counting-since stamp stays at the first open, not the reopen.
	since, err := reopened.CountingSince(monday)
	if err != nil {
		t.Fatal(err)
	}
	if !since.Equal(opened) {
		t.Fatalf("counting since %v, want first open %v", since, opened)
	}
}

/*
================
TestCountingSinceMidWeekOpenAndNextWeek
================
*/
func TestCountingSinceMidWeekOpenAndNextWeek(t *testing.T) {
	opened := monday.AddDate(0, 0, 2).Add(6 * time.Hour) // Wednesday 06:00.
	service, err := Open(t.TempDir(), fixedClock(opened))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = service.Close() }()
	since, err := service.CountingSince(monday)
	if err != nil {
		t.Fatal(err)
	}
	if !since.Equal(opened) {
		t.Fatalf("mid-week open counts from %v, want its own open %v", since, opened)
	}
	next := monday.AddDate(0, 0, 7)
	since, err = service.CountingSince(next)
	if err != nil {
		t.Fatal(err)
	}
	if !since.Equal(next) {
		t.Fatalf("next week counts from %v, want its Monday %v", since, next)
	}
}

/*
================
TestNoteSealDropNeverWaitsOnFlush

The database is locked by someone else, so the flush's transaction
cannot land. NoteSealDrop - which runs inside the loot settlement's
store door - must still return at once, and the count must survive
until the lock clears (#1680 review: the flush may never stall a
settlement behind its sink).
================
*/
func TestNoteSealDropNeverWaitsOnFlush(t *testing.T) {
	dir := t.TempDir()
	service, err := Open(dir, fixedClock(monday.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = service.Close() }()

	other, err := sql.Open("sqlite", filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	ctx := context.Background()
	conn, err := other.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `BEGIN EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	flushed := make(chan error, 1)
	go func() {
		close(started)
		flushed <- service.Flush()
	}()
	<-started
	noted := make(chan struct{})
	go func() {
		service.NoteSealDrop(9, TierSun, monday.Add(2*time.Hour))
		close(noted)
	}()
	select {
	case <-noted:
	case <-time.After(2 * time.Second):
		t.Fatal("NoteSealDrop waited on the flush's locked database")
	}
	select {
	case err := <-flushed:
		t.Fatalf("the flush finished while the database was locked: %v", err)
	default:
	}
	if _, err := conn.ExecContext(ctx, `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	counts, err := service.Counts(monday)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0] != (Count{CharacterID: 9, Tier: TierSun, Drops: 1}) {
		t.Fatalf("counts after the lock cleared: %+v", counts)
	}
}

/*
================
TestFlushPrunesOldWeeks
================
*/
func TestFlushPrunesOldWeeks(t *testing.T) {
	dir := t.TempDir()
	service, err := Open(dir, fixedClock(monday.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	old := monday.AddDate(0, 0, -7*retainedWeeks)
	service.NoteSealDrop(9, TierSun, old)
	service.NoteSealDrop(9, TierSun, monday)
	if err := service.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir, fixedClock(monday.Add(2*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	counts, err := reopened.Counts(old)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Fatalf("old week survived the prune: %+v", counts)
	}
	counts, err = reopened.Counts(monday)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0].Drops != 1 {
		t.Fatalf("current week after prune %+v", counts)
	}
}
