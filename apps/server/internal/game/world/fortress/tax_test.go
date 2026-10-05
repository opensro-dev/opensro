/*
===========================================================================

tax_test.go - durable tax changes and refusal without publication

===========================================================================
*/
package fortress

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestTaxRatePersistenceAndFailure
================
*/
func TestTaxRatePersistenceAndFailure(t *testing.T) {
	store := &memoryFortressStore{records: map[uint32]domain.FortressRecord{
		1: {FortressID: 1, GuildID: 41, TaxRate: 5, TaxGold: 1234},
	}}
	a := New([]Catalog{{ID: 1}})
	if err := a.Restore("a", store); err != nil {
		t.Fatal(err)
	}
	if !a.SetTaxRate("a", 1, -20) {
		t.Fatal("valid signed ratio refused")
	}
	store.fail = true
	if a.SetTaxRate("a", 1, 20) {
		t.Fatal("failed persistence confirmed")
	}
	r, _ := a.Get("a", 1)
	if r.TaxRate != -20 || r.TaxGold != 1234 || r.GuildID != 41 {
		t.Fatalf("failed update published or other state lost: %+v", r)
	}
	store.fail = false
	a.Occupy("a", 1, 42)
	restarted := New([]Catalog{{ID: 1}})
	if err := restarted.Restore("a", store); err != nil {
		t.Fatal(err)
	}
	r, _ = restarted.Get("a", 1)
	if r.TaxRate != -20 || r.TaxGold != 1234 || r.GuildID != 42 {
		t.Fatalf("occupation save lost tax state: %+v", r)
	}
	if restarted.SetTaxRate("a", 1, 21) || restarted.SetTaxRate("a", 1, -21) || restarted.SetTaxRate("a", 2, 1) {
		t.Fatal("invalid update accepted")
	}
	if other, _ := restarted.Get("b", 1); other.TaxRate != 0 || other.TaxGold != 0 {
		t.Fatal("tax state crossed divisions")
	}
}
