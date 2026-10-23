/*
===========================================================================

regioncombatfixture_test.go - the battlefield table the action tests use

A small synthetic table, not the operator's reference: the regions the
tests stand in, with the protection each test relies on.

===========================================================================
*/
package action

import (
	"slices"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// testTownRegion is a protected region (region 25000, in Jangan).
	testTownRegion = 0x61a8
	// testFieldRegion is a battlefield.
	testFieldRegion = 0x62a6
	// testUnknownRegion is a region the table does not know.
	testUnknownRegion = 0x5c5c
)

// testTowns are the protected regions the tests stand in; every other
// region but testUnknownRegion is a battlefield.
var testTowns = []uint16{0x5c87, 0x5e87, thiefTown, testTownRegion, 0x61a9, 0x62a7, 0x62a8, 0x62a9, 0x6699}

/*
================
testRegionCombat

Built from testTowns: a battlefield range between each pair of protected
or unknown regions.
================
*/
func testRegionCombat() *worldgeom.RegionCombat {
	holes := append([]uint16{testUnknownRegion}, testTowns...)
	slices.Sort(holes)
	var ranges []worldgeom.RegionCombatRange
	next := uint32(1) // region 0 stays unknown
	for _, hole := range holes {
		if uint32(hole) > next {
			ranges = append(ranges, worldgeom.RegionCombatRange{First: uint16(next), Last: hole - 1, Allowed: true})
		}
		if hole != testUnknownRegion {
			ranges = append(ranges, worldgeom.RegionCombatRange{First: hole, Last: hole})
		}
		next = uint32(hole) + 1
	}
	ranges = append(ranges, worldgeom.RegionCombatRange{First: uint16(next), Last: 0xfffe, Allowed: true})
	table, err := worldgeom.NewRegionCombat(ranges)
	if err != nil {
		panic(err) // a static table: a bad row is a test defect
	}
	return table
}

/*
================
newRegionTestRuntime

NewRuntime with the test battlefield table installed, as the composition
root installs the operator's.
================
*/
func newRegionTestRuntime(deps Dependencies, monsters *simulation.MonsterState) *Runtime {
	rt := NewRuntime(deps, monsters)
	rt.ConfigureRegionCombat(testRegionCombat())
	return rt
}
