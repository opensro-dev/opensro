/*
===========================================================================

tax.go - persisted fortress tax policy

The existing fortress authority owns the ratio alongside occupation. A
failed save leaves the old ratio visible to queries and commerce readers.

===========================================================================
*/
package fortress

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
