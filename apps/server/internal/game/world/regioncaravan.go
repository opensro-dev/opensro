/*
===========================================================================

regioncaravan.go - which bandit families ambush a caravan in a region

Caravan_GetContinentZone (60BDD0) maps the region's _RefRegion ContinentName
to a bandit zone: the Chinese continents are 0, the western ones 1, and any
other continent 2 (after a minidump), where no bandit is filed. The ranges
are generated from the same backup as the combat permissions.

===========================================================================
*/
package world

import "sort"

/*
================
regionCaravanZoneRange

Inclusive, nonoverlapping ranges generated from _RefRegion.ContinentName.
================
*/
type regionCaravanZoneRange struct {
	first, last uint16
	zone        uint8
}

/*
================
RegionCaravanZone

The bandit zone of a region; known is false for a region the reference
table does not name.
================
*/
func RegionCaravanZone(region uint16) (zone uint8, known bool) {
	i := sort.Search(len(regionCaravanZoneRanges), func(i int) bool { return regionCaravanZoneRanges[i].last >= region })
	if i == len(regionCaravanZoneRanges) || region < regionCaravanZoneRanges[i].first {
		return 0, false
	}
	return regionCaravanZoneRanges[i].zone, true
}
