/*
===========================================================================

pharaoh_temple_test.go - the Pharaoh Temple population's admission

The temple's nests stand in its own seven dungeon regions and join the
default world; every other dungeon region, every outdoor region, a row whose
source world is not its region's dungeon, and a kind outside the temple's
window or grades are refused. The other populations keep refusing dungeon
regions.

===========================================================================
*/

package monster

import (
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/pharaohtemple"
)

// The Sanctum of Punishment (Anubis): dungeoninfo id 12, source world 53.
var anubisSanctum = world.DungeonRegionID(12)

const anubisSourceWorld = 53

// The Sanctum of Immortality (Haroeris): dungeoninfo id 14, source world 55.
var haroSanctum = world.DungeonRegionID(14)

const haroSourceWorld = 55

/*
================
TestPharaohTempleOffAndAbsent
================
*/
func TestPharaohTempleOffAndAbsent(t *testing.T) {
	template := tombTemplate("MOB_SD_ANUBIS", 107, pharaohTempleGradeKeeper)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, pharaohTemplePopulationFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := AppendPharaohTemple(template, dir, false)
	if err != nil || len(got.Nests) != len(template.Nests) {
		t.Fatalf("off read the file: %v", err)
	}
	if _, err := AppendPharaohTemple(template, t.TempDir(), true); err == nil {
		t.Fatal("on without the built population booted")
	}
	if _, err := AppendPharaohTemple(template, dir, true); err == nil {
		t.Fatal("on with a malformed population booted")
	}
}

/*
================
TestPharaohTempleKeeperJoinsItsSanctum
================
*/
func TestPharaohTempleKeeperJoinsItsSanctum(t *testing.T) {
	template := tombTemplate("MOB_SD_ANUBIS", 107, pharaohTempleGradeKeeper)
	got, err := appendPharaohTempleData(template, tombWorldSource(t, "MOB_SD_ANUBIS", anubisSanctum, anubisSourceWorld))
	if err != nil {
		t.Fatalf("the keeper's nest was refused: %v", err)
	}
	if len(template.Nests) != 1 || len(got.Nests) != 2 {
		t.Fatalf("population did not append immutably: %d -> %d nests", len(template.Nests), len(got.Nests))
	}
	nest := got.Nests[1]
	if nest.RegionID != anubisSanctum || nest.WorldCode != "INS_DEFAULT" || !nest.Respawn {
		t.Fatalf("the nest lost its sanctum or world: %+v", nest)
	}
	ordinary := tombTemplate("MOB_SD_PRIEST_GUARD", 108, 0)
	if _, err := appendPharaohTempleData(ordinary,
		tombWorldSource(t, "MOB_SD_PRIEST_GUARD", anubisSanctum, anubisSourceWorld)); err != nil {
		t.Fatalf("an ordinary priest was refused: %v", err)
	}
}

/*
================
TestPharaohTempleRefusesWhatIsNotTheTemples
================
*/
func TestPharaohTempleRefusesWhatIsNotTheTemples(t *testing.T) {
	keeper := tombTemplate("MOB_SD_ANUBIS", 107, pharaohTempleGradeKeeper)
	cases := []struct {
		name     string
		template Template
		source   []byte
	}{
		{"an outdoor region", keeper, tombWorldSource(t, "MOB_SD_ANUBIS", 25000, anubisSourceWorld)},
		{"another dungeon (a Qin-Shi floor)", keeper, tombWorldSource(t, "MOB_SD_ANUBIS", 0x8002, anubisSourceWorld)},
		{"the wrong sanctum's world", keeper, tombWorldSource(t, "MOB_SD_ANUBIS", anubisSanctum, 54)},
		{"the field world", keeper, tombWorldSource(t, "MOB_SD_ANUBIS", anubisSanctum, 1)},
		{"a unique grade", tombTemplate("MOB_SD_ANUBIS", 107, 8),
			tombWorldSource(t, "MOB_SD_ANUBIS", anubisSanctum, anubisSourceWorld)},
		{"a kind below the window", tombTemplate("MOB_SD_ANUBIS", 104, pharaohTempleGradeKeeper),
			tombWorldSource(t, "MOB_SD_ANUBIS", anubisSanctum, anubisSourceWorld)},
		{"another family", tombTemplate("MOB_GOD_ANUBIS", 107, pharaohTempleGradeKeeper),
			tombWorldSource(t, "MOB_GOD_ANUBIS", anubisSanctum, anubisSourceWorld)},
	}
	for _, c := range cases {
		if _, err := appendPharaohTempleData(c.template, c.source); err == nil {
			t.Errorf("%s was appended", c.name)
		}
	}
}

