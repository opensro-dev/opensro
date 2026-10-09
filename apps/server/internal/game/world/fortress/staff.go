/*
===========================================================================

staff.go - the hired staff byte and its durable purchase

632100 admits a hire; 623DF9 applies its flags and charges both owners.
The authority lock keeps flags, capture and the war period ordered.

===========================================================================
*/
package fortress

import (
	"fmt"
	"opensro.online/server/internal/domain"
)

/*
================
HireStaff

Do not restrict the requested byte to the three UI choices: native tests
only overlap, then ORs the byte. A zero request still incurs the cost.
================
*/
func (a *Authority) HireStaff(divisionID string, fortressID uint32, actorID int64, flags uint8) (uint8, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	if state.warActive {
		return 0x18, nil
	}
	record, ok := state.records[fortressID]
	if !ok {
		return 3, nil
	}
	store, ok := a.store.(domain.FortressStaffStore)
	if !ok {
		return 2, fmt.Errorf("fortress: staff transaction unavailable")
	}
	next := domain.FortressRecord{FortressID: record.ID, GuildID: record.GuildID,
		TempGuildID: record.TempGuildID, TaxRate: record.TaxRate, TaxGold: record.TaxGold,
		BattleRecords: battleRows(record), StaffFlags: record.StaffFlags}
	code, err := store.HireFortressStaff(divisionID, next, actorID, flags)
	if code == 0 && err == nil {
		record.StaffFlags |= flags
		record.savedTax = record.TaxGold
	}
	return code, err
}
