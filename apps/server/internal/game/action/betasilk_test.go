/*
===========================================================================

betasilk_test.go - the beta Item Mall silk allowance and its native mode

The allowance is refilled at world entry, pays before the account's silk
and never reaches the store; off, the mall talks to the store directly.

===========================================================================
*/
package action

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
fakeMallStore

A store that keeps one silk balance and records every debit it commits.
================
*/
type fakeMallStore struct {
	silk   uint32
	debits []uint32
	fail   bool
}

/*
================
fakeMallStore.MallBalance
================
*/
func (s *fakeMallStore) MallBalance(*domain.Character) (domain.MallBalance, error) {
	return domain.MallBalance{Silk: s.silk}, nil
}

/*
================
fakeMallStore.PurchaseMall
================
*/
func (s *fakeMallStore) PurchaseMall(_ *domain.Character, cost domain.MallBalance, grant func([]domain.InventoryRow) ([]domain.InventoryRow, error)) (domain.MallBalance, error) {
	if s.fail {
		return domain.MallBalance{Silk: s.silk}, errors.New("grant failed")
	}
	if cost.Silk > s.silk {
		return domain.MallBalance{Silk: s.silk}, domain.MallInsufficientCurrency{}
	}
	s.silk -= cost.Silk
	s.debits = append(s.debits, cost.Silk)
	return domain.MallBalance{Silk: s.silk}, nil
}

/*
================
TestBetaSilkFromEnv
================
*/
func TestBetaSilkFromEnv(t *testing.T) {
	for value, want := range map[string]uint32{"": 0, "off": 0, "0": 0, "on": BetaSilkDefault, "TRUE": BetaSilkDefault, "250000": 250000} {
		t.Setenv(EnvBetaSilk, value)
		if got, err := BetaSilkFromEnv(); err != nil || got != want {
			t.Fatalf("%q: %d %v, want %d", value, got, err, want)
		}
	}
	for _, value := range []string{"lots", "-5", "99999999999"} {
		t.Setenv(EnvBetaSilk, value)
		if _, err := BetaSilkFromEnv(); err == nil {
			t.Fatalf("%q accepted", value)
		}
	}
}

/*
================
TestBetaSilkOffIsTheStore

Native mode hands the mall the store itself and no entry hook.
================
*/
func TestBetaSilkOffIsTheStore(t *testing.T) {
	store := &fakeMallStore{silk: 7}
	authority, refill := WithBetaSilk(store, 0)
	if authority != domain.MallAuthority(store) || refill != nil {
		t.Fatal("native mode wrapped the store")
	}
}

/*
================
TestBetaSilkAllowancePaysFirstAndRefillsAtEntry

Before entry the balance is native. Entry adds the allowance; a purchase
spends it before the account's silk, and the store is debited only for the
rest. A failed purchase spends nothing; the next entry refills.
================
*/
func TestBetaSilkAllowancePaysFirstAndRefillsAtEntry(t *testing.T) {
	store := &fakeMallStore{silk: 50}
	authority, refill := WithBetaSilk(store, 100)
	c := &domain.Character{AccountID: "acct"}
	grant := func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) { return rows, nil }
	if balance, _ := authority.MallBalance(c); balance.Silk != 50 {
		t.Fatalf("balance before entry %d, want the native 50", balance.Silk)
	}
	refill(c)
	if balance, _ := authority.MallBalance(c); balance.Silk != 150 {
		t.Fatalf("balance after entry %d, want 150", balance.Silk)
	}
	balance, err := authority.PurchaseMall(c, domain.MallBalance{Silk: 80}, grant)
	if err != nil || balance.Silk != 70 || store.silk != 50 || len(store.debits) != 1 || store.debits[0] != 0 {
		t.Fatalf("allowance purchase: %d %v, store %d %v", balance.Silk, err, store.silk, store.debits)
	}
	balance, err = authority.PurchaseMall(c, domain.MallBalance{Silk: 40}, grant)
	if err != nil || balance.Silk != 30 || store.silk != 30 || store.debits[1] != 20 {
		t.Fatalf("mixed purchase: %d %v, store %d %v", balance.Silk, err, store.silk, store.debits)
	}
	store.fail = true
	if balance, err = authority.PurchaseMall(c, domain.MallBalance{Silk: 10}, grant); err == nil || balance.Silk != 30 {
		t.Fatalf("failed purchase: %d %v", balance.Silk, err)
	}
	store.fail = false
	if _, err = authority.PurchaseMall(c, domain.MallBalance{Silk: 31}, grant); !errors.As(err, &domain.MallInsufficientCurrency{}) {
		t.Fatalf("over-budget purchase: %v", err)
	}
	refill(c)
	if balance, _ := authority.MallBalance(c); balance.Silk != 130 || store.silk != 30 {
		t.Fatalf("refill: %d, store %d", balance.Silk, store.silk)
	}
}
