/*
===========================================================================

tax.go - persisted fortress tax policy and the treasury it fills

The existing fortress authority owns the ratio alongside occupation. A
failed save leaves the old ratio visible to queries and commerce readers.
The treasury (TaxGold) grows by the tax merchants and gates collect for
the fortress and is written in steps, as the original batches it.

===========================================================================
*/
package fortress

import (
	"fmt"
	"math"

	"opensro.online/server/internal/domain"
)

// taxFlushThreshold is the native threshold 0x2710: the treasury is written
// once it has grown by more than this since its last write.
const taxFlushThreshold = 10000

/*
================
CollectTax

The native tax collection clamps signed gold, then checks period, fortress,
holder and master. Keep the period and treasury locked through the durable
transfer, so two requests cannot collect the same gold or race a capture.
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
		// The transaction wrote the whole record, the treasury included.
		record.savedTax = record.TaxGold
	}
	return collected, code, err
}

/*
================
SetTaxRate

The native rate setting admits signed percentages from -20 through 20 and
queues the durable update before the manager receives success.
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

/*
================
AccumulateTax

The character-side tax accumulation adds the tax one sale or fee paid to
the bound fortress and flushes it (the native flush, mode 0). Only a
positive amount counts: a negative ratio's discount never drains the
treasury. The sum saturates instead of wrapping (the native 64-bit add
would wrap, then clamp the result to zero).

The original marks the treasury written even when the write fails, so the
next attempt waits another 10000 gold; the port keeps the unsaved amount
and retries on the next accumulation (port-only, not native: a failed
write must not hide the gold from the next one).
================
*/
func (a *Authority) AccumulateTax(divisionID string, fortressID uint32, amount int64) {
	if a == nil || amount <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok {
		return
	}
	if record.TaxGold > math.MaxInt64-amount {
		record.TaxGold = math.MaxInt64
	} else {
		record.TaxGold += amount
	}
	if record.TaxGold-record.savedTax > taxFlushThreshold {
		// A failed write leaves savedTax behind, so the next accumulation
		// retries it; the gold stays in memory either way.
		_ = a.saveRecordLocked(divisionID, record)
	}
}
