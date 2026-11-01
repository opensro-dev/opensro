/*
===========================================================================

temple.go - optional temple band population (the 22 instanced worlds).

Port-only, not native. SRO_CAP110_TEMPLE is off by default and requires
SRO_CAP110_MONSTERS: the import writes cap110-temple.txt only on that
pairing (scripts/cap110_monsters.py), and AppendTemple refuses the same
pairing at load. Off never reads the temple file; the native game has
no temple, so off is the native state.

Unlike the Pharaoh Tombs' permanent worlds, each of the database's 22
temple worlds (63-84, internal/game/world/temple) is an instanced band:
the boot composition registers them as layered worlds
(instance.TempleDefinitions) only while this switch is on, and the
population joins its own band world instead of the default one. Entry,
the per-band level refusal and the party layering are the admission
slice's (#1800 step 3 slice 4); this file owns the population join.

The temple's nests use the database's second spawn kind beside the
ordinary one (512 of its 1616 claimed nests): the import carries it on
the row as SpawnType and appendNests preserves it on the nest. What the
kind changes at spawn time is pinned by the admission slice; until
then every temple nest stands as an ordinary nest, and the kind rides
along as a preserved fact rather than being dropped or refused.

===========================================================================
*/
package monster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/game/world/temple"
)

const EnvCap110Temple = "SRO_CAP110_TEMPLE"
const templePopulationFile = "cap110-temple.txt"

// Port-only, not native: the temple family's level window, the client
// table bound the import already sanity-checked (cap110_monsters.py).
const (
	templeFirstLevel = 35
	templeLastLevel  = 109
)

// The monster grades (MonsterType low nibble) the temple family uses:
// the import's grade set (0, 1, 3, 6, 8) with the native elite named.
const (
	templeGradeOrdinary = 0
	templeGradeUnique   = 8
)

/*
================
TempleEnabled
================
*/
func TempleEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvCap110Temple))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
AppendTemple

Off never reads the optional file. On fails before startup when the
cap-110 expansion is off - the requires refusal comes first, before
any temple data is read - or when the temple data is absent or
malformed. Registering the band worlds with the world catalogue is
the boot composition's job (enterworld), beside this call.
================
*/
func AppendTemple(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	if !Cap110Enabled() {
		return Template{}, fmt.Errorf("temple: %s requires %s (its population and drops)", EnvCap110Temple, EnvCap110Monsters)
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, templePopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("temple population: %w", err)
	}
	return appendTempleData(template, data)
}

/*
================
appendTempleData
================
*/
func appendTempleData(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "temple population", admitTemple, nil, templeWorldCode)
}

/*
================
admitTemple

Port-only, not native: the family's own species (MOB_GOD_) inside its
level window, at the grades its nests use.
================
*/
func admitTemple(ref MonsterRef, codename string) bool {
	if !strings.HasPrefix(codename, "MOB_GOD_") || ref.Level < templeFirstLevel || ref.Level > templeLastLevel {
		return false
	}
	switch ref.MonsterType {
	case templeGradeOrdinary, 1, 3, gradeElite, templeGradeUnique:
		return true
	}
	return false
}

/*
================
templeWorldCode

The world codename a temple nest joins, from the source world its row
carries. A row naming any other world is refused: the temple
population must not leak onto another world, and no other population
joins these.
================
*/
func templeWorldCode(row cap110Nest) (string, error) {
	code, ok := temple.CodeName(row.WorldID)
	if !ok {
		return "", fmt.Errorf("temple population has nest %d in unknown world %d", row.NestID, row.WorldID)
	}
	return code, nil
}
