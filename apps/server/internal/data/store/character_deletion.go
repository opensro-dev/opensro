package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

// Mature deletion is one authority transaction: archive the victim, heal all
// reciprocal social edges, then remove the live record.
const DeleteReservationWindow = 7 * 24 * time.Hour

// ReapMaturedDeletions archives every character whose deletion
// reservation matured (deleteReservedAt + DeleteReservationWindow <= now):
// the record's final bytes move from the live rows into the
// byte-preserved deletedCharacters archive (under the soft-delete contract,
// per-division count never shrinks and the id watermark never reuses).
// Returns the archived names.
//
// Unlike gameplay commits, archiving is FAIL-CLOSED: the database
// transaction commits FIRST and memory follows only on success, so a
// write outage leaves the character live and pending, and the next
// trigger (boot, or any agent-API character call) retries. An
// unparseable reservation timestamp is skipped loudly - garbage must
// never delete a character. Group membership is a second fail-closed
// backstop: reservations and joins already exclude one another under the
// store lock, the loader refuses persisted overlap, and the reaper still
// re-checks before archiving. Friendships do not block deletion: every
// surviving inbound edge is removed in the archive transaction.
func (s *Store) ReapMaturedDeletions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := s.now().Add(-DeleteReservationWindow)
	var victims []reapVictim
	for divisionID, records := range s.characters {
		for _, c := range records {
			if !c.DeletePending || c.DeleteReservedAt == "" {
				continue
			}
			reservedAt, err := time.Parse(time.RFC3339, c.DeleteReservedAt)
			if err != nil {
				log.Warnf("store: character %s/%s carries unparseable deleteReservedAt %q; NOT reaping it", divisionID, c.Name, c.DeleteReservedAt)
				continue
			}
			if reservedAt.After(cutoff) {
				continue
			}
			if s.characterHasGroupMembershipLocked(divisionID, c.ID) {
				log.Errorf("store: character %s/%s has a matured deletion reservation but still belongs to a guild or training camp; NOT reaping it", divisionID, c.Name)
				continue
			}
			raw, err := json.Marshal(c)
			if err != nil {
				log.Errorf("store: archiving %s/%s failed to marshal (%v); NOT reaping it", divisionID, c.Name, err)
				continue
			}
			victims = append(victims, reapVictim{divisionID: divisionID, c: c, raw: raw})
		}
	}
	if len(victims) == 0 {
		return nil
	}
	friendUpdates, err := s.friendUpdatesForReapLocked(victims)
	if err != nil {
		log.Errorf("store: delete-reap could not build atomic friend cleanup (%v); %d matured reservation(s) stay LIVE and pending", err, len(victims))
		return nil
	}

	if err := s.archiveTxLocked(victims, friendUpdates); err != nil {
		// The fail-open helper's log text ("applied in memory but NOT on
		// disk") would lie here - the reap is fail-closed and applied
		// NOTHING. Record the degradation with the accurate story.
		if s.health.FailedWrites == 0 {
			s.health.FailingSince = s.now()
		}
		s.health.FailedWrites++
		s.health.LastError = fmt.Sprintf("delete-reap: %v", err)
		s.publishHealthLocked()
		log.Errorf("store: delete-reap failed (%v); %d matured reservation(s) stay LIVE and pending (fail-closed) - the next trigger retries", err, len(victims))
		return nil
	}

	// The transaction is durable; now move the records in memory.
	names := make([]string, 0, len(victims))
	byDivision := map[string]map[*domain.Character]bool{}
	for _, victim := range victims {
		if byDivision[victim.divisionID] == nil {
			byDivision[victim.divisionID] = map[*domain.Character]bool{}
		}
		byDivision[victim.divisionID][victim.c] = true
		s.deleted[victim.divisionID] = append(s.deleted[victim.divisionID], victim.raw)
		delete(s.charDivision, victim.c)
		delete(s.changes.characters, victim.c)
		// The archived record's mailbox dies with it (the archive keeps
		// the character's final bytes, not its mail).
		delete(s.mailboxes[victim.divisionID], victim.c.ID)
		delete(s.changes.mailboxes, mailboxKey{division: victim.divisionID, charID: victim.c.ID})
		names = append(names, victim.divisionID+"/"+victim.c.Name)
	}
	for divisionID, gone := range byDivision {
		kept := s.characters[divisionID][:0]
		for _, c := range s.characters[divisionID] {
			if !gone[c] {
				kept = append(kept, c)
			}
		}
		clear(s.characters[divisionID][len(kept):])
		s.characters[divisionID] = kept
		s.rebuildCharacterLookupLocked(divisionID)
	}
	for _, update := range friendUpdates {
		domain.SwapFriends(update.c, update.next)
		delete(s.changes.characters, update.c)
	}
	s.recordWriteSuccessLocked()
	log.Infof("store: REAPED %d matured deletion reservation(s): %s", len(names), strings.Join(names, ", "))
	return names
}

