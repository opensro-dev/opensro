/*
===========================================================================

attendance.go - the attendance ledger: monthly check-ins per character (#3255)

Port-only, not native (SRO_ATTENDANCE). Neither the v1.150 client nor the
v1.188 reference server has an attendance calendar; the owner's ask
(2026-11-12) comes from a v2.355 third-party client's screenshots. This
owner keeps each character's month: which days it checked in and which
reward tiers it has received.

  - An attendance day opens at ResetHourUTC (04:00 UTC, the server
    clock), so a session that plays past midnight UTC checks in once.
  - A month is the calendar month of its attendance days: the 1st opens
    at 04:00 UTC on the 1st and the month closes at 04:00 UTC on the
    1st of the next. Every month starts empty.
  - Continuity is the longest run of consecutive checked days in the
    month, accumulation the count of checked days. INFERENCE (#3255
    names the tiers, not the run rule): the longest run, not the run
    ending today, so a reached tier stays claimable after a missed day.
  - Each track pays each of its tiers at most once per month; the claim
    bits live beside the day bits.

Check-ins are rare (one per character per day), so they are written
through, not batched. Every read and write holds mu across its IO; no
caller may hold an authority store door while calling in. The current
month's rows are cached once read, so the feed's polls stay in memory.

===========================================================================
*/
package attendance

