package enterworld

import (
	"path/filepath"
	"strconv"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestJanganFortressStartsWithItsStructures

The shipped event zones place Jangan's fort stone (zone 84), its three
guard towers and its three gates in INS_FORT_JA; the barricades (kind 2)
and the unplaced defensive sites hold nothing.
================
*/
func TestJanganFortressStartsWithItsStructures(t *testing.T) {
	licensed.RequireGameData(t)
	textdata := gamedatatest.TextdataDir(t)
	template, err := appendFortressStructures(monster.LoadTemplate(textdata), textdata,
		filepath.Join(gamedatatest.WorldAuthorityDir(t), "structure-zones.json"))
	if err != nil {
		t.Fatal(err)
	}
	byZone := map[uint32]monster.NestRow{}
	for _, nest := range template.Nests {
		if nest.EventStructID != 0 {
			byZone[nest.EventStructID] = nest
		}
	}
	stone, ok := byZone[84]
	if !ok || stone.WorldCode != "INS_FORT_JA" || template.Refs[stone.RefObjID].Codename != "STRUCTURE_FORT_STONE_JA_01" {
		t.Fatalf("fort stone nest %+v", stone)
	}
	if !template.Refs[stone.RefObjID].Structure || stone.MaxCount != 1 || stone.Respawn {
		t.Fatalf("fort stone is not a single, permanent structure: %+v", stone)
	}
	for _, zone := range []uint32{85, 86, 87, 88, 89, 90} {
		if _, ok := byZone[zone]; !ok {
			t.Errorf("zone %d holds no structure", zone)
		}
	}
	// An unbuilt defensive site has no nest; a barricade site's nest starts
	// vacant until construction places its barricade.
	if _, ok := byZone[91]; ok {
		t.Error("zone 91 should hold no nest")
	}
	if barricade, ok := byZone[109]; !ok || !barricade.StartVacant || template.Refs[barricade.RefObjID].TypeID4 != 6 {
		t.Errorf("zone 109 should be a vacant barricade site: %+v", barricade)
	}
}

/*
================
TestDefaultSiegeFortressRowsMatchShippedTextdata

The bootstrap's fortress rows are a hand projection of siegefortress.txt
(sub_7f22d0 case 0x36); every enabled row must agree with the shipped file,
including the column 10 tax target mask the tax management window ticks.
================
*/
func TestDefaultSiegeFortressRowsMatchShippedTextdata(t *testing.T) {
	licensed.RequireGameData(t)
	rows := ReadTextdataFile(filepath.Join(gamedatatest.TextdataDir(t), "siegefortress.txt"))
	defaults := DefaultSiegeFortressDataRows()
	enabled := 0
	for _, r := range rows {
		if len(r) < 14 || r[0] != "1" {
			continue
		}
		if enabled >= len(defaults) {
			t.Fatalf("shipped row %v has no bootstrap projection", r)
		}
		row := defaults[enabled]
		enabled++
		if strconv.FormatUint(uint64(row.FortressID), 10) != r[1] || row.CodeName != r[2] || row.NameStrID != r[4] ||
			strconv.FormatUint(uint64(row.TaxTargets), 10) != r[10] ||
			strconv.FormatUint(row.RequestFee, 10) != r[11] || row.OfficialNpcCode != r[13] {
			t.Fatalf("bootstrap row %+v differs from shipped %v", row, r)
		}
	}
	if enabled != len(defaults) {
		t.Fatalf("%d shipped rows, %d bootstrap rows", enabled, len(defaults))
	}
}
