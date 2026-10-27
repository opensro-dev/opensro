/*
===========================================================================

economy.go - the GameWorld's economy aggregates for the staff console

Port-only, not native. The staff console's Economy page (read-only, phase
1) reads this owner over the local operator listener (contract v1:
/economy/v1/meta, series, flows, distribution, flags). Nothing here reads
the live authority: the owner asks the store for a snapshot copy
(store.SnapshotTo, which holds off no door) every SnapshotEvery and once
shortly after start, and answers every request from the newest copy. Its
own small database (economy.db beside the authority) keeps what one copy
cannot: the gold total at every snapshot.

The live charts read the store's per-reason counters on
/transport/metrics instead; these aggregates are as fresh as the newest
snapshot (meta.snapshotAt, meta.ledgerThrough).

===========================================================================
*/
package economy

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// EnvSnapshotHours sets the snapshot cadence in hours; 0 turns the
// aggregates off. The default is the owner-agreed 6 hours.
const EnvSnapshotHours = "SRO_ECONOMY_SNAPSHOT_HOURS"

const (
	defaultSnapshotEvery = 6 * time.Hour
	// firstSnapshotDelay lets the GameWorld finish booting first.
	firstSnapshotDelay = time.Minute
	// SnapshotFile and HistoryFile sit beside the authority database.
	SnapshotFile = "economy-snapshot.db"
	HistoryFile  = "economy.db"
	// TokenFile is the operator token the console presents.
	TokenFile = "economy-token"
)

// historySchema keeps the gold held at every snapshot, per division.
const historySchema = `
CREATE TABLE IF NOT EXISTS gold_totals (
  at_s       INTEGER NOT NULL,
  division   TEXT    NOT NULL,
  total      INTEGER NOT NULL,
  characters INTEGER NOT NULL,
  PRIMARY KEY (at_s, division)
);
`

/*
================
Snapshotter

The store's snapshot door (store.SnapshotTo).
================
*/
type Snapshotter interface {
	SnapshotTo(path string) error
}

/*
================
Service

Owns the snapshot cadence, the newest copy's distribution and the gold
total history. mu guards the fields the handler reads.
================
*/
type Service struct {
	dir     string
	store   Snapshotter
	every   time.Duration
	now     func() time.Time
	history *sql.DB

	mu           sync.Mutex
	snapshotAt   time.Time
	distribution map[string]Distribution
	divisions    []string
	// killStats serves /economy/v1/killstats (killstats.go); false
	// answers 404 (SRO_KILL_STATS off, the default).
	killStats bool

	// sealPityStats serves /economy/v1/sealpity (sealpitystats.go);
	// false answers 404 (SRO_SEAL_PITY off, the default).
	sealPityStats bool

	// sealCensus enables the staff census route (census.go); censusLatest
	// is guarded by mu, censusMu serialises the runs themselves.
	sealCensus   bool
	censusMu     sync.Mutex
	censusLatest *SealCensus
}

/*
================
SnapshotEveryFromEnv

The cadence, 0 when the aggregates are off.
================
*/
func SnapshotEveryFromEnv() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(EnvSnapshotHours))
	if raw == "" {
		return defaultSnapshotEvery, nil
	}
	hours, err := strconv.ParseFloat(raw, 64)
	if err != nil || hours < 0 {
		return 0, fmt.Errorf("%s=%q: want a number of hours, 0 for off", EnvSnapshotHours, raw)
	}
	return time.Duration(hours * float64(time.Hour)), nil
}

/*
================
Open

The service for the authority in dir. Opens (creating) its history
database; takes no snapshot until Run.
================
*/
func Open(dir string, store Snapshotter, every time.Duration, now func() time.Time) (*Service, error) {
	db, err := sql.Open("sqlite", filepath.Join(dir, HistoryFile))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;" + historySchema + killStatsSchema + sealPityStatsSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("economy history: %w", err)
	}
	return &Service{dir: dir, store: store, every: every, now: now, history: db}, nil
}

/*
================
Service.Close
================
*/
func (s *Service) Close() error {
	return s.history.Close()
}

/*
================
Service.Run

Takes the first snapshot shortly after start, then one every interval,
until ctx ends. A failed snapshot is logged and retried at the next tick;
the previous copy keeps answering.
================
*/
func (s *Service) Run(ctx context.Context) {
	if s.every <= 0 {
		return
	}
	timer := time.NewTimer(firstSnapshotDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		started := time.Now()
		if err := s.Snapshot(); err != nil {
			log.WithError(err).Warn("economy: snapshot failed")
		} else {
			log.WithField("elapsed", time.Since(started).Round(time.Millisecond)).Info("economy: snapshot taken")
		}
		timer.Reset(s.every)
	}
}

/*
================
Service.Snapshot

One snapshot now: the store copies itself, the distribution is computed
from the copy once, and the gold totals join the history.
================
*/
func (s *Service) Snapshot() error {
	path := filepath.Join(s.dir, SnapshotFile)
	if err := s.store.SnapshotTo(path); err != nil {
		return err
	}
	at := s.now()
	db, err := openSnapshot(path)
	if err != nil {
		return err
	}
	defer db.Close()
	blocks, err := readDistribution(db, at)
	if err != nil {
		return err
	}
	divisions := make([]string, 0, len(blocks))
	for division, block := range blocks {
		if division != "" {
			divisions = append(divisions, division)
		}
		if _, err := s.history.Exec("INSERT OR REPLACE INTO gold_totals (at_s, division, total, characters) VALUES (?, ?, ?, ?)",
			at.Unix(), division, block.goldTotal, block.Gold.Characters); err != nil {
			return fmt.Errorf("economy history: %w", err)
		}
	}
	sortStrings(divisions)
	s.mu.Lock()
	s.snapshotAt, s.distribution, s.divisions = at, blocks, divisions
	s.mu.Unlock()
	return nil
}

/*
================
openSnapshot

A query-only connection to the newest copy.
================
*/
func openSnapshot(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
