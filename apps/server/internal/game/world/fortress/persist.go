/*
===========================================================================

persist.go - the fortress rows the authority store keeps

Occupation, a war's temporary holder and the war's requests survive a
restart, as the shard's _SiegeFortress and _SiegeFortressRequest rows do.
Restore loads a division once at startup and attaches the store; every
change that touches those rows saves before its caller goes on. A
request whose save fails is undone and refused, so the official never
reports an application the store does not hold. An occupation or capture
cannot be undone mid-war; a failed save there is logged by the store's
own failure accounting and the next change writes the row again.

===========================================================================
*/
package fortress

import (
	"fmt"
	"sort"

	"opensro.online/server/internal/domain"
)

/*
================
Restore

Loads a division's stored fortresses and requests over the fresh state
New builds, and keeps the store for later saves. Rows naming a fortress
this shard does not serve are an error: the catalog is the authority on
which fortresses exist.
================
*/
func (a *Authority) Restore(divisionID string, store domain.FortressStore) error {
	if a == nil || store == nil {
		return nil
	}
	records, requests, err := store.FortressState(divisionID)
	if err != nil {
		return fmt.Errorf("fortress: loading %s: %w", divisionID, err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	for _, row := range records {
		record, ok := state.records[row.FortressID]
		if !ok {
			return fmt.Errorf("fortress: stored fortress %d is not served", row.FortressID)
		}
		record.GuildID, record.TempGuildID = row.GuildID, row.TempGuildID
		record.StaffFlags = row.StaffFlags
		record.battles = make(map[int64]domain.FortressBattleRecord, len(row.BattleRecords))
		record.battleCheckpoints = make(map[int64]domain.FortressBattleRecord, len(row.BattleRecords))
		for _, battle := range row.BattleRecords {
			if battle.CharacterID <= 0 || battle.Rank > MaxBattleRank {
				return fmt.Errorf("fortress: invalid battle record")
			}
			if _, duplicate := record.battles[battle.CharacterID]; duplicate {
				return fmt.Errorf("fortress: duplicate battle record")
			}
			record.battles[battle.CharacterID] = battle
			record.battleCheckpoints[battle.CharacterID] = battle
		}
		if row.TaxRate < -20 || row.TaxRate > 20 || row.TaxGold < 0 {
			return fmt.Errorf("fortress: invalid tax state for fortress %d", row.FortressID)
		}
		record.TaxRate, record.TaxGold = row.TaxRate, row.TaxGold
	}
	for _, row := range requests {
		record, ok := state.records[row.FortressID]
		if !ok {
			return fmt.Errorf("fortress: stored request for fortress %d is not served", row.FortressID)
		}
		if record.Applicants == nil {
			record.Applicants = map[int64]RequestKind{}
		}
		record.Applicants[row.GuildID] = RequestKind(row.Kind)
	}
	a.store = store
	return nil
}

/*
================
saveRecordLocked

Writes one fortress's occupation rows; a nil store (fixtures, a shard
with no authority) keeps the state in memory only.
================
*/
func (a *Authority) saveRecordLocked(divisionID string, record *Record) error {
	if a.store == nil {
		return nil
	}
	return a.store.SaveFortress(divisionID, domain.FortressRecord{
		FortressID: record.ID, GuildID: record.GuildID, TempGuildID: record.TempGuildID,
		BattleRecords: battleRows(record), StaffFlags: record.StaffFlags,
		TaxRate: record.TaxRate, TaxGold: record.TaxGold,
	})
}

/*
================
saveRequestLocked
================
*/
func (a *Authority) saveRequestLocked(divisionID string, fortressID uint32, guildID int64, kind RequestKind, present bool) error {
	if a.store == nil {
		return nil
	}
	return a.store.SaveFortressRequest(divisionID, domain.FortressRequestRecord{
		FortressID: fortressID, GuildID: guildID, Kind: uint8(kind),
	}, present)
}

/*
================
Divisions

The divisions whose fortress tables are open, in name order.
================
*/
func (a *Authority) Divisions() []string {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.divisions))
	for id := range a.divisions {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
