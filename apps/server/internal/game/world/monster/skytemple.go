/*
===========================================================================

skytemple.go - optional Sky Temple field population (port-only).

Port-only, not native. SRO_SKYTEMPLE is off by default. The importer
(scripts/skytemple_import.py) writes skytemple-monsters.txt in the field
population's row schema (cap110.go); this is the consumer side, joining
each nest's codename against the installed monster references. The
fields are outdoor regions of the default world, so their nests join
it like the field population's.

Unlike the cap-110 families the population needs no second switch: its
character rows ride its own shard (characterdata_130000.txt) and its
drops are the native posture, so nothing here rides the cap-110 loot
catalogue the field switch builds. The authored level window is the
importer's sanity bound from the client table (131-178), not a spawn
filter; the player level cap is untouched (progression owns it).

===========================================================================
*/
package monster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const EnvSkytemple = "SRO_SKYTEMPLE"
const skytemplePopulationFile = "skytemple-monsters.txt"

// Port-only, not native: the family's admitted level window, the
// importer's sanity bound from the client table rather than a spawn
// filter. The kinds keep the levels the later client authors (131-178);
// the port's player cap stays where SRO_LEVEL_CAP puts it.
const (
	skytempleFirstLevel = 131
	skytempleLastLevel  = 178
)

/*
================
SkytempleEnabled
================
*/
func SkytempleEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvSkytemple))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
AppendSkytemple

Off never reads the optional file. On fails before startup when the
sky temple data is absent or malformed; a partial population must not
reach gameplay.
================
*/
func AppendSkytemple(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, skytemplePopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("skytemple population: %w", err)
	}
	return appendSkytempleData(template, data)
}

/*
================
appendSkytempleData
================
*/
func appendSkytempleData(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "skytemple population", admitSkytemple, nil, nil)
}

/*
================
admitSkytemple

Port-only, not native: the Sky Temple species (MOB_SKYTEMPLE_) of the
later client, inside the family's level window. The kinds the client
never places outdoors (summons and clones) carry no nests in the
population file, so no grade gate applies here: a placed kind is the
content, whatever grade the client authored for it. The other families
never admit these codenames.
================
*/
func admitSkytemple(ref MonsterRef, codename string) bool {
	if !strings.HasPrefix(codename, "MOB_SKYTEMPLE_") {
		return false
	}
	return ref.Level >= skytempleFirstLevel && ref.Level <= skytempleLastLevel
}
