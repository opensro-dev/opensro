/*
===========================================================================

persistence.go - durable authority commits, write health and backups

===========================================================================
*/
package store

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/platform/jsonscratch"
)

// Persistence is the store's single SQLite commit boundary. Domain mutation
// files only mark dirty state and enter this door while holding Store.mu.
/*
================
commitLocked
================
*/
func (s *Store) commitLocked(label string) {
	_ = s.commitCheckedLocked(label)
}

/*
================
commitCheckedLocked

commitLocked for a caller that acts on the outcome.
================
*/
func (s *Store) commitCheckedLocked(label string) error {
	s.noteGoldLocked(label)
	if err := s.commitOnceLocked(label); err != nil {
		s.recordWriteFailureLocked(label, err)
		return err
	}
	s.changes = newChangeSet()
	s.recordWriteSuccessLocked()
	return nil
}

// commitOnceLocked performs one transactional commit attempt.
/*
================
commitOnceLocked

One timed attempt: the phases commitTxLocked measures and the total are
recorded under label (lock_timing.go), success or not.
================
*/
func (s *Store) commitOnceLocked(label string) error {
	var phases commitPhaseDurations
	var rows commitRowCounts
	start := s.mu.now()
	err := s.commitTxLocked(&phases, &rows)
	phases[commitPhaseTotal] = s.mu.now().Sub(start)
	s.commits.record(label, phases, rows)
	return err
}

/*
================
committedCharacterKey

One character row's identity for the committed-record digests: stable
across the record replacement an offline door performs, unlike the
record pointer.
================
*/
type committedCharacterKey struct {
	division string
	id       int64
}

/*
================
commitRowCounts

The character rows one commit attempt wrote, and the dirty rows it
skipped because their bytes still matched the last committed digest.
================
*/
type commitRowCounts struct {
	written int64
	skipped int64
}

