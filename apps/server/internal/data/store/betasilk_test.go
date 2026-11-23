/*
===========================================================================

betasilk_test.go - beta silk credits land in the persisted mall wallet

The starter is granted once per account (the wallet row is the marker)
into granted silk; an hourly credit lands in earned silk (#4133) and
stops at the bank cap, which counts both balances together. Both
survive reopening the store.

===========================================================================
*/
package store

import "testing"

/*
================
TestBetaSilkStarterAndCreditPersist
================
*/
func TestBetaSilkStarterAndCreditPersist(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	if granted, err := s.GrantBetaSilkStarter("acct", 300); err != nil || !granted {
		t.Fatalf("first starter: granted=%v err=%v", granted, err)
	}
	if granted, err := s.GrantBetaSilkStarter("acct", 300); err != nil || granted {
		t.Fatalf("second starter must not grant: granted=%v err=%v", granted, err)
	}
	for _, want := range []uint32{50, 100} {
		balance, credited, err := s.CreditBetaSilk("acct", 50, 400)
		if err != nil || !credited || balance.Silk != 300 || balance.EarnedSilk != want {
			t.Fatalf("credit to %d earned: balance=%+v credited=%v err=%v", want, balance, credited, err)
		}
	}
	if balance, credited, err := s.CreditBetaSilk("acct", 50, 400); err != nil || credited || balance.Silk != 300 || balance.EarnedSilk != 100 {
		t.Fatalf("at the cap: balance=%+v credited=%v err=%v", balance, credited, err)
	}
	// A credit never passes the cap, and an account without a wallet gets one.
	if balance, _, err := s.CreditBetaSilk("fresh", 50, 30); err != nil || balance.Silk != 0 || balance.EarnedSilk != 30 {
		t.Fatalf("clamped new wallet: balance=%+v err=%v", balance, err)
	}
	s.Close()
	reopened := openTest(t, dir, newTestClock())
	if balance, err := readMallBalance(reopened.syncDBLocked(), "acct"); err != nil || balance.Silk != 300 || balance.EarnedSilk != 100 {
		t.Fatalf("credits must persist: balance=%+v err=%v", balance, err)
	}
	if granted, _ := reopened.GrantBetaSilkStarter("acct", 300); granted {
		t.Fatal("the starter must stay once per account across a restart")
	}
}
