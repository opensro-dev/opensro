/*
===========================================================================

optionalteleport_test.go - which imported rows the port serves

Synthetic textdata directories: no import is the native table (nil), the
outdoor map rows of named regions are served by their own ids, unnamed
regions are dropped, a dungeon group is served as its flag and its open
floors (the Ch'in Tomb's only with SRO_QINSHI_TOMB on, an incomplete group
not at all), Alexandria rows follow its overlay and SRO_ALEXANDRIA, a band
above the shard's level cap is lowered to it, and a malformed import is
refused.

===========================================================================
*/
package optionalteleport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/monster"
)

const (
	testJangan     = 25000
	testConstant   = 26959
	testAlexandria = 23088
	// The shipped table's cave flag regions and floors (refoptionalteleport
	// rows 29..38): Donwhang Stone Cave is 0x8001, the tomb floors 2..4
	// are 0x8006..0x8004, written signed as the table writes them.
	testDonwhangFlag = 27027
	testTombFlag     = 26284
	testDonwhangCave = -32767
	testTombFloor2   = -32762
	testTombFloor3   = -32763
)

/*
================
floorRow

A dungeon group's floor row: not drawn on the map, with the shipped
rows' level 70 band.
================
*/
func floorRow(id, group, region int64, name string) importRow {
	return importRow{Service: 1, ID: id, Zone: "SN_ZONE", Name: name, RegionID: region,
		X: -11719, Y: -47, Z: 5742, WorldID: 1, RegionGroup: group, MapPoint: 0, LevelMin: 70, LevelMax: 999}
}

/*
================
flagRow

A dungeon group's flag: a map row with no name of its own.
================
*/
func flagRow(id, group, region int64) importRow {
	row := outdoorRow(id, region, "")
	row.RegionGroup = group
	return row
}

/*
================
outdoorRow
================
*/
func outdoorRow(id, region int64, name string) importRow {
	return importRow{Service: 1, ID: id, Zone: "SN_ZONE", Name: name, RegionID: region,
		X: 100, Y: 5, Z: 200, WorldID: 1, RegionGroup: -1, MapPoint: 1}
}

