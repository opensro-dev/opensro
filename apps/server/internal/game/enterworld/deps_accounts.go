/*
===========================================================================

deps_accounts.go - the per-account event records behind Deps

The records that belong to an account rather than to one character: the
Halloween box allowance (#2547) and the Survival Arena's daily entries
(#3322). Production reads both from the authority store, through the
Deps record functions. Isolated gameplay fixtures run no durable
authority, so each record falls back to an in-memory map that lives as
long as the fixture. Either way the pointer is read or mutated only
inside a write door.

===========================================================================
*/
package enterworld

import "opensro.online/server/internal/domain"

/*
================
HalloweenAccount

Production supplies the authority's account record. The in-memory
fallback is for isolated gameplay fixtures, which do not run a durable
authority; it keeps one record per account id for the fixture's life.
================
*/
func (d *Deps) HalloweenAccount(accountID string) *domain.HalloweenBoxAccount {
	if d.HalloweenAccountRecord != nil {
		return d.HalloweenAccountRecord(accountID)
	}
	if d.fixtureHalloweenBoxes == nil {
		d.fixtureHalloweenBoxes = map[string]*domain.HalloweenBoxAccount{}
	}
	record := d.fixtureHalloweenBoxes[accountID]
	if record == nil {
		record = &domain.HalloweenBoxAccount{}
		d.fixtureHalloweenBoxes[accountID] = record
	}
	return record
}

/*
================
SookArenaAccount

Production supplies the authority's account record. The in-memory
fallback is for isolated gameplay fixtures, which do not run a durable
authority; it keeps one record per account id for the fixture's life,
as HalloweenAccount does.
================
*/
func (d *Deps) SookArenaAccount(accountID string) *domain.SookArenaAccount {
	if d.SookArenaAccountRecord != nil {
		return d.SookArenaAccountRecord(accountID)
	}
	if d.fixtureSookArenaAccounts == nil {
		d.fixtureSookArenaAccounts = map[string]*domain.SookArenaAccount{}
	}
	record := d.fixtureSookArenaAccounts[accountID]
	if record == nil {
		record = &domain.SookArenaAccount{}
		d.fixtureSookArenaAccounts[accountID] = record
	}
	return record
}
