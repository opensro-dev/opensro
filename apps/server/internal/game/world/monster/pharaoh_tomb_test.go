/*
===========================================================================
pharaoh_tomb_test.go - the optional tomb population's admission rules.
===========================================================================
*/
package monster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/world/instance"
)

/*
================
tombSource

One nest row of the given grade, level and region, encoded as the importer
writes it; tombWorldSource names the row's source world.
================
*/
func tombSource(t *testing.T, codename string, region uint16) []byte {
	t.Helper()
	return tombWorldSource(t, codename, region, int(instance.PharaohTomb1ID))
}

/*
================
tombWorldSource
================
*/
func tombWorldSource(t *testing.T, codename string, region uint16, world int) []byte {
	t.Helper()
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: codename, NestID: 29439, WorldID: world, RegionID: region,
		X: 100, Y: 0, Z: 200, Radius: 400, GenerateRadius: 300, MaxCount: 10,
		Respawn: true, RespawnDelayMinSec: 6, RespawnDelayMaxSec: 8,
		Controls: TacticsControls{ChangeTarget: 2, SightRange: 130}}}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

/*
================
tombTemplate
================
*/
func tombTemplate(codename string, level uint8, grade uint8) Template {
	ref := MonsterRef{RefObjID: 4321, Codename: codename, Level: level, MonsterType: grade}
	return TemplateFromParts(map[uint32]MonsterRef{ref.RefObjID: ref}, []NestRow{{MaxCount: 1}})
}

/*
================
TestPharaohTombAdmitsOrdinaryEliteAndUniqueGrades
================
*/
func TestPharaohTombAdmitsOrdinaryEliteAndUniqueGrades(t *testing.T) {
	for _, grade := range []uint8{pharaohGradeOrdinary, pharaohGradeElite, pharaohGradeUnique} {
		template := tombTemplate("MOB_SD_BASTET", 103, grade)
		got, err := appendPharaohTombData(template, tombSource(t, "MOB_SD_BASTET", 29631), 1)
		if err != nil {
			t.Fatalf("grade %d refused: %v", grade, err)
		}
		if len(template.Nests) != 1 || len(got.Nests) != 2 {
			t.Fatalf("grade %d: population did not append immutably", grade)
		}
		nest := got.Nests[1]
		if nest.WorldCode != "INS_PHARAOH_TOMB_1" || nest.RegionID != 29631 || nest.MaxCount != 10 || !nest.Respawn {
			t.Fatalf("grade %d: authored nest values lost: %+v", grade, nest)
		}
	}
}

/*
================
TestPharaohTombRefusesOtherGradesLevelsAndNames

The field population admits grade 0 only; the tomb widens that to the three
grades it uses and nothing else.
================
*/
func TestPharaohTombRefusesOtherGradesLevelsAndNames(t *testing.T) {
	cases := []struct {
		name     string
		codename string
		level    uint8
		grade    uint8
	}{
		{"champion grade", "MOB_SD_BASTET", 103, 1},
		{"giant grade", "MOB_SD_BASTET", 103, 4},
		{"too low", "MOB_SD_BASTET", 99, 0},
		{"too high", "MOB_SD_BASTET", 111, 0},
		{"not a desert monster", "MOB_CH_TIGER", 103, 0},
	}
	for _, c := range cases {
		template := tombTemplate(c.codename, c.level, c.grade)
		if _, err := appendPharaohTombData(template, tombSource(t, c.codename, 29631), 1); err == nil {
			t.Fatalf("%s admitted", c.name)
		}
	}
}

/*
================
TestPharaohTombRefusesDungeonRegions

The tombs are ordinary outdoor regions; a dungeon region (sign bit) belongs
to a different loader.
================
*/
func TestPharaohTombRefusesDungeonRegions(t *testing.T) {
	template := tombTemplate("MOB_SD_BASTET", 103, 0)
	if _, err := appendPharaohTombData(template, tombSource(t, "MOB_SD_BASTET", 0x8001), 1); err == nil {
		t.Fatal("dungeon region admitted")
	}
}

/*
================
TestPharaohTombNestsJoinTheirOwnWorlds

Each row carries its source world, and the nest joins that tomb's world
(#1750): the three tombs are separate permanent worlds, not one shared
flattening. A row naming the field world, or any other, is refused.
================
*/
func TestPharaohTombNestsJoinTheirOwnWorlds(t *testing.T) {
	for _, definition := range instance.PharaohTombDefinitions() {
		template := tombTemplate("MOB_SD_BASTET", 103, 0)
		got, err := appendPharaohTombData(template, tombWorldSource(t, "MOB_SD_BASTET", 29631, int(definition.ID)), 1)
		if err != nil {
			t.Fatalf("world %d refused: %v", definition.ID, err)
		}
		if got.Nests[1].WorldCode != definition.CodeName {
			t.Fatalf("world %d nest joined %q, want %q", definition.ID, got.Nests[1].WorldCode, definition.CodeName)
		}
	}
	for _, world := range []int{cap110FieldWorldID, 0, 99} {
		template := tombTemplate("MOB_SD_BASTET", 103, 0)
		if _, err := appendPharaohTombData(template, tombWorldSource(t, "MOB_SD_BASTET", 29631, world), 1); err == nil {
			t.Fatalf("tomb row in world %d admitted", world)
		}
	}
}