/*
================
writeTextdata

A textdata directory holding the import (when rows is not nil), a
textzonename naming the given regions, and optionally an Alexandria
overlay.
================
*/
func writeTextdata(t *testing.T, rows []importRow, zones map[int64]string, overlay bool) string {
	t.Helper()
	dir := t.TempDir()
	if rows != nil {
		data, err := json.Marshal(importFile{Format: importFormat, Source: json.RawMessage(`{}`), Rows: rows})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, File), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var lines []string
	for region, name := range zones {
		cells := []string{"1", strconv.FormatInt(region, 10), "k", "", "", "", "", "", name}
		lines = append(lines, strings.Join(cells, "\t"))
	}
	if err := os.WriteFile(filepath.Join(dir, "textzonename.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if overlay {
		body := `{"format":"sro-server-alexandria-overlay","version":1,"regions":[23088],` +
			`"teleports":["GATE_SD1"],"buildings":["STORE_SD_GATE1"]}`
		if err := os.WriteFile(filepath.Join(dir, alexandria.OverlayFile), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

/*
================
ids
================
*/
func ids(points []Point) []uint32 {
	out := make([]uint32, 0, len(points))
	for _, point := range points {
		out = append(out, point.ID)
	}
	return out
}

/*
================
TestNoImportIsTheNativeTable
================
*/
func TestNoImportIsTheNativeTable(t *testing.T) {
	points, err := Load(writeTextdata(t, nil, map[int64]string{testJangan: "Jangan"}, false))
	if err != nil || points != nil {
		t.Fatalf("no import: %+v, %v; want nil", points, err)
	}
}

/*
================
TestServesOutdoorMapRowsByTheirOwnIDs
================
*/
func TestServesOutdoorMapRowsByTheirOwnIDs(t *testing.T) {
	retired := outdoorRow(5, testJangan, "Retired")
	retired.Service = 0
	unnamedRegion := outdoorRow(6, 26100, "Unnamed Ruins")
	unnamed := outdoorRow(9, testConstant, "")
	rows := []importRow{outdoorRow(11, testJangan, "Jangan"), unnamed, retired, unnamedRegion}
	dir := writeTextdata(t, rows, map[int64]string{testJangan: "Jangan", testConstant: "Constantinople"}, false)
	points, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(points); len(got) != 2 || got[0] != 9 || got[1] != 11 {
		t.Fatalf("served ids %v, want [9 11]", got)
	}
	// A row without its own English name takes the region's.
	if points[0].Name != "Constantinople" || points[1].RegionID != testJangan || points[1].X != 100 {
		t.Fatalf("points %+v", points)
	}
	if points[0].Group != 0 || points[0].Floor || !points[0].Destination() {
		t.Fatalf("a field point is not a plain destination: %+v", points[0])
	}
}

/*
================
TestServesDungeonGroupsAsTheirFlagAndFloors

The shipped table's two groups: Donwhang Stone Cave (group 1) is v1.150
content and always served; the Ch'in Tomb floors (group 2) follow
SRO_QINSHI_TOMB, and with them closed the tomb's flag goes too. A flag
is drawn but is no destination; a floor keeps its signed dungeon region
as the unsigned id and its own name.
================
*/
func TestServesDungeonGroupsAsTheirFlagAndFloors(t *testing.T) {
	donwhangFloor := floorRow(30, 1, testDonwhangCave, "Donwhang Stone Cave")
	donwhangFloor.LevelMin = 50
	rows := []importRow{
		outdoorRow(11, testJangan, "Jangan"),
		flagRow(29, 1, testDonwhangFlag), donwhangFloor,
		flagRow(31, 2, testTombFlag),
		floorRow(32, 2, testTombFloor2, "Tomb of Qin-Shi Emperor lv.2"),
		floorRow(33, 2, testTombFloor3, "Tomb of Qin-Shi Emperor lv.3"),
	}
	zones := map[int64]string{testJangan: "Jangan", testDonwhangFlag: "Donwhang", testTombFlag: "Jangan"}
	dir := writeTextdata(t, rows, zones, false)
	t.Setenv(monster.EnvQinShiTomb, "off")
	points, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(points); len(got) != 3 || got[0] != 11 || got[1] != 29 || got[2] != 30 {
		t.Fatalf("tomb closed: served ids %v, want [11 29 30]", got)
	}
	t.Setenv(monster.EnvQinShiTomb, "on")
	if points, err = Load(dir); err != nil {
		t.Fatal(err)
	}
	if got := ids(points); len(got) != 6 {
		t.Fatalf("tomb open: served ids %v, want all six rows", got)
	}
	flag, floor := points[3], points[4]
	if flag.ID != 31 || flag.Group != 2 || flag.Floor || flag.Destination() || flag.Name != "Jangan" {
		t.Fatalf("tomb flag %+v", flag)
	}
	if floor.ID != 32 || floor.Group != 2 || !floor.Floor || !floor.Destination() || floor.RegionID != 0x8006 ||
		floor.X != -11719 || floor.Name != "Tomb of Qin-Shi Emperor lv.2" || floor.Admits(69) || !floor.Admits(70) {
		t.Fatalf("tomb floor %+v", floor)
	}
}

/*
================
TestDropsAnIncompleteDungeonGroup

A floor without its flag cannot be chosen, and a flag without a floor
would open an empty menu: neither is served. A grouped row on a field
region that is not a map point is no floor either.
================
*/
func TestDropsAnIncompleteDungeonGroup(t *testing.T) {
	notADungeon := floorRow(40, 3, testJangan, "Field Floor")
	rows := []importRow{
		outdoorRow(11, testJangan, "Jangan"),
		floorRow(30, 1, testDonwhangCave, "Orphan Floor"),
		flagRow(31, 2, testTombFlag),
		flagRow(39, 3, testDonwhangFlag), notADungeon,
	}
	zones := map[int64]string{testJangan: "Jangan", testDonwhangFlag: "Donwhang", testTombFlag: "Jangan"}
	t.Setenv(monster.EnvQinShiTomb, "on")
	points, err := Load(writeTextdata(t, rows, zones, false))
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(points); len(got) != 1 || got[0] != 11 {
		t.Fatalf("served ids %v, want the field point only", got)
	}
}

/*
================
TestAlexandriaRowsFollowItsOverlay

A native root names no Alexandria region, so the row is dropped; a variant
root names it and serves it only while SRO_ALEXANDRIA is on.
================
*/
func TestAlexandriaRowsFollowItsOverlay(t *testing.T) {
	south := outdoorRow(52, testAlexandria, "Test Band")
	south.LevelMin, south.LevelMax = 95, 999
	rows := []importRow{outdoorRow(11, testJangan, "Jangan"), south}
	native := writeTextdata(t, rows, map[int64]string{testJangan: "Jangan"}, false)
	variant := writeTextdata(t, rows, map[int64]string{testJangan: "Jangan", testAlexandria: "Alexandria"}, true)
	for _, tc := range []struct {
		dir, flag string
		want      int
	}{{native, "on", 1}, {variant, "off", 1}, {variant, "on", 2}} {
		t.Setenv(alexandria.EnvAlexandria, tc.flag)
		points, err := Load(tc.dir)
		if err != nil || len(points) != tc.want {
			t.Fatalf("SRO_ALEXANDRIA=%s: %v, %v; want %d points", tc.flag, ids(points), err, tc.want)
		}
	}
}

/*
================
TestAdmitsTheLevelBand
================
*/
func TestAdmitsTheLevelBand(t *testing.T) {
	open, band := Point{}, Point{LevelMin: 50, LevelMax: 60}
	for level, want := range map[int64]bool{1: false, 49: false, 50: true, 60: true, 61: false} {
		if !open.Admits(level) || band.Admits(level) != want {
			t.Fatalf("level %d: open %v band %v, want true %v", level, open.Admits(level), band.Admits(level), want)
		}
	}
}

/*
================
TestCapLevelBandsAdmitsTheCap

Alexandria's 95/999 rows on a 90-cap shard admit level 90; the 110 cap
keeps the imported minimum, and an open row stays open.
================
*/
func TestCapLevelBandsAdmitsTheCap(t *testing.T) {
	for _, tc := range []struct {
		levelCap    int64
		admits      map[int64]bool
		wantMinimum int64
	}{
		{90, map[int64]bool{89: false, 90: true}, 90},
		{110, map[int64]bool{90: false, 94: false, 95: true, 110: true}, 95},
	} {
		points := []Point{{LevelMin: 95, LevelMax: 999}, {}}
		CapLevelBands(points, tc.levelCap)
		if points[0].LevelMin != tc.wantMinimum || points[1].LevelMin != 0 || points[0].LevelMax != 999 {
			t.Fatalf("cap %d: bands %+v", tc.levelCap, points)
		}
		for level, want := range tc.admits {
			if points[0].Admits(level) != want || !points[1].Admits(level) {
				t.Fatalf("cap %d level %d: admits %v, want %v", tc.levelCap, level, points[0].Admits(level), want)
			}
		}
	}
}

/*
================
TestRefusesAMalformedImport
================
*/
func TestRefusesAMalformedImport(t *testing.T) {
	zones := map[int64]string{testJangan: "Jangan"}
	repeated := writeTextdata(t, []importRow{outdoorRow(1, testJangan, "A"), outdoorRow(1, testJangan, "B")}, zones, false)
	if _, err := Load(repeated); err == nil {
		t.Fatal("a repeated id was accepted")
	}
	empty := writeTextdata(t, []importRow{}, zones, false)
	if _, err := Load(empty); err == nil {
		t.Fatal("an import with no rows was accepted")
	}
	unknown := writeTextdata(t, nil, zones, false)
	if err := os.WriteFile(filepath.Join(unknown, File), []byte(`{"format":"`+importFormat+`","rows":[{"extra":1}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(unknown); err == nil {
		t.Fatal("an unknown field was accepted")
	}
}
