/*
===========================================================================

regioncaravan_test.go - region continents as caravan bandit zones

===========================================================================
*/
package world

import "testing"

/*
================
TestRegionCaravanZonesFollowTheContinent

23196 is CHINA, 24900 Eu, 30898 Pharaoh (a continent the native maps to
zone 2, where no bandit is filed). 0x0001 is not a reference region.
================
*/
func TestRegionCaravanZonesFollowTheContinent(t *testing.T) {
	for region, want := range map[uint16]uint8{23196: 0, 24900: 1, 30898: 2} {
		if zone, known := RegionCaravanZone(region); !known || zone != want {
			t.Fatalf("region %d zone %d/%v want %d", region, zone, known, want)
		}
	}
	if _, known := RegionCaravanZone(0x0001); known {
		t.Fatal("unknown region has a zone")
	}
}