// reapVictim is one matured reservation on its way to the archive.
type reapVictim struct {
	divisionID string
	c          *domain.Character
	raw        json.RawMessage
}

// reapFriendUpdate is one surviving character whose inbound edges to this
// reap batch must disappear in the SAME transaction as the archive rows.
type reapFriendUpdate struct {
	divisionID string
	c          *domain.Character
	next       []domain.FriendRecord
	raw        json.RawMessage
}

func (s *Store) friendUpdatesForReapLocked(victims []reapVictim) ([]reapFriendUpdate, error) {
	victimIDs := map[string]map[int64]bool{}
	for _, victim := range victims {
		if victimIDs[victim.divisionID] == nil {
			victimIDs[victim.divisionID] = map[int64]bool{}
		}
		victimIDs[victim.divisionID][victim.c.ID] = true
	}
	var updates []reapFriendUpdate
	for divisionID, ids := range victimIDs {
		for _, character := range s.characters[divisionID] {
			if character == nil || ids[character.ID] {
				continue
			}
			current := domain.FriendsView(character)
			next := make([]domain.FriendRecord, 0, len(current))
			for _, edge := range current {
				if !ids[edge.ID] {
					next = append(next, edge)
				}
			}
			if len(next) == len(current) {
				continue
			}
			record := *character
			record.Friends = next
			raw, err := json.Marshal(&record)
			if err != nil {
				return nil, fmt.Errorf("marshaling friend cleanup for %s/%s: %w", divisionID, character.Name, err)
			}
			updates = append(updates, reapFriendUpdate{
				divisionID: divisionID,
				c:          character,
				next:       next,
				raw:        raw,
			})
		}
	}
	return updates, nil
}

// archiveTxLocked moves the victims inside one transaction: the live row
// deletes, the final record bytes append to the archive, and every surviving
// inbound friend edge disappears.
func (s *Store) archiveTxLocked(victims []reapVictim, friendUpdates []reapFriendUpdate) error {
	if s.commitFail != nil {
		return s.commitFail
	}
	if s.db == nil {
		db, err := openDB(s.dbPath)
		if err != nil {
			return err
		}
		if err := ensureSchema(db); err != nil {
			db.Close()
			return err
		}
		s.db = db
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The next archive seq comes from the DATABASE, not from
	// len(s.deleted): the two agree in a healthy store, but a hand-edited
	// gap in the table's seq column (rows removed below a surviving
	// higher seq) would make the in-memory count collide with that
	// survivor on the (division, seq) primary key. MAX(seq)+1 inside the
	// transaction can never collide.
	nextSeq := map[string]int{}
	for _, update := range friendUpdates {
		if _, err := tx.Exec(
			"INSERT INTO characters (division, id, name_lower, record) VALUES (?, ?, ?, ?) ON CONFLICT(division, id) DO UPDATE SET name_lower = excluded.name_lower, record = excluded.record",
			update.divisionID, update.c.ID, strings.ToLower(update.c.Name), string(update.raw),
		); err != nil {
			return err
		}
	}
	for _, victim := range victims {
		seq, seen := nextSeq[victim.divisionID]
		if !seen {
			if err := tx.QueryRow("SELECT COALESCE(MAX(seq)+1, 0) FROM deleted_characters WHERE division = ?", victim.divisionID).Scan(&seq); err != nil {
				return err
			}
		}
		if _, err := tx.Exec("DELETE FROM characters WHERE division = ? AND id = ?", victim.divisionID, victim.c.ID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM memos WHERE division = ? AND char_id = ?", victim.divisionID, victim.c.ID); err != nil {
			return err
		}
		nextSeq[victim.divisionID] = seq + 1
		if _, err := tx.Exec("INSERT INTO deleted_characters (division, seq, record) VALUES (?, ?, ?)", victim.divisionID, seq, string(victim.raw)); err != nil {
			return err
		}
	}
	if err := upsertMetaTx(tx, metaKeyUpdatedAtMs, fmt.Sprintf("%d", s.now().UnixMilli())); err != nil {
		return err
	}
	return tx.Commit()
}

// commitLocked writes the operation's dirty entities in one transaction.
// Callers hold s.mu. Dirty flags clear only on SUCCESS: a failed commit
// keeps them, so the next successful commit self-heals (D5).
