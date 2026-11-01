/*
===========================================================================

db_write.go - normalized authority writes within the caller's transaction

===========================================================================
*/
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/platform/jsonscratch"
)

// Transaction writers translate dirty in-memory authority records into the
// normalized SQLite layout. They never own transaction lifecycle.
/*
================
upsertMetaTx
================
*/
func upsertMetaTx(tx *sql.Tx, key, value string) error {
	_, err := tx.Exec("INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// characterRowTx is the ONE owner of a character-row write outside the
// commit (persistence.go): a separate transaction that writes character
// rows - an offline door's own commit (storage, the mall, the fortress
// doors, a guild war's stakes, a rename, the delete reap). It records
// the digest of every row it writes and every row it deletes, and its
// Commit retains exactly those in the store's committedDigests once
// the transaction is durable. A door therefore cannot leave a retained
// digest describing bytes the disk no longer holds: with only the
// commit's bytes retained, a later in-memory revert to the pre-door
// bytes would hash to the stale digest, be skipped, and the door's
// bytes would come back on reopen. There is no raw character-row
// writer left for a new door to reach for instead: upsertCharacter and
// writeCharacterRecord below are the only doors in, and both record.
/*
================
characterRowTx
================
*/
type characterRowTx struct {
	*sql.Tx
	store   *Store
	written map[committedCharacterKey][32]byte
	deleted map[committedCharacterKey]bool
	// copies stages the committed copies (character_sections.go) for
	// the rows this transaction writes, retained at Commit under the
	// written digests' discipline.
	copies map[committedCharacterKey]*domain.Character
}

/*
================
beginCharacterTxLocked

A character-writing door's transaction: beginTxLocked (which first
flushes deferred rows) wrapped so its character rows are recorded.
================
*/
func (s *Store) beginCharacterTxLocked() (*characterRowTx, error) {
	tx, err := s.beginTxLocked()
	if err != nil {
		return nil, err
	}
	return &characterRowTx{Tx: tx, store: s,
		written: map[committedCharacterKey][32]byte{}, deleted: map[committedCharacterKey]bool{},
		copies: map[committedCharacterKey]*domain.Character{}}, nil
}

/*
================
characterRowTx::upsertCharacter

Writes one character row inside the door's transaction and records
its digest for Commit to retain.
================
*/
func (w *characterRowTx) upsertCharacter(division string, c *domain.Character) error {
	// The section skip (#2141), as in the commit path: a row whose
	// sections all equal its committed copy already holds these
	// bytes on disk. The digest is re-recorded so Commit retains it
	// beside the (unchanged) copy, exactly as a write would.
	key := committedCharacterKey{division: division, id: c.ID}
	if digest, digested := w.store.committedDigests[key]; digested {
		if committed, copied := w.store.committedRows[key]; copied && len(characterSectionsChanged(c, committed)) == 0 {
			w.written[key] = digest
			delete(w.deleted, key)
			return nil
		}
	}
	record, err := encodeCharacter(division, c)
	if err != nil {
		return err
	}
	return w.writeCharacterRecord(division, c, record)
}

/*
================
characterRowTx::writeCharacterRecord

upsertCharacter for a caller that already holds the row's bytes (the
delete reap's friend updates): same write, same recording.
================
*/
func (w *characterRowTx) writeCharacterRecord(division string, c *domain.Character, record []byte) error {
	if err := writeCharacterTx(w.Tx, division, c, record); err != nil {
		return err
	}
	key := committedCharacterKey{division: division, id: c.ID}
	w.written[key] = sha256.Sum256(record)
	w.copies[key] = domain.CloneCharacter(c)
	delete(w.deleted, key)
	return nil
}

/*
================
characterRowTx::deleteCharacter

Removes one character row and records the removal: on Commit the
retained digest goes with it, so a later character reusing the id is
never skipped against a dead row's bytes.
================
*/
func (w *characterRowTx) deleteCharacter(division string, id int64) error {
	if _, err := w.Tx.Exec("DELETE FROM characters WHERE division = ? AND id = ?", division, id); err != nil {
		return err
	}
	key := committedCharacterKey{division: division, id: id}
	w.deleted[key] = true
	delete(w.written, key)
	delete(w.copies, key)
	return nil
}

/*
================
characterRowTx::Commit

The wrapped transaction's commit: only once the rows are durable do
their digests become the store's retained ones (a delete's digest is
dropped). A rolled-back or failed door retains nothing, so it can
never make a later commit skip a row the disk does not hold.
================
*/
func (w *characterRowTx) Commit() error {
	if err := w.Tx.Commit(); err != nil {
		return err
	}
	for key := range w.deleted {
		delete(w.store.committedDigests, key)
		delete(w.store.committedRows, key)
	}
	for key, digest := range w.written {
		w.store.committedDigests[key] = digest
	}
	for key, row := range w.copies {
		w.store.committedRows[key] = row
	}
	return nil
}

/*
================
characterRowTx::Rollback
================
*/
func (w *characterRowTx) Rollback() error {
	w.written = map[committedCharacterKey][32]byte{}
	w.deleted = map[committedCharacterKey]bool{}
	w.copies = map[committedCharacterKey]*domain.Character{}
	return w.Tx.Rollback()
}

/*
================
encodeCharacter

The character row's JSON. The commit times it apart from the write
(lock_timing.go).
================
*/
func encodeCharacter(division string, c *domain.Character) ([]byte, error) {
	record, err := json.Marshal(c)
	if err != nil {
		// Plain-data structs do not fail to marshal; when one does, the
		// whole commit fails LOUD instead of silently dropping a
		// character row (the same posture the JSON engine had).
		return nil, fmt.Errorf("marshaling character %s/%s: %w", division, c.Name, err)
	}
	return record, nil
}

/*
================
encodeCharacterBorrowed

encodeCharacter into the caller's scratch instead of a fresh Marshal:
the same bytes, borrowed from the scratch until its next encode. The
commit path encodes every dirty character under the store lock on
every commit; Marshal's per-call owned copy was the store's largest
single allocation source (bytes.Clone in the production profile).
Callers consume the record synchronously (writeCharacterTx copies it
into the statement), so the borrowed lifetime is exact.
================
*/
func encodeCharacterBorrowed(scratch *jsonscratch.Scratch, division string, c *domain.Character) ([]byte, error) {
	record, err := scratch.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshaling character %s/%s: %w", division, c.Name, err)
	}
	return record, nil
}

/*
================
writeCharacterTx
================
*/
func writeCharacterTx(tx *sql.Tx, division string, c *domain.Character, record []byte) error {
	_, err := tx.Exec(
		"INSERT INTO characters (division, id, name_lower, record) VALUES (?, ?, ?, ?) ON CONFLICT(division, id) DO UPDATE SET name_lower = excluded.name_lower, record = excluded.record",
		division, c.ID, strings.ToLower(c.Name), string(record))
	return err
}

// replaceGroundTx replaces the WHOLE ground plane inside a transaction.
// Only rare whole-plane rewrites use it (a character rename); the
// per-commit path is writeGroundDiffTx.
/*
================
replaceGroundTx
================
*/
func replaceGroundTx(tx *sql.Tx, scratch *jsonscratch.Scratch, divisions map[string][]domain.GroundItemRecord) error {
	if _, err := tx.Exec("DELETE FROM ground_items"); err != nil {
		return err
	}
	divisionIDs := make([]string, 0, len(divisions))
	for divisionID := range divisions {
		divisionIDs = append(divisionIDs, divisionID)
	}
	sort.Strings(divisionIDs)
	for _, divisionID := range divisionIDs {
		for _, item := range divisions[divisionID] {
			record, err := scratch.Marshal(item)
			if err != nil {
				return fmt.Errorf("marshaling ground %s/gid %d: %w", divisionID, item.Gid, err)
			}
			if _, err := tx.Exec("INSERT INTO ground_items (division, gid, record) VALUES (?, ?, ?)", divisionID, item.Gid, string(record)); err != nil {
				return err
			}
		}
	}
	return nil
}

const (
	upsertGroundSQL = "INSERT INTO ground_items (division, gid, record) VALUES (?, ?, ?) " +
		"ON CONFLICT(division, gid) DO UPDATE SET record = excluded.record"
	deleteGroundSQL = "DELETE FROM ground_items WHERE division = ? AND gid = ?"
)

/*
================
writeGroundDiffTx

Brings ground_items from the committed plane to the next one, writing
only the rows that differ: a gone gid is deleted, a new or changed row
is upserted. committed must be what the table holds: the store's
loadedRecords, set at load, after every committed ground write, and
never on a rolled-back one.

The live plane holds about 1,600 rows (prod, 2026-10-28). Rewriting it
whole on every ground-dirty commit cost 13% of the GameWorld's CPU,
on the tick goroutine. The diff is a merge walk over gid-sorted rows
(Registry.Snapshot sorts, the load query orders by gid), so a commit
that changed one drop does one statement and allocates nothing for
the rows it skips. Returns the rows written and deleted.
================
*/
func writeGroundDiffTx(tx *sql.Tx, scratch *jsonscratch.Scratch, committed, next map[string][]domain.GroundItemRecord) (int, int, error) {
	divisionIDs := make([]string, 0, len(next)+len(committed))
	for divisionID := range next {
		divisionIDs = append(divisionIDs, divisionID)
	}
	for divisionID := range committed {
		if _, both := next[divisionID]; !both {
			divisionIDs = append(divisionIDs, divisionID)
		}
	}
	sort.Strings(divisionIDs)
	written, deleted := 0, 0
	for _, divisionID := range divisionIDs {
		w, d, err := writeGroundDivisionDiffTx(tx, scratch, divisionID, sortedGround(committed[divisionID]), sortedGround(next[divisionID]))
		if err != nil {
			return 0, 0, err
		}
		written, deleted = written+w, deleted+d
	}
	return written, deleted, nil
}

/*
================
writeGroundDivisionDiffTx

One division's merge walk; both row lists are sorted by gid.
================
*/
func writeGroundDivisionDiffTx(tx *sql.Tx, scratch *jsonscratch.Scratch, divisionID string, old, next []domain.GroundItemRecord) (int, int, error) {
	written, deleted := 0, 0
	i, j := 0, 0
	for i < len(old) || j < len(next) {
		switch {
		case j == len(next) || i < len(old) && old[i].Gid < next[j].Gid:
			if _, err := tx.Exec(deleteGroundSQL, divisionID, old[i].Gid); err != nil {
				return 0, 0, err
			}
			deleted++
			i++
		case i == len(old) || next[j].Gid < old[i].Gid:
			if err := upsertGroundTx(tx, scratch, divisionID, &next[j]); err != nil {
				return 0, 0, err
			}
			written++
			j++
		default:
			// Same gid. DeepEqual can only err towards a rewrite, never
			// towards a missed change, and it follows any new field.
			if !reflect.DeepEqual(&old[i], &next[j]) {
				if err := upsertGroundTx(tx, scratch, divisionID, &next[j]); err != nil {
					return 0, 0, err
				}
				written++
			}
			i, j = i+1, j+1
		}
	}
	return written, deleted, nil
}

/*
================
upsertGroundTx
================
*/
func upsertGroundTx(tx *sql.Tx, scratch *jsonscratch.Scratch, divisionID string, item *domain.GroundItemRecord) error {
	record, err := scratch.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshaling ground %s/gid %d: %w", divisionID, item.Gid, err)
	}
	_, err = tx.Exec(upsertGroundSQL, divisionID, item.Gid, string(record))
	return err
}

