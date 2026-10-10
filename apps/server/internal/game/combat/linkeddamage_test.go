/*
===========================================================================

linkeddamage_test.go - the fence and Pain Quota shares of a hit

===========================================================================
*/

package combat

import "testing"

/*
================
TestFenceShareMovesItsLane

A Physical Fence (mask 7) moves its percent of the physical lane only, a
Magical Fence (mask 11) of the magical lane, both lanes with mask 15; 100
percent moves the whole hit and leaves the lanes; no share bit moves
nothing.
================
*/
func TestFenceShareMovesItsLane(t *testing.T) {
	hit := Result{Damage: 1000, PhysicalDamage: 700, MagicalDamage: 300}
	for _, tc := range []struct {
		name            string
		mask, percent   uint32
		keep, phys, mag uint32
		moved           uint32
	}{
		{"physical fence", 7, 33, 769, 469, 300, 231},
		{"magical fence", 11, 36, 892, 700, 192, 108},
		{"both lanes", 15, 50, 500, 350, 150, 500},
		{"whole hit", 7, 100, 0, 700, 300, 1000},
		{"no share bit", 4, 33, 1000, 700, 300, 0},
	} {
		got, moved := FenceShare(tc.mask, tc.percent, hit)
		if moved != tc.moved || got.Damage != tc.keep || got.PhysicalDamage != tc.phys || got.MagicalDamage != tc.mag {
			t.Fatalf("%s: kept %+v moved %d", tc.name, got, moved)
		}
	}
	// The move never exceeds the hit (5A1050).
	if got, moved := FenceShare(15, 99, Result{Damage: 10, PhysicalDamage: 700, MagicalDamage: 300}); moved != 10 || got.Damage != 0 {
		t.Fatalf("overdrawn hit: kept %+v moved %d", got, moved)
	}
}

/*
================
TestQuotaShareDividesTheRest

Pain Quota 35: the recipient keeps 65 percent and the members divide the
rest, the remainder lost; no member leaves the hit whole.
================
*/
func TestQuotaShareDividesTheRest(t *testing.T) {
	hit := Result{Damage: 1001}
	got, share := QuotaShare(35, hit, 3)
	if got.Damage != 650 || share != 117 {
		t.Fatalf("kept %d share %d, want 650 and 117", got.Damage, share)
	}
	if got, share := QuotaShare(35, hit, 0); got.Damage != 1001 || share != 0 {
		t.Fatalf("no member: kept %d share %d", got.Damage, share)
	}
	if got, share := QuotaShare(35, Result{Damage: 1}, 4); got.Damage != 1 || share != 0 {
		t.Fatalf("zero share: kept %d share %d", got.Damage, share)
	}
}
