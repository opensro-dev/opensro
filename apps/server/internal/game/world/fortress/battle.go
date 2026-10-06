/*
===========================================================================

battle.go - fortress battle records and authored rank transitions

The existing fortress authority owns records keyed by character. Combat owns
eligibility and buff installation; this module owns counts and persistence.
61F580 promotes one rank only after its indirect skill succeeds. 622380
checkpoints at count modulo ten equal to one, and immediately on promotion.

===========================================================================
*/
package fortress

import (
	"fmt"
	"sort"

	"opensro.online/server/internal/domain"
)

const MaxBattleRank uint8 = 6
const battleCheckpointInterval = 10

/*
================
BattleRank

Enabled v1.150 siegefortressbattlerank.txt rows: kill threshold and skill.
================
*/
func BattleRank(rank uint8) (uint32, uint32, bool) {
	rows := [...]struct{ kills, skill uint32 }{{15, 20510}, {25, 20511}, {45, 20512}, {70, 20513}, {100, 20514}, {150, 20515}}
	if rank == 0 || rank > MaxBattleRank {
		return 0, 0, false
	}
	row := rows[rank-1]
	return row.kills, row.skill, true
}

/*
================
BattleRecord
================
*/
func (a *Authority) BattleRecord(divisionID string, fortressID uint32, characterID int64) (domain.FortressBattleRecord, bool) {
	if a == nil {
		return domain.FortressBattleRecord{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok {
		return domain.FortressBattleRecord{}, false
	}
	row, ok := r.battles[characterID]
	return row, ok
}

/*
================
BattleChange
================
*/
type BattleChange struct {
	FortressID  uint32
	CharacterID int64
	Kill        bool
	NowMs       int64
}

/*
================
RecordBattle

61F2D0/61F580 create the first death/kill record at rank zero. Existing
kills increment as uint32 and offer the next rank without skipping ranks.
The caller installs that rank's skill before CommitBattleRank.
================
*/
func (a *Authority) RecordBattle(divisionID string, change BattleChange) (domain.FortressBattleRecord, uint8, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	r, ok := state.records[change.FortressID]
	if !ok || !state.warActive || change.CharacterID <= 0 {
		return domain.FortressBattleRecord{}, 0, fmt.Errorf("fortress: invalid battle record admission")
	}
	if r.battles == nil {
		r.battles = map[int64]domain.FortressBattleRecord{}
	}
	row, exists := r.battles[change.CharacterID]
	if !exists {
		row = domain.FortressBattleRecord{CharacterID: change.CharacterID, RankAtMs: change.NowMs}
	}
	if change.Kill {
		row.Kills++
	} else {
		row.Deaths++
	}
	r.battles[change.CharacterID] = row
	var promote uint8
	if change.Kill && exists {
		threshold, _, ok := BattleRank(row.Rank + 1)
		if ok && row.Kills >= threshold {
			promote = row.Rank + 1
		}
	}
	// Below the top rank, the kill path returns without the periodic update
	// until a promotion. Deaths and top-rank kills use 622380's modulo rule.
	checkpoint := !exists || (!change.Kill || row.Rank == MaxBattleRank) &&
		(row.Kills%battleCheckpointInterval == 1 || row.Deaths%battleCheckpointInterval == 1)
	if checkpoint {
		return row, promote, a.checkpointBattleLocked(divisionID, r, row)
	}
	return row, promote, nil
}

/*
================
CommitBattleRank

61CA70 updates rank and local timestamp only after BeginIndirectSkill.
================
*/
func (a *Authority) CommitBattleRank(divisionID string, change BattleChange, rank uint8) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.divisionLocked(divisionID).records[change.FortressID]
	if !ok {
		return fmt.Errorf("fortress: unknown battle fortress")
	}
	row, ok := r.battles[change.CharacterID]
	threshold, _, valid := BattleRank(rank)
	if !ok || !valid || rank != row.Rank+1 || row.Kills < threshold {
		return fmt.Errorf("fortress: invalid battle rank transition")
	}
	row.Rank, row.RankAtMs = rank, change.NowMs
	r.battles[change.CharacterID] = row
	return a.checkpointBattleLocked(divisionID, r, row)
}

/*
================
battleRows

Copy sorted value rows while the authority lock is held; stores never see
an alias of live state. The existing fortress JSON owns this child table.
================
*/
func battleRows(r *Record) []domain.FortressBattleRecord {
	var rows []domain.FortressBattleRecord
	for _, row := range r.battleCheckpoints {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CharacterID < rows[j].CharacterID })
	return rows
}

/*
================
checkpointBattleLocked

Only this character's checkpoint advances. Saving another fortress field
must not flush counts which 622380 has not queued to the native database.
================
*/
func (a *Authority) checkpointBattleLocked(division string, r *Record, row domain.FortressBattleRecord) error {
	if r.battleCheckpoints == nil {
		r.battleCheckpoints = map[int64]domain.FortressBattleRecord{}
	}
	old, exists := r.battleCheckpoints[row.CharacterID]
	r.battleCheckpoints[row.CharacterID] = row
	if err := a.saveRecordLocked(division, r); err != nil {
		if exists {
			r.battleCheckpoints[row.CharacterID] = old
		} else {
			delete(r.battleCheckpoints, row.CharacterID)
		}
		return err
	}
	return nil
}

/*
================
ReleaseBattleRecords

601170 -> 628D50 releases the live record map after residents lose rank
buffs. It does not enqueue QUERY_SIEGE_BATTLE_RECORD_DELETE; stored rows
remain owned by the shard database and 629590 loads them at server start.
================
*/
func (a *Authority) ReleaseBattleRecords(division string, id uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r := a.divisionLocked(division).records[id]; r != nil {
		r.battles = nil
	}
}
