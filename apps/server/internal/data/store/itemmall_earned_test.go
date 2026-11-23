/*
===========================================================================

itemmall_earned_test.go - the two silk balances at purchase time (#4133)

A purchase draws earned silk first; the grant learns whether earned
silk alone paid, because that sets the delivered items' bound flag.
Grants, credits and balances that predate the split are granted.

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
earnedWallet

A store whose mall-account wallet holds the given granted silk and,
through the real credit path, the given earned silk.
================
*/
func earnedWallet(t *testing.T) (*Store, *domain.Character) {
	t.Helper()
	s := openTest(t, t.TempDir(), newTestClock())
	character := seededCharacter()
	if err := s.CreateCharacter(testDivision, "earned-account", character); err != nil {
		t.Fatal(err)
	}
	return s, character
}

/*
================
TestMallPurchaseDrawsEarnedFirst
================
*/
func TestMallPurchaseDrawsEarnedFirst(t *testing.T) {
	t.Parallel()

	s, character := earnedWallet(t)
	if _, err := s.syncDBLocked().Exec("INSERT INTO mall_accounts VALUES (?, ?, ?, ?)", "earned-account", 100, 20, 10); err != nil {
		t.Fatal(err)
	}
	if _, credited, err := s.CreditBetaSilk("earned-account", 50, 1500); err != nil || !credited {
		t.Fatalf("earned credit: %v %v", credited, err)
	}
	buy := func(cost domain.MallBalance) (domain.MallBalance, bool, error) {
		earnedOnly := false
		remaining, err := s.PurchaseMall(character, cost, func(rows []domain.InventoryRow, only bool) ([]domain.InventoryRow, error) {
			earnedOnly = only
			return rows, nil
		})
		return remaining, earnedOnly, err
	}

	// Earned silk covers a silk-only price whole: earned pays, granted
	// is untouched, and the grant hears earnedOnly.
	remaining, earnedOnly, err := buy(domain.MallBalance{Silk: 40})
	want := domain.MallBalance{Silk: 100, EarnedSilk: 10, GiftSilk: 20, Points: 10}
	if err != nil || !earnedOnly || remaining != want {
		t.Fatalf("earned purchase: %+v %v %v", remaining, earnedOnly, err)
	}

	// Earned runs out mid-price: the rest falls to granted silk and the
	// purchase is no longer earned alone.
	remaining, earnedOnly, err = buy(domain.MallBalance{Silk: 30})
	want = domain.MallBalance{Silk: 80, EarnedSilk: 0, GiftSilk: 20, Points: 10}
	if err != nil || earnedOnly || remaining != want {
		t.Fatalf("mixed purchase: %+v %v %v", remaining, earnedOnly, err)
	}

	// A gift-silk price is never an earned purchase, and a price past
	// both balances together is refused without moving anything.
	if _, earnedOnly, err = buy(domain.MallBalance{GiftSilk: 5}); err != nil || earnedOnly {
		t.Fatalf("gift purchase: %v %v", earnedOnly, err)
	}
	if _, _, err = buy(domain.MallBalance{Silk: 81}); err == nil {
		t.Fatal("overspent both balances together")
	}
	balance, err := s.MallBalance(character)
	want = domain.MallBalance{Silk: 80, EarnedSilk: 0, GiftSilk: 15, Points: 10}
	if err != nil || balance != want {
		t.Fatalf("wallet after refusals: %+v %v", balance, err)
	}
}

/*
================
TestBetaSilkCreditLandsInEarnedAndCapCountsBoth
================
*/
func TestBetaSilkCreditLandsInEarnedAndCapCountsBoth(t *testing.T) {
	t.Parallel()

	s, _ := earnedWallet(t)
	if _, err := s.syncDBLocked().Exec("INSERT INTO mall_accounts VALUES (?, ?, ?, ?)", "earned-account", 1480, 0, 0); err != nil {
		t.Fatal(err)
	}
	balance, credited, err := s.CreditBetaSilk("earned-account", 50, 1500)
	if err != nil || !credited || balance.Silk != 1480 || balance.EarnedSilk != 20 {
		t.Fatalf("trimmed credit: %+v %v %v", balance, credited, err)
	}
	if _, credited, err = s.CreditBetaSilk("earned-account", 50, 1500); err != nil || credited {
		t.Fatalf("credit past the whole-wallet cap: %v %v", credited, err)
	}
}

/*
================
TestMallWalletPredatingSplitReadsGranted
================
*/
func TestMallWalletPredatingSplitReadsGranted(t *testing.T) {
	t.Parallel()

	s, character := earnedWallet(t)
	// A layout-14 wallet: a mall_accounts row and nothing else. Its
	// whole balance is granted, so a purchase can never be earned-only.
	if _, err := s.syncDBLocked().Exec("INSERT INTO mall_accounts VALUES (?, ?, ?, ?)", "earned-account", 100, 0, 0); err != nil {
		t.Fatal(err)
	}
	balance, err := s.MallBalance(character)
	if err != nil || balance != (domain.MallBalance{Silk: 100}) {
		t.Fatalf("pre-split balance: %+v %v", balance, err)
	}
	earnedOnly := true
	remaining, err := s.PurchaseMall(character, domain.MallBalance{Silk: 30}, func(rows []domain.InventoryRow, only bool) ([]domain.InventoryRow, error) {
		earnedOnly = only
		return rows, nil
	})
	if err != nil || earnedOnly || remaining != (domain.MallBalance{Silk: 70}) {
		t.Fatalf("pre-split purchase: %+v %v %v", remaining, earnedOnly, err)
	}
}

/*
================
TestSilkHistoryKeepsTheSplit
================
*/
func TestSilkHistoryKeepsTheSplit(t *testing.T) {
	t.Parallel()

	s, character := earnedWallet(t)
	if _, err := s.syncDBLocked().Exec("INSERT INTO mall_accounts VALUES (?, ?, ?, ?)", "earned-account", 100, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreditBetaSilk("earned-account", 50, 1500); err != nil {
		t.Fatal(err)
	}
	grant := func(rows []domain.InventoryRow, _ bool) ([]domain.InventoryRow, error) { return rows, nil }
	if _, err := s.PurchaseMall(character, domain.MallBalance{Silk: 30}, grant); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PurchaseMall(character, domain.MallBalance{Silk: 40}, grant); err != nil {
		t.Fatal(err)
	}
	rows, total, err := s.SilkHistory(character, 1, 10)
	if err != nil || total != 70 || len(rows) != 2 {
		t.Fatalf("history: %+v %d %v", rows, total, err)
	}
	// Newest first: the 40 purchase drew the last 20 earned, the 30
	// purchase before it was earned whole.
	if rows[0].Charged != 40 || rows[0].EarnedSilk != 20 || rows[1].Charged != 30 || rows[1].EarnedSilk != 30 {
		t.Fatalf("split lost: %+v", rows)
	}
}
