/*
===========================================================================

townarea_data_test.go - the shipped town list (#1985)

Pins what the rule changes on the real regioninfo.txt: Donwhang's RECT row
is a line and adds nothing, a Hotan wall in a battlefield region is town,
and the fortresses (#TOWN sections with no protected region) are not.

===========================================================================
*/
package townarea

import (
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestShippedTownWalls
================
*/
func TestShippedTownWalls(t *testing.T) {
	areas, err := Load(gamedatatest.TextdataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d town wall parts", areas.Len())
	for _, c := range []struct {
		name string
		at   worldgeom.RegionXZ
		want bool
	}{
		// "153 104 RECT 0 640 1920 640": a line, no ground.
		{"Donwhang north of its core", worldgeom.RegionXZ{RegionID: region(153, 104), X: 900, Z: 900}, false},
		// "134 91 RECT 960 580 1920 1920" in a battlefield region.
		{"Hotan's outer wall", worldgeom.RegionXZ{RegionID: region(134, 91), X: 1500, Z: 1000}, true},
		{"Hotan's outer wall, the open side", worldgeom.RegionXZ{RegionID: region(134, 91), X: 500, Z: 1000}, false},
		// The Jangan fortress, a #TOWN section of battlefield regions only.
		{"a fortress", worldgeom.RegionXZ{RegionID: region(70, 69), X: 900, Z: 900}, false},
	} {
		if got := areas.Contains(c.at); got != c.want {
			t.Errorf("%s: town = %v, want %v", c.name, got, c.want)
		}
	}
}
