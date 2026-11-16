/*
===========================================================================

jobhonor.go - the job honor ledger: points, the account cap and tiers (#3530)

Port-only, not native (SRO_JOB_HONOR, owner-approved spec 2026-11-15). The
later reference server has no job honor: its only honor system is the
Academy's (the #3530 native check). The game side (game/action/jobhonor.go)
scores server-verified job events and hands the points here; this owner
keeps them and ranks them.

  - Points are kept per honor day, character and job. An honor day opens
    at TierHourUTC (05:00 UTC, the server clock) so the daily cap and the
    tier change share one boundary.
  - The cap is DailyCap points per account per honor day, shared by every
    character of the account: a per-character cap would multiply with alts.
  - At each boundary the last WindowDays honor days are ranked per job and
    the tiers written as one snapshot: King is the job's #1, Gold the top
    tenth, Silver the top three tenths.

Locking follows platform/sealboard: Award runs inside a trade settlement,
inside an authority store door, so it touches only memory. The account
day totals it caps against are loaded once at Open for the current day; a
new day starts empty, so no award ever reads the database. Flush swaps the
pending map out under the short lock and writes it with the lock released,
merging the batch back if the write fails. flushMu single-flights the
flushes and every read, so a read never misses points a flush holds.

===========================================================================
*/
package jobhonor

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// FileName is the ledger database beside the shard authority state.
const FileName = "job-honor.db"

// The owner's numbers (#3530 spec).
const (
	// DailyCap is the points one account may earn in one honor day.
	DailyCap = 1000
	// TierHourUTC opens each honor day and changes the tiers.
	TierHourUTC = 5
	// WindowDays is the rolling ranking window, in honor days.
	WindowDays = 7
)

// Jobs the ledger ranks, by their domain job type values.
const (
	JobTrader = 1
	JobThief  = 2
	JobHunter = 3
)

// Tiers, lowest first.
const (
	TierNone   = 0
	TierSilver = 1
	TierGold   = 2
	TierKing   = 3
)

const (
	// retainedDays bounds the stored points and snapshots.
	retainedDays = 35
	// flushInterval is how often Run writes the pending points; the
	// boundary check runs on the same ticker.
	flushInterval = time.Minute
	// lastPeriodKey keys the newest snapshot's period in the meta table.
	lastPeriodKey = "last_period"
	// The tier cuts, as fractions of the ranked characters, rounded up.
	goldShare   = 0.10
	silverShare = 0.30
)

/*
================
Award

One scored job event. Points are what the rules gave it before the cap;
Award returns what the cap let through.
================
*/
type Award struct {
	Account     string
	CharacterID int64
	Job         int
	Points      int
	At          time.Time
}

/*
================
Standing

One character's points for one job over a window, its place among the
job's ranked characters and the tier that place earns. Held is the tier
the newest snapshot gave it (TierNone when it was not ranked).
================
*/
type Standing struct {
	CharacterID int64
	Job         int
	Points      int64
	Rank        int
	Tier        int
	Held        int
}

// pointKey is the pending rollup's key.
type pointKey struct {
	day       int64
	character int64
	job       int
	account   string
}

/*
================
Service

The ledger and its store. mu guards the in-memory state and is never held
across IO; flushMu single-flights the flush and the reads.
================
*/
type Service struct {
	db      *sql.DB
	now     func() time.Time
	flushMu sync.Mutex
	mu      sync.Mutex
	pending map[pointKey]int64
	// day and used are the current honor day's account totals, flushed
	// and pending together.
	day    int64
	used   map[string]int
	closed bool
}

/*
================
DayStart

The 05:00 UTC opening the honor day that contains t.
================
*/
func DayStart(t time.Time) time.Time {
	t = t.UTC()
	start := time.Date(t.Year(), t.Month(), t.Day(), TierHourUTC, 0, 0, 0, time.UTC)
	if t.Before(start) {
		start = start.AddDate(0, 0, -1)
	}
	return start
}

/*
================
NextChange

When the tiers next change after t.
================
*/
func NextChange(t time.Time) time.Time {
	return DayStart(t).AddDate(0, 0, 1)
}

