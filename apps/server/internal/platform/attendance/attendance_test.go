/*
===========================================================================

attendance_test.go - the 04:00 day, the month and the check-in store

===========================================================================
*/
package attendance

import (
	"errors"
	"testing"
	"time"
)

// clock is a settable test clock.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func utc(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
}

func openAt(t *testing.T, dir string, c *clock) *Service {
	t.Helper()
	service, err := Open(dir, c.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

/*
================
TestTodayTurnsAtFourUTC

03:59 UTC is still the day before; 04:00 opens the new one. The first
hours of a month belong to the last day of the one before.
================
*/
func TestTodayTurnsAtFourUTC(t *testing.T) {
	cases := []struct {
		at       time.Time
		monthKey int
		day      int
	}{
		{utc(2026, 11, 26, 3, 59), 202611, 25},
		{utc(2026, 11, 26, 4, 0), 202611, 26},
		{utc(2026, 12, 1, 3, 59), 202611, 30},
		{utc(2026, 12, 1, 4, 0), 202612, 1},
		{utc(2027, 1, 1, 2, 0), 202612, 31},
		{utc(2027, 1, 1, 4, 0), 202701, 1},
		{time.Date(2026, 11, 26, 5, 30, 0, 0, time.FixedZone("UTC+2", 2*3600)), 202611, 25},
	}
	for _, tc := range cases {
		key, day := Today(tc.at)
		if key != tc.monthKey || day != tc.day {
			t.Fatalf("Today(%s) = %d/%d, want %d/%d", tc.at, key, day, tc.monthKey, tc.day)
		}
	}
	if got := DayStart(utc(2026, 11, 26, 3, 59)); !got.Equal(utc(2026, 11, 25, 4, 0)) {
		t.Fatalf("DayStart before 04:00 = %s", got)
	}
	if got := NextReset(utc(2026, 11, 26, 4, 0)); !got.Equal(utc(2026, 11, 27, 4, 0)) {
		t.Fatalf("NextReset at 04:00 = %s", got)
	}
}

/*
================
TestDaysIn
================
*/
func TestDaysIn(t *testing.T) {
	for key, want := range map[int]int{202601: 31, 202602: 28, 202802: 29, 202604: 30, 202612: 31} {
		if got := DaysIn(key); got != want {
			t.Fatalf("DaysIn(%d) = %d, want %d", key, got, want)
		}
	}
}

/*
================
TestTracksCountTheMonth

Accumulation counts the checked days; continuity is the longest run,
kept after a later gap.
================
*/
func TestTracksCountTheMonth(t *testing.T) {
	var m Month
	for _, day := range []int{1, 2, 3, 4, 5, 7, 8, 31} {
		m.Checked |= 1 << (day - 1)
	}
	if got := m.Accumulation(); got != 8 {
		t.Fatalf("accumulation %d, want 8", got)
	}
	if got := m.Continuity(); got != 5 {
		t.Fatalf("continuity %d, want 5", got)
	}
	if m.CheckedOn(6) || !m.CheckedOn(31) || m.CheckedOn(0) || m.CheckedOn(32) {
		t.Fatal("CheckedOn disagrees with the bits")
	}
	if (Month{}).Continuity() != 0 || (Month{Checked: 0xffffffff >> 1}).Continuity() != 31 {
		t.Fatal("continuity edges")
	}
}

/*
================
TestCheckInOncePerDay

A first check-in marks the day and persists across a reopen; a second
the same attendance day writes nothing; the next day adds its bit.
================
*/
func TestCheckInOncePerDay(t *testing.T) {
	dir := t.TempDir()
	c := &clock{at: utc(2026, 11, 26, 4, 0)}
	service := openAt(t, dir, c)

	if m, err := service.Current(7); err != nil || m != (Month{Key: 202611}) {
		t.Fatalf("empty month %+v, %v", m, err)
	}
	m, checked, err := service.CheckIn(7)
	if err != nil || !checked || !m.CheckedOn(26) || m.Accumulation() != 1 {
		t.Fatalf("first check-in %+v, %v, %v", m, checked, err)
	}
	c.at = utc(2026, 11, 27, 3, 59)
	if _, checked, err := service.CheckIn(7); err != nil || checked {
		t.Fatalf("same-day check-in checked=%v, %v", checked, err)
	}
	c.at = utc(2026, 11, 27, 4, 0)
	if m, checked, err := service.CheckIn(7); err != nil || !checked || m.Continuity() != 2 {
		t.Fatalf("next-day check-in %+v, %v, %v", m, checked, err)
	}
	if m, _ := service.Current(8); m.Checked != 0 {
		t.Fatalf("another character sees %+v", m)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CheckIn(7); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed check-in %v", err)
	}

	reopened := openAt(t, dir, c)
	m, err = reopened.Current(7)
	if err != nil || !m.CheckedOn(26) || !m.CheckedOn(27) || m.Accumulation() != 2 {
		t.Fatalf("reopened %+v, %v", m, err)
	}
	if _, _, err := reopened.CheckIn(0); err == nil {
		t.Fatal("character 0 checked in")
	}
}

/*
================
TestNewMonthStartsEmpty

The month turns at 04:00 UTC on the 1st; the old month's row stays
behind, and retention prunes it once it ages out.
================
*/
func TestNewMonthStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	c := &clock{at: utc(2026, 12, 1, 3, 0)}
	service := openAt(t, dir, c)
	if m, _, err := service.CheckIn(7); err != nil || m.Key != 202611 || !m.CheckedOn(30) {
		t.Fatalf("late-night check-in %+v, %v", m, err)
	}
	c.at = utc(2026, 12, 1, 4, 0)
	m, err := service.Current(7)
	if err != nil || m != (Month{Key: 202612}) {
		t.Fatalf("new month %+v, %v", m, err)
	}
	if m, checked, err := service.CheckIn(7); err != nil || !checked || m.Key != 202612 || !m.CheckedOn(1) {
		t.Fatalf("first of the month %+v, %v, %v", m, checked, err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}

	c.at = utc(2028, 1, 15, 12, 0)
	pruned := openAt(t, dir, c)
	var rows int
	if err := pruned.db.QueryRow(`SELECT COUNT(*) FROM attendance_months`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("after retention %d rows, want only 202612", rows)
	}
}

/*
================
TestClaimTiersOnce

A claim needs the days, pays once, and Unclaim clears the bit for a
failed grant rollback. Continuity and accumulation are separate tracks.
================
*/
func TestClaimTiersOnce(t *testing.T) {
	c := &clock{at: utc(2026, 11, 10, 12, 0)}
	service := openAt(t, t.TempDir(), c)
	for day := 1; day <= 5; day++ {
		c.at = utc(2026, 11, day, 12, 0)
		if _, _, err := service.CheckIn(7); err != nil {
			t.Fatal(err)
		}
	}
	c.at = utc(2026, 11, 10, 12, 0)
	m, err := service.ClaimContinuity(7, 0)
	if err != nil || m.ContinuityClaimed&1 == 0 || m.Continuity() != 5 {
		t.Fatalf("continuity claim %+v, %v", m, err)
	}
	if _, err := service.ClaimContinuity(7, 0); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("repeat continuity %v", err)
	}
	if _, err := service.ClaimContinuity(7, 1); !errors.Is(err, ErrNotEarned) {
		t.Fatalf("uneared continuity tier 1 %v", err)
	}
	if _, err := service.ClaimContinuity(7, 99); !errors.Is(err, ErrBadTier) {
		t.Fatalf("bad continuity tier %v", err)
	}

	m, err = service.ClaimAccumulation(7, 0)
	if err != nil || m.AccumulationClaimed&1 == 0 {
		t.Fatalf("accumulation claim %+v, %v", m, err)
	}
	if _, err := service.ClaimAccumulation(7, 0); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("repeat accumulation %v", err)
	}
	if err := service.UnclaimAccumulation(7, m.Key, 0); err != nil {
		t.Fatal(err)
	}
	m, err = service.Current(7)
	if err != nil || m.AccumulationClaimed&1 != 0 || m.ContinuityClaimed&1 == 0 {
		t.Fatalf("after unclaim %+v, %v", m, err)
	}
	if RewardFor(0) != RewardCodename || RewardFor(4) != "ITEM_CH_TBLADE_09_C_RARE" {
		t.Fatalf("reward %q", RewardFor(0))
	}
}