/*
================
TestFieldPopulationStillRefusesTombGrades

Sharing the nest loader must not widen the field population.
================
*/
func TestFieldPopulationStillRefusesTombGrades(t *testing.T) {
	template := tombTemplate("MOB_SD_BASTET", 103, pharaohGradeElite)
	if _, err := appendCap110Data(template, tombSource(t, "MOB_SD_BASTET", 29631)); err == nil {
		t.Fatal("field population admitted an elite monster")
	}
}

/*
================
TestPharaohTombOffAndRequirements
================
*/
func TestPharaohTombOffAndRequirements(t *testing.T) {
	template := tombTemplate("MOB_SD_BASTET", 103, 0)
	if _, err := AppendPharaohTomb(template, t.TempDir(), false); err != nil {
		t.Fatalf("off read optional data: %v", err)
	}
	t.Setenv(EnvCap110Monsters, "off")
	if _, err := AppendPharaohTomb(template, t.TempDir(), true); err == nil {
		t.Fatal("tomb admitted without the cap-110 expansion")
	}
	t.Setenv(EnvCap110Monsters, "on")
	if _, err := AppendPharaohTomb(template, t.TempDir(), true); err == nil {
		t.Fatal("on admitted missing input")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, pharaohTombPopulationFile), tombSource(t, "MOB_SD_BASTET", 29631), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := AppendPharaohTomb(template, dir, true)
	if err != nil || len(got.Nests) != 2 {
		t.Fatalf("installed population refused: %v", err)
	}
}

/*
================
TestPharaohTombFlagWords
================
*/
func TestPharaohTombFlagWords(t *testing.T) {
	for word, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvPharaohTomb, word)
		if got := PharaohTombEnabled(); got != want {
			t.Fatalf("%q = %t, want %t", word, got, want)
		}
	}
}

/*
================
TestPharaohTombStampsTheGearRateOnItsMonstersOnly
================
*/
func TestPharaohTombStampsTheGearRateOnItsMonstersOnly(t *testing.T) {
	boss := MonsterRef{RefObjID: 4321, Codename: "MOB_SD_SPHINX", Level: 101, MonsterType: pharaohGradeUnique}
	field := MonsterRef{RefObjID: 4322, Codename: "MOB_SD_FIELD", Level: 101}
	template := TemplateFromParts(map[uint32]MonsterRef{boss.RefObjID: boss, field.RefObjID: field}, []NestRow{{MaxCount: 1}})
	got, err := appendPharaohTombData(template, tombSource(t, "MOB_SD_SPHINX", 29631), 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.Refs[boss.RefObjID].PharaohGearRate != 5 || got.Refs[field.RefObjID].PharaohGearRate != 0 {
		t.Fatal("only the tomb's monster carries the rate")
	}
	if template.Refs[boss.RefObjID].PharaohGearRate != 0 {
		t.Fatal("the template it was given was modified")
	}
	if len(got.Nests) != 2 || got.Nests[1].RegionID != 29631 {
		t.Fatalf("nests lost by stamping: %+v", got.Nests)
	}
}

/*
================
TestAppendPharaohTombReadsTheGearRate
================
*/
func TestAppendPharaohTombReadsTheGearRate(t *testing.T) {
	t.Setenv(EnvCap110Monsters, "on")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, pharaohTombPopulationFile), tombSource(t, "MOB_SD_BASTET", 29631), 0o600); err != nil {
		t.Fatal(err)
	}
	template := tombTemplate("MOB_SD_BASTET", 103, pharaohGradeElite)
	t.Setenv(EnvPharaohGearRate, "7")
	got, err := AppendPharaohTomb(template, dir, true)
	if err != nil || got.Refs[4321].PharaohGearRate != 7 {
		t.Fatalf("rate not stamped: %v %+v", err, got.Refs[4321])
	}
	t.Setenv(EnvPharaohGearRate, "banana")
	if _, err := AppendPharaohTomb(template, dir, true); err == nil {
		t.Fatal("a mistyped rate started the server")
	}
}

/*
================
TestPharaohGearRateWords
================
*/
func TestPharaohGearRateWords(t *testing.T) {
	for word, want := range map[string]int{"": 1, "1": 1, " 5 ": 5, "20": 20} {
		t.Setenv(EnvPharaohGearRate, word)
		got, err := PharaohGearRateFromEnv()
		if err != nil || got != want {
			t.Fatalf("%q = %d, %v; want %d", word, got, err, want)
		}
	}
	for _, word := range []string{"0", "-2", "21", "off", "on", "2.5", "x"} {
		t.Setenv(EnvPharaohGearRate, word)
		if got, err := PharaohGearRateFromEnv(); err == nil || got != 1 {
			t.Fatalf("%q admitted as %d", word, got)
		}
	}
}