/*
================
Open

Opens (creating on first use) job-honor.db in dir and loads the current
honor day's account totals. now is the clock, injected for tests.
================
*/
func Open(dir string, now func() time.Time) (*Service, error) {
	if dir == "" {
		return nil, fmt.Errorf("jobhonor: no directory")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("jobhonor: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, FileName))
	if err != nil {
		return nil, fmt.Errorf("jobhonor: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS job_honor_points (
	day_s        INTEGER NOT NULL,
	character_id INTEGER NOT NULL,
	job          INTEGER NOT NULL,
	account_id   TEXT NOT NULL,
	points       INTEGER NOT NULL,
	PRIMARY KEY (day_s, character_id, job)
);
CREATE INDEX IF NOT EXISTS job_honor_points_account ON job_honor_points (day_s, account_id);
CREATE TABLE IF NOT EXISTS job_honor_tiers (
	period_s     INTEGER NOT NULL,
	character_id INTEGER NOT NULL,
	job          INTEGER NOT NULL,
	points       INTEGER NOT NULL,
	rank         INTEGER NOT NULL,
	tier         INTEGER NOT NULL,
	PRIMARY KEY (period_s, character_id, job)
);
CREATE TABLE IF NOT EXISTS job_honor_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("jobhonor: %w", err)
	}
	service := &Service{db: db, now: now, pending: map[pointKey]int64{}, used: map[string]int{}}
	service.day = DayStart(now()).Unix()
	rows, err := db.Query(`SELECT account_id, SUM(points) FROM job_honor_points WHERE day_s = ? GROUP BY account_id`, service.day)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("jobhonor: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var account string
		var points int
		if err := rows.Scan(&account, &points); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("jobhonor: %w", err)
		}
		service.used[account] = points
	}
	if err := rows.Err(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("jobhonor: %w", err)
	}
	return service, nil
}

/*
================
Service::Award

Records an event's points under the account's daily cap and returns how
many it kept (0 when the cap is spent). Memory only.
================
*/
func (s *Service) Award(a Award) int {
	if s == nil || a.Points <= 0 || a.Account == "" || a.CharacterID <= 0 || a.Job < JobTrader || a.Job > JobHunter {
		return 0
	}
	day := DayStart(a.At).Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0
	}
	if day != s.day {
		if day < s.day {
			// A late event from the day before scores nothing: its
			// day's cap is no longer tracked.
			return 0
		}
		s.day, s.used = day, map[string]int{}
	}
	kept := min(a.Points, DailyCap-s.used[a.Account])
	if kept <= 0 {
		return 0
	}
	s.used[a.Account] += kept
	s.pending[pointKey{day: day, character: a.CharacterID, job: a.Job, account: a.Account}] += int64(kept)
	return kept
}

/*
================
Service::Used

The points the account kept today so far.
================
*/
func (s *Service) Used(account string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.day != DayStart(s.now()).Unix() {
		return 0
	}
	return s.used[account]
}

/*
================
Service::Flush

Writes the pending points. A failed write merges them back.
================
*/
func (s *Service) Flush() error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	return s.flushLocked()
}

