/*
===========================================================================

arabia.go - optional Arabia continent population (port-only).

Port-only, not native. SRO_ARABIA is off by default. The importer
(scripts/arabia_import.py) writes arabia-monsters.txt in the field
population's row schema (cap110.go); this is the consumer side, joining
each nest's codename against the installed monster references. The town
and the three fields are outdoor regions of the default world; the
Baghdad dungeon's nests join its own port-only world (world/arabia),
registered while the same switch is on.

Unlike the cap-110 families the population needs no second switch: its
character rows ride its own shard (characterdata_170000.txt) and its
drops are the native posture, so nothing here rides the cap-110 loot
catalogue the field switch builds. The authored level window is the
importer's sanity bound from the client table (120-130), not a spawn
filter; the player level cap is untouched (progression owns it).

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
const arabiaPopulationFile = "arabia-monsters.txt"

// Port-only, not native: the family's admitted level window, the
// importer's sanity bound from the client table rather than a spawn
// filter. The kinds keep the levels the later client authors
// (120-130); the port's player cap stays where SRO_LEVEL_CAP puts it.
const (
	arabiaFirstLevel = 120
	arabiaLastLevel  = 130
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
Arabia data is absent or malformed; a partial population must not
reach gameplay.
================
*/
func AppendArabia(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, arabiaPopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("arabia population: %w", err)
	}
	return appendArabiaData(template, data)
}

/*
================
appendArabiaData
================
*/
func appendArabiaData(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "arabia population", admitArabia, nil, arabiaWorldCode)
}

/*
================
admitArabia

Port-only, not native: the Arabia species (MOB_AR_ and MOB_ARABIA_) of
the later client, inside the family's level window. The kinds the
source never places carry no nests in the population file, so no grade
gate applies here: a placed kind is the content, whatever grade the
client authored for it. The other families never admit these
codenames.
================
*/
func admitArabia(ref MonsterRef, codename string) bool {
	if !strings.HasPrefix(codename, "MOB_AR_") && !strings.HasPrefix(codename, "MOB_ARABIA_") {
		return false
	}
	return ref.Level >= arabiaFirstLevel && ref.Level <= arabiaLastLevel
}

/*
================
arabiaWorldCode

The world codename an Arabia nest joins, from the source world its row
carries: the town and fields stand in the default world, the dungeon
in its own. A row naming any other world is refused: the Arabia
population must not leak onto another world, and no other population
joins the dungeon.
================
*/
func arabiaWorldCode(row cap110Nest) (string, error) {
	if row.WorldID == cap110FieldWorldID {
		return "INS_DEFAULT", nil
	}
	code, ok := arabia.CodeName(row.WorldID)
	if !ok {
		return "", fmt.Errorf("arabia population has nest %d in unknown world %d", row.NestID, row.WorldID)
	}
	return code, nil
}
