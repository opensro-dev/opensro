/*
===========================================================================

silkgrant_test.go - operator silk credits

===========================================================================
*/
package store

import (
	"math"
	"testing"
)

/*
================
TestGrantSilkCreatesAddsAndRefusesOverflow
================
*/
func TestGrantSilkCreatesAddsAndRefusesOverflow(t *testing.T) {
	s, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	balance, err := s.GrantSilk("account-1", 100000)
	if err != nil || balance.Silk != 100000 {
		t.Fatalf("first grant %+v, %v", balance, err)
	}
	if balance, err = s.GrantSilk("account-1", 500); err != nil || balance.Silk != 100500 {
		t.Fatalf("second grant %+v, %v", balance, err)
	}
	if _, err = s.GrantSilk("account-1", math.MaxUint32); err == nil {
		t.Fatal("an overflowing grant was accepted")
	}
	if balance, err = s.GrantSilk("account-1", 1); err != nil || balance.Silk != 100501 {
		t.Fatalf("a refused overflow changed the wallet: %+v, %v", balance, err)
	}
	if _, err = s.GrantSilk("", 1); err == nil {
		t.Fatal("a grant without an account was accepted")
	}
}
