/*
===========================================================================

tax.go - persisted fortress tax policy

The existing fortress authority owns the ratio alongside occupation. A
failed save leaves the old ratio visible to queries and commerce readers.

===========================================================================
*/
package fortress

import (
	"fmt"

	"opensro.online/server/internal/domain"
)

/*
================
CollectTax

62F7C0 clamps signed gold, then checks period, fortress, holder and master.
Keep the period and treasury locked through the durable transfer, so two
requests cannot collect the same gold or race a capture.
================
*/
func (a *Authority) CollectTax(divisionID string, fortressID uint32, actorID int64, requested int64) (int64, uint8, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	if !state.taxPeriod {
		return 0, domain.FortressTaxErrPeriod, nil
	}
	record, ok := state.records[fortressID]
	if !ok {
		return 0, domain.FortressTaxErrUnknown, nil
	}
	store, ok := a.store.(domain.FortressTaxStore)
	if !ok {
		return 0, domain.FortressTaxErrFailure, fmt.Errorf("fortress: tax transaction unavailable")
	}
	next := domain.FortressRecord{FortressID: record.ID, GuildID: record.GuildID,
		TempGuildID: record.TempGuildID, TaxRate: record.TaxRate, TaxGold: record.TaxGold,
		BattleRecords: battleRows(record), StaffFlags: record.StaffFlags}
	collected, code, err := store.CollectFortressTax(divisionID, next, actorID, max(requested, 0))
	if code == 0 && err == nil {
		record.TaxGold -= collected
	}
	return collected, code, err
}

/*
================
SetTaxRate

62F640 admits signed percentages from -20 through 20; 621220 queues the
durable update before the manager receives success.
================
*/
func (a *Authority) SetTaxRate(divisionID string, fortressID uint32, rate int16) bool {
	if a == nil || rate < -20 || rate > 20 {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok || record.TaxRate == rate {
		return false
	}
	previous := record.TaxRate
	record.TaxRate = rate
	if err := a.saveRecordLocked(divisionID, record); err != nil {
		record.TaxRate = previous
		return false
	}
	return true
}
