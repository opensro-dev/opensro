/*
===========================================================================

hunting2_test.go - the second hunting grounds population's admission.

The switch's boot legs (off never reads, on refuses missing or
malformed data) ride TestHunting2OffAndOn; this file owns the data
level: which rows the population admits and the world each nest
joins - the default world, whose regions these are.

===========================================================================
*/
package monster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

/*
================
hunting2Source

One hunting ground nest row on the default world, encoded as the
import writes it.
================
*/
func hunting2Source(t *testing.T, codename string, world int) []byte {
	t.Helper()
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: codename, NestID: 42001, WorldID: world, RegionID: 25000,
		X: 100, Y: 0, Z: 200, Radius: 0, GenerateRadius: 0, MaxCount: 1,
		Respawn: true, RespawnDelayMinSec: 1, RespawnDelayMaxSec: 3,
		Controls: TacticsControls{SightRange: 100}}}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

/*
================
TestHunting2AdmitsPlacedKindsAcrossTheWindow
================
*/
func TestHunting2AdmitsPlacedKindsAcrossTheWindow(t *testing.T) {
	for _, grade := range []uint8{0, 1} {
		for _, level := range []uint8{hunting2FirstLevel, 100, hunting2LastLevel} {
			template := tombTemplate("MOB_TQ_SOLDIER", level, grade)
			got, err := AppendHunting2(template, writeHunting2(t, "MOB_TQ_SOLDIER"), true)
			if err != nil {
				t.Fatalf("grade %d level %d refused: %v", grade, level, err)
			}
			if len(template.Nests) != 1 || len(got.Nests) != 2 {
				t.Fatalf("grade %d level %d: population did not append immutably", grade, level)
			}
			nest := got.Nests[1]
			if nest.WorldCode != "INS_DEFAULT" || nest.RegionID != 25000 || nest.MaxCount != 1 || !nest.Respawn {
				t.Fatalf("grade %d level %d: authored nest values lost: %+v", grade, level, nest)
			}
		}
	}
	// The set re-stages species other families own; a placed kind
	// joins on its own authored level whatever its prefix.
	template := tombTemplate("MOB_SD_RESTAGED", hunting2LastLevel, 1)
	got, err := AppendHunting2(template, writeHunting2(t, "MOB_SD_RESTAGED"), true)
	if err != nil || len(got.Nests) != 2 {
		t.Fatalf("a re-staged species refused: %v", err)
	}
}

/*
================
writeHunting2

Install the one-row population in a scratch textdata folder.
================
*/
func writeHunting2(t *testing.T, codename string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, hunting2PopulationFile), hunting2Source(t, codename, cap110FieldWorldID), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

/*
================
TestHunting2RefusesOtherGradesLevelsNamesAndWorlds
================
*/
func TestHunting2RefusesOtherGradesLevelsNamesAndWorlds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		codename string
		level    uint8
		grade    uint8
	}{
		{"grade 2", "MOB_TQ_SOLDIER", 100, 2},
		{"grade 8", "MOB_TQ_SOLDIER", 100, 8},
		{"below the window", "MOB_TQ_SOLDIER", hunting2FirstLevel - 1, 0},
		{"above the window", "MOB_TQ_SOLDIER", hunting2LastLevel + 1, 0},
		{"an area NPC", "NPC_TQ_SOLDIER", 100, 0},
	} {
		template := tombTemplate(tc.codename, tc.level, tc.grade)
		if _, err := AppendHunting2(template, writeHunting2(t, tc.codename), true); err == nil {
			t.Fatalf("%s admitted", tc.name)
		}
	}
	// A row naming any world but the default one is refused: these
	// regions belong to it alone.
	template := tombTemplate("MOB_TQ_SOLDIER", 100, 0)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, hunting2PopulationFile), hunting2Source(t, "MOB_TQ_SOLDIER", 90), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendHunting2(template, dir, true); err == nil {
		t.Fatal("a nest in a foreign world admitted")
	}
}

/*
================
TestHunting2OffAndOn

Off returns the template untouched even with malformed data on
disk; on fails before startup when the file is absent or malformed.
================
*/
func TestHunting2OffAndOn(t *testing.T) {
	template := tombTemplate("MOB_TQ_SOLDIER", 100, 0)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, hunting2PopulationFile), []byte("not a population"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := AppendHunting2(template, dir, false)
	if err != nil || len(got.Nests) != len(template.Nests) {
		t.Fatalf("off read the population: %v (%d nests)", err, len(got.Nests))
	}
	if _, err := AppendHunting2(template, dir, true); err == nil {
		t.Fatal("malformed population admitted")
	}
	if _, err := AppendHunting2(template, t.TempDir(), true); err == nil {
		t.Fatal("absent population admitted")
	}
}
