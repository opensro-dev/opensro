/*
===========================================================================

silkgrant.go - operator silk credits into the account's mall wallet

GMs and operators grant silk (action/operator_silk.go, operator tooling,
not a gameplay rule). The credit lands in mall_accounts.silk, the wallet
the Item Mall debits, so it is real silk and survives restarts.

===========================================================================
*/
package store

import (
	"fmt"

	"opensro.online/server/internal/domain"
)

/*
================
GrantSilk

Add amount to the account's silk, creating the wallet when it has none.
One transaction; a total that would pass the wallet's uint32 is refused
whole rather than clamped.
================
*/
func (s *Store) GrantSilk(accountID string, amount uint32) (domain.MallBalance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if accountID == "" || s.db == nil {
		return domain.MallBalance{}, fmt.Errorf("silk grant: account unavailable")
	}
	tx, err := s.beginTxLocked()
	if err != nil {
		return domain.MallBalance{}, err
	}
	defer func() { _ = tx.Rollback() }()
	balance, err := creditSilkTx(tx, accountID, amount, s.now().UnixMilli())
	if err != nil {
		return domain.MallBalance{}, err
	}
	return balance, tx.Commit()
}
