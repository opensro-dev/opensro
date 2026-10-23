/*
===========================================================================

regioncaravan_test.go - caravan zones by region

===========================================================================
*/
package world

import (
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestRegionCaravanZone

A synthetic table: a zone-0 run, a zone-1 region, a zone-2 region, and
unknown regions around them.
================
*/
func TestRegionCaravanZone(t *testing.T) {
	table, err := NewRegionCaravan([]RegionCaravanRange{
		{First: 10, Last: 12, Zone: 0},
		{First: 13, Last: 13, Zone: 1},
		{First: 20, Last: 20, Zone: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	for region, want := range map[uint16]uint8{10: 0, 12: 0, 13: 1, 20: 2} {
		if zone, known := table.Zone(region); !known || zone != want {
			t.Fatalf("region %d zone %d/%v want %d", region, zone, known, want)
		}
	}
	for _, region := range []uint16{0, 9, 14, 21} {
		if _, known := table.Zone(region); known {
			t.Fatalf("region %d has a zone", region)
		}
	}
	var none *RegionCaravan
	if _, known := none.Zone(10); known {
		t.Fatal("a missing table knows a region")
	}
	if _, err := NewRegionCaravan([]RegionCaravanRange{{First: 5, Last: 6}, {First: 6, Last: 7}}); err == nil {
		t.Fatal("overlapping ranges admitted")
	}
}

/*
================
TestRegionCaravanReferenceCensus

The operator's table (source-data, skips without it): 23196 is CHINA, 24900
Europe, 30898 Pharaoh (a continent the native maps to zone 2); 0x0001 is
not a reference region; every region of the reference has a zone.
================
*/
func TestRegionCaravanReferenceCensus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(RegionCaravanFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, licensed.SourceDataFile(t, "server/"+RegionCaravanFile), 0o644); err != nil {
		t.Fatal(err)
	}
	table, err := LoadRegionCaravan(dir)
	if err != nil || table == nil {
		t.Fatalf("load: %v, %v", table, err)
	}
	for region, want := range map[uint16]uint8{23196: 0, 24900: 1, 30898: 2} {
		if zone, known := table.Zone(region); !known || zone != want {
			t.Fatalf("region %d zone %d/%v want %d", region, zone, known, want)
		}
	}
	if _, known := table.Zone(0x0001); known {
		t.Fatal("unknown region has a zone")
	}
	count := 0
	for _, span := range table.table.spans {
		count += int(span.last) - int(span.first) + 1
	}
	if count != 3194 {
		t.Fatalf("reference census %d", count)
	}
}
