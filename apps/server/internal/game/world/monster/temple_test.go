/*
===========================================================================

temple_test.go - the temple band population's admission and world join.

The switch's boot/refusal legs (off never reads, on requires the
cap-110 expansion before any read) are opensro-web's temple_boot_test.go;
this file owns the data level: which rows the temple admits and which
band world each nest joins.
===========================================================================
*/
package monster

import (
	"encoding/json"
	"testing"
)

/*
================
templeSource

One temple nest row in the given band world, encoded as the import
writes it; spawnType is the database's second spawn kind when nonzero.
================
*/
func templeSource(t *testing.T, codename string, world, spawnType int) []byte {
	t.Helper()
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: codename, NestID: 31001, WorldID: world, RegionID: 21000,
		X: 100, Y: 0, Z: 200, Radius: 400, GenerateRadius: 300, MaxCount: 10,
		Respawn: true, RespawnDelayMinSec: 6, RespawnDelayMaxSec: 8, SpawnType: spawnType,
		Controls: TacticsControls{ChangeTarget: 2, SightRange: 130}}}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

/*
================
TestTempleAdmitsItsGradesAcrossTheWindow
================
*/
func TestTempleAdmitsItsGradesAcrossTheWindow(t *testing.T) {
	for _, grade := range []uint8{0, 1, 3, gradeElite, templeGradeUnique} {
		for _, level := range []uint8{templeFirstLevel, 70, templeLastLevel} {
			template := tombTemplate("MOB_GOD_SOLDIER", level, grade)
			got, err := appendTempleData(template, templeSource(t, "MOB_GOD_SOLDIER", 70, 0))
			if err != nil {
				t.Fatalf("grade %d level %d refused: %v", grade, level, err)
			}
			if len(template.Nests) != 1 || len(got.Nests) != 2 {
				t.Fatalf("grade %d level %d: population did not append immutably", grade, level)
			}
			nest := got.Nests[1]
			if nest.WorldCode != "INS_GOD_FLAME_71_80_LEVEL_2" || nest.RegionID != 21000 || nest.MaxCount != 10 || !nest.Respawn {
				t.Fatalf("grade %d level %d: authored nest values lost: %+v", grade, level, nest)
			}
		}
	}
}

/*
================
TestTempleRefusesOtherGradesLevelsAndNames
================
*/
func TestTempleRefusesOtherGradesLevelsAndNames(t *testing.T) {
	cases := []struct {
		name     string
		codename string
		level    uint8
		grade    uint8
	}{
		{"grade 2", "MOB_GOD_SOLDIER", 70, 2},
		{"grade 4", "MOB_GOD_SOLDIER", 70, 4},
		{"grade 7", "MOB_GOD_SOLDIER", 70, 7},
		{"below the window", "MOB_GOD_SOLDIER", templeFirstLevel - 1, 0},
		{"above the window", "MOB_GOD_SOLDIER", templeLastLevel + 1, 0},
		{"field species", "MOB_SD_FIELD", 105, 0},
		{"qinshi species", "MOB_TQ_SOLDIER", 90, 0},
	}
	for _, c := range cases {
		template := tombTemplate(c.codename, c.level, c.grade)
		if _, err := appendTempleData(template, templeSource(t, c.codename, 70, 0)); err == nil {
			t.Fatalf("%s admitted", c.name)
		}
	}
}

/*
================
TestTempleNestsJoinTheirOwnBands

Every band world 63-84 takes its nests; a row naming any other world
is refused - the temple population must not leak onto another world.
================
*/
func TestTempleNestsJoinTheirOwnBands(t *testing.T) {
	for world := 63; world <= 84; world++ {
		template := tombTemplate("MOB_GOD_SOLDIER", 70, 0)
		got, err := appendTempleData(template, templeSource(t, "MOB_GOD_SOLDIER", world, 0))
		if err != nil {
			t.Fatalf("world %d refused: %v", world, err)
		}
		want := got.Nests[1].WorldCode
		if want == "" || want == "INS_DEFAULT" {
			t.Fatalf("world %d nest joined %q", world, want)
		}
	}
	for _, world := range []int{0, 1, 62, 85, cap110FieldWorldID} {
		template := tombTemplate("MOB_GOD_SOLDIER", 70, 0)
		if _, err := appendTempleData(template, templeSource(t, "MOB_GOD_SOLDIER", world, 0)); err == nil {
			t.Fatalf("temple row in world %d admitted", world)
		}
	}
}

/*
================
TestTempleCarriesTheSecondSpawnKind

The import carries the database's second spawn kind on the row; the
consumer preserves it on the nest instead of refusing or dropping it.
================
*/
func TestTempleCarriesTheSecondSpawnKind(t *testing.T) {
	template := tombTemplate("MOB_GOD_CAPTAIN", 70, 0)
	got, err := appendTempleData(template, templeSource(t, "MOB_GOD_CAPTAIN", 70, 1))
	if err != nil {
		t.Fatalf("kind-1 nest refused: %v", err)
	}
	if got.Nests[1].SpawnType != 1 {
		t.Fatalf("spawn kind = %d, want 1 carried", got.Nests[1].SpawnType)
	}
	template = tombTemplate("MOB_GOD_CAPTAIN", 70, 0)
	got, err = appendTempleData(template, templeSource(t, "MOB_GOD_CAPTAIN", 70, 0))
	if err != nil {
		t.Fatalf("kind-0 nest refused: %v", err)
	}
	if got.Nests[1].SpawnType != 0 {
		t.Fatalf("spawn kind = %d, want 0", got.Nests[1].SpawnType)
	}
}
