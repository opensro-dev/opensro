/*
===========================================================================

regioncombat_test.go - protected town and battlefield lookup boundaries

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
TestRegionPlayerCombat

A synthetic table: a battlefield, a protected run, a battlefield, and
unknown regions on both sides and in the gap.
================
*/
func TestRegionPlayerCombat(t *testing.T) {
	table, err := NewRegionCombat([]RegionCombatRange{
		{First: 0x62a6, Last: 0x62a6, Allowed: true},
		{First: 0x62a7, Last: 0x62a9, Allowed: false},
		{First: 0x62aa, Last: 0x62aa, Allowed: true},
		{First: 0x7000, Last: 0x7001, Allowed: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		region         uint16
		allowed, known bool
	}{
		{0x62a6, true, true},
		{0x62a7, false, true},
		{0x62a8, false, true},
		{0x62a9, false, true},
		{0x62aa, true, true},
		{0x62ab, false, false},
		{0, false, false},
		{0xffff, false, false},
	} {
		allowed, known := table.PlayerCombat(tc.region)
		if allowed != tc.allowed || known != tc.known {
			t.Fatalf("region %04x: allowed=%v known=%v", tc.region, allowed, known)
		}
		if table.Town(tc.region) != (tc.known && !tc.allowed) || table.Battlefield(tc.region) != (tc.known && tc.allowed) {
			t.Fatalf("region %04x: town/battlefield disagree with the lookup", tc.region)
		}
	}
	var none *RegionCombat
	if _, known := none.PlayerCombat(0x62a6); known || none.Town(0x62a7) || none.Battlefield(0x62a6) {
		t.Fatal("a missing table knows a region")
	}
}

/*
================
TestRegionCombatRefusesBadRanges
================
*/
func TestRegionCombatRefusesBadRanges(t *testing.T) {
	for _, ranges := range [][]RegionCombatRange{
		{{First: 2, Last: 1}},
		{{First: 1, Last: 3}, {First: 3, Last: 4}},
		{{First: 5, Last: 6}, {First: 1, Last: 2}},
	} {
		if _, err := NewRegionCombat(ranges); err == nil {
			t.Fatalf("admitted %v", ranges)
		}
	}
}

/*
================
TestLoadRegionCombat

An absent table is none; a present one must name its format.
================
*/
func TestLoadRegionCombat(t *testing.T) {
	dir := t.TempDir()
	if table, err := LoadRegionCombat(dir); table != nil || err != nil {
		t.Fatalf("absent table: %v, %v", table, err)
	}
	write := func(text string) {
		path := filepath.Join(dir, filepath.FromSlash(RegionCombatFile))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"format":"sro-region-combat-v1","ranges":[[1,2,true],[3,3,false]]}`)
	table, err := LoadRegionCombat(dir)
	if err != nil || !table.Battlefield(2) || !table.Town(3) {
		t.Fatalf("loaded %v, %v", table, err)
	}
	for _, bad := range []string{
		`{"format":"other","ranges":[]}`,
		`{"format":"sro-region-combat-v1","ranges":[[1,"x",true]]}`,
		`{"format":"sro-region-combat-v1","ranges":[[2,1,true]]}`,
	} {
		write(bad)
		if _, err := LoadRegionCombat(dir); err == nil {
			t.Fatalf("admitted %s", bad)
		}
	}
}

/*
================
TestRegionCombatReferenceCensus

The operator's table (source-data, skips without it): every region of the
reference is reachable through the compact ranges.
================
*/
func TestRegionCombatReferenceCensus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(RegionCombatFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, licensed.SourceDataFile(t, "server/"+RegionCombatFile), 0o644); err != nil {
		t.Fatal(err)
	}
	table, err := LoadRegionCombat(dir)
	if err != nil || table == nil {
		t.Fatalf("load: %v, %v", table, err)
	}
	count, protected := 0, 0
	for _, row := range table.ranges {
		for id := uint32(row.First); id <= uint32(row.Last); id++ {
			allowed, known := table.PlayerCombat(uint16(id))
			if !known || allowed != row.Allowed {
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
