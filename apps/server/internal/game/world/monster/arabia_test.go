/*
===========================================================================
arabia_test.go - the optional Arabia population's admission rules.
===========================================================================
*/
package monster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/world/arabia"
)

/*
================
arabiaSource

One nest row of the given codename and region, encoded as the importer
writes it; arabiaWorldSource names the row's source world.
================
*/
func arabiaSource(t *testing.T, codename string, region uint16) []byte {
	t.Helper()
	return arabiaWorldSource(t, codename, region, cap110FieldWorldID)
}

/*
================
arabiaWorldSource
================
*/
func arabiaWorldSource(t *testing.T, codename string, region uint16, world int) []byte {
	t.Helper()
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: codename, NestID: 42001, WorldID: world, RegionID: region,
		X: 100, Y: 0, Z: 200, Radius: 0, GenerateRadius: 0, MaxCount: 1,
		Respawn: true, RespawnDelayMinSec: 60, RespawnDelayMaxSec: 120,
		Controls: TacticsControls{SightRange: 100}}}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

/*
================
TestArabiaAdmitsItsWindow

The placed kinds join with their authored values at either edge of
the window and at any grade the client authored: the population file
only carries placed kinds, so admission is the prefix and the level
window alone. A field row joins the default world.
================
*/
func TestArabiaAdmitsItsWindow(t *testing.T) {
	for _, level := range []uint8{arabiaFirstLevel, 125, arabiaLastLevel} {
		for _, grade := range []uint8{0, 3, 8} {
			template := tombTemplate("MOB_AR_KOBOLD", level, grade)
			got, err := appendArabiaData(template, arabiaSource(t, "MOB_AR_KOBOLD", 21850))
			if err != nil {
				t.Fatalf("level %d grade %d refused: %v", level, grade, err)
			}
			if len(template.Nests) != 1 || len(got.Nests) != 2 {
				t.Fatalf("level %d grade %d: population did not append immutably", level, grade)
			}
			nest := got.Nests[1]
			if nest.WorldCode != "INS_DEFAULT" || nest.RegionID != 21850 || nest.MaxCount != 1 || !nest.Respawn {
				t.Fatalf("level %d grade %d: authored nest values lost: %+v", level, grade, nest)
			}
		}
	}
}

/*
================
TestArabiaRefusesOutsideItsWindow

Levels below the continent's first kind and above its last stay out,
as do other families' codenames and prefix lookalikes.
================
*/
func TestArabiaRefusesOutsideItsWindow(t *testing.T) {
	cases := []struct {
		name     string
		codename string
		level    uint8
	}{
		{"too low", "MOB_AR_KOBOLD", arabiaFirstLevel - 1},
		{"too high", "MOB_AR_KOBOLD", arabiaLastLevel + 1},
		{"sky temple monster", "MOB_SKYTEMPLE_A_KK_PLANAR", 125},
		{"native monster", "MOB_CH_TIGER", 125},
		{"prefix lookalike", "MOB_ARX_KOBOLD", 125},
		{"missing underscore", "MOB_ARABIA", 125},
	}
	for _, c := range cases {
		template := tombTemplate(c.codename, c.level, 0)
		if _, err := appendArabiaData(template, arabiaSource(t, c.codename, 21850)); err == nil {
			t.Fatalf("%s admitted", c.name)
		}
	}
}

/*
================
TestArabiaWorlds

The dungeon's nests join GROUP_ARABIA_DUNGEON; a row naming any other
world (the boss lair's 113, Petra's 114, the garden's 115 - none of
them registered) is refused, as is a codename the installed references
do not carry.
================
*/
func TestArabiaWorlds(t *testing.T) {
	template := tombTemplate("MOB_ARABIA_KARKADANN", 125, 0)
	got, err := appendArabiaData(template, arabiaWorldSource(t, "MOB_ARABIA_KARKADANN", 23378, arabia.DungeonWorldID))
	if err != nil {
		t.Fatalf("dungeon nest refused: %v", err)
	}
	if nest := got.Nests[1]; nest.WorldCode != "GROUP_ARABIA_DUNGEON" || nest.RegionID != 23378 {
		t.Fatalf("dungeon nest joined %+v", nest)
	}
	for _, world := range []int{0, 57, 113, 114, 115} {
		template := tombTemplate("MOB_AR_KOBOLD", 125, 0)
		if _, err := appendArabiaData(template, arabiaWorldSource(t, "MOB_AR_KOBOLD", 21850, world)); err == nil {
			t.Fatalf("arabia row in world %d admitted", world)
		}
	}
	template = tombTemplate("MOB_AR_KOBOLD", 125, 0)
	if _, err := appendArabiaData(template, arabiaSource(t, "MOB_AR_NOT_INSTALLED", 21850)); err == nil {
		t.Fatal("row naming a species the references do not carry admitted")
	}
}

/*
================
TestArabiaHiveCapRows

A capped unique's points carry their shared hive key and cap through
the loader; a row naming a cap without its key (or the reverse) is
refused, so a partial cap can never reach the spawn engine.
================
*/
func TestArabiaHiveCapRows(t *testing.T) {
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{
		{Codename: "MOB_ARABIA_KARKADANN", NestID: 42011, WorldID: cap110FieldWorldID, RegionID: 22102,
			MaxCount: 1, Respawn: true, HiveKey: "MOB_ARABIA_KARKADANN", HiveMaxCount: 1},
		{Codename: "MOB_ARABIA_KARKADANN", NestID: 42012, WorldID: cap110FieldWorldID, RegionID: 22103,
			MaxCount: 1, Respawn: true, HiveKey: "MOB_ARABIA_KARKADANN", HiveMaxCount: 1},
	}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	template := tombTemplate("MOB_ARABIA_KARKADANN", 125, 0)
	got, err := appendArabiaData(template, data)
	if err != nil {
		t.Fatalf("capped points refused: %v", err)
	}
	for _, nest := range got.Nests[1:] {
		if nest.HiveKey != "MOB_ARABIA_KARKADANN" || nest.HiveMaxCount != 1 {
			t.Fatalf("capped point lost its hive: %+v", nest)
		}
	}
	broken := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{
		{Codename: "MOB_ARABIA_KARKADANN", NestID: 42013, WorldID: cap110FieldWorldID, RegionID: 22102,
			MaxCount: 1, Respawn: true, HiveMaxCount: 1},
	}}
	data, err = json.Marshal(broken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appendArabiaData(template, data); err == nil {
		t.Fatal("hive cap without its key admitted")
	}
}

/*
================
TestArabiaOffAndOn

Off never reads the optional file; on refuses missing input and needs
no other family's switch (its drops are the native posture). A valid
file appends.
================
*/
func TestArabiaOffAndOn(t *testing.T) {
	template := tombTemplate("MOB_AR_KOBOLD", 125, 0)
	if _, err := AppendArabia(template, t.TempDir(), false); err != nil {
		t.Fatalf("off read optional data: %v", err)
	}
	if _, err := AppendArabia(template, t.TempDir(), true); err == nil {
		t.Fatal("on admitted missing input")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, arabiaPopulationFile), arabiaSource(t, "MOB_AR_KOBOLD", 21850), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := AppendArabia(template, dir, true)
	if err != nil || len(got.Nests) != 2 {
		t.Fatalf("installed population refused: %v", err)
	}
}

/*
================
TestArabiaFlagWords
================
*/
func TestArabiaFlagWords(t *testing.T) {
	for word, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvArabia, word)
		if got := ArabiaEnabled(); got != want {
			t.Fatalf("%q = %t, want %t", word, got, want)
		}
	}
}
