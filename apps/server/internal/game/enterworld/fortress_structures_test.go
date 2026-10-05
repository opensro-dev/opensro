package enterworld

import (
	"path/filepath"
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
	for _, zone := range []uint32{91, 109} {
		if _, ok := byZone[zone]; ok {
			t.Errorf("zone %d should start empty", zone)
		}
	}
}
