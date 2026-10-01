/*
===========================================================================

persistence.go - durable authority commits, write health and backups

===========================================================================
*/
package store

import (
	"fmt"
	"os"
	"sort"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

// Persistence is the store's single SQLite commit boundary. Domain mutation
// files only mark dirty state and enter this door while holding Store.mu.
/*
================
commitLocked
================
*/
func (s *Store) commitLocked(label string) {
	if err := s.commitOnceLocked(); err != nil {
		s.recordWriteFailureLocked(label, err)
		return
	}
	s.changes = newChangeSet()
	s.recordWriteSuccessLocked()
}

// commitOnceLocked performs one transactional commit attempt.
/*
================
commitOnceLocked
================
*/
func (s *Store) commitOnceLocked() error {
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

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

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

	if s.changes.all {
		for divisionID, records := range s.characters {
			for _, c := range records {
				if err := upsertCharacterTx(tx, divisionID, c); err != nil {
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
			if err := upsertCharacterTx(tx, divisionID, c); err != nil {
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

	if groundSnapshot != nil {
		if err := replaceGroundTx(tx, groundSnapshot.Divisions); err != nil {
			return err
		}
		if err := upsertMetaTx(tx, metaKeyGidCounter, fmt.Sprintf("%d", groundSnapshot.GidCounter)); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if groundSnapshot != nil {
		s.ground.loadedGidCounter = groundSnapshot.GidCounter
		s.ground.loadedRecords = groundSnapshot.Divisions
		s.ground.committedRev = groundRev
		s.ground.revisionRecorded = true
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
