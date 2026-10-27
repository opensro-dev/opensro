/*
===========================================================================

regions_along_test.go - the region sequence a straight walk enters

RegionsAlong lists every sector a chord enters, so a per-region rule can
test each change the way the reference server tests each movement step
(#721). These cases pin the traversal order, a clipped corner, an exact
corner, the reverse direction and the plane cases.

===========================================================================
*/
package world

import (
	"reflect"
	"testing"
)

/*
================
TestRegionsAlong
================
*/
func TestRegionsAlong(t *testing.T) {
	r := func(x, y int) uint16 { return RegionIDForSectors(x, y) }
	cases := []struct {
		name     string
		from, to RegionXZ
		want     []uint16
	}{
		{"inside one region", RegionXZ{r(100, 90), 100, 100}, RegionXZ{r(100, 90), 1800, 1800}, []uint16{r(100, 90)}},
		{"one seam east", RegionXZ{r(100, 90), 1800, 500}, RegionXZ{r(101, 90), 100, 500}, []uint16{r(100, 90), r(101, 90)}},
		{"through a region and out", RegionXZ{r(100, 90), 1800, 500}, RegionXZ{r(102, 90), 100, 500}, []uint16{r(100, 90), r(101, 90), r(102, 90)}},
		{"west, reverse order", RegionXZ{r(102, 90), 100, 500}, RegionXZ{r(100, 90), 1800, 500}, []uint16{r(102, 90), r(101, 90), r(100, 90)}},
		// From (1900, 1000) to (2000, 2000) in r(100,90)'s frame: the chord
		// crosses x=1920 at z=1200, before the z=1920 seam, so it enters the
		// east sector and then the north-east one.
		{"clipped corner goes east first", RegionXZ{r(100, 90), 1900, 1000}, RegionXZ{r(101, 91), 80, 80}, []uint16{r(100, 90), r(101, 90), r(101, 91)}},
		{"clipped corner goes north first", RegionXZ{r(100, 90), 1000, 1900}, RegionXZ{r(101, 91), 80, 80}, []uint16{r(100, 90), r(100, 91), r(101, 91)}},
		{"exact corner steps both axes", RegionXZ{r(100, 90), 1820, 1820}, RegionXZ{r(101, 91), 100, 100}, []uint16{r(100, 90), r(101, 91)}},
		{"dungeon plane is two ends", RegionXZ{0x8001, 0, 0}, RegionXZ{0x8002, 5000, 0}, []uint16{0x8001, 0x8002}},
		{"dungeon plane, one region", RegionXZ{0x8001, 0, 0}, RegionXZ{0x8001, 5000, 0}, []uint16{0x8001}},
		{"between planes is two ends", RegionXZ{r(100, 90), 0, 0}, RegionXZ{0x8001, 0, 0}, []uint16{r(100, 90), 0x8001}},
	}
	for _, c := range cases {
		if got := RegionsAlong(c.from, c.to); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: RegionsAlong = %04x, want %04x", c.name, got, c.want)
		}
	}
}
