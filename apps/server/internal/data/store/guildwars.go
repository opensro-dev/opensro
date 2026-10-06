/*
===========================================================================

guildwars.go - atomic guild-war stakes, combat accounting and settlement

SR_ShardManager 43BE90 / 43C320 / 43C130 commit these operations through
three database procedures. Each corresponding port operation is one SQLite
transaction; live character and guild copies change only after commit.

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"opensro.online/server/internal/domain"
)

const (
	guildWarScoreOptions     = 8
	guildWarMaxCombinedStake = 1000000000
	guildWarMaxOpponents     = 50
)

const guildWarSchema = `
CREATE TABLE IF NOT EXISTS guild_wars (
 division TEXT NOT NULL,
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 guild_a INTEGER NOT NULL,
 guild_b INTEGER NOT NULL,
 record TEXT NOT NULL
);
`

/*
================
GuildWars
================
*/
func (s *Store) GuildWars() domain.GuildWarStore { return storeGuildWarDoor{s: s} }

/*
================
storeGuildWarDoor
================
*/
type storeGuildWarDoor struct{ s *Store }

/*
================
validateGuildWars

Startup validates without creating tables or repairing records.
================
*/
func validateGuildWars(db *sql.DB) error {
	rows, err := db.Query("SELECT DISTINCT division FROM guild_wars")
	if err != nil {
		return err
	}
	var divisions []string
	for rows.Next() {
		var division string
		if err := rows.Scan(&division); err != nil {
			rows.Close()
			return err
		}
		divisions = append(divisions, division)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, division := range divisions {
		wars, err := readGuildWars(db, division)
		if err != nil {
			return err
		}
		pairs := make(map[[2]int64]bool)
		for _, war := range wars {
			pair := war.Guilds
			if pair[0] > pair[1] {
				pair[0], pair[1] = pair[1], pair[0]
			}
			if pairs[pair] {
				return fmt.Errorf("guild war %d: duplicate opponents", war.ID)
			}
			pairs[pair] = true
			for _, guildID := range war.Guilds {
				var count int
				if err := db.QueryRow("SELECT count(*) FROM guilds WHERE division = ? AND guild_id = ?", division, guildID).Scan(&count); err != nil {
					return err
				}
				if count != 1 {
					return fmt.Errorf("guild war %d: missing guild %d", war.ID, guildID)
				}
			}
		}
	}

	return nil
}

/*
================
readGuildWars
================
*/
func readGuildWars(db *sql.DB, division string) ([]domain.GuildWarRecord, error) {
	rows, err := db.Query("SELECT id, guild_a, guild_b, record FROM guild_wars WHERE division = ? ORDER BY id", division)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.GuildWarRecord
	for rows.Next() {
		var id uint32
		var a, b int64
		var raw string
		if err := rows.Scan(&id, &a, &b, &raw); err != nil {
			return nil, err
		}
		var war domain.GuildWarRecord
		if err := decodeJSONStrict([]byte(raw), &war); err != nil {
			return nil, err
		}
		if war.ID != id || a <= 0 || b <= 0 || a == b || war.Guilds != [2]int64{a, b} || war.ScoreIndex >= guildWarScoreOptions || war.Stake > guildWarMaxCombinedStake || war.EndMs < 0 {
			return nil, fmt.Errorf("guild war %d: inconsistent record", id)
		}
		out = append(out, war)
	}
	return out, rows.Err()
}

/*
================
GuildWars
================
*/
func (door storeGuildWarDoor) GuildWars(division string) ([]domain.GuildWarRecord, error) {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	if door.s.db == nil {
		return nil, errors.New("guild war: authority unavailable")
	}
	return readGuildWars(door.s.db, division)
}

/*
================
writeGuildWarTx
================
*/
func writeGuildWarTx(tx *sql.Tx, division string, war domain.GuildWarRecord) error {
	raw, err := json.Marshal(war)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE guild_wars SET record = ? WHERE division = ? AND id = ?", string(raw), division, war.ID)
	return err
}

/*
================
BeginGuildWar

Both masters pay their individual stakes. The war row holds their sum.
The database's AUTOINCREMENT key survives deletion and process restart.
================
*/
func (door storeGuildWarDoor) BeginGuildWar(division string, start domain.GuildWarStart) (record domain.GuildWarRecord, code uint8, err error) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if err != nil {
			s.recordWriteFailureLocked("guild-war-begin", err)
		}
	}()
	war := start.Record
	if war.Guilds[0] == war.Guilds[1] || war.Stake > guildWarMaxCombinedStake || war.Stake%2 != 0 || war.ScoreIndex >= guildWarScoreOptions {
		return record, 3, nil
	}
	wars, err := readGuildWars(s.db, division)
	if err != nil {
		return record, 2, err
	}
	var counts [2]int
	for _, other := range wars {
		if other.Guilds == war.Guilds || other.Guilds == [2]int64{war.Guilds[1], war.Guilds[0]} {
			return record, 0x3a, nil
		}
		for side, guildID := range war.Guilds {
			if other.Guilds[0] == guildID || other.Guilds[1] == guildID {
				counts[side]++
			}
		}
	}
	for side, count := range counts {
		if count >= guildWarMaxOpponents {
			return record, uint8(0x3e + side), nil
		}
	}
	var actors [2]*domain.Character
	var next [2]domain.Character
	for side, master := range start.Masters {
		id, _, _, _, actor, refusal := (storeGuildDoor{s: s}).authorizedGuildActorLocked(division, master, domain.GuildAuthorization{LeaderOnly: true})
		if refusal.Refused() || id != war.Guilds[side] {
			return record, 0x1e, nil
		}
		if actor.Gold == nil || *actor.Gold < int64(war.Stake/2) {
			return record, 0x0c, nil
		}
		actors[side] = actor
		next[side] = *actor
		gold := *actor.Gold - int64(war.Stake/2)
		next[side].Gold = &gold
	}
	tx, err := s.db.Begin()
	if err != nil {
		return record, 2, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec("INSERT INTO guild_wars (division, guild_a, guild_b, record) VALUES (?, ?, ?, '{}')", division, war.Guilds[0], war.Guilds[1])
	if err != nil {
		return record, 2, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return record, 2, err
	}
	if id > int64(^uint32(0)) {
		return record, 2, errors.New("guild war: exhausted native ID space")
	}
	war.ID = uint32(id)
	if err = writeGuildWarTx(tx, division, war); err != nil {
		return record, 2, err
	}
	for i := range next {
		if err = upsertCharacterTx(tx, division, &next[i]); err != nil {
			return record, 2, err
		}
	}
	if err = tx.Commit(); err != nil {
		return record, 2, err
	}
	for i, actor := range actors {
		actor.Gold = next[i].Gold
	}
	s.recordWriteSuccessLocked()
	return war, 0, nil
}