/*
================
commitTxLocked
================
*/
func (s *Store) commitTxLocked(phases *commitPhaseDurations, rows *commitRowCounts) error {
	if s.commitFail != nil {
		return s.commitFail
	}
	if s.db == nil {
		return fmt.Errorf("authority database is not open")
	}

	// The ground plane re-persists only when its revision advanced.
	var groundSnapshot *domain.GroundSnapshot
	groundRev := uint64(0)
	if s.ground.source != nil {
		groundRev = s.ground.source.Revision()
		if !s.ground.revisionRecorded || groundRev != s.ground.committedRev {
			snapshot := s.ground.source.Snapshot()
			groundSnapshot = &snapshot
		}
	}

	begun := s.mu.now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeTradeRewards(tx, s.meta.TradeRewards); err != nil {
		return err
	}
	if err := writeJobRankings(tx, s.meta.JobRankings); err != nil {
		return err
	}

	if err := upsertMetaTx(tx, metaKeySchemaVersion, fmt.Sprintf("%d", CurrentVersion)); err != nil {
		return err
	}
	// The layout stamp rides every commit like the schema stamp above.
	if err := upsertMetaTx(tx, metaKeyLayoutVersion, fmt.Sprintf("%d", CurrentLayoutVersion)); err != nil {
		return err
	}
	if err := upsertMetaTx(tx, metaKeyUpdatedAtMs, fmt.Sprintf("%d", s.now().UnixMilli())); err != nil {
		return err
	}

	// Digests of the character rows this attempt writes, staged here
	// and retained only once the transaction commits (below).
	stagedDigests := map[committedCharacterKey][32]byte{}
	rowCommit := characterRowCommit{tx: tx, scratch: &s.encodeScratch, committed: s.committedDigests,
		staged: stagedDigests, rows: rows, now: s.mu.now, encode: &phases[commitPhaseEncode]}
	if s.changes.all {
		for divisionID, records := range s.characters {
			for _, c := range records {
				if err := upsertTimedTx(&rowCommit, divisionID, c); err != nil {
					return err
				}
			}
		}
		for divisionID := range s.meta.NextCharID {
			s.changes.nextCharID[divisionID] = true
		}
		for divisionID := range s.meta.NextGuildID {
			s.changes.nextGuildID[divisionID] = true
		}
	} else {
		for c := range s.changes.characters {
			divisionID, known := s.charDivision[c]
			if !known {
				return fmt.Errorf("dirty character %q has no division binding", c.Name)
			}
			if err := upsertTimedTx(&rowCommit, divisionID, c); err != nil {
				return err
			}
		}
	}

	for divisionID := range s.changes.nextCharID {
		if _, err := tx.Exec("INSERT INTO next_char_id (division, next_id) VALUES (?, ?) ON CONFLICT(division) DO UPDATE SET next_id = excluded.next_id", divisionID, s.meta.NextCharID[divisionID]); err != nil {
			return err
		}
	}

	for divisionID := range s.changes.nextGuildID {
		if _, err := tx.Exec("INSERT INTO next_guild_id (division, next_id) VALUES (?, ?) ON CONFLICT(division) DO UPDATE SET next_id = excluded.next_id", divisionID, s.meta.NextGuildID[divisionID]); err != nil {
			return err
		}
	}

	if s.changes.all {
		// The unscoped door does not know whether fn touched a mailbox;
		// rewriting them all is the same lossless answer the character
		// rows take (mailboxes are capped at 20 rows each).
		for divisionID, boxes := range s.mailboxes {
			for charID, mailbox := range boxes {
				if err := replaceMailboxTx(tx, divisionID, charID, mailbox); err != nil {
					return err
				}
			}
		}
	} else {
		for key := range s.changes.mailboxes {
			if err := replaceMailboxTx(tx, key.division, key.charID, s.mailboxes[key.division][key.charID]); err != nil {
				return err
			}
		}
	}

	if s.changes.all {
		// Same lossless answer as the mailbox plane above: the unscoped
		// door does not know whether fn touched a guild, and a guild's
		// member set is bounded by the client's u8 member count.
		for divisionID, guilds := range s.guilds {
			for guildID, guild := range guilds {
				if err := replaceGuildTx(tx, divisionID, guildID, guild, s.guildMembers[divisionID][guildID]); err != nil {
					return err
				}
			}
		}
	} else {
		for key := range s.changes.guilds {
			guild, ok := s.guilds[key.division][key.guildID]
			if !ok {
				return fmt.Errorf("dirty guild %s/%d has no live row", key.division, key.guildID)
			}
			if err := replaceGuildTx(tx, key.division, key.guildID, guild, s.guildMembers[key.division][key.guildID]); err != nil {
				return err
			}
		}
	}

	// Dissolved guilds are DELETE-only and run on BOTH paths: the
	// dirtyAll sweep above iterates the live maps, which a dissolved
	// guild has already left, so nothing else would ever remove its
	// rows. The set is retained on a failed commit like every other
	// dirty flag (D5) and cleared only on success.
	for key := range s.changes.dissolvedGuild {
		if err := deleteGuildTx(tx, key.division, key.guildID); err != nil {
			return err
		}
	}

	// Training camps: the guild plane's commit shape exactly (whole-camp
	// replace per dirty key; the unscoped dirtyAll sweep rewrites every
	// live camp - the same lossless answer as the mailbox plane).
	if s.changes.all {
		for divisionID, camps := range s.camps {
			for campID, camp := range camps {
				if err := replaceCampTx(tx, divisionID, campID, camp, s.campMembers[divisionID][campID]); err != nil {
					return err
				}
			}
		}
	} else {
		for key := range s.changes.camps {
			camp, ok := s.camps[key.division][key.campID]
			if !ok {
				return fmt.Errorf("dirty training camp %s/%d has no live row", key.division, key.campID)
			}
			if err := replaceCampTx(tx, key.division, key.campID, camp, s.campMembers[key.division][key.campID]); err != nil {
				return err
			}
		}
	}

	for key := range s.changes.dissolvedCamp {
		if err := deleteCampTx(tx, key.division, key.campID); err != nil {
			return err
		}
	}

	if groundSnapshot != nil {
		if _, _, err := writeGroundDiffTx(tx, &s.encodeScratch, s.ground.loadedRecords, groundSnapshot.Divisions); err != nil {
			return err
		}
		if err := upsertMetaTx(tx, metaKeyGidCounter, fmt.Sprintf("%d", groundSnapshot.GidCounter)); err != nil {
			return err
		}
	}

	if err := insertLedgerRowsTx(tx, s.ledger.pending); err != nil {
		return err
	}
	phases[commitPhaseStatements] = s.mu.now().Sub(begun) - phases[commitPhaseEncode]
	committing := s.mu.now()
	if s.beforeSQLiteCommit != nil {
		s.beforeSQLiteCommit()
	}
	err = tx.Commit()
	phases[commitPhaseSQLite] = s.mu.now().Sub(committing)
	if err != nil {
		return err
	}
	// The whole dirty set is written: rows a deferred door left are on disk.
	s.batch.pending = false
	s.countLedgerRows(s.ledger.pending)
	s.ledger.pending = nil

	if groundSnapshot != nil {
		s.ground.loadedGidCounter = groundSnapshot.GidCounter
		s.ground.loadedRecords = groundSnapshot.Divisions
		s.ground.committedRev = groundRev
		s.ground.revisionRecorded = true
	}
	for key, digest := range stagedDigests {
		s.committedDigests[key] = digest
	}
	return nil
}

