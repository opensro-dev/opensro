/*
===========================================================================

regioncombat.go - authored battlefield permissions for player combat

The native player attack predicate checks both region records before checking
relations. Keeping the reference lookup here makes all action owners use the
same town protection. Unknown regions have no combat authority.

The table is the operator's: the per-region battlefield flag of the server's
region reference, carried in the projection's derived data
(RegionCombatFile). Without it every region is unknown, so player combat and
the rules that need a town or a battlefield stay off.

===========================================================================
*/
package world

// RegionCombatFile is the region combat table in the derived data.
const RegionCombatFile = "world/region-combat.json"

// regionCombatFormat names the table's layout: inclusive [first, last,
// allowed] ranges, sorted and nonoverlapping.
const regionCombatFormat = "sro-region-combat-v1"

/*
================
RegionCombatRange

Inclusive region ids sharing one battlefield flag.
================
*/
type RegionCombatRange struct {
	First, Last uint16
	Allowed     bool
}

/*
================
RegionCombat

The loaded table. A nil table knows no region.
================
*/
type RegionCombat struct {
	table *regionTable[bool]
}

/*
================
NewRegionCombat

Refuses inverted, overlapping or unsorted ranges: a lookup over them would
answer for the wrong region.
================
*/
func NewRegionCombat(ranges []RegionCombatRange) (*RegionCombat, error) {
	spans := make([]regionSpan[bool], len(ranges))
	for i, r := range ranges {
		spans[i] = regionSpan[bool]{first: r.First, last: r.Last, value: r.Allowed}
	}
	table, err := newRegionTable("region combat", spans)
	if err != nil {
		return nil, err
	}
	return &RegionCombat{table: table}, nil
}

/*
================
LoadRegionCombat

The table from derivedDir, or nil when there is none.
================
*/
func LoadRegionCombat(derivedDir string) (*RegionCombat, error) {
	table, err := loadRegionTable[bool](derivedDir, RegionCombatFile, regionCombatFormat)
	if err != nil || table == nil {
		return nil, err
	}
	return &RegionCombat{table: table}, nil
}

/*
================
PlayerCombat

52943E..52946E refuses missing regions separately from protected regions.
This does not create a world or grant permission to enter one.
================
*/
func (t *RegionCombat) PlayerCombat(region uint16) (allowed, known bool) {
	if t == nil {
		return false, false
	}
	return t.table.lookup(region)
}

/*
================
Town

A region the table marks as no battlefield; an unknown region is not one.
================
*/
func (t *RegionCombat) Town(region uint16) bool {
	allowed, known := t.PlayerCombat(region)
	return known && !allowed
}

/*
================
Battlefield
================
*/
func (t *RegionCombat) Battlefield(region uint16) bool {
	allowed, known := t.PlayerCombat(region)
	return known && allowed
}
