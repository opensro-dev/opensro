/*
===========================================================================

highlevelseals.go - an optional rare (SoX) roll for monsters of level 95-110

Port-only, not native (owner 2026-11-25, #4393: "make it default off, and
off on server"). Neither original server rolls a rare item above level 94:
the vSRO and ISRO-R _RefDropClassSel_RareEquip rows are empty from 95
through 180 (scripts/data/loot/*-drops-source.json), and the ISRO-R reward
groups that hold degree-10 seals (73-76, 95) are assigned only to monsters
v1.150 lacks, with every assignment's service byte off. So B4 and the
cap-110 field roll no seal at all.

SRO_HIGH_LEVEL_SEALS fills each empty rare row from 95 to 110 with one
class: the Seal of Star of the degree the level's ordinary row already
drops (SealGroupAt, the unique seal reward's rule), at the probability of
the highest native rare row (level 94's 0.2689% in the shipped catalog).
Inference: a native row would have carried on the level-94 pattern, a Star
of the level's own degree at the same mass; Moon and Sun stay unique and
alchemy rewards. With the cap-110 catalog installed that is degree 10
through 101 and degree 11 from 102 (its mirrored ordinary rows); without
it, degree 10. "off" leaves every row native.

===========================================================================
*/
package loot

import (
	"os"
	"strings"
)

// EnvHighLevelSeals switches the level 95-110 seal roll on.
const EnvHighLevelSeals = "SRO_HIGH_LEVEL_SEALS"

const (
	// highLevelSealLastLevel is the highest level the rule fills: the
	// cap-110 field's top monster level.
	highLevelSealLastLevel = 110
	// sealTierStar is SealGroupAt's Star tier.
	sealTierStar = 0
)

/*
================
HighLevelSealsEnabled
================
*/
func HighLevelSealsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvHighLevelSeals))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
InitHighLevelSeals

Startup only, after InitCap110: the rows follow whichever ordinary table
is installed. Off leaves the catalog untouched.
================
*/
func InitHighLevelSeals(enabled bool) {
	if !enabled {
		return
	}
	equipment = withHighLevelSeals(equipment)
}

/*
================
withHighLevelSeals

A copy of c whose empty rare rows above the highest native rare row, up to
highLevelSealLastLevel, roll the level's Star at that row's mass. The
native rows, and every row of the ordinary table, are shared unchanged.
================
*/
func withHighLevelSeals(c equipmentCatalog) equipmentCatalog {
	rare := c.classes[1]
	template := -1
	for level := len(rare) - 1; level >= 0; level-- {
		if len(rare[level]) > 0 {
			template = level
			break
		}
	}
	if template < 0 {
		return c
	}
	mass := rare[template][len(rare[template])-1].threshold
	next := append([][]classThreshold(nil), rare...)
	for level := template + 1; level < len(next) && level < highLevelSealLastLevel; level++ {
		if len(next[level]) > 0 {
			continue
		}
		group, ok := c.sealGroupAt(uint8(level+1), sealTierStar)
		if !ok {
			continue
		}
		next[level] = []classThreshold{{group: group, threshold: mass}}
	}
	c.classes[1] = next
	return c
}
