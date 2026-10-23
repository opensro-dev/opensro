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

import (
	"encoding/json"
	"fmt"
	"sort"

	"opensro.online/server/internal/gamedata"
)

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
	ranges []RegionCombatRange
}

/*
================
NewRegionCombat

Refuses inverted, overlapping or unsorted ranges: a lookup over them would
answer for the wrong region.
================
*/
func NewRegionCombat(ranges []RegionCombatRange) (*RegionCombat, error) {
	for i, r := range ranges {
		if r.First > r.Last || i > 0 && r.First <= ranges[i-1].Last {
			return nil, fmt.Errorf("region combat: range %d (%04x-%04x) is inverted or overlaps", i, r.First, r.Last)
		}
	}
	return &RegionCombat{ranges: append([]RegionCombatRange(nil), ranges...)}, nil
}

/*
================
LoadRegionCombat

The table from derivedDir, or nil when there is none.
================
*/
func LoadRegionCombat(derivedDir string) (*RegionCombat, error) {
	data, err := gamedata.ReadDerived(derivedDir, RegionCombatFile)
	if err != nil || data == nil {
		return nil, err
	}
	var document struct {
		Format string
		Ranges [][3]json.RawMessage
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("region combat: %w", err)
	}
	if document.Format != regionCombatFormat {
		return nil, fmt.Errorf("region combat: format %q, want %q", document.Format, regionCombatFormat)
	}
	ranges := make([]RegionCombatRange, len(document.Ranges))
	for i, row := range document.Ranges {
		r := &ranges[i]
		if json.Unmarshal(row[0], &r.First) != nil || json.Unmarshal(row[1], &r.Last) != nil ||
			json.Unmarshal(row[2], &r.Allowed) != nil {
			return nil, fmt.Errorf("region combat: malformed range %d", i)
		}
	}
	return NewRegionCombat(ranges)
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
	i := sort.Search(len(t.ranges), func(i int) bool { return t.ranges[i].Last >= region })
	if i == len(t.ranges) || region < t.ranges[i].First {
		return false, false
	}
	return t.ranges[i].Allowed, true
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
