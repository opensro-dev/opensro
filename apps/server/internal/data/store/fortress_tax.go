/*
===========================================================================

fortress_tax.go - atomic treasury withdrawals by the fortress holder

The fortress authority serializes collection with capture and tax changes.
This door serializes the master's balance and membership with the database
commit; neither in-memory owner changes when a write fails.

===========================================================================
*/
package store

import (
	"encoding/json"
	"math"

	"opensro.online/server/internal/domain"
)

/*
================
CollectFortressTax

62F7C0 admits the current holder's master; 62347E credits the query amount.
The recovered _SiegeFortressTaxCollect refuses over-treasury requests;
626180 maps its failure to code 2. Refuse signed balance overflow as the
SQL BIGINT addition would, without consuming any treasury.
================
*/
func (door storeFortressDoor) CollectFortressTax(division string, record domain.FortressRecord, actorID int64, requested int64) (collected int64, code uint8, err error) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if err != nil {
			s.recordWriteFailureLocked("fortress-tax", err)
		}
	}()
	if s.db == nil {
		return 0, domain.FortressTaxErrFailure, errFortressUnavailable
	}
	guildID, _, members, actorIndex, actor, refusal := (storeGuildDoor{s: s}).authorizedGuildActorLocked(
		division, actorID, domain.GuildAuthorization{})
	holder := record.GuildID
	if record.TempGuildID != 0 {
		holder = record.TempGuildID
	}
	if refusal.Refused() || holder == 0 || guildID != holder {
		return 0, domain.FortressTaxErrOwner, nil
	}
	if members[actorIndex].Grade != 0 {
		return 0, domain.FortressTaxErrMaster, nil
	}
	amount := max(requested, 0)
	if amount > record.TaxGold {
		return 0, domain.FortressTaxErrFailure, nil
	}
	gold := int64(0)
	if actor.Gold != nil {
		gold = *actor.Gold
	}
	if amount < 0 || gold < 0 || amount > math.MaxInt64-gold {
		return 0, domain.FortressTaxErrFailure, nil
	}
	gold += amount
	next := *actor
	next.Gold = &gold
	record.TaxGold -= amount
	raw, err := json.Marshal(record)
	if err != nil {
		return 0, domain.FortressTaxErrFailure, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, domain.FortressTaxErrFailure, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = upsertCharacterTx(tx, division, &next); err != nil {
		return 0, domain.FortressTaxErrFailure, err
	}
	if _, err = tx.Exec("INSERT INTO fortresses (division, fortress_id, record) VALUES (?, ?, ?) ON CONFLICT(division, fortress_id) DO UPDATE SET record = excluded.record", division, record.FortressID, string(raw)); err != nil {
		return 0, domain.FortressTaxErrFailure, err
	}
	if err = tx.Commit(); err != nil {
		return 0, domain.FortressTaxErrFailure, err
	}
	actor.Gold = &gold
	s.recordWriteSuccessLocked()
	return amount, 0, nil
}