/*
================
TestUnclaimRollsBackTheClaimedMonth

A grant refused across the 04:00 UTC turn of the 1st rolls back the
month the claim marked, and leaves the new month untouched.
================
*/
func TestUnclaimRollsBackTheClaimedMonth(t *testing.T) {
	c := &clock{at: time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC)}
	service := openAt(t, t.TempDir(), c)
	for day := 1; day <= 5; day++ {
		c.at = time.Date(2026, 11, day, 5, 0, 0, 0, time.UTC)
		if _, _, err := service.CheckIn(7); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := service.ClaimAccumulation(7, 0)
	if err != nil {
		t.Fatal(err)
	}
	c.at = time.Date(2026, 12, 1, 4, 0, 0, 0, time.UTC)
	if _, _, err := service.CheckIn(7); err != nil {
		t.Fatal(err)
	}
	if err := service.UnclaimAccumulation(7, claimed.Key, 0); err != nil {
		t.Fatal(err)
	}
	var bitsLeft uint8
	if err := service.db.QueryRow(`SELECT accumulation_claimed FROM attendance_months
WHERE character_id = 7 AND month_key = 202611`).Scan(&bitsLeft); err != nil || bitsLeft != 0 {
		t.Fatalf("November claim bits %d, %v; want 0", bitsLeft, err)
	}
	december, err := service.Current(7)
	if err != nil || december.Key != 202612 || december.Checked != 1 || december.AccumulationClaimed != 0 {
		t.Fatalf("December %+v, %v", december, err)
	}
}
