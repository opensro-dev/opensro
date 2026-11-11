/*
===========================================================================

boot_verify.go - what the store proves before it serves, and what after

Boot used to prove the whole database before the server listened:
PRAGMA quick_check over every page, then VACUUM INTO a full spare copy.
Both scale with the file, and the file is not the state: on production
(2026-11-11) the authority held about 11 MB of game state beside 2,171 MB
of economy ledger, an append-only log no boot step reads. The scan took
143 s on a cold copy and the server was restart-looped by its own health
check for booting too slowly (opensro-ops#277).

So the gate is the state. Before serving, quickCheckState checks every
table the boot loads, each with its indexes; that is what the quarantine
and bak ladder exists to protect, and it costs milliseconds. After the
store is adopted, the boot verifier runs the full check on its own
connection and, when the file proved good, writes the one-generation
spare copy the way SnapshotTo writes a snapshot. A failed full check
cannot quarantine a database that is being served: it is reported in
Health and logged at Error for an operator.

The spare copy therefore dates from shortly after boot, not from before
the first commit. It is still the previous generation the ladder falls
back to, and never older than it used to be.

===========================================================================
*/
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// bootLogTables are the append-only tables no boot step loads. The gate
// before serving leaves them to the verifier's full check.
var bootLogTables = map[string]bool{
	"economy_ledger": true,
}

// bootVerifierStopTimeout bounds Close's wait for an interrupted verifier.
const bootVerifierStopTimeout = 5 * time.Second

// bootSpareSuffix marks the spare copy while it is being written.
const bootSpareSuffix = ".partial"

/*
================
quickCheckState

SQLite's integrity probe over every table the boot loads, one table and
its indexes at a time (PRAGMA quick_check(TABLE), SQLite 3.33). Any
answer but "ok" is corruption of the state, and the caller's ladder
quarantines the file.
================
*/
func quickCheckState(db *sql.DB) error {
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	// A damaged schema page ends the walk early; that is corruption too.
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, name := range tables {
		if bootLogTables[name] {
			continue
		}
		var verdict string
		pragma := "PRAGMA quick_check('" + strings.ReplaceAll(name, "'", "''") + "')"
		if err := db.QueryRow(pragma).Scan(&verdict); err != nil {
			return fmt.Errorf("quick_check %s: %w", name, err)
		}
		if verdict != "ok" {
			return fmt.Errorf("quick_check %s: %s", name, verdict)
		}
	}
	return nil
}

/*
================
bootVerifier

The goroutine that finishes what the gate left: the full integrity
check and the spare copy. One per opened database; Close interrupts it
and waits.
================
*/
type bootVerifier struct {
	cancel  context.CancelFunc
	stopped chan struct{}
}

/*
================
Store.startBootVerifier

Starts the verifier for the database just adopted. It takes no store
lock: its one result, a failed check, is published through an atomic
that Health reads.
================
*/
func (s *Store) startBootVerifier() {
	ctx, cancel := context.WithCancel(context.Background())
	verifier := &bootVerifier{cancel: cancel, stopped: make(chan struct{})}
	s.verifier = verifier
	dbPath, bakPath := s.dbPath, s.dbBakPath
	go func() {
		defer close(verifier.stopped)
		db, err := connectDB(dbPath)
		if err != nil {
			log.Warnf("store: boot verifier could not open %s: %v", DBFileName, err)
			return
		}
		defer db.Close()
		started := time.Now()
		if err := fullQuickCheck(ctx, db); err != nil {
			if ctx.Err() != nil {
				return
			}
			message := err.Error()
			s.integrityError.Store(&message)
			log.Errorf("store: BACKGROUND INTEGRITY CHECK FAILED for %s: %v - the state tables passed at boot and the store keeps serving; inspect the database and restore the affected table from a backup", DBFileName, err)
			return
		}
		checked := time.Since(started)
		if err := refreshSpare(ctx, db, bakPath); err != nil {
			if ctx.Err() == nil {
				log.Warnf("store: refreshing %s failed: %v", DBBakFileName, err)
			}
			return
		}
		log.Infof("store: full integrity check ok in %s; %s refreshed in %s (after boot, off the serving path)",
			checked.Round(time.Millisecond), DBBakFileName, (time.Since(started) - checked).Round(time.Millisecond))
	}()
}

/*
================
bootVerifier.stop

Interrupts the verifier and waits for it, bounded by
bootVerifierStopTimeout. An interrupted spare copy leaves only its
partial file, which the next boot's stray cleanup removes.
================
*/
func (v *bootVerifier) stop() {
	v.cancel()
	select {
	case <-v.stopped:
	case <-time.After(bootVerifierStopTimeout):
		log.Warnf("store: boot verifier still running after %s; closing without it", bootVerifierStopTimeout)
	}
}

/*
================
Store.waitBootVerified

Blocks until the verifier has finished. Tests use it to see the spare
copy a boot writes; a server never waits for it.
================
*/
func (s *Store) waitBootVerified() {
	s.mu.RLock()
	verifier := s.verifier
	s.mu.RUnlock()
	if verifier != nil {
		<-verifier.stopped
	}
}

/*
================
fullQuickCheck

The whole-file integrity probe on the verifier's own connection. It
holds a read snapshot, so the writer is never blocked; the WAL cannot
start over while it runs, which the checkpointer's ceiling absorbs.
================
*/
func fullQuickCheck(ctx context.Context, db *sql.DB) error {
	var verdict string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&verdict); err != nil {
		return fmt.Errorf("quick_check: %w", err)
	}
	if verdict != "ok" {
		return fmt.Errorf("quick_check: %s", verdict)
	}
	return nil
}

/*
================
refreshSpare

Writes the one-generation spare copy with VACUUM INTO (a consistent,
checkpointed, compact image), beside its target, and renames it over
the target only when complete: a crash or an interrupt leaves the
previous generation in place.
================
*/
func refreshSpare(ctx context.Context, db *sql.DB, bakPath string) error {
	partial := bakPath + bootSpareSuffix
	if err := os.Remove(partial); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", partial); err != nil {
		_ = os.Remove(partial)
		return err
	}
	if err := os.Rename(partial, bakPath); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return nil
}
