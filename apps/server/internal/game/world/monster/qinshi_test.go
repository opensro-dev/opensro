/*
===========================================================================
qinshi_test.go - the optional qinshi population's admission rules.
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
qinshiSource

One nest row of the given codename and region, encoded as the importer
writes it; qinshiWorldSource names the row's source world.
================
*/
func qinshiSource(t *testing.T, codename string, region uint16) []byte {
	t.Helper()
	return qinshiWorldSource(t, codename, region, cap110FieldWorldID)
}

/*
================
qinshiWorldSource
================
*/
func qinshiWorldSource(t *testing.T, codename string, region uint16, world int) []byte {
	t.Helper()
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: codename, NestID: 31001, WorldID: world, RegionID: region,
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
TestQinshiAdmitsItsGradesAndPrefixes

Both prefixes of the family row (the tomb species and the quest species),
at each admitted grade, join the default world with their authored values.
================
*/
func TestQinshiAdmitsItsGradesAndPrefixes(t *testing.T) {
	for _, codename := range []string{"MOB_TQ_KERBEROS", "MOB_QT_TQ_SOLDIER"} {
		for _, grade := range []uint8{qinshiGradeOrdinary, qinshiGradeMid, qinshiGradeElite, qinshiGradeUnique} {
			template := tombTemplate(codename, 95, grade)
			got, err := appendQinshiData(template, qinshiSource(t, codename, 25207))
			if err != nil {
				t.Fatalf("%s grade %d refused: %v", codename, grade, err)
			}
			if len(template.Nests) != 1 || len(got.Nests) != 2 {
				t.Fatalf("%s grade %d: population did not append immutably", codename, grade)
			}
			nest := got.Nests[1]
			if nest.WorldCode != "INS_DEFAULT" || nest.RegionID != 25207 || nest.MaxCount != 10 || !nest.Respawn {
				t.Fatalf("%s grade %d: authored nest values lost: %+v", codename, grade, nest)
			}
		}
	}
}

/*
================
TestQinshiRefusesOtherGradesLevelsAndNames

The window is the importer's bound (81-110), not the field's 100-110:
the family's 91-99 content must pass, and everything outside the row
stays out.
================
*/
func TestQinshiRefusesOtherGradesLevelsAndNames(t *testing.T) {
	cases := []struct {
		name     string
		codename string
		level    uint8
		grade    uint8
	}{
		{"champion grade", "MOB_TQ_KERBEROS", 95, 1},
		{"giant grade", "MOB_TQ_KERBEROS", 95, 4},
		{"too low", "MOB_TQ_KERBEROS", 80, 0},
		{"too high", "MOB_TQ_KERBEROS", 111, 0},
		{"desert monster", "MOB_SD_BASTET", 95, 0},
		{"native monster", "MOB_CH_TIGER", 95, 0},
		{"prefix lookalike", "MOB_TQX_KERBEROS", 95, 0},
	}
	for _, c := range cases {
		template := tombTemplate(c.codename, c.level, c.grade)
		if _, err := appendQinshiData(template, qinshiSource(t, c.codename, 25207)); err == nil {
			t.Fatalf("%s admitted", c.name)
		}
	}
	for _, level := range []uint8{qinshiFirstLevel, cap110LastLevel} {
		template := tombTemplate("MOB_TQ_KERBEROS", level, 0)
		if _, err := appendQinshiData(template, qinshiSource(t, "MOB_TQ_KERBEROS", 25207)); err != nil {
			t.Fatalf("window edge level %d refused: %v", level, err)
		}
	}
}

/*
================
TestQinshiRefusesOtherWorldsAndUnknownCodenames

The family's nests stand in world 1; a row naming any other world is
refused, as is a codename the installed references do not carry (the
consumer-side join step 1 pinned).
================
*/
func TestQinshiRefusesOtherWorldsAndUnknownCodenames(t *testing.T) {
	for _, world := range []int{0, 57, 63} {
		template := tombTemplate("MOB_TQ_KERBEROS", 95, 0)
		if _, err := appendQinshiData(template, qinshiWorldSource(t, "MOB_TQ_KERBEROS", 25207, world)); err == nil {
			t.Fatalf("qinshi row in world %d admitted", world)
		}
	}
	template := tombTemplate("MOB_TQ_KERBEROS", 95, 0)
	if _, err := appendQinshiData(template, qinshiSource(t, "MOB_TQ_NOT_INSTALLED", 25207)); err == nil {
		t.Fatal("row naming a species the references do not carry admitted")
	}
}

/*
================
TestQinshiOffAndRequirements
================
*/
func TestQinshiOffAndRequirements(t *testing.T) {
	template := tombTemplate("MOB_TQ_KERBEROS", 95, 0)
	if _, err := AppendQinshi(template, t.TempDir(), false); err != nil {
		t.Fatalf("off read optional data: %v", err)
	}
	t.Setenv(EnvCap110Monsters, "off")
	if _, err := AppendQinshi(template, t.TempDir(), true); err == nil {
		t.Fatal("qinshi admitted without the cap-110 expansion")
	}
	t.Setenv(EnvCap110Monsters, "on")
	if _, err := AppendQinshi(template, t.TempDir(), true); err == nil {
		t.Fatal("on admitted missing input")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, qinshiPopulationFile), qinshiSource(t, "MOB_TQ_KERBEROS", 25207), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := AppendQinshi(template, dir, true)
	if err != nil || len(got.Nests) != 2 {
		t.Fatalf("installed population refused: %v", err)
	}
}

/*
================
TestQinshiFlagWords
================
*/
func TestQinshiFlagWords(t *testing.T) {
	for word, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvCap110Qinshi, word)
		if got := QinshiEnabled(); got != want {
			t.Fatalf("%q = %t, want %t", word, got, want)
		}
	}
}