/*
================
TestPharaohTempleOsirisJoinsTheTempleFromTheFieldWorld

The one exception (#3786): v1.188 groups the Temple's Osiris under the
field world (hive 2761, world 1), not under the Temple's world 60. The
authored row joins the default world in the Temple region at its
source grade (8); the field world opens for no other kind, in no other
sanctum, and the named grade opens for no other kind.
================
*/
func TestPharaohTempleOsirisJoinsTheTempleFromTheFieldWorld(t *testing.T) {
	osiris := tombTemplate(pharaohTempleOsirisCodename, 110, pharaohTempleOsirisGrade)
	got, err := appendPharaohTempleData(osiris,
		tombWorldSource(t, pharaohTempleOsirisCodename, pharaohtemple.TempleRegionID, cap110FieldWorldID))
	if err != nil {
		t.Fatalf("the Osiris nest was refused: %v", err)
	}
	nest := got.Nests[len(got.Nests)-1]
	if nest.RegionID != pharaohtemple.TempleRegionID || nest.WorldCode != "INS_DEFAULT" {
		t.Fatalf("the Osiris nest lost its temple or world: %+v", nest)
	}
	refusals := []struct {
		name     string
		template Template
		source   []byte
	}{
		{"the field world in a sanctum", osiris,
			tombWorldSource(t, pharaohTempleOsirisCodename, anubisSanctum, cap110FieldWorldID)},
		{"the field world on a keeper kind", tombTemplate("MOB_SD_ANUBIS", 107, pharaohTempleGradeKeeper),
			tombWorldSource(t, "MOB_SD_ANUBIS", pharaohtemple.TempleRegionID, cap110FieldWorldID)},
		{"a sanctum's world on the Osiris row", osiris,
			tombWorldSource(t, pharaohTempleOsirisCodename, pharaohtemple.TempleRegionID, anubisSourceWorld)},
		{"an unnamed grade on the Osiris row", tombTemplate(pharaohTempleOsirisCodename, 110, 6),
			tombWorldSource(t, pharaohTempleOsirisCodename, pharaohtemple.TempleRegionID, cap110FieldWorldID)},
	}
	for _, c := range refusals {
		if _, err := appendPharaohTempleData(c.template, c.source); err == nil {
			t.Errorf("%s was appended", c.name)
		}
	}
}

/*
================
TestPharaohTempleHaroerisJoinsHisSanctum

The summoned unique (#3787): no source places him by nest, so the
importer stands him authored in his own sanctum (world 55). The loader
needs no exception for him: he is a keeper-grade kind (grade 3, level
109 in the pinned client) inside the temple window, and his row names
its sanctum's own source world, so the ordinary keeper rule and the
source-world mapping place him; the unique grade and the field world
stay refused for him as for every keeper.
================
*/
func TestPharaohTempleHaroerisJoinsHisSanctum(t *testing.T) {
	template := tombTemplate("MOB_SD_HAROERIS", 109, pharaohTempleGradeKeeper)
	got, err := appendPharaohTempleData(template,
		tombWorldSource(t, "MOB_SD_HAROERIS", haroSanctum, haroSourceWorld))
	if err != nil {
		t.Fatalf("the Haroeris nest was refused: %v", err)
	}
	nest := got.Nests[len(got.Nests)-1]
	if nest.RegionID != haroSanctum || nest.WorldCode != "INS_DEFAULT" || !nest.Respawn {
		t.Fatalf("the Haroeris nest lost its sanctum or world: %+v", nest)
	}
	refusals := []struct {
		name     string
		template Template
		source   []byte
	}{
		{"the field world", template,
			tombWorldSource(t, "MOB_SD_HAROERIS", haroSanctum, cap110FieldWorldID)},
		{"another sanctum's world", template,
			tombWorldSource(t, "MOB_SD_HAROERIS", haroSanctum, anubisSourceWorld)},
		{"a unique grade on the keeper-grade kind", tombTemplate("MOB_SD_HAROERIS", 109, 8),
			tombWorldSource(t, "MOB_SD_HAROERIS", haroSanctum, haroSourceWorld)},
	}
	for _, c := range refusals {
		if _, err := appendPharaohTempleData(c.template, c.source); err == nil {
			t.Errorf("%s was appended", c.name)
		}
	}
}

/*
================
TestFieldPopulationsStillRefuseDungeonRegions

The region rule is the temple's alone: a population that names none keeps
the outdoor-only rule.
================
*/
func TestFieldPopulationsStillRefuseDungeonRegions(t *testing.T) {
	template := tombTemplate("MOB_SD_ANUBIS", 107, pharaohTempleGradeKeeper)
	admitAll := func(MonsterRef, string) bool { return true }
	defaultWorld := func(cap110Nest) (string, error) { return "INS_DEFAULT", nil }
	source := tombWorldSource(t, "MOB_SD_ANUBIS", anubisSanctum, anubisSourceWorld)
	if _, err := appendNests(template, source, "field", admitAll, nil, defaultWorld); err == nil {
		t.Fatal("a population without a region rule appended a dungeon nest")
	}
}
