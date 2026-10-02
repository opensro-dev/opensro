/*
===========================================================================

regioncombat_test.go - protected town and battlefield lookup boundaries

===========================================================================
*/
package world

import "testing"

/*
================
TestRegionPlayerCombat
================
*/
func TestRegionPlayerCombat(t *testing.T) {
	for _, tc := range []struct {
		region         uint16
		allowed, known bool
	}{
		{0x62a6, true, true},
		{0x62a7, false, true},
		{0x62a8, false, true},
		{0x62a9, false, true},
		{0x62aa, true, true},
		{0, false, false},
		{0xffff, false, false},
	} {
		allowed, known := RegionPlayerCombat(tc.region)
		if allowed != tc.allowed || known != tc.known {
			t.Fatalf("region %04x: allowed=%v known=%v", tc.region, allowed, known)
		}
	}
}

/*
================
TestRegionCombatRangesCoverReferenceRows

Validate the compact representation, including every boundary and gap.
================
*/
func TestRegionCombatRangesCoverReferenceRows(t *testing.T) {
	count, protected := 0, 0
	for i, row := range regionCombatRanges {
		if row.first > row.last || i > 0 && row.first <= regionCombatRanges[i-1].last {
			t.Fatalf("overlapping or inverted range %d", i)
		}
		for id := uint32(row.first); id <= uint32(row.last); id++ {
			allowed, known := RegionPlayerCombat(uint16(id))
			if !known || allowed != row.allowed {
				t.Fatalf("lost reference row %04x", id)
			}
			count++
			if !allowed {
				protected++
			}
		}
	}
	if count != 3194 || protected != 103 {
		t.Fatalf("reference census=%d protected=%d", count, protected)
	}
}
