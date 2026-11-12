/*
===========================================================================

jupiter.go - optional Jupiter field and temple population (port-only).

Port-only, not native. SRO_JUPITER is off by default; SRO_JUPITER_TEMPLE
adds the two Mirror Dimension temple sets beside the field and refuses
to start without the field, the same pairing the importer writes
(scripts/jupiter_import.py). Off never reads either population file;
the native game has no Jupiter, so off is the native state.

The later client's Jupiter worlds (internal/game/world/jupiter) are
ordinary outdoor worlds: the boot composition registers them as
permanent worlds (instance.JupiterFieldDefinitions and
JupiterTempleDefinitions) only while their switch is on, and each
population joins its own world instead of the default one. Entry -
which temple copy a gate admits, and the gates themselves - is the
admission slice's; this file owns the population join.

The kinds keep the levels the later client authors and their drops
are the native posture: the import writes no reward table, so nothing
here rides an optional loot catalogue.

===========================================================================
*/
package monster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/game/world/jupiter"
)

const EnvJupiter = "SRO_JUPITER"
const EnvJupiterTemple = "SRO_JUPITER_TEMPLE"
const jupiterFieldPopulationFile = "jupiter-field.txt"
const jupiterTemplePopulationFile = "jupiter-temple.txt"

// Port-only, not native: the family's admitted level window, the client
// table bound the import already sanity-checked (jupiter_import.py).
const (
	jupiterFirstLevel = 111
	jupiterLastLevel  = 120
)

// The monster grades (MonsterType low nibble) the Jupiter family uses:
// the import's grade set (0, 1, 3, 6, 8) with the native elite named.
const (
	jupiterGradeOrdinary = 0
	jupiterGradeUnique   = 8
)

/*
================
JupiterEnabled
================
*/
func JupiterEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJupiter))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
JupiterTempleEnabled
================
*/
func JupiterTempleEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJupiterTemple))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
AppendJupiter

Off never reads the optional file. On fails before startup when the
field data is absent or malformed; a partial population must not
reach gameplay. Registering the field world with the world catalogue
is the boot composition's job (enterworld), beside this call.
================
*/
func AppendJupiter(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, jupiterFieldPopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("jupiter field population: %w", err)
	}
	return appendJupiterData(template, data)
}

/*
================
AppendJupiterTemple

Off never reads the optional file. On fails before startup when the
field switch it requires is off - the requires refusal comes first,
before any temple data is read - or when the temple data is absent
or malformed. Registering the temple worlds with the world catalogue
is the boot composition's job (enterworld), beside this call.
================
*/
func AppendJupiterTemple(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	if !JupiterEnabled() {
		return Template{}, fmt.Errorf("jupiter temple: %s requires %s (its field, population and worlds)", EnvJupiterTemple, EnvJupiter)
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, jupiterTemplePopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("jupiter temple population: %w", err)
	}
	return appendJupiterTempleData(template, data)
}

/*
================
appendJupiterData
================
*/
func appendJupiterData(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "jupiter field population", admitJupiter, nil, jupiterWorldCode)
}

/*
================
appendJupiterTempleData
================
*/
func appendJupiterTempleData(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "jupiter temple population", admitJupiter, nil, jupiterWorldCode)
}

// The one guest the source places in the field beside the family: an
// Arabia kind at the window's own top level and a family grade. Like
// the cap-110 field's Apis, it is admitted by name, on the same window.
const jupiterGuestCode = "MOB_ARABIA_KIDEMONAS"

/*
================
admitJupiter

Port-only, not native: the family's own species (MOB_JUPITER_) inside
its level window, at the grades its nests use; the field's named
guest joins on the same window.
================
*/
func admitJupiter(ref MonsterRef, codename string) bool {
	if !strings.HasPrefix(codename, "MOB_JUPITER_") && codename != jupiterGuestCode {
		return false
	}
	if ref.Level < jupiterFirstLevel || ref.Level > jupiterLastLevel {
		return false
	}
	switch ref.MonsterType {
	case jupiterGradeOrdinary, 1, 3, gradeElite, jupiterGradeUnique:
		return true
	}
	return false
}

/*
================
jupiterWorldCode

The world codename a Jupiter nest joins, from the source world its
row carries. A row naming any other world is refused: the Jupiter
population must not leak onto another world, and no other population
joins these.
================
*/
func jupiterWorldCode(row cap110Nest) (string, error) {
	code, ok := jupiter.CodeName(row.WorldID)
	if !ok {
		return "", fmt.Errorf("jupiter population has nest %d in unknown world %d", row.NestID, row.WorldID)
	}
	return code, nil
}
