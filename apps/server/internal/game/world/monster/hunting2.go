/*
===========================================================================

hunting2.go - optional second hunting grounds population (port-only).

Port-only, not native. SRO_HUNTING2 is off by default. The second
hunting ground set (the later client's RN_NEW_HUNTING_SECOND_FIELD
regions) postdates the later shard backup, so its population is the
client's own npcpos placements turned into nests at build time
(scripts/hunting2_import.py): one nest of one monster per placement
point, on the default world like the cap-110 field. Off never reads
the population file; the native game has no such set, so off is the
native state.

The kinds keep the levels the later client authors: the set
re-stages known species across levels 49-140, and the authored level
is the spawn level. Nothing here raises the player level cap, and
the kinds drop on the native posture: the import writes no reward
table.

===========================================================================
*/
package monster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const EnvHunting2 = "SRO_HUNTING2"
const hunting2PopulationFile = "hunting2-field.txt"

// Port-only, not native: the placed kinds' authored level window,
// the client table bound the import already sanity-checked
// (hunting2_import.py). The grades are the authored pair.
const (
	hunting2FirstLevel = 49
	hunting2LastLevel  = 140
)

/*
================
Hunting2Enabled
================
*/
func Hunting2Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvHunting2))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
AppendHunting2

Off never reads the optional file. On fails before startup when the
data is absent or malformed; a partial population must not reach
gameplay. The rows name the default world and join it (appendNests'
field rule), so no world registration rides this switch.
================
*/
func AppendHunting2(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, hunting2PopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("hunting2 population: %w", err)
	}
	return appendNests(template, data, "hunting2 population", admitHunting2, nil, nil)
}

/*
================
admitHunting2

Port-only, not native: the kinds the client places in the set's
regions (every MOB_* row), at their authored levels and grades.
================
*/
func admitHunting2(ref MonsterRef, codename string) bool {
	if !strings.HasPrefix(codename, "MOB_") {
		return false
	}
	if ref.Level < hunting2FirstLevel || ref.Level > hunting2LastLevel {
		return false
	}
	return ref.MonsterType == 0 || ref.MonsterType == 1
}
