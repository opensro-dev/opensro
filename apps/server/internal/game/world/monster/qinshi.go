/*
===========================================================================

qinshi.go - optional Qin-Shi tomb upper-floor population (#1800 step 2).

Port-only, not native. SRO_CAP110_QINSHI is off by default. The importer
(scripts/cap110_monsters.py, QINSHI_FILE) writes cap110-qinshi.txt in the
field population's row schema (cap110.go); this is the consumer side of
the family table's qinshi row, joining each nest's codename against the
installed monster references. The family's nests stand in world 1, so
they join the default world like the field population's.

The family needs SRO_CAP110_MONSTERS as well and refuses to start without
it: its drop rules ride the shared cap-110 loot catalogue the field
switch builds (cap110_rewards.py), as the Pharaoh precedent does
(pharaoh_tomb.go). Not SRO_QINSHI_TOMB: that switch gates the shipped
tomb floors and defaults on in the shard settings registry, so this
family must never ride it.

===========================================================================
*/
package monster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const EnvCap110Qinshi = "SRO_CAP110_QINSHI"
const qinshiPopulationFile = "cap110-qinshi.txt"

// Port-only, not native: the qinshi family's admitted level window, the
// importer's sanity bound from the client table rather than a spawn
// filter. The content fills the 91-99 gap (#1800).
const qinshiFirstLevel = 81

// The source grades the importer's qinshi row admits (QINSHI_FAMILY in
// scripts/cap110_monsters.py): ordinary, grade 3, the elite and the
// unique. Grade 3 is the row's mid grade; neither code base names a role
// for it, so it keeps its number's name here.
const (
	qinshiGradeOrdinary = 0
	qinshiGradeMid      = 3
	qinshiGradeElite    = gradeElite
	qinshiGradeUnique   = 8
)

/*
================
QinshiEnabled
================
*/
func QinshiEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvCap110Qinshi))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
AppendQinshi

Off never reads the optional file. On fails before startup when the
cap-110 expansion is off, or when the qinshi data is absent or malformed;
a partial population must not reach gameplay.
================
*/
func AppendQinshi(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	if !Cap110Enabled() {
		return Template{}, fmt.Errorf("qinshi population: %s requires %s (its drops ride the shared cap-110 loot catalogue)", EnvCap110Qinshi, EnvCap110Monsters)
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, qinshiPopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("qinshi population: %w", err)
	}
	return appendQinshiData(template, data)
}

/*
================
appendQinshiData
================
*/
func appendQinshiData(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "qinshi population", admitQinshi, nil, nil)
}

/*
================
admitQinshi

Port-only, not native: the tomb (MOB_TQ_) and quest (MOB_QT_) species of
the later database, inside the family's level window and grade set. The
field and tomb families never admit these codenames, so a species claimed
by two families keeps the one identity the shared allocator gave it.
================
*/
func admitQinshi(ref MonsterRef, codename string) bool {
	if !strings.HasPrefix(codename, "MOB_TQ_") && !strings.HasPrefix(codename, "MOB_QT_") {
		return false
	}
	if ref.Level < qinshiFirstLevel || ref.Level > cap110LastLevel {
		return false
	}
	switch ref.MonsterType {
	case qinshiGradeOrdinary, qinshiGradeMid, qinshiGradeElite, qinshiGradeUnique:
		return true
	}
	return false
}
