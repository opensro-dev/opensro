/*
===========================================================================

arabia.go - optional Arabia continent population (port-only).

Port-only, not native. SRO_ARABIA is off by default; off never reads
either population file, the native game has no Arabia, so off is the
native state. The town and the three fields stand on the persistent
world, so their nests (scripts/arabia_import.py) join the default
world as the cap-110 field population does; the Baghdad dungeon's
nests join their own world (internal/game/world/arabia), registered
by the boot composition only while the switch is on. Entry - travel
to Baghdad and the dungeon's gate - is the admission slice's; this
file owns the population join. The kinds keep the levels the later
client authors and their drops are the native posture: the import
writes no reward table.

===========================================================================
*/
package monster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/game/world/arabia"
)

const EnvArabia = "SRO_ARABIA"
const arabiaFieldPopulationFile = "arabia-field.txt"
const arabiaDungeonPopulationFile = "arabia-dungeon.txt"

// Port-only, not native: the family's admitted level window, the
// client table bound the import already sanity-checked
// (arabia_import.py). The backup's nest kinds sit at 121-130 inside it.
const (
	arabiaFirstLevel = 120
	arabiaLastLevel  = 130
)

// The monster grades (MonsterType low nibble) the Arabia family uses:
// the import's grade set with the native elite named.
const (
	arabiaGradeOrdinary = 0
	arabiaGradeUnique   = 8
)

/*
================
ArabiaEnabled
================
*/
func ArabiaEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvArabia))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
AppendArabia

Off never reads the optional file. On fails before startup when the
field data is absent or malformed; a partial population must not
reach gameplay. The field nests join the default world, whose
catalogue needs no registration.
================
*/
func AppendArabia(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, arabiaFieldPopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("arabia field population: %w", err)
	}
	return appendArabiaData(template, data)
}

/*
================
AppendArabiaDungeon

Off never reads the optional file. On fails before startup when the
dungeon data is absent or malformed. Registering the dungeon world
with the world catalogue is the boot composition's job (enterworld),
beside this call.
================
*/
func AppendArabiaDungeon(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, arabiaDungeonPopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("arabia dungeon population: %w", err)
	}
	return appendArabiaDungeonData(template, data)
}

/*
================
appendArabiaData
================
*/
func appendArabiaData(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "arabia field population", admitArabia, nil, nil)
}

/*
================
appendArabiaDungeonData
================
*/
func appendArabiaDungeonData(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "arabia dungeon population", admitArabia, nil, arabiaWorldCode)
}

/*
================
admitArabia

Port-only, not native: the family's own species (MOB_AR_* and
MOB_ARABIA_*) inside its level window, at the grades its nests use.
================
*/
func admitArabia(ref MonsterRef, codename string) bool {
	if !strings.HasPrefix(codename, "MOB_AR_") && !strings.HasPrefix(codename, "MOB_ARABIA_") {
		return false
	}
	if ref.Level < arabiaFirstLevel || ref.Level > arabiaLastLevel {
		return false
	}
	switch ref.MonsterType {
	case arabiaGradeOrdinary, 1, 3, gradeElite, arabiaGradeUnique:
		return true
	}
	return false
}

/*
================
arabiaWorldCode

The world codename an Arabia dungeon nest joins, from the source
world its row carries. A row naming any other world is refused: the
dungeon population must not leak onto another world, and no other
population joins this one.
================
*/
func arabiaWorldCode(row cap110Nest) (string, error) {
	code, ok := arabia.CodeName(row.WorldID)
	if !ok {
		return "", fmt.Errorf("arabia dungeon population has nest %d in unknown world %d", row.NestID, row.WorldID)
	}
	return code, nil
}
