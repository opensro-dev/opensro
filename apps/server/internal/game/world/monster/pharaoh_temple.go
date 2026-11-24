/*
===========================================================================

pharaoh_temple.go - the Pharaoh Temple's population (port-only, not native)

The job temple's seven dungeons (package pharaohtemple: the Temple and six
sanctums, regions 0x800A-0x8010) ride SRO_PHARAOH_TEMPLE. The variant build
stages their 26 kinds and nests (scripts/pharaoh_temple_import.py) into
pharaoh-temple.txt, the cap-110 Version-2 row schema; this file appends them
while the switch is on. The five sanctum keepers (Selkis, Neith, Anubis,
Isis, Seth) are among them.

The temple is a dungeon region of the persistent world, as the Donwhang cave
and the Qin-Shi floors are: every nest joins the default world. The source
groups each dungeon's nests under a world of its own (51-56, 60), which
pharaohtemple keeps as the dungeon's SourceWorld; a row whose world is not
its region's is refused, so a nest can never land in the wrong sanctum. One
nest is the exception: v1.188 groups the Temple's MOB_SD_OSIRIS_3 under the
field world (its hive), not under the Temple's world 60, and that authored
row alone joins the default world from the field world
(pharaohTempleOsirisNest).

The native v1.188 server places the same kinds with the same nest counts in
these regions (the importer's banner has the evidence).

===========================================================================
*/

package monster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/game/world/pharaohtemple"
)

const pharaohTemplePopulationFile = "pharaoh-temple.txt"

// The temple kinds' level window and grades in the v1.696 client the
// importer converts them from: ordinary priests and guardians, and the
// keepers at grade 3 (a sanity bound, as the other later worlds' are).
const (
	pharaohTempleFirstLevel  = 105
	pharaohTempleLastLevel   = 110
	pharaohTempleGradeKeeper = 3
)

// The one unique among the temple's kinds, at the source grade its tomb
// brothers carry (8), not the keepers' 3: admitted by name, as the field
// admits Apis, without opening the grade to the family.
const (
	pharaohTempleOsirisCodename = "MOB_SD_OSIRIS_3"
	pharaohTempleOsirisGrade    = 8
)

/*
================
PharaohTempleEnabled

The temple's switch, as its identity leaf reads it (a value the leaf
refuses reads as off here; the boot check refuses it by name).
================
*/
func PharaohTempleEnabled() bool {
	return pharaohtemple.Enabled()
}

/*
================
AppendPharaohTemple

Off never reads the file. On requires it: the variant build writes it with
the temple's dungeons, so a missing or malformed file is a broken build, not
an empty temple.
================
*/
func AppendPharaohTemple(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, pharaohTemplePopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("pharaoh temple population: %w", err)
	}
	return appendPharaohTempleData(template, data)
}

/*
================
appendPharaohTempleData
================
*/
func appendPharaohTempleData(template Template, data []byte) (Template, error) {
	return appendNestRows(template, data, nestRules{
		Label:   "pharaoh temple population",
		Admit:   admitPharaohTemple,
		WorldOf: pharaohTempleWorldCode,
		Region:  pharaohtemple.IsRegion,
	})
}

/*
================
admitPharaohTemple

The desert family's kinds (MOB_SD_) inside the temple's level window, at
the grades its nests use.
================
*/
func admitPharaohTemple(ref MonsterRef, codename string) bool {
	if !strings.HasPrefix(codename, "MOB_SD_") {
		return false
	}
	if ref.Level < pharaohTempleFirstLevel || ref.Level > pharaohTempleLastLevel {
		return false
	}
	return ref.MonsterType == 0 || ref.MonsterType == pharaohTempleGradeKeeper ||
		(codename == pharaohTempleOsirisCodename && ref.MonsterType == pharaohTempleOsirisGrade)
}

/*
================
pharaohTempleWorldCode

Every temple nest joins the default world; its source world must be the
one its region's dungeon is grouped under.
================
*/
func pharaohTempleWorldCode(row cap110Nest) (string, error) {
	for _, dungeon := range pharaohtemple.Dungeons() {
		if dungeon.Region != row.RegionID {
			continue
		}
		if dungeon.SourceWorld == row.WorldID || pharaohTempleOsirisNest(row, dungeon) {
			return "INS_DEFAULT", nil
		}
		return "", fmt.Errorf("pharaoh temple nest %d stands in %s (region %#x) but names world %d, not %d",
			row.NestID, dungeon.Name, row.RegionID, row.WorldID, dungeon.SourceWorld)
	}
	return "", fmt.Errorf("pharaoh temple nest %d is in region %#x, not a temple dungeon", row.NestID, row.RegionID)
}

/*
================
pharaohTempleOsirisNest

The one nest its dungeon's source world does not own: v1.188 groups the
Temple's Osiris under the field world (hive 2761, world 1), not under the
Temple's world 60. That authored row alone joins the default world from
the field world; every other row naming the field world in a temple
region is refused.
================
*/
func pharaohTempleOsirisNest(row cap110Nest, dungeon pharaohtemple.Dungeon) bool {
	return dungeon.ID == pharaohtemple.TempleDungeonID && row.Codename == pharaohTempleOsirisCodename &&
		row.WorldID == cap110FieldWorldID
}