/*
================
flushLocked
================
*/
func (s *Service) flushLocked() error {
	s.mu.Lock()
	batch := s.pending
	s.pending = map[pointKey]int64{}
	s.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	if err := s.write(batch); err != nil {
		s.mu.Lock()
		for key, points := range batch {
			s.pending[key] += points
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

/*
================
write
================
*/
func (s *Service) write(batch map[pointKey]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for key, points := range batch {
		if _, err := tx.Exec(`INSERT INTO job_honor_points (day_s, character_id, job, account_id, points)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (day_s, character_id, job) DO UPDATE SET points = points + excluded.points`,
			key.day, key.character, key.job, key.account, points); err != nil {
			return err
		}
	}
	return tx.Commit()
}

/*
================
Service::Recompute

Writes the snapshot for the newest boundary at or before the clock, when
it has not been written yet, and prunes what is past retention. True when
a snapshot was written.
================
*/
func (s *Service) Recompute() (bool, error) {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	period := DayStart(s.now())
	last, err := s.lastPeriodLocked()
	if err != nil {
		return false, err
	}
	if last >= period.Unix() {
		return false, nil
	}
	if err := s.flushLocked(); err != nil {
		return false, err
	}
	standings, err := s.rankLocked(period.AddDate(0, 0, -WindowDays), period)
	if err != nil {
		return false, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, row := range standings {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO job_honor_tiers (period_s, character_id, job, points, rank, tier)
VALUES (?, ?, ?, ?, ?, ?)`, period.Unix(), row.CharacterID, row.Job, row.Points, row.Rank, row.Tier); err != nil {
			return false, err
		}
	}
	horizon := period.AddDate(0, 0, -retainedDays).Unix()
	if _, err := tx.Exec(`DELETE FROM job_honor_points WHERE day_s < ?`, horizon); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM job_honor_tiers WHERE period_s < ?`, horizon); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO job_honor_meta (key, value) VALUES (?, ?)`, lastPeriodKey,
		strconv.FormatInt(period.Unix(), 10)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

/*
================
lastPeriodLocked

The newest snapshot's period in unix seconds, 0 when none.
================
*/
func (s *Service) lastPeriodLocked() (int64, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM job_honor_meta WHERE key = ?`, lastPeriodKey).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(value, 10, 64)
}

/*
================
rankLocked

Every character's points per job over [from, to) of honor days, ranked
per job with their tiers. Pending points are flushed by the caller.
================
*/
func (s *Service) rankLocked(from, to time.Time) ([]Standing, error) {
	rows, err := s.db.Query(`SELECT character_id, job, SUM(points) FROM job_honor_points
WHERE day_s >= ? AND day_s < ? GROUP BY character_id, job`, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Standing
	for rows.Next() {
		var row Standing
		if err := rows.Scan(&row.CharacterID, &row.Job, &row.Points); err != nil {
			return nil, err
		}
		if row.Points > 0 {
			out = append(out, row)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	AssignTiers(out)
	return out, nil
}

/*
================
AssignTiers

Ranks standings per job (points down, then character id up, so a tie
never reshuffles between reads) and sets each one's rank and tier. The
slice is reordered: job, then rank.
================
*/
func AssignTiers(standings []Standing) {
	sort.Slice(standings, func(i, j int) bool {
		a, b := standings[i], standings[j]
		if a.Job != b.Job {
			return a.Job < b.Job
		}
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		return a.CharacterID < b.CharacterID
	})
	counts := map[int]int{}
	for _, row := range standings {
		counts[row.Job]++
	}
	place := map[int]int{}
	for i := range standings {
		row := &standings[i]
		place[row.Job]++
		row.Rank = place[row.Job]
		row.Tier = tierFor(row.Rank, counts[row.Job])
	}
}

/*
================
tierFor

King is #1; Gold the top goldShare and Silver the top silverShare of the
ranked characters, each rounded up so a small job still has them.
================
*/
func tierFor(rank, ranked int) int {
	switch {
	case rank == 1:
		return TierKing
	case rank <= ceilShare(ranked, goldShare):
		return TierGold
	case rank <= ceilShare(ranked, silverShare):
		return TierSilver
	}
	return TierNone
}

/*
================
ceilShare
================
*/
func ceilShare(n int, share float64) int {
	cut := int(float64(n) * share)
	if float64(cut) < float64(n)*share {
		cut++
	}
	return cut
}

/*
================
Service::Current

The live ranking: the window that the next change will rank (the last
WindowDays-1 honor days and today so far), its tiers ("after tonight's
change") and the tiers each character holds now.
================
*/
func (s *Service) Current() ([]Standing, error) {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	if err := s.flushLocked(); err != nil {
		return nil, err
	}
	next := NextChange(s.now())
	standings, err := s.rankLocked(next.AddDate(0, 0, -WindowDays), next)
	if err != nil {
		return nil, err
	}
	held, _, err := s.snapshotLocked()
	if err != nil {
		return nil, err
	}
	tiers := map[[2]int64]int{}
	for _, row := range held {
		tiers[[2]int64{row.CharacterID, int64(row.Job)}] = row.Tier
	}
	for i := range standings {
		standings[i].Held = tiers[[2]int64{standings[i].CharacterID, int64(standings[i].Job)}]
	}
	return standings, nil
}

/*
================
Service::Previous

The newest snapshot (the ranking that set today's tiers) and its period;
the zero time when no change has run yet.
================
*/
func (s *Service) Previous() ([]Standing, time.Time, error) {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	return s.snapshotLocked()
}

/*
================
snapshotLocked
================
*/
func (s *Service) snapshotLocked() ([]Standing, time.Time, error) {
	period, err := s.lastPeriodLocked()
	if err != nil || period == 0 {
		return nil, time.Time{}, err
	}
	rows, err := s.db.Query(`SELECT character_id, job, points, rank, tier FROM job_honor_tiers
WHERE period_s = ? ORDER BY job, rank`, period)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()
	var out []Standing
	for rows.Next() {
		var row Standing
		if err := rows.Scan(&row.CharacterID, &row.Job, &row.Points, &row.Rank, &row.Tier); err != nil {
			return nil, time.Time{}, err
		}
		row.Held = row.Tier
		out = append(out, row)
	}
	return out, time.Unix(period, 0).UTC(), rows.Err()
}

/*
================
Service::Run

Flushes every flushInterval and writes the snapshot when a boundary has
passed, until ctx ends; a final flush follows.
================
*/
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		if _, err := s.Recompute(); err != nil {
			log.WithError(err).Warn("job honor: tier change failed; retried next minute")
		}
		select {
		case <-ctx.Done():
			if err := s.Flush(); err != nil {
				log.WithError(err).Warn("job honor: final flush failed")
			}
			return
		case <-ticker.C:
			if err := s.Flush(); err != nil {
				log.WithError(err).Warn("job honor: flush failed; points kept for the next one")
			}
		}
	}
}

/*
================
Service::Close

Flushes and closes; later awards score nothing.
================
*/
func (s *Service) Close() error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	flushErr := s.flushLocked()
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if err := s.db.Close(); err != nil {
		return err
	}
	return flushErr
}

/*
================
Service::AwardJobHonor

The game runtime's sink (action.JobHonorSink): Award by its fields.
================
*/
func (s *Service) AwardJobHonor(account string, characterID int64, job int, points int, at time.Time) int {
	return s.Award(Award{Account: account, CharacterID: characterID, Job: job, Points: points, At: at})
}
