/*
===========================================================================
skytemple_test.go - the optional sky temple population's admission rules.
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
skytempleSource

One nest row of the given codename and region, encoded as the importer
writes it; skytempleWorldSource names the row's source world.
================
*/
func skytempleSource(t *testing.T, codename string, region uint16) []byte {
	t.Helper()
	return skytempleWorldSource(t, codename, region, cap110FieldWorldID)
}

/*
================
skytempleWorldSource
================
*/
func skytempleWorldSource(t *testing.T, codename string, region uint16, world int) []byte {
	t.Helper()
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: codename, NestID: 41001, WorldID: world, RegionID: region,
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
TestSkytempleAdmitsItsWindow

The placed kinds join the default world with their authored values, at
either edge of the window and at any grade the client authored: the
population file only carries placed kinds, so admission is the prefix
and the level window alone.
================
*/
func TestSkytempleAdmitsItsWindow(t *testing.T) {
	for _, level := range []uint8{skytempleFirstLevel, 150, skytempleLastLevel} {
		for _, grade := range []uint8{0, 3, 8} {
			template := tombTemplate("MOB_SKYTEMPLE_A_KK_PLANAR", level, grade)
			got, err := appendSkytempleData(template, skytempleSource(t, "MOB_SKYTEMPLE_A_KK_PLANAR", 25567))
			if err != nil {
				t.Fatalf("level %d grade %d refused: %v", level, grade, err)
			}
			if len(template.Nests) != 1 || len(got.Nests) != 2 {
				t.Fatalf("level %d grade %d: population did not append immutably", level, grade)
			}
			nest := got.Nests[1]
			if nest.WorldCode != "INS_DEFAULT" || nest.RegionID != 25567 || nest.MaxCount != 1 || !nest.Respawn {
				t.Fatalf("level %d grade %d: authored nest values lost: %+v", level, grade, nest)
			}
		}
	}
}

/*
================
TestSkytempleRefusesOutsideItsWindow

Levels below the fields' first kind and above its last stay out, as do
other families' codenames and prefix lookalikes.
================
*/
func TestSkytempleRefusesOutsideItsWindow(t *testing.T) {
	cases := []struct {
		name     string
		codename string
		level    uint8
	}{
		{"too low", "MOB_SKYTEMPLE_A_KK_PLANAR", skytempleFirstLevel - 1},
		{"too high", "MOB_SKYTEMPLE_A_KK_PLANAR", skytempleLastLevel + 1},
		{"desert monster", "MOB_SD_BASTET", 150},
		{"native monster", "MOB_CH_TIGER", 150},
		{"prefix lookalike", "MOB_SKYTEMPLEX_PLANAR", 150},
	}
	for _, c := range cases {
		template := tombTemplate(c.codename, c.level, 0)
		if _, err := appendSkytempleData(template, skytempleSource(t, c.codename, 25567)); err == nil {
			t.Fatalf("%s admitted", c.name)
		}
	}
}

/*
================
TestSkytempleRefusesOtherWorldsAndUnknownCodenames

The fields stand in world 1; a row naming any other world is refused,
as is a codename the installed references do not carry.
================
*/
func TestSkytempleRefusesOtherWorldsAndUnknownCodenames(t *testing.T) {
	for _, world := range []int{0, 57, 132} {
		template := tombTemplate("MOB_SKYTEMPLE_A_KK_PLANAR", 150, 0)
		if _, err := appendSkytempleData(template, skytempleWorldSource(t, "MOB_SKYTEMPLE_A_KK_PLANAR", 25567, world)); err == nil {
			t.Fatalf("skytemple row in world %d admitted", world)
		}
	}
	template := tombTemplate("MOB_SKYTEMPLE_A_KK_PLANAR", 150, 0)
	if _, err := appendSkytempleData(template, skytempleSource(t, "MOB_SKYTEMPLE_A_NOT_INSTALLED", 25567)); err == nil {
		t.Fatal("row naming a species the references do not carry admitted")
	}
}

/*
================
TestSkytempleOffAndOn

Off never reads the optional file; on refuses missing input and needs
no other family's switch (its drops are the native posture). A valid
file appends.
================
*/
func TestSkytempleOffAndOn(t *testing.T) {
	template := tombTemplate("MOB_SKYTEMPLE_A_KK_PLANAR", 150, 0)
	if _, err := AppendSkytemple(template, t.TempDir(), false); err != nil {
		t.Fatalf("off read optional data: %v", err)
	}
	if _, err := AppendSkytemple(template, t.TempDir(), true); err == nil {
		t.Fatal("on admitted missing input")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, skytemplePopulationFile), skytempleSource(t, "MOB_SKYTEMPLE_A_KK_PLANAR", 25567), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := AppendSkytemple(template, dir, true)
	if err != nil || len(got.Nests) != 2 {
		t.Fatalf("installed population refused: %v", err)
	}
}

/*
================
TestSkytempleFlagWords
================
*/
func TestSkytempleFlagWords(t *testing.T) {
	for word, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvSkytemple, word)
		if got := SkytempleEnabled(); got != want {
			t.Fatalf("%q = %t, want %t", word, got, want)
		}
	}
}