/*
================
AccountGuildWarCombat

43C320 updates war and member accounts together, with uint32 wrap.
================
*/
func (door storeGuildWarDoor) AccountGuildWarCombat(division string, combat domain.GuildWarCombat) (record domain.GuildWarRecord, code uint8, err error) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if err != nil {
			s.recordWriteFailureLocked("guild-war-combat", err)
		}
	}()
	wars, err := readGuildWars(s.db, division)
	if err != nil {
		return record, 2, err
	}
	found := false
	for _, war := range wars {
		if war.ID == combat.WarID {
			record = war
			found = true
			break
		}
	}
	if !found {
		return record, 0x40, nil
	}
	ids := [2]int64{combat.KillerID, combat.VictimID}
	var guildIDs [2]int64
	for i, id := range ids {
		gid, _, _, _, _, refusal := (storeGuildDoor{s: s}).authorizedGuildActorLocked(division, id, domain.GuildAuthorization{})
		if refusal.Refused() || gid != record.Guilds[0] && gid != record.Guilds[1] {
			return record, 3, nil
		}
		guildIDs[i] = gid
	}
	if guildIDs[0] == guildIDs[1] {
		return record, 3, nil
	}
	side := 0
	if guildIDs[0] == record.Guilds[1] {
		side = 1
	}
	record.Scores[side] += uint32(combat.Score)
	tx, err := s.db.Begin()
	if err != nil {
		return record, 2, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = writeGuildWarTx(tx, division, record); err != nil {
		return record, 2, err
	}
	var nextMembers [2][]domain.GuildMemberRecord
	for i, id := range ids {
		gid := guildIDs[i]
		nextMembers[i] = append([]domain.GuildMemberRecord{}, s.guildMembers[division][gid]...)
		for index := range nextMembers[i] {
			member := &nextMembers[i][index]
			if member.CharID != id {
				continue
			}
			if i == 0 {
				member.Dword30 += uint32(combat.Score)
				member.Dword34++
			} else {
				member.Dword38++
			}
		}
		if err = replaceGuildTx(tx, division, gid, s.guilds[division][gid], nextMembers[i]); err != nil {
			return record, 2, err
		}
	}
	if err = tx.Commit(); err != nil {
		return record, 2, err
	}
	for i, gid := range guildIDs {
		s.guildMembers[division][gid] = nextMembers[i]
	}
	s.recordWriteSuccessLocked()
	return record, 0, nil
}

/*
================
EndGuildWar

43C130 credits the complete stake to the winning guild's compensation.
Deleting the war and crediting the guild are indivisible and idempotent.
================
*/
func (door storeGuildWarDoor) EndGuildWar(division string, id uint32, winner int64) (code uint8, err error) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if err != nil {
			s.recordWriteFailureLocked("guild-war-end", err)
		}
	}()
	wars, err := readGuildWars(s.db, division)
	if err != nil {
		return 2, err
	}
	var war domain.GuildWarRecord
	for _, row := range wars {
		if row.ID == id {
			war = row
			break
		}
	}
	guild, exists := s.guilds[division][winner]
	if war.ID == 0 || !exists || winner != war.Guilds[0] && winner != war.Guilds[1] {
		return 2, nil
	}
	guild.WarCompensation += int64(war.Stake)
	tx, err := s.db.Begin()
	if err != nil {
		return 2, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = replaceGuildTx(tx, division, winner, guild, s.guildMembers[division][winner]); err != nil {
		return 2, err
	}
	if _, err = tx.Exec("DELETE FROM guild_wars WHERE division = ? AND id = ?", division, id); err != nil {
		return 2, err
	}
	if err = tx.Commit(); err != nil {
		return 2, err
	}
	s.guilds[division][winner] = guild
	s.recordWriteSuccessLocked()
	return 0, nil
}

/*
================
GuildWarMemberScores
================
*/
func (door storeGuildWarDoor) GuildWarMemberScores(division string, guildID int64) ([]domain.GuildWarMemberScore, error) {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	var out []domain.GuildWarMemberScore
	for _, member := range door.s.guildMembers[division][guildID] {
		out = append(out, domain.GuildWarMemberScore{CharacterID: member.CharID, GuildID: guildID, Score: member.Dword30, Kills: member.Dword34, Deaths: member.Dword38})
	}
	return out, nil
}
