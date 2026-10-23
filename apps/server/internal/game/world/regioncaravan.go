/*
===========================================================================

regioncaravan.go - which bandit families ambush a caravan in a region

Caravan_GetContinentZone (60BDD0) maps the region's continent name to a
bandit zone: the Chinese continents are 0, the western ones 1, and any
other continent 2 (after a minidump), where no bandit is filed.

The table is the operator's, from the same server region reference as the
combat permissions, carried in the projection's derived data
(RegionCaravanFile). Without it no region has a zone, so no caravan draws
bandits.

===========================================================================
*/
package world

// RegionCaravanFile is the caravan zone table in the derived data.
const RegionCaravanFile = "world/region-caravan.json"

// regionCaravanFormat names the table's layout: inclusive [first, last,
// zone] ranges, sorted and nonoverlapping.
const regionCaravanFormat = "sro-region-caravan-v1"

/*
================
RegionCaravanRange

Inclusive region ids sharing one bandit zone.
================
*/
type RegionCaravanRange struct {
	First, Last uint16
	Zone        uint8
}

/*
================
RegionCaravan

The loaded table. A nil table knows no region.
================
*/
type RegionCaravan struct {
	table *regionTable[uint8]
}

/*
================
NewRegionCaravan
================
*/
func NewRegionCaravan(ranges []RegionCaravanRange) (*RegionCaravan, error) {
	spans := make([]regionSpan[uint8], len(ranges))
	for i, r := range ranges {
		spans[i] = regionSpan[uint8]{first: r.First, last: r.Last, value: r.Zone}
	}
	table, err := newRegionTable("region caravan", spans)
	if err != nil {
		return nil, err
	}
	return &RegionCaravan{table: table}, nil
}

/*
================
LoadRegionCaravan

The table from derivedDir, or nil when there is none.
================
*/
func LoadRegionCaravan(derivedDir string) (*RegionCaravan, error) {
	table, err := loadRegionTable[uint8](derivedDir, RegionCaravanFile, regionCaravanFormat)
	if err != nil || table == nil {
		return nil, err
	}
	return &RegionCaravan{table: table}, nil
}

/*
================
Zone

The bandit zone of a region; known is false for a region the table does
not name.
================
*/
func (t *RegionCaravan) Zone(region uint16) (zone uint8, known bool) {
	if t == nil {
		return 0, false
	}
	return t.table.lookup(region)
}
