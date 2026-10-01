/*
===========================================================================

itemmall_test.go - account sharing and durable debit/delivery atomicity

===========================================================================
*/
package store

import (
	"errors"
	"sync/atomic"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestMallPurchaseAtomicity
================
*/
func TestMallPurchaseAtomicity(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	first := seededCharacter()
	second := seededCharacter()
	second.Name = "MallSibling"
	for _, character := range []*domain.Character{first, second} {
		if err := s.CreateCharacter(testDivision, "mall-account", character); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("INSERT INTO mall_accounts VALUES (?, ?, ?, ?)", "mall-account", 100, 20, 10); err != nil {
		t.Fatal(err)
	}
	cost := domain.MallBalance{Silk: 30, GiftSilk: 2, Points: 3}
	grant := func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) {
		rows[0].StackCount = 2
		return rows, nil
	}
	assertUnchanged := func() {
		t.Helper()
		balance, err := s.MallBalance(first)
		if err != nil || balance != (domain.MallBalance{Silk: 100, GiftSilk: 20, Points: 10}) || first.MissionInventory[0].StackCount != 1 {
			t.Fatalf("failed purchase changed authority: %+v, %v, %+v", balance, err, first.MissionInventory)
		}
	}
	if _, err := s.PurchaseMall(first, cost, func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) {
		rows[0].StackCount = 99
		return nil, errors.New("inventory full")
	}); err == nil {
		t.Fatal("accepted a failed grant")
	}
	assertUnchanged()
	s.FailCommits(errors.New("disk failure"))
	if _, err := s.PurchaseMall(first, cost, grant); err == nil {
		t.Fatal("accepted a failed durable commit")
	}
	assertUnchanged()
	s.FailCommits(nil)
	remaining, err := s.PurchaseMall(first, cost, grant)
	want := domain.MallBalance{Silk: 70, GiftSilk: 18, Points: 7}
	if err != nil || remaining != want || first.MissionInventory[0].StackCount != 2 {
		t.Fatalf("purchase did not commit both planes: %+v %v", remaining, err)
	}
	shared, err := s.MallBalance(second)
	if err != nil || shared != want || second.MissionInventory[0].StackCount != 1 {
		t.Fatalf("account currency or character isolation failed: %+v %v", shared, err)
	}
	if _, err := s.PurchaseMall(second, domain.MallBalance{Silk: 71}, grant); err == nil {
		t.Fatal("overspent the shared account")
	}
	s.Close()
	reopened := openTest(t, dir, clock)
	characters := reopened.Characters().CharactersForDivision(testDivision)
	for _, character := range characters {
		balance, err := reopened.MallBalance(character)
		if err != nil || balance != want {
			t.Fatalf("balance lost after restart: %+v %v", balance, err)
		}
		if character.Name == first.Name && character.MissionInventory[0].StackCount != 2 {
			t.Fatal("delivered inventory lost after restart")
		}
	}
}

/*
================
TestMallPurchaseUntrustedAccountIdentity

SQL punctuation remains account data in reads and writes. A copied character
cannot cross the store's pointer ownership boundary to spend another wallet.
================
*/
func TestMallPurchaseUntrustedAccountIdentity(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	account := "mall'; UPDATE mall_accounts SET silk = 0; --"
	character := seededCharacter()
	if err := s.CreateCharacter(testDivision, account, character); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{account, "untouched"} {
		if _, err := s.db.Exec("INSERT INTO mall_accounts VALUES (?, ?, ?, ?)", id, 100, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	var granted atomic.Int32
	grant := func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) { granted.Add(1); return rows, nil }
	impostor := *character
	if _, err := s.PurchaseMall(&impostor, domain.MallBalance{Silk: 1}, grant); err == nil {
		t.Fatal("accepted a character outside store ownership")
	}
	balance, err := s.PurchaseMall(character, domain.MallBalance{Silk: 30}, grant)
	if err != nil || balance.Silk != 70 || granted.Load() != 1 {
		t.Fatalf("bound identity purchase: %+v, %v, grants=%d", balance, err, granted.Load())
	}
	other, err := readMallBalance(s.db, "untouched")
	if err != nil || other.Silk != 100 {
		t.Fatalf("changed another account: %+v %v", other, err)
	}
}

/*
================
TestMallRacePurchasesCannotDoubleSpend

Two characters on one account race at the durable authority boundary. Exactly
one delivery can consume a balance that only covers one purchase.
================
*/
func TestMallRacePurchasesCannotDoubleSpend(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	characters := []*domain.Character{seededCharacter(), seededCharacter()}
	characters[1].Name = "MallRace"
	for _, character := range characters {
		if err := s.CreateCharacter(testDivision, "shared", character); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("INSERT INTO mall_accounts VALUES (?, ?, ?, ?)", "shared", 100, 0, 0); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, len(characters))
	var granted atomic.Int32
	for _, character := range characters {
		go func(character *domain.Character) {
			<-start
			_, err := s.PurchaseMall(character, domain.MallBalance{Silk: 60}, func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) {
				granted.Add(1)
				rows[0].StackCount = 2
				return rows, nil
			})
			results <- err
		}(character)
	}
	close(start)
	successful := 0
	for range characters {
		if <-results == nil {
			successful++
		}
	}
	balance, err := s.MallBalance(characters[0])
	if err != nil || successful != 1 || granted.Load() != 1 || balance.Silk != 40 {
		t.Fatalf("double-spend boundary: successes=%d grants=%d balance=%+v error=%v", successful, granted.Load(), balance, err)
	}
	delivered := 0
	for _, character := range characters {
		if character.MissionInventory[0].StackCount == 2 {
			delivered++
		}
	}
	if delivered != 1 {
		t.Fatalf("delivered %d purchases for one debit", delivered)
	}
}

/*
================
TestMallBalancesRejectOutOfRangeStorage
================
*/
func TestMallBalancesRejectOutOfRangeStorage(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	for _, amount := range []int64{-1, 4294967296} {
		for _, statement := range []string{
			"INSERT INTO mall_accounts VALUES ('bad', ?, 0, 0)",
			"INSERT INTO mall_accounts VALUES ('bad', 0, ?, 0)",
			"INSERT INTO mall_accounts VALUES ('bad', 0, 0, ?)",
		} {
			if _, err := s.db.Exec(statement, amount); err == nil {
				t.Fatalf("accepted currency outside uint32: %d", amount)
			}
		}
	}
}
