/*
===========================================================================

regioncombat.go - authored battlefield permissions for player combat

The native player attack predicate checks both region records before checking
relations. Keeping the reference lookup here makes all action owners use the
same town protection. Unknown regions have no combat authority.

===========================================================================
*/
package world

/*
================
regionCombatRange

Inclusive, nonoverlapping ranges generated from _RefRegion.IsBattleField.
================
*/
type regionCombatRange struct {
	first, last uint16
	allowed     bool
}

// The verdicts RegionPlayerCombat answers, one per region id.
const (
	regionVerdictUnknown = iota
	regionVerdictAllowed
	regionVerdictDenied
)

/*
================
regionCombatVerdicts

The ranges above as a direct-index table: a region id is a
uint16, and the verdict is asked per stepped actor per leg (the
safe-zone vanish) as well as per attack validation, so the
binary search the ranges used to pay per call is paid once, at
package load, for all 65536 ids (#3027). The ranges stay the
source of truth - this table is only their expansion, and the
package tests check it against them.
================
*/
var regionCombatVerdicts = buildRegionCombatVerdicts()

/*
================
buildRegionCombatVerdicts
================
*/
func buildRegionCombatVerdicts() [65536]uint8 {
	var verdicts [65536]uint8
	for _, row := range regionCombatRanges {
		verdict := uint8(regionVerdictDenied)
		if row.allowed {
			verdict = regionVerdictAllowed
		}
		for id := int(row.first); id <= int(row.last); id++ {
			verdicts[id] = verdict
		}
	}
	return verdicts
}

/*
================
RegionPlayerCombat

The reference server's attack-target validation refuses missing regions
separately from protected regions. This does not create a world or grant
permission to enter one.
================
*/
func RegionPlayerCombat(region uint16) (allowed, known bool) {
	switch regionCombatVerdicts[region] {
	case regionVerdictAllowed:
		return true, true
	case regionVerdictDenied:
		return false, true
	}
	return false, false
}
