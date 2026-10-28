/*
===========================================================================

db_write.go - normalized authority writes within the caller's transaction

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"opensro.online/server/internal/domain"
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

// upsertCharacterTx writes one character row inside a transaction.
/*
================
upsertCharacterTx
================
*/
func upsertCharacterTx(tx *sql.Tx, division string, c *domain.Character) error {
	record, err := encodeCharacter(division, c)
	if err != nil {
		return err
	}
	return writeCharacterTx(tx, division, c, record)
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
func replaceGroundTx(tx *sql.Tx, divisions map[string][]domain.GroundItemRecord) error {
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
			record, err := json.Marshal(item)
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
func writeGroundDiffTx(tx *sql.Tx, committed, next map[string][]domain.GroundItemRecord) (int, int, error) {
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
		w, d, err := writeGroundDivisionDiffTx(tx, divisionID, sortedGround(committed[divisionID]), sortedGround(next[divisionID]))
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
func writeGroundDivisionDiffTx(tx *sql.Tx, divisionID string, old, next []domain.GroundItemRecord) (int, int, error) {
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
			if err := upsertGroundTx(tx, divisionID, &next[j]); err != nil {
				return 0, 0, err
			}
			written++
			j++
		default:
			// Same gid. DeepEqual can only err towards a rewrite, never
			// towards a missed change, and it follows any new field.
			if !reflect.DeepEqual(&old[i], &next[j]) {
				if err := upsertGroundTx(tx, divisionID, &next[j]); err != nil {
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
func upsertGroundTx(tx *sql.Tx, divisionID string, item *domain.GroundItemRecord) error {
	record, err := json.Marshal(item)
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
			if err := upsertCharacterTx(tx, divisionID, c); err != nil {
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
