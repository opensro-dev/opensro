/*
===========================================================================

sealboard.go - the weekly seal-drop counter (#1641, #1604 P9)

Port-only, not native. The service counts sealed equipment drops at
committed monster-loot settlement, keyed by week, character and seal tier
(Star, Moon, Sun). It stores no character name, no job alias, no drop time
and no monster: the public board (a later slice) joins the character ID to
the roster's current job alias at read time, so a hidden or deleted
character leaves the board and an alias change moves the counts.

Counts live in memory and flush to seal-board.db beside the shard's
authority state every few minutes and at shutdown; a crash loses only the
unflushed increments. Retention is twelve weeks.

===========================================================================
*/
package sealboard

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// FileName is the seal board database beside the shard authority state.
const FileName = "seal-board.db"

// Tier indexes: the board counts Star, Moon and Sun apart.
const (
	TierStar = 0
	TierMoon = 1
	TierSun  = 2
)

// Tiers is how many seal tiers the board counts.
const Tiers = 3

const (
	// weekSeconds is one week in seconds; weeks open Monday 00:00 UTC.
	weekSeconds = 7 * 24 * 60 * 60
	// retainedWeeks bounds the stored history.
	retainedWeeks = 12
	// flushInterval is how often Run writes the pending counts.
	flushInterval = 5 * time.Minute
)

/*
================
Count

One character's drops of one seal tier inside one week. The character ID
is internal: it never leaves the server process or this database.
================
*/
type Count struct {
	CharacterID int64
	Tier        int
	Drops       int64
}

// countKey is the pending rollup's key: week, character, tier.
type countKey struct {
	week      int64
	character int64
	tier      int
}

/*
================
Service

The counter and its store. NoteSealDrop is one map increment after a loot
commit: no IO and no store door on the tick path. Flush holds the same
mutex across its transaction, so a Count read can never miss increments
swapped out from under it.
================
*/
type Service struct {
	db       *sql.DB
	now      func() time.Time
	openedAt time.Time

	mu       sync.Mutex
	pending  map[countKey]int64
	counting map[int64]int64
	closed   bool
}

/*
================
WeekStart

The Monday 00:00 UTC opening the week that contains t.
================
*/
func WeekStart(t time.Time) time.Time {
	t = t.UTC()
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	// Weekday runs Sunday=0..Saturday=6; Monday is six days after Sunday.
	back := (int(t.Weekday()) + 6) % 7
	return midnight.AddDate(0, 0, -back)
}