/*
================
sortedGround

The rows in gid order. Every producer already sorts, so this is a
check; an unsorted list (a future producer) is sorted on a copy rather
than trusted, because a wrong order would delete live rows.
================
*/
func sortedGround(rows []domain.GroundItemRecord) []domain.GroundItemRecord {
	less := func(a, b int) bool { return rows[a].Gid < rows[b].Gid }
	if sort.SliceIsSorted(rows, less) {
		return rows
	}
	sorted := append([]domain.GroundItemRecord(nil), rows...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].Gid < sorted[b].Gid })
	return sorted
}

// replaceMailboxTx replaces ONE character's mailbox rows inside a
// transaction. A mailbox is capped at the client panel's 20 rows, so a
// whole-mailbox replace on dirty commits is O(small) like the ground
// plane, and it keeps seq == list position - the wire-index contract the
// letter lane's u8 indices rely on.
/*
================
replaceMailboxTx
================
*/
func replaceMailboxTx(tx *sql.Tx, division string, charID int64, mailbox []domain.LetterRecord) error {
	if _, err := tx.Exec("DELETE FROM memos WHERE division = ? AND char_id = ?", division, charID); err != nil {
		return err
	}
	for seq, letter := range mailbox {
		record, err := json.Marshal(letter)
		if err != nil {
			return fmt.Errorf("marshaling memo %s/%d[%d]: %w", division, charID, seq, err)
		}
		if _, err := tx.Exec("INSERT INTO memos (division, char_id, seq, record) VALUES (?, ?, ?, ?)", division, charID, seq, string(record)); err != nil {
			return err
		}
	}
	return nil
}

