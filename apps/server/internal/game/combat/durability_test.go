/*
===========================================================================

durability_test.go - maximum durability and the repair quote

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestRepairQuoteChargesTheClientPriceForWhatTheGoldCovers

789630: CostRepair / maximum per point (at least 1), plus CostRevive for a
broken item; 496E60: only the points the gold pays for.
================
*/
func TestRepairQuoteChargesTheClientPriceForWhatTheGoldCovers(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		current, maximum             uint32
		costRepair, costRevive, gold int64
		restored                     uint32
		cost                         int64
		ok                           bool
	}{
		{"worn", 40, 100, 200, 0, 1000, 100, 120, true},
		{"broken pays the revive", 0, 100, 200, 50, 1000, 100, 248, true},
		{"gold covers ten points", 40, 100, 200, 0, 21, 50, 20, true},
		{"cheap items cost one a point", 40, 100, 50, 0, 1000, 100, 60, true},
		{"nothing missing", 100, 100, 200, 0, 1000, 100, 0, false},
		{"no gold", 40, 100, 200, 0, 1, 40, 0, false},
	} {
		restored, cost, ok := RepairQuote(tc.current, tc.maximum, tc.costRepair, tc.costRevive, tc.gold)
		if restored != tc.restored || cost != tc.cost || ok != tc.ok {
			t.Errorf("%s: %d %d %v, want %d %d %v", tc.name, restored, cost, ok, tc.restored, tc.cost, tc.ok)
		}
	}
}

/*
================
staticMagicOptions
================
*/
type staticMagicOptions map[uint32]*enterworld.MagicOptionRow

/*
================
MagicOptionByParamID
================
*/
func (s staticMagicOptions) MagicOptionByParamID(id uint32) (*enterworld.MagicOptionRow, bool) {
	row, ok := s[id]
	return row, ok
}

/*
================
TestEquipmentMaxDurabilityFollowsVarianceAndOptions

Variance field 0 picks the itemdata range; 'nrep' +400% multiplies by
five (496A70).
================
*/
func TestEquipmentMaxDurabilityFollowsVarianceAndOptions(t *testing.T) {
	low := int64(40)
	ref := &enterworld.ItemRef{MaxDurability: 71, VarianceIntMin1c0: &low}
	row := enterworld.InventoryRow{Codename: "SWORD", VarianceBits: "31"}
	if got, err := EquipmentMaxDurability(ref, row, nil); err != nil || got != 71 {
		t.Fatalf("variance 31: %d %v, want 71", got, err)
	}
	row.VarianceBits = "0"
	if got, _ := EquipmentMaxDurability(ref, row, nil); got != 40 {
		t.Fatalf("variance 0: %d, want 40", got)
	}
	options := staticMagicOptions{7: {Tag: optionTag("nrep")}}
	row.MagicOptions = []uint64{400<<32 | 7}
	if got, err := EquipmentMaxDurability(ref, row, options); err != nil || got != 200 {
		t.Fatalf("nrep +400%%: %d %v, want 200", got, err)
	}
	if allowed, _ := RepairableByOptions(row, options); allowed {
		t.Fatal("a non-repairable item was allowed a repair")
	}
}

/*
================
TestDurabilityRoundsAsTheClientShowsIt

The v1.150 client shows lo + span*v/31 + 0.5, truncated (78BD00). The
server's maximum and a drop's first durability must be that same number
for every variance, or a fresh drop reads one point short and its repair
is refused as whole. An 85 shield (80..98) at variance 1 is 81, not 80.
================
*/
func TestDurabilityRoundsAsTheClientShowsIt(t *testing.T) {
	low := int64(80)
	ref := &enterworld.ItemRef{MaxDurability: 98, VarianceIntMin1c0: &low}
	for v := uint8(0); v <= 31; v++ {
		client := uint32(float64(98-80)*float64(v)/31 + 80 + 0.5)
		if got := DurabilityFromVariance(ref, v); got != client {
			t.Fatalf("variance %d: server %d, client shows %d", v, got, client)
		}
	}
	if got := DurabilityFromVariance(ref, 1); got != 81 {
		t.Fatalf("variance 1: %d, want 81", got)
	}
}