/*
================
Open

Opens (creating on first use) seal-board.db in dir and loads the current
week's counting-since stamp. now is the clock, injected for tests.
================
*/
func Open(dir string, now func() time.Time) (*Service, error) {
	if dir == "" {
		return nil, fmt.Errorf("sealboard: no directory")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, FileName))
	if err != nil {
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	db.SetMaxOpenConns(1)
	service := &Service{
		db:       db,
		now:      now,
		openedAt: now().UTC(),
		pending:  make(map[countKey]int64),
		counting: make(map[int64]int64),
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS seal_board_counts (
	week_start_s INTEGER NOT NULL,
	character_id INTEGER NOT NULL,
	tier         INTEGER NOT NULL,
	drops        INTEGER NOT NULL,
	PRIMARY KEY (week_start_s, character_id, tier)
);
CREATE TABLE IF NOT EXISTS seal_board_meta (
	key   TEXT PRIMARY KEY,
	value INTEGER NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	if _, err := service.CountingSince(WeekStart(service.openedAt)); err != nil {
		_ = db.Close()
		return nil, err
	}
	return service, nil
}

/*
================
countingCandidateLocked

When counting for weekStart began, as far as this process knows: the week
opening, unless the service itself opened later inside that same week
(a mid-week enable counts from its own first open, never backfilled).
================
*/
func (s *Service) countingCandidateLocked(weekStart int64) int64 {
	candidate := weekStart
	if opened := s.openedAt.Unix(); opened > candidate && opened < weekStart+weekSeconds {
		candidate = opened
	}
	return candidate
}

/*
================
CountingSince

When counting for the week opening at weekStart began, persisted so a
restart inside the week keeps the first open's stamp.
================
*/
func (s *Service) CountingSince(weekStart time.Time) (time.Time, error) {
	week := weekStart.UTC().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	if value, ok := s.counting[week]; ok {
		return time.Unix(value, 0).UTC(), nil
	}
	key := fmt.Sprintf("counting_since:%d", week)
	var stored int64
	err := s.db.QueryRow(`SELECT value FROM seal_board_meta WHERE key = ?`, key).Scan(&stored)
	switch {
	case err == nil:
		s.counting[week] = stored
		return time.Unix(stored, 0).UTC(), nil
	case err != sql.ErrNoRows:
		return time.Time{}, fmt.Errorf("sealboard: %w", err)
	}
	value := s.countingCandidateLocked(week)
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO seal_board_meta (key, value) VALUES (?, ?)`, key, value); err != nil {
		return time.Time{}, fmt.Errorf("sealboard: %w", err)
	}
	s.counting[week] = value
	return time.Unix(value, 0).UTC(), nil
}

/*
================
NoteSealDrop

Counts one sealed drop for a character. Invalid identities and tiers are
refused silently: the settlement path must never fail over a board count.
================
*/
func (s *Service) NoteSealDrop(characterID int64, tier int, at time.Time) {
	if s == nil || characterID <= 0 || tier < 0 || tier >= Tiers {
		return
	}
	if at.IsZero() {
		at = s.now()
	}
	week := WeekStart(at).Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if _, ok := s.counting[week]; !ok {
		s.counting[week] = s.countingCandidateLocked(week)
	}
	s.pending[countKey{week: week, character: characterID, tier: tier}]++
}

/*
================
Counts

One week's counts, flushed rows and pending increments merged, ordered by
character then tier. The caller joins aliases; IDs stay internal.
================
*/
func (s *Service) Counts(weekStart time.Time) ([]Count, error) {
	week := weekStart.UTC().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	merged := make(map[countKey]int64)
	rows, err := s.db.Query(`SELECT character_id, tier, drops FROM seal_board_counts WHERE week_start_s = ?`, week)
	if err != nil {
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	for rows.Next() {
		var characterID, drops int64
		var tier int
		if err := rows.Scan(&characterID, &tier, &drops); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("sealboard: %w", err)
		}
		merged[countKey{week: week, character: characterID, tier: tier}] += drops
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	for key, drops := range s.pending {
		if key.week == week {
			merged[key] += drops
		}
	}
	counts := make([]Count, 0, len(merged))
	for key, drops := range merged {
		counts = append(counts, Count{CharacterID: key.character, Tier: key.tier, Drops: drops})
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].CharacterID != counts[j].CharacterID {
			return counts[i].CharacterID < counts[j].CharacterID
		}
		return counts[i].Tier < counts[j].Tier
	})
	return counts, nil
}

/*
================
Flush

Writes the pending increments and any new counting-since stamps in one
transaction, then prunes weeks older than the retained window.
================
*/
func (s *Service) Flush() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

/*
================
flushLocked
================
*/
func (s *Service) flushLocked() error {
	if s.closed && s.db == nil {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for key, drops := range s.pending {
		if _, err := tx.Exec(`
INSERT INTO seal_board_counts (week_start_s, character_id, tier, drops)
VALUES (?, ?, ?, ?)
ON CONFLICT (week_start_s, character_id, tier)
DO UPDATE SET drops = drops + excluded.drops`, key.week, key.character, key.tier, drops); err != nil {
			return fmt.Errorf("sealboard: %w", err)
		}
	}
	for week, since := range s.counting {
		key := fmt.Sprintf("counting_since:%d", week)
		if _, err := tx.Exec(`INSERT OR IGNORE INTO seal_board_meta (key, value) VALUES (?, ?)`, key, since); err != nil {
			return fmt.Errorf("sealboard: %w", err)
		}
	}
	currentWeek := WeekStart(s.now()).Unix()
	oldestKept := currentWeek - (retainedWeeks-1)*weekSeconds
	if _, err := tx.Exec(`DELETE FROM seal_board_counts WHERE week_start_s < ?`, oldestKept); err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM seal_board_meta WHERE key LIKE 'counting_since:%' AND CAST(substr(key, 16) AS INTEGER) < ?`, oldestKept); err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	s.pending = make(map[countKey]int64)
	return nil
}

/*
================
Run

Flushes every flushInterval until ctx ends, then flushes once more.
================
*/
func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := s.Flush(); err != nil {
				log.WithError(err).Warn("sealboard: final flush failed")
			}
			return
		case <-ticker.C:
			if err := s.Flush(); err != nil {
				log.WithError(err).Warn("sealboard: flush failed")
			}
		}
	}
}

/*
================
Close

Flushes and closes the store. Safe to call twice.
================
*/
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.flushLocked()
	if closeErr := s.db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	return nil
}
