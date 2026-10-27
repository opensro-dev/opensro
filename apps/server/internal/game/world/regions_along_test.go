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
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// regionsAlongGuard bounds one RegionsAlong call in the tests: a traversal
// that never ends must fail the test, not hang the suite.
const regionsAlongGuard = time.Second

/*
================
regionsAlongWithin

RegionsAlong run under regionsAlongGuard; ok is false when it did not return.
================
*/
func regionsAlongWithin(from, to RegionXZ) (regions []uint16, ok bool) {
	done := make(chan []uint16, 1)
	go func() { done <- RegionsAlong(from, to) }()
	select {
	case regions = <-done:
		return regions, true
	case <-time.After(regionsAlongGuard):
		return nil, false
	}
}

/*
================
TestRegionsAlongEndsOnASeamTie

The case that once looped forever: the chord ends exactly on an x seam
while the z axis also reaches a seam at the end (both at fraction 1). The
traversal must stop at the end's own region.
================
*/
func TestRegionsAlongEndsOnASeamTie(t *testing.T) {
	from := RegionXZ{RegionID: 22644, X: 1806.6, Z: 0}
	to := RegionXZ{RegionID: 23156, X: 0, Z: 0}
	got, ok := regionsAlongWithin(from, to)
	if !ok {
		t.Fatal("RegionsAlong did not return for a chord ending on a seam tie")
	}
	want := []uint16{22644, RegionIDForSectors(SectorX(22644), SectorY(22644)+1), 23156}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RegionsAlong = %04x, want %04x", got, want)
	}
}

/*
================
TestRegionsAlongProperties

Seeded chords, many of them starting or ending exactly on seams and corners:
every call returns, the list starts at from's region and ends at to's
canonical region, and each step moves to an adjacent, different sector.
================
*/
func TestRegionsAlongProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(1447))
	coordinate := func() float64 {
		switch rng.Intn(3) {
		case 0:
			return 0
		case 1:
			return float64(rng.Intn(4)) * OutdoorRegionSize / 2
		}
		return rng.Float64() * OutdoorRegionSize
	}
	for i := 0; i < 5000; i++ {
		from := RegionXZ{RegionID: RegionIDForSectors(100+rng.Intn(3), 90+rng.Intn(3)), X: coordinate(), Z: coordinate()}
		from = NormalizeOutdoor(from)
		to := NormalizeOutdoor(RegionXZ{RegionID: RegionIDForSectors(100+rng.Intn(3), 90+rng.Intn(3)), X: coordinate(), Z: coordinate()})
		got, ok := regionsAlongWithin(from, to)
		if !ok {
			t.Fatalf("case %d %+v -> %+v: RegionsAlong did not return", i, from, to)
		}
		if got[0] != from.RegionID || got[len(got)-1] != to.RegionID {
			t.Fatalf("case %d %+v -> %+v: regions %04x do not run from 0x%04x to 0x%04x", i, from, to, got, from.RegionID, to.RegionID)
		}
		for k := 1; k < len(got); k++ {
			if got[k] == got[k-1] || !SamePlaneAdjacent(got[k-1], got[k]) {
				t.Fatalf("case %d %+v -> %+v: step %04x -> %04x is not one sector", i, from, to, got[k-1], got[k])
			}
		}
	}
}

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
