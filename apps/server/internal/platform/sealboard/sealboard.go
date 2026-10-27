/*
===========================================================================

sealboard.go - the weekly seal-drop counter (#1641, #1604 P9)

Port-only, not native. The service counts sealed equipment drops at
committed monster-loot settlement, keyed by week, character, degree
and seal tier (Star, Moon, Sun), with forced drops (the seal pity
guarantee, #1690) flagged apart. It stores no character name, no job
alias, no drop time and no monster: the public board (a later slice)
joins the character ID to the roster's current job alias at read time,
so a hidden or deleted character leaves the board and an alias change
moves the counts. Rows counted before degrees were kept carry degree
0 (see ensureBoardShape).

Counts live in memory and flush to seal-board.db beside the shard's
authority state every few minutes and at shutdown; a crash loses only the
unflushed increments. Retention is twelve weeks.

Locking is the point of the shape: NoteSealDrop runs inside the loot
settlement, which runs inside an authority store door, so it may only
touch the in-memory rollup. The flush swaps the pending map out under
that same short lock, does its SQLite work with the lock released, and
merges the batch back if the write fails; flushMu single-flights the
flushes and the Counts reads, so a read can never miss increments a
flush has swapped out but not yet written. A stuck database therefore
stalls only the flush goroutine, never a settlement (#1680 review).

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
	"strconv"
	"strings"
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
	// countingPrefix keys a week's counting-since stamp in the meta table.
	countingPrefix = "counting_since:"
)

/*
================
Count

One character's drops of one seal tier at one degree inside one week;
Forced marks seal pity paid drops (#1690). The character ID is internal:
it never leaves the server process or this database.
================
*/
type Count struct {
	CharacterID int64
	Degree      int
	Tier        int
	Forced      bool
	Drops       int64
}

// countKey is the pending rollup's key: week, character, degree, tier,
// forced.
type countKey struct {
	week      int64
	character int64
	degree    int
	tier      int
	forced    bool
}

