/*
===========================================================================

regioncombat.go - authored battlefield permissions for player combat

The native player attack predicate checks both region records before checking
relations. Keeping the reference lookup here makes all action owners use the
same town protection. Unknown regions have no combat authority.

===========================================================================
*/
package world

import "sort"

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

/*
================
RegionPlayerCombat

52943E..52946E refuses missing regions separately from protected regions.
This does not create a world or grant permission to enter one.
================
*/
func RegionPlayerCombat(region uint16) (allowed, known bool) {
	i := sort.Search(len(regionCombatRanges), func(i int) bool { return regionCombatRanges[i].last >= region })
	if i == len(regionCombatRanges) || region < regionCombatRanges[i].first {
		return false, false
	}
	return regionCombatRanges[i].allowed, true
}
