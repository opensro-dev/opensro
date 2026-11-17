/*
===========================================================================

roc.go - the optional Roc chain's quest monster nests (port-only switch).

SRO_ROC is off by default. The Roc chain is native to the later clients
(#3399); v1.150 ships its regions, items and quest text but not the
quest kinds QNO_RM_MEETROC_1 gathers from, and it never places the
guardian Rockies QNO_RM_MEETROC_6 hunts. The Roc import
(scripts/roc_import.py) stages the two missing kinds and writes their
six v1.188 nests (one shared-cap hive on the Roc mountain) and the
guardians' seven (committed by
scripts/build/import_roc_quest_nests.py) as roc-monsters.txt in the
field population's row schema (cap110.go). This is the consumer side.

The nests stand in native outdoor regions of the default world, so they
need no other switch. The arena kinds the same import stages stand in
no nest: the arena spawns them when it opens (step 4).

The switch is the port's; the content is native to the later client.

===========================================================================
*/
package monster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const EnvRoc = "SRO_ROC"
const rocPopulationFile = "roc-monsters.txt"

// The quest kinds the Roc population may place, all at level 90: the
// two QNO_RM_MEETROC_1 drops from, as the VSRO client authors them (the
// importer's KINDS refuses another level), and the seven guardian
// Rockies QNO_RM_MEETROC_6 hunts, as v1.150's characterdata ships them.
const rocQuestKindLevel = 90

var rocQuestKinds = map[string]bool{
	"MOB_QT_RM_FEATHERMASK_01": true,
	"MOB_QT_RM_ROCKY_CLON_01":  true,
	"MOB_QT_01_ROCKY_CLON2":    true,
	"MOB_QT_02_ROCKY_CLON2":    true,
	"MOB_QT_03_ROCKY_CLON2":    true,
	"MOB_QT_04_ROCKY_CLON2":    true,
	"MOB_QT_05_ROCKY_CLON2":    true,
	"MOB_QT_06_ROCKY_CLON2":    true,
	"MOB_QT_07_ROCKY_CLON2":    true,
}

/*
================
RocEnabled
================
*/
func RocEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvRoc))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
AppendRoc

Off never reads the optional file. On fails before startup when the Roc
population is absent or malformed; a partial population must not reach
gameplay.
================
*/
func AppendRoc(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, rocPopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("roc population: %w", err)
	}
	return appendRocData(template, data)
}

/*
================
appendRocData
================
*/
func appendRocData(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "roc population", admitRoc, nil, nil)
}

/*
================
admitRoc

Only the chain's two quest kinds at their authored level: no other
family places them, and no native nest names them.
================
*/
func admitRoc(ref MonsterRef, codename string) bool {
	return rocQuestKinds[codename] && ref.Level == rocQuestKindLevel
}