// replaceGuildTx replaces ONE guild's rows - the guild row plus its whole
// member set - inside a transaction. The dirty unit is the whole guild
// (guildKey), and a member set is bounded by the client's u8 member count,
// so the whole-set replace is O(small) like a mailbox, and it keeps
// seq == list position - the wire order the 0x32C4 member loop emits.
/*
================
replaceGuildTx
================
*/
func replaceGuildTx(tx *sql.Tx, division string, guildID int64, guild domain.GuildRecord, members []domain.GuildMemberRecord) error {
	if _, err := tx.Exec("DELETE FROM guilds WHERE division = ? AND guild_id = ?", division, guildID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM guild_members WHERE division = ? AND guild_id = ?", division, guildID); err != nil {
		return err
	}
	record, err := json.Marshal(guild)
	if err != nil {
		return fmt.Errorf("marshaling guild %s/%d: %w", division, guildID, err)
	}
	if _, err := tx.Exec("INSERT INTO guilds (division, guild_id, record) VALUES (?, ?, ?)", division, guildID, string(record)); err != nil {
		return err
	}
	for seq, member := range members {
		memberRecord, err := json.Marshal(member)
		if err != nil {
			return fmt.Errorf("marshaling guild member %s/%d[%d]: %w", division, guildID, seq, err)
		}
		if _, err := tx.Exec("INSERT INTO guild_members (division, guild_id, char_id, record, seq) VALUES (?, ?, ?, ?, ?)", division, guildID, member.CharID, string(memberRecord), seq); err != nil {
			return err
		}
	}
	return nil
}

