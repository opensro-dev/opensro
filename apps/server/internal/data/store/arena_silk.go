/*
===========================================================================
arena_silk.go - atomically spend one earned voucher and credit its account.
===========================================================================
*/
package store

import (
	"fmt"
	"math"
	"opensro.online/server/internal/domain"
)

/*
================
RedeemArenaSilk
================
*/
func (s *Store) RedeemArenaSilk(c *domain.Character, slot uint8) (domain.MallBalance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	division, known := s.charDivision[c]
	if !known || c == nil || c.DeletePending || c.AccountID == "" || s.db == nil {
		return domain.MallBalance{}, fmt.Errorf("voucher owner unavailable")
	}
	at := -1
	for i, row := range c.MissionInventory {
		if row.Slot == int64(slot) {
			if at >= 0 {
				return domain.MallBalance{}, fmt.Errorf("ambiguous voucher")
			}
			at = i
		}
	}
	if at < 0 || c.MissionInventory[at].RefObjID != domain.ArenaSilkScrollID || c.MissionInventory[at].Codename != domain.ArenaSilkScrollCode || (c.MissionInventory[at].StackCount <= 0 || c.MissionInventory[at].StackCount > domain.ArenaSilkScrollMaxStack) {
		return domain.MallBalance{}, fmt.Errorf("voucher unavailable")
	}
	balance, err := readMallBalance(s.db, c.AccountID)
	if err != nil {
		return balance, err
	}
	if balance.Silk > math.MaxUint32-domain.ArenaSilkScrollValue {
		return balance, fmt.Errorf("wallet overflow")
	}
	next := *c
	next.MissionInventory = append([]domain.InventoryRow(nil), c.MissionInventory...)
	if next.MissionInventory[at].StackCount == 1 {
		next.MissionInventory = append(next.MissionInventory[:at], next.MissionInventory[at+1:]...)
	} else {
		next.MissionInventory[at].StackCount--
	}
	balance.Silk += domain.ArenaSilkScrollValue
	if err = s.commitMallPurchaseLocked(division, &next, balance, domain.MallBalance{}, domain.MallBalance{Silk: domain.ArenaSilkScrollValue}); err != nil {
		s.recordWriteFailureLocked("arena-silk", err)
		return domain.MallBalance{}, err
	}
	c.MissionInventory = next.MissionInventory
	if s.changes.empty() {
		s.recordWriteSuccessLocked()
	}
	return balance, nil
}
