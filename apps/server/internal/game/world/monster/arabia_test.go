/*
===========================================================================

arabia_test.go - the Arabia populations' admission and world join.

The field nests join the default world; the Baghdad dungeon's join
their own world, and each file refuses the other's world.

===========================================================================
*/
package monster

import (
	"encoding/json"
	"testing"
)

/*
================
arabiaSource

One Arabia nest row encoded as the import writes it; spawnType is the
database's second spawn kind when nonzero.
================
*/
func arabiaSource(t *testing.T, codename string, world, spawnType int) []byte {
	t.Helper()
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: codename, NestID: 42001, WorldID: world, RegionID: 23000,
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
TestArabiaFieldAdmitsAndJoinsTheDefaultWorld
================
*/
func TestArabiaFieldAdmitsAndJoinsTheDefaultWorld(t *testing.T) {
	for _, grade := range []uint8{arabiaGradeOrdinary, 1, 3, gradeElite, arabiaGradeUnique} {
		for _, level := range []uint8{arabiaFirstLevel, 125, arabiaLastLevel} {
			template := tombTemplate("MOB_AR_SOLDIER", level, grade)
			got, err := appendArabiaData(template, arabiaSource(t, "MOB_AR_SOLDIER", 1, 0))
			if err != nil {
				t.Fatalf("grade %d level %d refused: %v", grade, level, err)
			}
			if len(template.Nests) != 1 || len(got.Nests) != 2 {
				t.Fatalf("grade %d level %d: population did not append immutably", grade, level)
			}
			nest := got.Nests[1]
			if nest.WorldCode != "INS_DEFAULT" || nest.RegionID != 23000 || nest.MaxCount != 10 || !nest.Respawn {
				t.Fatalf("grade %d level %d: authored nest values lost: %+v", grade, level, nest)
			}
		}
	}
}

/*
================
TestArabiaDungeonJoinsItsOwnWorld

The dungeon's rows carry the source's second spawn kind; it survives
the join, and the nest stands in the dungeon world, not the default.
================
*/
func TestArabiaDungeonJoinsItsOwnWorld(t *testing.T) {
	template := tombTemplate("MOB_ARABIA_SOLDIER", 130, arabiaGradeOrdinary)
	got, err := appendArabiaDungeonData(template, arabiaSource(t, "MOB_ARABIA_SOLDIER", 112, 1))
	if err != nil {
		t.Fatalf("dungeon nest refused: %v", err)
	}
	nest := got.Nests[1]
	if nest.WorldCode != "GROUP_ARABIA_DUNGEON" || nest.SpawnType != 1 {
		t.Fatalf("dungeon nest joined %+v", nest)
	}
}

/*
================
TestArabiaRefusesOtherGradesLevelsNamesAndWorlds
================
*/
func TestArabiaRefusesOtherGradesLevelsNamesAndWorlds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		codename string
		level    uint8
		grade    uint8
	}{
		{"grade 2", "MOB_AR_SOLDIER", 125, 2},
		{"grade 4", "MOB_AR_SOLDIER", 125, 4},
		{"below the window", "MOB_AR_SOLDIER", arabiaFirstLevel - 1, 0},
		{"above the window", "MOB_AR_SOLDIER", arabiaLastLevel + 1, 0},
		{"another family", "MOB_JUPITER_SOLDIER", 125, 0},
		{"an area NPC", "NPC_AR_SOLDIER", 125, 0},
	} {
		template := tombTemplate(tc.codename, tc.level, tc.grade)
		if _, err := appendArabiaData(template, arabiaSource(t, tc.codename, 1, 0)); err == nil {
			t.Fatalf("%s admitted", tc.name)
		}
	}
	// Each file refuses the other's world.
	field := tombTemplate("MOB_AR_SOLDIER", 125, arabiaGradeOrdinary)
	if _, err := appendArabiaData(field, arabiaSource(t, "MOB_AR_SOLDIER", 112, 0)); err == nil {
		t.Fatal("a dungeon nest joined the field file")
	}
	dungeon := tombTemplate("MOB_AR_SOLDIER", 125, arabiaGradeOrdinary)
	if _, err := appendArabiaDungeonData(dungeon, arabiaSource(t, "MOB_AR_SOLDIER", 1, 0)); err == nil {
		t.Fatal("a field nest joined the dungeon file")
	}
}

/*
================
TestArabiaEnabled

The switch reads only its on values; anything else is the native state.
================
*/
func TestArabiaEnabled(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "0": false, "nonsense": false, "1": true, "on": true, "TRUE": true} {
		t.Setenv(EnvArabia, value)
		if ArabiaEnabled() != want {
			t.Fatalf("SRO_ARABIA=%q: got %v, want %v", value, ArabiaEnabled(), want)
		}
	}
}
