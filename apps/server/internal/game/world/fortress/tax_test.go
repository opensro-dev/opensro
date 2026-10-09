/*
===========================================================================

tax_test.go - durable tax changes and refusal without publication

===========================================================================
*/
package fortress

import (
	"math"
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

/*
================
TestTreasuryAccumulatesAndFlushesInSteps

486330/62A550/6201A0: only a positive amount counts; the treasury is
written once it has grown by more than 10000 since its last write, and a
failed write keeps the unsaved gold for the next attempt.
================
*/
func TestTreasuryAccumulatesAndFlushesInSteps(t *testing.T) {
	store := &memoryFortressStore{records: map[uint32]domain.FortressRecord{
		1: {FortressID: 1, GuildID: 41, TaxRate: 10, TaxGold: 500},
	}}
	a := New([]Catalog{{ID: 1}})
	if err := a.Restore("a", store); err != nil {
		t.Fatal(err)
	}
	saved := func() int64 { return store.records[1].TaxGold }
	a.AccumulateTax("a", 1, 0)
	a.AccumulateTax("a", 1, -300)
	if r, _ := a.Get("a", 1); r.TaxGold != 500 {
		t.Fatalf("a discount drained the treasury: %d", r.TaxGold)
	}
	a.AccumulateTax("a", 1, 10000)
	if r, _ := a.Get("a", 1); r.TaxGold != 10500 || saved() != 500 {
		t.Fatalf("growth of exactly 10000 must wait: live %d saved %d", r.TaxGold, saved())
	}
	a.AccumulateTax("a", 1, 1)
	if saved() != 10501 {
		t.Fatalf("growth past 10000 was not written: %d", saved())
	}
	store.fail = true
	a.AccumulateTax("a", 1, 10001)
	if saved() != 10501 {
		t.Fatal("a failed write changed the stored treasury")
	}
	store.fail = false
	a.AccumulateTax("a", 1, 1)
	if saved() != 20503 {
		t.Fatalf("the unsaved gold was not retried: %d", saved())
	}
	a.AccumulateTax("a", 1, math.MaxInt64)
	if r, _ := a.Get("a", 1); r.TaxGold != math.MaxInt64 {
		t.Fatalf("the treasury wrapped: %d", r.TaxGold)
	}
}