// deleteGuildTx removes ONE guild's rows - the guild row plus its whole
// member set - inside a transaction (replaceGuildTx's DELETE half with
// no re-insert: the dissolution door's persistence action).
/*
================
deleteGuildTx
================
*/
func deleteGuildTx(tx *sql.Tx, division string, guildID int64) error {
	if _, err := tx.Exec("DELETE FROM guilds WHERE division = ? AND guild_id = ?", division, guildID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM guild_members WHERE division = ? AND guild_id = ?", division, guildID); err != nil {
		return err
	}
	return nil
}

// replaceCampTx replaces ONE training camp's rows - the camp row plus its
// whole member set - inside a transaction (replaceGuildTx's twin; the
// member set is bounded by the client's 8-row roster assert
// (CTrainingCampData_ReadRoster: "TraningCampMember is Over than 8"),
// so the whole-set replace is O(small), and seq == list position is
// the wire order the 0x3AC5 status-10 sub-1 roster loop emits).
/*
================
replaceCampTx
================
*/
func replaceCampTx(tx *sql.Tx, division string, campID int64, camp domain.TrainingCampRecord, members []domain.TrainingCampMemberRecord) error {
	if _, err := tx.Exec("DELETE FROM training_camps WHERE division = ? AND camp_id = ?", division, campID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM training_camp_members WHERE division = ? AND camp_id = ?", division, campID); err != nil {
		return err
	}
	record, err := json.Marshal(camp)
	if err != nil {
		return fmt.Errorf("marshaling training camp %s/%d: %w", division, campID, err)
	}
	if _, err := tx.Exec("INSERT INTO training_camps (division, camp_id, record) VALUES (?, ?, ?)", division, campID, string(record)); err != nil {
		return err
	}
	for seq, member := range members {
		memberRecord, err := json.Marshal(member)
		if err != nil {
			return fmt.Errorf("marshaling training camp member %s/%d[%d]: %w", division, campID, seq, err)
		}
		if _, err := tx.Exec("INSERT INTO training_camp_members (division, camp_id, char_id, record, seq) VALUES (?, ?, ?, ?, ?)", division, campID, member.CharID, string(memberRecord), seq); err != nil {
			return err
		}
	}
	return nil
}