// recordWriteFailureLocked implements D5's fail-open-loud: first failure
// logs at Error, repeats rate-limit, Health carries the degradation.
/*
================
recordWriteFailureLocked
================
*/
func (s *Store) recordWriteFailureLocked(label string, err error) {
	if s.health.FailedWrites == 0 {
		s.health.FailingSince = s.now()
	}
	s.health.FailedWrites++
	s.health.LastError = fmt.Sprintf("%s: %v", label, err)
	s.publishHealthLocked()
	if s.health.FailedWrites == 1 || s.now().Sub(s.lastFailLogAt) >= failureLogInterval {
		s.lastFailLogAt = s.now()
		log.Errorf("store: PERSIST FAILING (%d failure(s) since %s) - op %q failed to commit; earlier failed mutations may remain in memory: %v",
			s.health.FailedWrites, s.health.FailingSince.Format(time.RFC3339), label, err)
	}
}

// recordWriteSuccessLocked heals the Health record after an outage.
/*
================
recordWriteSuccessLocked
================
*/
func (s *Store) recordWriteSuccessLocked() {
	if s.health.FailedWrites > 0 {
		log.Infof("store: persist recovered after %d failed write(s); the on-disk state is current again", s.health.FailedWrites)
	}
	s.health.FailedWrites = 0
	s.health.LastError = ""
	s.health.FailingSince = time.Time{}
	s.health.LastCommitAt = s.now()
	s.health.Commits++
	s.publishHealthLocked()
}

// BackupTo writes a transactionally consistent snapshot of the LAST
// COMMITTED generation to path (VACUUM INTO: compact, checkpointed, no
// sidecars). Operator backups and the acceptance suite's generation
// witnesses both ride this. Never call from inside a Mutate closure.
/*
================
BackupTo
================
*/
func (s *Store) BackupTo(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return fmt.Errorf("no database open (nothing committed yet)")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err := s.db.Exec("VACUUM INTO ?", path)
	return err
}

// FailCommits injects a commit failpoint (nil clears): every commit fails
// with err while set, exercising the D5 fail-open path. TEST SEAM for the
// cross-package acceptance suite; production never calls it.
/*
================
FailCommits
================
*/
func (s *Store) FailCommits(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitFail = err
}

// Close checkpoints and releases the database handle, then drops this
// instance's single-writer claim (the OS lock on authority.lock falls
// with the last in-process holder). Idempotent. Production calls Close only
// after the ticker, transport, and HTTP API have stopped, so no writer can
// race the final checkpoint. Tests and migration tools also use it to release
// Windows file handles. Close is terminal for this Store instance.
/*
================
Close
================
*/
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		// A graceful stop never drops a tick: rows a deferred door left
		// (tickbatch.go) are written before the final checkpoint.
		_ = s.flushDeferredLocked()
		if _, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			log.Warnf("store: wal_checkpoint at close failed: %v", err)
		}
		if err := s.db.Close(); err != nil {
			log.Warnf("store: closing the database failed: %v", err)
		}
		s.db = nil
	}
	if s.releaseClaim != nil {
		s.releaseClaim()
		s.releaseClaim = nil
	}
}

// DivisionIDs lists divisions with live characters, sorted (boot logs).
/*
================
DivisionIDs
================
*/
func (s *Store) DivisionIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.characters))
	for divisionID := range s.characters {
		out = append(out, divisionID)
	}
	sort.Strings(out)
	return out
}

/*
================
characterRowCommit

The character-row half of one commit attempt: the transaction, the
encoder scratch, the committed and staged digests, the row counts and
the encode clock every upsertTimedTx call of the attempt shares.
================
*/
type characterRowCommit struct {
	tx        *sql.Tx
	scratch   *jsonscratch.Scratch
	committed map[committedCharacterKey][32]byte
	staged    map[committedCharacterKey][32]byte
	rows      *commitRowCounts
	now       func() time.Time
	encode    *time.Duration
}

/*
================
upsertTimedTx

upsertCharacterTx for the commit: the encoding's time is added to
encode. A dirty character whose encoded bytes still hash to the last
committed digest is not rewritten: doors mark characters dirty with
no change signal (mutateCharacterLocked), so the commit is the first
place that can see a touch changed nothing, and the write it skips
was a full record string copy into the statement plus the statement
itself (live hour-12: writeCharacterTx 279 MB flat). The digest is
staged, never retained here: the caller keeps it only once the
transaction commits, so a rolled-back attempt cannot make a later
commit skip a row the disk does not hold.
================
*/
func upsertTimedTx(w *characterRowCommit, division string, c *domain.Character) error {
	start := w.now()
	record, err := encodeCharacterBorrowed(w.scratch, division, c)
	*w.encode += w.now().Sub(start)
	if err != nil {
		return err
	}
	key := committedCharacterKey{division: division, id: c.ID}
	digest := sha256.Sum256(record)
	if last, known := w.committed[key]; known && last == digest {
		w.rows.skipped++
		return nil
	}
	if err := writeCharacterTx(w.tx, division, c, record); err != nil {
		return err
	}
	w.staged[key] = digest
	w.rows.written++
	return nil
}