/*
================
Service

The counter and its store. mu guards only the in-memory rollup (pending,
counting, closed) and is never held across IO; flushMu single-flights
the flush path and Counts, which must observe the rollup and the store
as one.
================
*/
type Service struct {
	db       *sql.DB
	now      func() time.Time
	openedAt time.Time

	flushMu  sync.Mutex
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

Opens (creating on first use) seal-board.db in dir and loads the
counting-since stamps. now is the clock, injected for tests.
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
	if err := ensureBoardShape(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS seal_board_meta (
	key   TEXT PRIMARY KEY,
	value INTEGER NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	if err := service.loadCounting(); err != nil {
		_ = db.Close()
		return nil, err
	}
	// The current week's stamp starts in memory; the first flush persists
	// it (CountingSince never does IO after Open).
	if _, err := service.CountingSince(WeekStart(service.openedAt)); err != nil {
		_ = db.Close()
		return nil, err
	}
	return service, nil
}

// boardCountsSchema is the counts table with the degree and forced
// dimensions (#1690).
const boardCountsSchema = `
CREATE TABLE seal_board_counts (
	week_start_s INTEGER NOT NULL,
	character_id INTEGER NOT NULL,
	degree       INTEGER NOT NULL,
	tier         INTEGER NOT NULL,
	forced       INTEGER NOT NULL,
	drops        INTEGER NOT NULL,
	PRIMARY KEY (week_start_s, character_id, degree, tier, forced)
);`

/*
================
ensureBoardShape

Creates the counts table, or rebuilds a pre-#1690 one (week, character,
tier) with the degree and forced dimensions: its rows keep counting
under degree 0, "counted before degrees", the only degree a note can
never carry. Runs before the service serves anything.
================
*/
func ensureBoardShape(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(seal_board_counts)`)
	if err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	exists, hasDegree := false, false
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var fallback any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &fallback, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("sealboard: %w", err)
		}
		exists = true
		if name == "degree" {
			hasDegree = true
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	if !exists {
		if _, err := db.Exec(boardCountsSchema); err != nil {
			return fmt.Errorf("sealboard: %w", err)
		}
		return nil
	}
	if hasDegree {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{
		`ALTER TABLE seal_board_counts RENAME TO seal_board_counts_before_degrees`,
		boardCountsSchema,
		`INSERT INTO seal_board_counts (week_start_s, character_id, degree, tier, forced, drops)
			SELECT week_start_s, character_id, 0, tier, 0, drops FROM seal_board_counts_before_degrees`,
		`DROP TABLE seal_board_counts_before_degrees`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("sealboard: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	return nil
}

/*
================
loadCounting

Reads every persisted counting-since stamp into memory, once at Open.
================
*/
func (s *Service) loadCounting() error {
	rows, err := s.db.Query(`SELECT key, value FROM seal_board_meta WHERE key LIKE ?`, countingPrefix+"%")
	if err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key string
		var value int64
		if err := rows.Scan(&key, &value); err != nil {
			return fmt.Errorf("sealboard: %w", err)
		}
		week, err := strconv.ParseInt(strings.TrimPrefix(key, countingPrefix), 10, 64)
		if err != nil {
			continue
		}
		s.counting[week] = value
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	return nil
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

When counting for the week opening at weekStart began. Memory only: the
stamp is persisted by the next flush (and at Close), so a restart inside
the week keeps the first open's stamp.
================
*/
func (s *Service) CountingSince(weekStart time.Time) (time.Time, error) {
	week := weekStart.UTC().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	if value, ok := s.counting[week]; ok {
		return time.Unix(value, 0).UTC(), nil
	}
	value := s.countingCandidateLocked(week)
	s.counting[week] = value
	return time.Unix(value, 0).UTC(), nil
}

/*
================
NoteSealDrop

Counts one sealed drop for a character: its degree, its tier and
whether the seal pity guarantee forced it. Invalid identities, degrees
and tiers are refused silently: the settlement path must never fail
over a board count. Memory only - it runs inside the loot settlement's
store door.
================
*/
func (s *Service) NoteSealDrop(characterID int64, degree, tier int, forced bool, at time.Time) {
	if s == nil || characterID <= 0 || degree < 1 || degree > 9 || tier < 0 || tier >= Tiers {
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
	s.pending[countKey{week: week, character: characterID, degree: degree, tier: tier, forced: forced}]++
}

/*
================
Counts

One week's counts, flushed rows and pending increments merged, ordered by
character then tier. The caller joins aliases; IDs stay internal.
flushMu is held so an in-flight flush cannot swap increments out from
under the read; mu is released before the store is read.
================
*/
func (s *Service) Counts(weekStart time.Time) ([]Count, error) {
	week := weekStart.UTC().Unix()
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	s.mu.Lock()
	pending := make(map[countKey]int64, len(s.pending))
	for key, drops := range s.pending {
		pending[key] = drops
	}
	s.mu.Unlock()
	merged := pending
	rows, err := s.db.Query(`SELECT character_id, degree, tier, forced, drops FROM seal_board_counts WHERE week_start_s = ?`, week)
	if err != nil {
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	for rows.Next() {
		var characterID, drops int64
		var degree, tier, forced int
		if err := rows.Scan(&characterID, &degree, &tier, &forced, &drops); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("sealboard: %w", err)
		}
		merged[countKey{week: week, character: characterID, degree: degree, tier: tier, forced: forced != 0}] += drops
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sealboard: %w", err)
	}
	counts := make([]Count, 0, len(merged))
	for key, drops := range merged {
		if key.week == week {
			counts = append(counts, Count{CharacterID: key.character, Degree: key.degree, Tier: key.tier, Forced: key.forced, Drops: drops})
		}
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].CharacterID != counts[j].CharacterID {
			return counts[i].CharacterID < counts[j].CharacterID
		}
		if counts[i].Degree != counts[j].Degree {
			return counts[i].Degree < counts[j].Degree
		}
		if counts[i].Tier != counts[j].Tier {
			return counts[i].Tier < counts[j].Tier
		}
		return !counts[i].Forced && counts[j].Forced
	})
	return counts, nil
}

/*
================
Flush

Writes the pending increments and any new counting-since stamps, then
prunes weeks older than the retained window. Safe to call concurrently.
================
*/
func (s *Service) Flush() error {
	if s == nil {
		return nil
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	return s.flush()
}

/*
================
flush

One flush; the caller holds flushMu. The batch is swapped out under mu
and written with mu released; on failure it merges back, so no count is
lost and no settlement ever waits on the store.
================
*/
func (s *Service) flush() error {
	batch, stamps, closed := s.swap()
	if closed {
		return nil
	}
	if err := s.write(batch, stamps); err != nil {
		s.mu.Lock()
		for key, drops := range batch {
			s.pending[key] += drops
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

/*
================
swap

Takes the pending batch and the counting stamps out of the rollup.
================
*/
func (s *Service) swap() (batch map[countKey]int64, stamps map[int64]int64, closed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, true
	}
	batch = s.pending
	s.pending = make(map[countKey]int64)
	stamps = make(map[int64]int64, len(s.counting))
	for week, since := range s.counting {
		stamps[week] = since
	}
	return batch, stamps, false
}

/*
================
write

One transaction: the batch upserts, the stamps, and the retention prune.
Runs with no service lock held.
================
*/
func (s *Service) write(batch map[countKey]int64, stamps map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for key, drops := range batch {
		forced := 0
		if key.forced {
			forced = 1
		}
		if _, err := tx.Exec(`
INSERT INTO seal_board_counts (week_start_s, character_id, degree, tier, forced, drops)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (week_start_s, character_id, degree, tier, forced)
DO UPDATE SET drops = drops + excluded.drops`, key.week, key.character, key.degree, key.tier, forced, drops); err != nil {
			return fmt.Errorf("sealboard: %w", err)
		}
	}
	for week, since := range stamps {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO seal_board_meta (key, value) VALUES (?, ?)`,
			countingPrefix+strconv.FormatInt(week, 10), since); err != nil {
			return fmt.Errorf("sealboard: %w", err)
		}
	}
	currentWeek := WeekStart(s.now()).Unix()
	oldestKept := currentWeek - (retainedWeeks-1)*weekSeconds
	if _, err := tx.Exec(`DELETE FROM seal_board_counts WHERE week_start_s < ?`, oldestKept); err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM seal_board_meta WHERE key LIKE ? AND CAST(substr(key, ?) AS INTEGER) < ?`,
		countingPrefix+"%", len(countingPrefix)+1, oldestKept); err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
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

Writes what is left and closes the store. Safe to call twice.
================
*/
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	batch := s.pending
	s.pending = make(map[countKey]int64)
	stamps := make(map[int64]int64, len(s.counting))
	for week, since := range s.counting {
		stamps[week] = since
	}
	s.mu.Unlock()
	err := s.write(batch, stamps)
	if closeErr := s.db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("sealboard: %w", err)
	}
	return nil
}