import (
	"database/sql"
	"errors"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// FileName is the ledger database beside the shard authority state.
const FileName = "attendance.db"

// ResetHourUTC opens each attendance day (owner's choice, #3255).
const ResetHourUTC = 4

// The reward tiers in days, lowest first (#3255).
var (
	ContinuityTiers   = [...]int{3, 8, 16, 23, 30}
	AccumulationTiers = [...]int{5, 10, 15, 21, 27}
)

// retainedMonths bounds the stored months; older rows are pruned at Open.
const retainedMonths = 13

// ErrClosed is returned by a ledger that has been closed.
var ErrClosed = errors.New("attendance: closed")

/*
================
Month

One character's month: Key is YYYYMM, Checked has bit d-1 set for each
checked day d, and the claim masks have bit i set for each received
tier i of ContinuityTiers / AccumulationTiers.
================
*/
type Month struct {
	Key                 int
	Checked             uint32
	ContinuityClaimed   uint8
	AccumulationClaimed uint8
}

/*
================
Month::CheckedOn
================
*/
func (m Month) CheckedOn(day int) bool {
	return day >= 1 && day <= 31 && m.Checked&(1<<(day-1)) != 0
}

/*
================
Month::Accumulation

The checked days.
================
*/
func (m Month) Accumulation() int {
	return bits.OnesCount32(m.Checked)
}

/*
================
Month::Continuity

The longest run of consecutive checked days.
================
*/
func (m Month) Continuity() int {
	longest, run := 0, 0
	for day := 1; day <= 31; day++ {
		if m.CheckedOn(day) {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

/*
================
attendanceDate

The calendar date an instant's attendance day carries.
================
*/
func attendanceDate(t time.Time) (int, time.Month, int) {
	return t.UTC().Add(-ResetHourUTC * time.Hour).Date()
}

/*
================
Today

The month key (YYYYMM) and day of the month of the attendance day that
contains t.
================
*/
func Today(t time.Time) (monthKey, day int) {
	year, month, day := attendanceDate(t)
	return year*100 + int(month), day
}

/*
================
DayStart

The 04:00 UTC opening the attendance day that contains t.
================
*/
func DayStart(t time.Time) time.Time {
	year, month, day := attendanceDate(t)
	return time.Date(year, month, day, ResetHourUTC, 0, 0, 0, time.UTC)
}

/*
================
NextReset

When the attendance day after t's opens.
================
*/
func NextReset(t time.Time) time.Time {
	return DayStart(t).AddDate(0, 0, 1)
}

/*
================
DaysIn

The days of the month a key (YYYYMM) names.
================
*/
func DaysIn(monthKey int) int {
	first := time.Date(monthKey/100, time.Month(monthKey%100), 1, 0, 0, 0, 0, time.UTC)
	return first.AddDate(0, 1, -1).Day()
}

/*
================
Service

The ledger and its store. mu guards the cache and the database.
================
*/
type Service struct {
	db  *sql.DB
	now func() time.Time
	mu  sync.Mutex
	// month is the key the cache holds; a new month empties it.
	month  int
	cache  map[int64]Month
	closed bool
}

/*
================
Open

Opens (creating on first use) attendance.db in dir and prunes months
past retention. now is the clock, injected for tests.
================
*/
func Open(dir string, now func() time.Time) (*Service, error) {
	if dir == "" {
		return nil, fmt.Errorf("attendance: no directory")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("attendance: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, FileName))
	if err != nil {
		return nil, fmt.Errorf("attendance: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS attendance_months (
	character_id         INTEGER NOT NULL,
	month_key            INTEGER NOT NULL,
	checked_bits         INTEGER NOT NULL DEFAULT 0,
	continuity_claimed   INTEGER NOT NULL DEFAULT 0,
	accumulation_claimed INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (character_id, month_key)
);`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("attendance: %w", err)
	}
	year, month, _ := attendanceDate(now())
	horizon := time.Date(year, month-retainedMonths, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.Exec(`DELETE FROM attendance_months WHERE month_key < ?`,
		horizon.Year()*100+int(horizon.Month())); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("attendance: %w", err)
	}
	return &Service{db: db, now: now, cache: map[int64]Month{}}, nil
}

/*
================
loadLocked

The character's row for monthKey, from the cache when it is the
cache's month. A missing row is an empty month.
================
*/
func (s *Service) loadLocked(characterID int64, monthKey int) (Month, error) {
	if s.closed {
		return Month{}, ErrClosed
	}
	if monthKey != s.month {
		s.month, s.cache = monthKey, map[int64]Month{}
	}
	if m, ok := s.cache[characterID]; ok {
		return m, nil
	}
	m := Month{Key: monthKey}
	err := s.db.QueryRow(`SELECT checked_bits, continuity_claimed, accumulation_claimed
FROM attendance_months WHERE character_id = ? AND month_key = ?`, characterID, monthKey).
		Scan(&m.Checked, &m.ContinuityClaimed, &m.AccumulationClaimed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Month{}, fmt.Errorf("attendance: %w", err)
	}
	s.cache[characterID] = m
	return m, nil
}

/*
================
Service::Current

The character's month as of the clock.
================
*/
func (s *Service) Current(characterID int64) (Month, error) {
	key, _ := Today(s.now())
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(characterID, key)
}

/*
================
Service::CheckIn

Marks today checked for the character. checked is false when today was
already checked, and nothing is written.
================
*/
func (s *Service) CheckIn(characterID int64) (month Month, checked bool, err error) {
	if characterID <= 0 {
		return Month{}, false, fmt.Errorf("attendance: character %d", characterID)
	}
	key, day := Today(s.now())
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadLocked(characterID, key)
	if err != nil {
		return Month{}, false, err
	}
	if m.CheckedOn(day) {
		return m, false, nil
	}
	bit := uint32(1) << (day - 1)
	if _, err := s.db.Exec(`INSERT INTO attendance_months (character_id, month_key, checked_bits)
VALUES (?, ?, ?)
ON CONFLICT (character_id, month_key) DO UPDATE SET checked_bits = checked_bits | excluded.checked_bits`,
		characterID, key, bit); err != nil {
		return Month{}, false, fmt.Errorf("attendance: %w", err)
	}
	m.Checked |= bit
	s.cache[characterID] = m
	return m, true, nil
}

/*
================
Service::Now

The ledger's clock.
================
*/
func (s *Service) Now() time.Time {
	if s == nil || s.now == nil {
		return time.Now()
	}
	return s.now()
}

/*
================
Service::Close

Closes the store; later calls return ErrClosed.
================
*/
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}
