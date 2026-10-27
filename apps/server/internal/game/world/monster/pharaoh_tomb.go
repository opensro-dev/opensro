/*
===========================================================================
pharaoh_tomb.go - optional Pharaoh Tomb 1-3 population (Kings Valley).

Port-only, not native. SRO_PHARAOH_TOMB is off by default. The three tombs
are ordinary outdoor regions in the later server's database, each tagged with
its own GameWorldID (57, 58, 59) (#1750). The reference server has no tomb
world class and no tomb triggers: a database-defined world whose codename
matches none of its special worlds runs as an ordinary persistent world, so
each tomb here is its own permanent world. The boot composition registers
the three (instance.PharaohTombDefinitions, from the pharaohtomb leaf) only
while this switch is on, and the population joins its own world instead of
the default one. An earlier build flattened all three onto the default
world; the tomb gates keep their authored destination worlds (57-59), so a
gate entered from the field arrives in the tomb its population joined.

The importer (scripts/cap110_monsters.py, TOMB_FILE) writes cap110-tomb.txt in the same row
schema as the field population (cap110.go), each row carrying its source
world. Unlike the field it admits the elite (grade 6) and unique (grade 8)
monsters the tombs are made of. Equipment drops come from the cap-110 loot
catalogue, so the tomb requires SRO_CAP110_MONSTERS as well and refuses to
start without it.

The tomb bosses (Sphinx, Sekhmet, Osiris, Horus, Nephthys) are single-spawn
nests: an ordinary persistent nest that respawns after its authored delay,
like any other unique. There are no instance triggers to port.
===========================================================================
*/
package monster

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/world/pharaohtomb"
)

const EnvPharaohTomb = "SRO_PHARAOH_TOMB"
const pharaohTombPopulationFile = "cap110-tomb.txt"

// The monster grades (MonsterType low nibble) the tombs use: ordinary, the
// native elite (gradescale.go) and the unique grade Apis also carries.
const (
	pharaohGradeOrdinary = 0
	pharaohGradeElite    = gradeElite
	pharaohGradeUnique   = 8
)

/*
================
PharaohTombEnabled
================
*/
func PharaohTombEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvPharaohTomb))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
AppendPharaohTomb

Off never reads the optional file. On fails before startup when the
cap-110 expansion is off, or when the tomb data is absent or malformed.
Registering the tomb worlds with the world catalogue is the boot
composition's job (enterworld), beside this call.
================
*/
func AppendPharaohTomb(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	if !Cap110Enabled() {
		return Template{}, fmt.Errorf("pharaoh tomb: %s requires %s (its equipment drops)", EnvPharaohTomb, EnvCap110Monsters)
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, pharaohTombPopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("pharaoh tomb population: %w", err)
	}
	rate, err := PharaohGearRateFromEnv()
	if err != nil {
		return Template{}, err
	}
	return appendPharaohTombData(template, data, rate)
}

/*
================
appendPharaohTombData

The tomb's monsters carry the gear rate on a copy of the reference table, so
the template it was given is unchanged. The field and the tomb never share a
reference row: the tomb's codenames are its own.
================
*/
func appendPharaohTombData(template Template, data []byte, gearRate int) (Template, error) {
	result, err := appendNests(template, data, "pharaoh tomb population", admitPharaohTomb, nil, pharaohWorldCode)
	if err != nil {
		return Template{}, err
	}
	refs := make(map[uint32]MonsterRef, len(result.Refs))
	for id, ref := range result.Refs {
		refs[id] = ref
	}
	for _, nest := range result.Nests[len(template.Nests):] {
		ref := refs[nest.RefObjID]
		ref.PharaohGearRate = uint8(gearRate)
		refs[nest.RefObjID] = ref
	}
	marked := TemplateFromParts(refs, result.Nests)
	marked.SummonRefs = result.SummonRefs
	marked.EvidenceMatches = result.EvidenceMatches
	return marked, nil
}

/*
================
pharaohWorldCode

The world codename a tomb nest joins, from the source world its row
carries. A row naming any other world is refused: the tomb population
must not leak onto another world, and no other population joins these.
================
*/
func pharaohWorldCode(row cap110Nest) (string, error) {
	code, ok := pharaohtomb.CodeName(row.WorldID)
	if !ok {
		return "", fmt.Errorf("pharaoh tomb population has nest %d in unknown world %d", row.NestID, row.WorldID)
	}
	return code, nil
}

// EnvPharaohGearRate multiplies the equipment a Pharaoh Tomb elite or unique
// drops (port-only, not native). 1 or unset is the native rule.
const EnvPharaohGearRate = "SRO_PHARAOH_TOMB_GEAR_RATE"

// MaxPharaohGearRate bounds the rate: the multiplied rolls all happen in one kill.
const MaxPharaohGearRate = 20

/*
================
PharaohGearRateFromEnv

The tomb gear rate: unset, empty or "1" is 1 (native); "2" to
MaxPharaohGearRate multiplies the equipment rolls. Anything else is an error,
so a typo cannot silently run the native rule.
================
*/
func PharaohGearRateFromEnv() (int, error) {
	value := strings.TrimSpace(os.Getenv(EnvPharaohGearRate))
	if value == "" {
		return 1, nil
	}
	rate, err := strconv.Atoi(value)
	if err != nil || rate < 1 || rate > MaxPharaohGearRate {
		return 1, fmt.Errorf("%s must be a whole number from 1 to %d, not %q", EnvPharaohGearRate, MaxPharaohGearRate, value)
	}
	return rate, nil
}

/*
================
admitPharaohTomb
================
*/
func admitPharaohTomb(ref MonsterRef, codename string) bool {
	if !strings.HasPrefix(codename, "MOB_SD_") || ref.Level < cap110FirstLevel || ref.Level > cap110LastLevel {
		return false
	}
	switch ref.MonsterType {
	case pharaohGradeOrdinary, pharaohGradeElite, pharaohGradeUnique:
		return true
	}
	return false
}
