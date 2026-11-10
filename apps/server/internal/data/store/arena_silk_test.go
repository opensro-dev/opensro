/*
===========================================================================
arena_silk_test.go - no consume/credit split, replay or failed-save loss.
===========================================================================
*/
package store

import (
	"errors"
	"opensro.online/server/internal/domain"
	"testing"
)

/*
================
TestArenaSilkAtomicRedemption
================
*/
func TestArenaSilkAtomicRedemption(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := seededCharacter()
	c.MissionInventory = []domain.InventoryRow{{Slot: 13, RefObjID: domain.ArenaSilkScrollID, Codename: domain.ArenaSilkScrollCode, StackCount: 1, TypeFlags: 492}}
	if err := s.CreateCharacter(testDivision, "arena-account", c); err != nil {
		t.Fatal(err)
	}
	s.commitFail = errors.New("disk fault")
	if _, err := s.RedeemArenaSilk(c, 13); err == nil || len(c.MissionInventory) != 1 {
		t.Fatal("failed commit spent voucher")
	}
	s.commitFail = nil
	b, err := s.RedeemArenaSilk(c, 13)
	if err != nil || b.Silk != 10 || len(c.MissionInventory) != 0 {
		t.Fatalf("redemption: %+v %v", b, err)
	}
	if _, err = s.RedeemArenaSilk(c, 13); err == nil {
		t.Fatal("replayed voucher credited")
	}
	b, err = s.MallBalance(c)
	if err != nil || b.Silk != 10 {
		t.Fatal("wallet changed twice")
	}
}

/*
================
TestArenaSilkStackRedemptionAndFailedSave
================
*/
func TestArenaSilkStackRedemptionAndFailedSave(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := seededCharacter()
	c.MissionInventory = []domain.InventoryRow{{Slot: 13, RefObjID: domain.ArenaSilkScrollID, Codename: domain.ArenaSilkScrollCode, StackCount: 20, TypeFlags: 492}}
	if err := s.CreateCharacter(testDivision, "stack-account", c); err != nil {
		t.Fatal(err)
	}
	s.commitFail = errors.New("disk fault")
	if _, err := s.RedeemArenaSilk(c, 13); err == nil || c.MissionInventory[0].StackCount != 20 {
		t.Fatal("failed commit consumed stack")
	}
	s.commitFail = nil
	for used := 1; used <= 20; used++ {
		balance, err := s.RedeemArenaSilk(c, 13)
		if err != nil || balance.Silk != uint32(used*10) {
			t.Fatalf("use %d: %+v %v", used, balance, err)
		}
		if used < 20 && (len(c.MissionInventory) != 1 || c.MissionInventory[0].StackCount != int64(20-used)) {
			t.Fatal("wrong stack remainder")
		}
	}
	if len(c.MissionInventory) != 0 {
		t.Fatal("empty stack retained")
	}
	if _, err := s.RedeemArenaSilk(c, 13); err == nil {
		t.Fatal("empty stack replay credited")
	}
}