// deleteCampTx removes ONE training camp's rows - the camp row plus its
// whole member set - inside a transaction (replaceCampTx's DELETE half with
// no re-insert: the DisbandCamp door's persistence action).
/*
================
deleteCampTx
================
*/
func deleteCampTx(tx *sql.Tx, division string, campID int64) error {
	if _, err := tx.Exec("DELETE FROM training_camps WHERE division = ? AND camp_id = ?", division, campID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM training_camp_members WHERE division = ? AND camp_id = ?", division, campID); err != nil {
		return err
	}
	return nil
}

// seedAuthorityDBTx populates a freshly created current-schema database.
// Transaction ownership stays with the caller so publishing cannot expose a
// partial seed.
/*
================
seedAuthorityDBTx
================
*/
func seedAuthorityDBTx(tx *sql.Tx, data *authoritySeed) error {
	divisionSet := map[string]bool{}
	for divisionID := range data.characters {
		divisionSet[divisionID] = true
	}
	for divisionID := range data.deleted {
		divisionSet[divisionID] = true
	}
	for divisionID := range data.ground {
		divisionSet[divisionID] = true
	}
	divisionIDs := make([]string, 0, len(divisionSet))
	for divisionID := range divisionSet {
		divisionIDs = append(divisionIDs, divisionID)
	}
	sort.Strings(divisionIDs)

	for _, divisionID := range divisionIDs {
		for _, c := range data.characters[divisionID] {
			// A fresh database behind no live store: no retained
			// digest exists yet, so the raw row writer is exact here.
			record, err := encodeCharacter(divisionID, c)
			if err != nil {
				return err
			}
			if err := writeCharacterTx(tx, divisionID, c, record); err != nil {
				return err
			}
		}
		for i, raw := range data.deleted[divisionID] {
			if _, err := tx.Exec("INSERT INTO deleted_characters (division, seq, record) VALUES (?, ?, ?)", divisionID, i, string(raw)); err != nil {
				return err
			}
		}
		for _, item := range data.ground[divisionID] {
			record, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if _, err := tx.Exec("INSERT INTO ground_items (division, gid, record) VALUES (?, ?, ?)", divisionID, item.Gid, string(record)); err != nil {
				return err
			}
		}
	}
	for divisionID, next := range data.meta.NextCharID {
		if _, err := tx.Exec("INSERT INTO next_char_id (division, next_id) VALUES (?, ?) ON CONFLICT(division) DO UPDATE SET next_id = excluded.next_id", divisionID, next); err != nil {
			return err
		}
	}
	if err := upsertMetaTx(tx, metaKeySchemaVersion, fmt.Sprintf("%d", CurrentVersion)); err != nil {
		return err
	}
	if err := upsertMetaTx(tx, metaKeyLayoutVersion, fmt.Sprintf("%d", CurrentLayoutVersion)); err != nil {
		return err
	}
	if err := upsertMetaTx(tx, metaKeyGidCounter, fmt.Sprintf("%d", data.meta.GidCounter)); err != nil {
		return err
	}
	if err := writeTradeRewards(tx, data.meta.TradeRewards); err != nil {
		return err
	}
	if err := writeJobRankings(tx, data.meta.JobRankings); err != nil {
		return err
	}
	return upsertMetaTx(tx, metaKeyUpdatedAtMs, fmt.Sprintf("%d", data.updatedAt))
}
